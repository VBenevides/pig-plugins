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
	"github.com/VBenevides/pig-plugins/internal/footerstatus"
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
	quotaAccount      string
	primaryQuotaAt    time.Time
	lowQuotaAsked     string
	retryPending      bool
	retryUsed         bool
	retryTarget       quota.Slot
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
		e.OnEvent(event, func(ctx sdk.Context, data map[string]any) (any, error) {
			if event == "model_select" {
				x.reconcile(ctx)
			}
			if event == "agent_end" {
				x.mu.Lock()
				pending := x.retryPending
				target := x.retryTarget
				x.retryPending = false
				x.mu.Unlock()
				willRetry, _ := data["willRetry"].(bool)
				if pending && !willRetry && ctx.ModelProvider() == target.Provider && ctx.Model() == target.Model {
					if err := ctx.SendUserMessage("Continue the interrupted task.", "followUp"); err != nil {
						x.report(ctx, err)
					}
				}
			}
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

// reconcile keeps the footer badge and fallback state in line with the active
// model, including manual /model switches and host-applied defaults.
func (x *extension) reconcile(ctx sdk.Context) {
	primary, fallback := x.slots()
	provider, model := ctx.ModelProvider(), ctx.Model()
	switch {
	case provider == primary.Provider && model == primary.Model:
		x.selected(ctx, true)
	case provider == fallback.Provider && model == fallback.Model:
		x.selected(ctx, false)
	default:
		x.mu.Lock()
		x.usingPrimary = false
		x.mu.Unlock()
		footerstatus.Set(ctx, "auto-model", "")
	}
}
func (x *extension) selected(ctx sdk.Context, primary bool) {
	x.mu.Lock()
	x.usingPrimary = primary
	x.mu.Unlock()
	label, color := "⚡ fallback", "warning"
	if primary {
		label, color = "🧠 primary", "success"
	}
	footerstatus.Set(ctx, "auto-model", ctx.UITheme().Fg(color, label))
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
	run, cancel := sdkctx.Request(ctx)
	available, err := x.primaryAvailable(run, ctx, primary.Provider)
	cancel()
	if err != nil {
		x.report(ctx, err)
	} else if available != nil {
		if *available {
			if err := x.clearPrimaryCooldown(primary.Provider); err != nil {
				return err
			}
			left = 0
		} else if left <= 0 {
			left = quota.Cooldown(nil, now)
		}
	}
	if left > 0 {
		if err := x.setCooldown(primary.Provider, now.Add(left)); err != nil {
			return err
		}
		if ok, err := x.choose(ctx, fallback); ok {
			x.selected(ctx, false)
			ctx.Notify("Primary rate-limited, using "+clean(fallback.Model), "info")
			return nil
		} else {
			return err
		}
	}
	if ok, err := x.choose(ctx, primary); ok {
		x.selected(ctx, true)
		return nil
	} else if err != nil {
		x.report(ctx, err)
	}
	if ok, err := x.choose(ctx, fallback); ok {
		x.selected(ctx, false)
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
	ticker := time.NewTicker(primaryQuotaInterval)
	defer ticker.Stop()
	var latest *sdk.Context
	for {
		select {
		case <-x.life.Done():
			return
		case request := <-x.refresh:
			latest = &request.ctx
			x.refreshQuota(request.ctx, request.force)
		case <-ticker.C:
			if latest != nil {
				x.refreshQuota(*latest, false)
			}
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
	footerstatus.Set(ctx, "auto-model-quota", ctx.UITheme().Fg(color, text))
}
func (x *extension) refreshQuota(ctx sdk.Context, force bool) {
	if err := x.recoverPrimary(ctx); err != nil {
		if x.life.Err() == nil {
			x.report(ctx, err)
		}
	}
	provider := ctx.ModelProvider()
	if provider == "" {
		return
	}
	account, hasAccount, err := selectedAccount(ctx, provider)
	if err != nil {
		x.report(ctx, err)
		x.status(ctx, nil)
		return
	}
	x.mu.Lock()
	changed := provider != x.quotaProvider || account.ID != x.quotaAccount
	if !force && !changed && time.Since(x.quotaAt) < time.Minute {
		x.mu.Unlock()
		return
	}
	x.quotaAt, x.quotaProvider, x.quotaAccount = time.Now(), provider, account.ID
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
			footerstatus.Set(ctx, "auto-model-quota", ctx.UITheme().Fg("dim", "∞ (API key)"))
		}
		return
	}
	var status *quota.StatusQuota
	var remaining *float64
	if hasAccount {
		status, remaining, err = x.accountQuota(x.life, ctx, account)
	}
	if err != nil {
		if x.life.Err() != nil {
			return
		}
		x.report(ctx, err)
	}
	current, _, lookupErr := selectedAccount(ctx, provider)
	if lookupErr != nil {
		x.report(ctx, lookupErr)
		return
	}
	if ctx.ModelProvider() == provider && current.ID == account.ID && x.life.Err() == nil {
		x.status(ctx, status)
		if err := x.lowQuota(nativeModelSwitcher{ctx, x}, account, remaining); err != nil {
			x.report(ctx, err)
		}
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
	return x.switchOnLimit(nativeModelSwitcher{ctx, x}, provider)
}

func (x *extension) switchOnLimit(ctx modelSwitcher, provider string) error {
	if !x.autoEnabled || provider != ctx.ModelProvider() {
		return nil
	}
	primary, fallback := x.slots()
	failedModel := ctx.Model()
	target, toPrimary := fallback, false
	switch {
	case ctx.ModelProvider() == primary.Provider && ctx.Model() == primary.Model:
	case ctx.ModelProvider() == fallback.Provider && ctx.Model() == fallback.Model:
		target, toPrimary = primary, true
	default:
		return nil
	}
	x.mu.Lock()
	used := x.retryUsed
	x.mu.Unlock()
	if used {
		return nil
	}
	account, found, err := selectedAccount(ctx, provider)
	if err != nil {
		return err
	}
	if found {
		switched, err := x.rotateAccountWithPolicy(ctx, account, false)
		if switched {
			x.mu.Lock()
			x.retryPending, x.retryUsed = true, true
			x.retryTarget = quota.Slot{Provider: provider, Model: failedModel}
			x.mu.Unlock()
		}
		if err != nil || switched {
			return err
		}
	}
	if found {
		current, stillSelected, err := selectedAccount(ctx, provider)
		if err != nil {
			return err
		}
		if !stillSelected || current.ID != account.ID {
			return nil
		}
	}
	x.mu.Lock()
	used = x.retryUsed
	x.mu.Unlock()
	if used {
		return nil
	}
	if primary == fallback {
		return nil
	}
	account, found, err = selectedAccount(ctx, target.Provider)
	if err != nil || !found {
		return err
	}
	remaining, err := x.accountRemaining(x.life, ctx, account)
	if err != nil || remaining == nil || *remaining <= 0 {
		return err
	}
	current, found, err := selectedAccount(ctx, target.Provider)
	if err != nil || !found || current.ID != account.ID || provider != ctx.ModelProvider() {
		return err
	}
	currPrimary, currFallback := x.slots()
	if currPrimary != primary || currFallback != fallback || ctx.Model() != failedModel || x.life.Err() != nil {
		return nil
	}
	ok, err := ctx.chooseModel(target)
	if err != nil || !ok {
		return err
	}
	ctx.markSelected(toPrimary)
	x.mu.Lock()
	x.retryPending, x.retryUsed = true, true
	x.retryTarget = target
	x.primaryQuotaAt = time.Now()
	x.mu.Unlock()
	ctx.Notify("Quota exhausted, switched to "+clean(target.Model)+" and continuing", "warning")
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
	if message["stopReason"] != "error" && text == "" {
		x.mu.Lock()
		x.retryUsed = false
		x.retryPending = false
		x.mu.Unlock()
	}
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
