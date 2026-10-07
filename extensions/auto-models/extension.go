// Package automodels ports pi-auto-models@0.1.14 to native PiG extensions.
package automodels

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	quota "github.com/VBenevides/pig-plugins/internal/automodels"
	"github.com/VBenevides/pig-plugins/internal/sdkctx"
)

const Name = "auto-models"

type refreshRequest struct {
	ctx   sdk.Context
	force bool
}
type extension struct {
	mu                sync.Mutex
	store             quota.Store
	client            quota.Client
	config            quota.Config
	primary, fallback quota.Slot
	rates             map[string]quota.RateLimitInfo
	usingPrimary      bool
	requestProvider   string
	quotaAt           time.Time
	quotaProvider     string
	autoEnabled       bool
	loadErr           error
	life              context.Context
	stop              context.CancelFunc
	refresh           chan refreshRequest
	done              chan struct{}
}

func Extension() *sdk.Extension {
	// A fused factory shares the PiG executable's arguments. Packed cells do not.
	info, ok := debug.ReadBuildInfo()
	fused := ok && info.Main.Path == "github.com/MichaelKinsy/PiG"
	x := newExtension(quota.Store{}, quota.Client{}, fused && !quota.HasExplicitModelFlag(os.Args[1:]))
	e := sdk.New(Name)
	e.RegisterCommand("usage", sdk.CommandOptions{Description: "Show Claude / Codex quota usage", Handler: x.usage})
	e.RegisterCommand("auto-model", sdk.CommandOptions{Description: "Configure primary and fallback models", Handler: x.configure})
	e.OnSessionStart(func(ctx sdk.Context, _ map[string]any) (any, error) {
		ctx.SetWorkingVisible(true)
		if x.loadErr != nil {
			x.report(ctx, x.loadErr)
			return nil, nil
		}
		if !fused {
			ctx.Notify("auto-models: automatic model selection requires a fused binary; /usage and /auto-model remain available", "warning")
		}
		if x.autoEnabled {
			if err := x.startup(ctx); err != nil {
				x.report(ctx, err)
			}
		}
		x.queueRefresh(ctx, true)
		return nil, nil
	})
	e.OnEvent("before_provider_request", func(ctx sdk.Context, data map[string]any) (any, error) {
		provider := ctx.ModelProvider()
		if model, ok := data["model"].(map[string]any); ok {
			if p, ok := model["provider"].(string); ok && p != "" {
				provider = p
			}
		}
		x.mu.Lock()
		x.requestProvider = provider
		x.mu.Unlock()
		return nil, nil
	})
	e.OnEvent("after_provider_response", x.response)
	e.OnEvent("message_end", x.messageEnd)
	for _, event := range []string{"agent_end", "model_select"} {
		event := event
		e.OnEvent(event, func(ctx sdk.Context, _ map[string]any) (any, error) {
			x.queueRefresh(ctx, event == "model_select")
			return nil, nil
		})
	}
	e.OnSessionShutdown(func(_ sdk.Context, _ map[string]any) (any, error) {
		x.stop()
		select {
		case <-x.done:
		case <-time.After(2 * time.Second):
			log.Print("auto-models: quota worker did not stop before shutdown deadline")
		}
		return nil, nil
	})
	return e
}

func newExtension(store quota.Store, client quota.Client, enabled bool) *extension {
	config, err := store.LoadConfig()
	rates, rateErr := store.LoadRateLimits()
	if err == nil {
		err = rateErr
	}
	if rates == nil {
		rates = map[string]quota.RateLimitInfo{}
	}
	primary, fallback := quota.Defaults(config)
	life, stop := context.WithCancel(context.Background())
	x := &extension{store: store, client: client, config: config, primary: primary, fallback: fallback, rates: rates, autoEnabled: enabled, loadErr: err, life: life, stop: stop, refresh: make(chan refreshRequest, 1), done: make(chan struct{})}
	go x.refreshLoop()
	return x
}

func (x *extension) report(ctx sdk.Context, err error) {
	ctx.Notify("auto-models: "+clean(err.Error()), "warning")
	log.Printf("auto-models: %v", err)
}
func clean(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, value)
}
func (x *extension) slots() (quota.Slot, quota.Slot) {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.primary, x.fallback
}
func (x *extension) rate(provider string) *quota.RateLimitInfo {
	x.mu.Lock()
	defer x.mu.Unlock()
	value, ok := x.rates[provider]
	if !ok {
		return nil
	}
	return &value
}
func (x *extension) setCooldown(provider string, expires time.Time) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.store.SetRateLimit(provider, expires)
}
func (x *extension) cooldown(provider string, now time.Time) (time.Duration, error) {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.store.RateLimitLeft(provider, now)
}
func (x *extension) saveRate(provider string, value quota.RateLimitInfo) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	// Do not publish the new snapshot before it is persisted.
	previous, existed := x.rates[provider]
	x.rates[provider] = value
	if err := x.store.SaveRateLimits(x.rates); err != nil {
		if existed {
			x.rates[provider] = previous
		} else {
			delete(x.rates, provider)
		}
		return err
	}
	return nil
}
func (x *extension) choose(ctx sdk.Context, slot quota.Slot) (bool, error) {
	if ctx.ModelRegistry().Find(slot.Provider, slot.Model) == nil {
		return false, fmt.Errorf("model %s/%s not found", slot.Provider, slot.Model)
	}
	ok, err := ctx.SetModel(slot.Provider + "/" + slot.Model)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, fmt.Errorf("no available credentials for %s/%s", slot.Provider, slot.Model)
	}
	ctx.SetThinkingLevel(slot.Thinking)
	return true, nil
}
func (x *extension) selected(ctx sdk.Context, primary bool, slot quota.Slot) {
	x.mu.Lock()
	x.usingPrimary = primary
	x.mu.Unlock()
	symbol, color := "⚡ ", "warning"
	if primary {
		symbol, color = "🧠 ", "success"
	}
	ctx.SetStatus("auto-model", ctx.UITheme().Fg(color, symbol+clean(slot.Model)))
}
func (x *extension) startup(ctx sdk.Context) error {
	primary, fallback := x.slots()
	now := time.Now()
	left, err := x.cooldown(primary.Provider, now)
	if err != nil {
		return err
	}
	if passive := quota.PassiveCooldown(x.rate(primary.Provider), now); passive > left {
		left = passive
	}
	if left > 0 && primary.Provider == "anthropic" {
		auth, err := x.store.LoadAuth()
		if err != nil {
			return err
		}
		if entry, ok := auth[primary.Provider]; ok && validOAuth(entry, now) {
			run, cancel := sdkctx.Request(ctx)
			usage, fetchErr := x.client.FetchClaude(run, entry)
			cancel()
			if fetchErr != nil {
				x.report(ctx, fetchErr)
			} else if available := quota.ClaudeAvailable(usage); available != nil && *available {
				x.mu.Lock()
				err = x.store.ClearRateLimit(primary.Provider)
				if err == nil {
					delete(x.rates, primary.Provider)
					err = x.store.SaveRateLimits(x.rates)
				}
				x.mu.Unlock()
				if err != nil {
					return err
				}
				left = 0
			}
		}
	}
	if left > 0 {
		if err := x.setCooldown(primary.Provider, now.Add(left)); err != nil {
			return err
		}
		if ok, err := x.choose(ctx, fallback); ok {
			x.selected(ctx, false, fallback)
			ctx.Notify("Primary rate-limited, using "+clean(fallback.Model), "info")
			return nil
		} else {
			return err
		}
	}
	if ok, err := x.choose(ctx, primary); ok {
		x.selected(ctx, true, primary)
		return nil
	} else if err != nil {
		x.report(ctx, err)
	}
	if ok, err := x.choose(ctx, fallback); ok {
		x.selected(ctx, false, fallback)
		return nil
	} else {
		return err
	}
}
func validOAuth(entry quota.AuthEntry, now time.Time) bool {
	return entry.Type == "oauth" && entry.Access != "" && entry.Expires >= float64(now.UnixMilli())
}

func (x *extension) queueRefresh(ctx sdk.Context, force bool) {
	request := refreshRequest{ctx, force}
	select {
	case <-x.life.Done():
		return
	default:
	}
	select {
	case x.refresh <- request:
		return
	default:
	}
	// Coalesce refreshes; only the newest model context needs a status update.
	select {
	case previous := <-x.refresh:
		request.force = request.force || previous.force
	default:
	}
	select {
	case x.refresh <- request:
	case <-x.life.Done():
	default:
	}
}
func (x *extension) refreshLoop() {
	defer close(x.done)
	for {
		select {
		case <-x.life.Done():
			return
		case request := <-x.refresh:
			x.refreshQuota(request.ctx, request.force)
		}
	}
}
func (x *extension) status(ctx sdk.Context, status *quota.StatusQuota) {
	text, color := "Quota ?", "dim"
	if status != nil {
		text = fmt.Sprintf("%s %d%%", status.Label, status.Percent)
		color = "success"
		if status.Percent >= 90 {
			color = "error"
		} else if status.Percent >= 70 {
			color = "warning"
		}
	}
	ctx.SetStatus("auto-model-quota", ctx.UITheme().Fg(color, text))
}
func (x *extension) refreshQuota(ctx sdk.Context, force bool) {
	provider := ctx.ModelProvider()
	if provider == "" {
		return
	}
	x.mu.Lock()
	changed := provider != x.quotaProvider
	if !force && !changed && time.Since(x.quotaAt) < time.Minute {
		x.mu.Unlock()
		return
	}
	x.quotaAt, x.quotaProvider = time.Now(), provider
	x.mu.Unlock()
	if changed {
		x.status(ctx, nil)
	}
	oauth, err := ctx.ModelRegistry().IsUsingOAuth(map[string]any{"provider": provider, "id": ctx.Model()})
	if err != nil {
		x.report(ctx, err)
		return
	}
	if !oauth {
		if ctx.ModelProvider() == provider {
			ctx.SetStatus("auto-model-quota", ctx.UITheme().Fg("dim", "∞ (API key)"))
		}
		return
	}
	auth, err := x.store.LoadAuth()
	var status *quota.StatusQuota
	if err == nil {
		entry, ok := auth[provider]
		if ok && validOAuth(entry, time.Now()) {
			switch provider {
			case "anthropic":
				var usage *quota.ClaudeUsage
				usage, err = x.client.FetchClaude(x.life, entry)
				if err == nil {
					status = quota.ClaudeStatusQuota(usage)
				}
			case "openai-codex":
				var usage *quota.CodexUsage
				usage, err = x.client.FetchCodex(x.life, entry)
				if err == nil {
					status = quota.CodexStatusQuota(usage)
				}
			}
		}
	}
	if err != nil {
		if x.life.Err() != nil {
			return
		}
		x.report(ctx, err)
		cached := x.rate(provider)
		if cached != nil && !quota.IsStale(cached, time.Now()) && cached.Utilization != "" {
			if n, parseErr := strconv.ParseFloat(cached.Utilization, 64); parseErr == nil && !math.IsNaN(n) && !math.IsInf(n, 0) {
				status = &quota.StatusQuota{Label: "5h", Percent: int(math.Round(n * 100))}
			}
		}
	}
	if ctx.ModelProvider() == provider && x.life.Err() == nil {
		x.status(ctx, status)
	}
}

func (x *extension) provider(ctx sdk.Context) string {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.requestProvider != "" {
		return x.requestProvider
	}
	return ctx.ModelProvider()
}
func (x *extension) fallbackOnLimit(ctx sdk.Context, provider string, left time.Duration) error {
	primary, fallback := x.slots()
	x.mu.Lock()
	using := x.usingPrimary
	x.mu.Unlock()
	if !using || (provider != "" && provider != primary.Provider) {
		return nil
	}
	ok, err := x.choose(ctx, fallback)
	if !ok {
		return err
	}
	x.selected(ctx, false, fallback)
	ctx.Notify(fmt.Sprintf("Primary rate-limited, switched to %s, retry in %dmin", clean(fallback.Model), int(math.Round(left.Minutes()))), "warning")
	return nil
}
func (x *extension) response(ctx sdk.Context, data map[string]any) (any, error) {
	headers := map[string]string{}
	if raw, ok := data["headers"].(map[string]any); ok {
		for key, value := range raw {
			if text, ok := value.(string); ok {
				headers[strings.ToLower(key)] = text
			}
		}
	}
	provider := x.provider(ctx)
	now := time.Now()
	info := quota.ParseAnthropicHeaders(headers, now)
	if info == nil {
		info = quota.ParseOpenAIHeaders(headers, now)
	}
	if info != nil && provider != "" {
		if err := x.saveRate(provider, *info); err != nil {
			x.report(ctx, err)
		}
		if left := quota.PassiveCooldown(info, now); left > 0 {
			if err := x.setCooldown(provider, now.Add(left)); err != nil {
				x.report(ctx, err)
			}
		}
		if info.Utilization != "" && provider == ctx.ModelProvider() {
			if n, err := strconv.ParseFloat(info.Utilization, 64); err == nil && !math.IsNaN(n) && !math.IsInf(n, 0) {
				x.mu.Lock()
				x.quotaAt, x.quotaProvider = now, provider
				x.mu.Unlock()
				x.status(ctx, &quota.StatusQuota{Label: "5h", Percent: int(math.Round(n * 100))})
			}
		}
	}
	status, _ := data["status"].(float64)
	if status == 429 || status == 529 {
		left := quota.Cooldown(headers, now)
		if provider != "" {
			if err := x.setCooldown(provider, now.Add(left)); err != nil {
				x.report(ctx, err)
			}
		}
		if err := x.fallbackOnLimit(ctx, provider, left); err != nil {
			x.report(ctx, err)
		}
	}
	return nil, nil
}
func (x *extension) messageEnd(ctx sdk.Context, data map[string]any) (any, error) {
	message, ok := data["message"].(map[string]any)
	if !ok || message["role"] != "assistant" {
		return nil, nil
	}
	text, _ := message["errorMessage"].(string)
	if !quota.RateLimitError(text) {
		return nil, nil
	}
	provider, _ := message["provider"].(string)
	if provider == "" {
		provider = x.provider(ctx)
	}
	if provider == "" {
		return nil, nil
	}
	now := time.Now()
	left := quota.Cooldown(nil, now)
	previous := x.rate(provider)
	value := quota.RateLimitInfo{}
	if previous != nil {
		value = *previous
		if reset, err := strconv.ParseFloat(value.Reset, 64); err == nil && !math.IsNaN(reset) && !math.IsInf(reset, 0) {
			if expiry := time.UnixMilli(int64(reset * 1000)); expiry.After(now) {
				left = expiry.Sub(now)
			}
		}
	}
	value.Utilization, value.Status, value.CapturedAt = "1", "rate_limited", float64(now.UnixMilli())
	if err := x.saveRate(provider, value); err != nil {
		x.report(ctx, err)
	}
	if err := x.setCooldown(provider, now.Add(left)); err != nil {
		x.report(ctx, err)
	}
	if err := x.fallbackOnLimit(ctx, provider, left); err != nil {
		x.report(ctx, err)
	}
	return nil, nil
}
