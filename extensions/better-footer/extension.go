// Package betterfooter ports pi-better-footer@0.1.3 to the native SDK.
package betterfooter

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sync"
	"time"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/agentdir"
	quota "github.com/VBenevides/pig-plugins/internal/automodels"
	bf "github.com/VBenevides/pig-plugins/internal/betterfooter"
	"github.com/VBenevides/pig-plugins/internal/footerstatus"
)

const Name = "better-footer"

var sharedQuota bf.QuotaStore

type extension struct {
	mu                     sync.Mutex
	saveMu                 sync.Mutex
	settings               bf.Settings
	settingsPath           string
	args                   []string
	fused                  bool
	disabled, restoring    bool
	generation             uint64
	cycle                  uint64
	ctx                    sdk.Context
	mounted                bool
	state                  bf.RenderState
	speed                  bf.SpeedTracker
	stats                  bf.SessionStats
	store                  *bf.QuotaStore
	reader                 bf.Reader
	client                 quota.Client
	quotaGeneration        uint64
	requestQuotaGeneration uint64
	requestKey             string
	requestGeneration      uint64
	lastActivity, lastPoll time.Time
	lastKnownAt            int64
	leaf                   string
	gitQueued, forceQuota  bool
	pendingCycle           map[string]any
	draw                   chan struct{}
	drawDone               chan struct{}
	unsubscribe            func()
	unsubscribeStatus      func()
	inputThinking          string
	inputAt                time.Time
	wake                   chan struct{}
	life                   context.Context
	stop                   context.CancelFunc
	done                   chan struct{}
	origin                 time.Time
}

// Extension is the fused native factory. CLI arguments belong to PiG only in fused builds.
func Extension() *sdk.Extension {
	info, ok := debug.ReadBuildInfo()
	fused := ok && info.Main.Path == "github.com/MichaelKinsy/PiG"
	home, _ := os.UserHomeDir()
	life, stop := context.WithCancel(context.Background())
	x := &extension{store: &sharedQuota, fused: fused, settingsPath: bf.SettingsPath(os.Getenv), wake: make(chan struct{}, 1), draw: make(chan struct{}, 1), drawDone: make(chan struct{}), life: life, stop: stop, done: make(chan struct{}), origin: time.Now()}
	if fused {
		x.args = append([]string(nil), os.Args[1:]...)
	}
	x.state.Home = home
	x.settings = bf.DefaultSettings()
	x.reader = bf.Reader{Store: x.store, AuthPath: filepath.Join(agentdir.Dir(os.Getenv), "auth.json"), Env: bf.Environment{Getenv: os.Getenv, Home: home, GOOS: runtime.GOOS}}
	x.reader.Fetch = x.fetchNative
	e := sdk.New(Name)
	e.Flag("no-recent-model", sdk.FlagOptions{Description: "Neither restore nor record the recent model and thinking level for this run", Type: sdk.FlagBoolean})
	e.RegisterCommand("better-footer", sdk.CommandOptions{Description: "Configure model/thinking persistence and skipping exhausted scoped models", Handler: x.configure})
	e.OnSessionStart(x.start)
	e.OnSessionShutdown(x.shutdown)
	e.OnEvent("model_select", x.modelSelect)
	e.OnEvent("thinking_level_select", func(ctx sdk.Context, data map[string]any) (any, error) {
		x.mu.Lock()
		x.state.Thinking = text(data, "level")
		x.lastActivity = time.Now()
		if !x.restoring {
			x.lastKnownAt = x.lastActivity.UnixMilli()
		}
		x.mu.Unlock()
		x.signal(false, false)
		return nil, x.remember(ctx)
	})
	e.OnEvent("before_provider_request", func(ctx sdk.Context, data map[string]any) (any, error) {
		model := object(data, "model")
		if model == nil {
			model = ctx.ModelRegistry().Find(ctx.ModelProvider(), ctx.Model())
		}
		key, err := modelQuotaKey(ctx, model)
		x.mu.Lock()
		if text(model, "provider") == ctx.ModelProvider() && text(model, "id") == ctx.Model() {
			x.setQuotaKeyLocked(key)
		}
		x.requestKey = key
		x.requestGeneration = x.generation
		x.requestQuotaGeneration = x.quotaGeneration
		x.mu.Unlock()
		return nil, err
	})
	e.OnEvent("after_provider_response", x.response)
	e.OnEvent("message_start", func(_ sdk.Context, data map[string]any) (any, error) {
		if text(object(data, "message"), "role") == "assistant" {
			x.mu.Lock()
			x.speed.MessageStart()
			x.mu.Unlock()
		}
		return nil, nil
	})
	e.OnEvent("message_update", func(_ sdk.Context, data map[string]any) (any, error) {
		if text(object(data, "message"), "role") != "assistant" {
			return nil, nil
		}
		delta := object(data, "assistantMessageEvent")
		x.mu.Lock()
		changed := x.speed.Delta(text(delta, "type"), text(delta, "delta"), time.Since(x.origin))
		x.state.Speed = x.speed.Speed
		x.state.Estimated = x.speed.Estimated
		x.mu.Unlock()
		if changed {
			x.redraw()
		}
		return nil, nil
	})
	e.OnEvent("message_end", x.messageEnd)
	for _, name := range []string{"input", "agent_start", "agent_end", "tool_execution_end"} {
		name := name
		e.OnEvent(name, func(_ sdk.Context, data map[string]any) (any, error) {
			x.mu.Lock()
			x.lastActivity = time.Now()
			switch name {
			case "agent_start":
				x.speed.AgentStart(time.Since(x.origin))
			case "agent_end":
				x.speed.AgentEnd(time.Since(x.origin))
			}
			x.state.Speed, x.state.Estimated = x.speed.Speed, x.speed.Estimated
			x.mu.Unlock()
			git := name == "input" || name == "agent_end" || name == "tool_execution_end" && !readOnly(text(data, "toolName"))
			x.signal(git, false)
			x.redraw()
			return nil, nil
		})
	}
	go x.worker()
	go x.drawLoop()
	return e
}
func object(m map[string]any, key string) map[string]any { v, _ := m[key].(map[string]any); return v }
func text(m map[string]any, key string) string           { v, _ := m[key].(string); return v }
func number(m map[string]any, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case json.Number:
		n, _ := v.Float64()
		return n
	}
	return 0
}
func readOnly(name string) bool {
	return name == "read" || name == "grep" || name == "find" || name == "ls"
}
func (x *extension) report(ctx interface {
	HasUI() bool
	Notify(string, string)
}, err error) {
	if err != nil {
		message := bf.SanitizePlain(err.Error())
		log.Printf("better-footer: %s", message)
		if ctx.HasUI() {
			ctx.Notify("better-footer: "+message, "warning")
		}
	}
}
func (x *extension) signal(git, force bool) {
	x.mu.Lock()
	x.gitQueued = x.gitQueued || git
	x.forceQuota = x.forceQuota || force
	x.mu.Unlock()
	select {
	case x.wake <- struct{}{}:
	default:
	}
	x.redraw()
}
func (x *extension) redraw() {
	select {
	case x.draw <- struct{}{}:
	default:
	}
}
func modelQuotaKey(ctx sdk.Context, model map[string]any) (string, error) {
	return modelQuotaKeyWithSource(ctx, model, ctx)
}

func modelQuotaKeyWithSource(ctx sdk.Context, model map[string]any, source accountList) (string, error) {
	if model == nil {
		return "", nil
	}
	provider := text(model, "provider")
	oauth := false
	if provider == "openai" && text(model, "api") != "pi-virtual" {
		var err error
		oauth, err = ctx.ModelRegistry().IsUsingOAuth(model)
		if err != nil {
			return "", err
		}
	}
	base := bf.QuotaKey(text(model, "api"), provider, text(model, "baseUrl"), func() bool { return oauth })
	return nativeQuotaKey(source, provider, base)
}
func (x *extension) start(ctx sdk.Context, data map[string]any) (any, error) {
	settings, err := bf.LoadSettings(x.settingsPath)
	x.report(ctx, err)
	flag, err := ctx.GetFlag("no-recent-model")
	x.report(ctx, err)
	if x.unsubscribe != nil {
		x.unsubscribe()
		x.unsubscribe = nil
	}
	if x.unsubscribeStatus != nil {
		x.unsubscribeStatus()
	}
	// A badge set by another extension must show now, not on the next 5 s tick or streamed token.
	x.unsubscribeStatus = footerstatus.Subscribe(x.redraw)
	x.mu.Lock()
	home := x.state.Home
	x.settings = settings
	x.disabled = flag == true
	x.generation++
	x.cycle++
	x.ctx = ctx
	x.mounted = ctx.Mode() == "tui"
	x.state = bf.RenderState{Home: home, Cwd: ctx.Cwd()}
	x.stats = bf.SessionStats{}
	x.speed = bf.SpeedTracker{Live: x.mounted}
	x.lastActivity = time.Now()
	x.lastKnownAt = x.lastActivity.UnixMilli()
	x.lastPoll = time.Time{}
	x.leaf = ""
	x.pendingCycle = nil
	x.mu.Unlock()
	if !x.fused && settings.KeepRecentModel && !x.disabled {
		ctx.Notify("better-footer: recent model restoration requires a fused binary to respect explicit PiG CLI options; recording is also disabled in packed extensions", "warning")
	}
	x.restore(ctx, text(data, "reason"))
	x.syncModel(ctx)
	entries, err := ctx.SessionManager().GetEntries()
	x.report(ctx, err)
	if err == nil {
		x.seedErrors(ctx, entries)
	}
	if ctx.Mode() == "tui" {
		if err := ctx.SetFooterRenderer(x.render(ctx)); err != nil {
			x.report(ctx, err)
		}
	}
	if ctx.Mode() == "tui" {
		unsub, err := ctx.OnTerminalInput(func(_ string) sdk.TerminalInputResult {
			x.mu.Lock()
			x.inputThinking = x.state.Thinking
			x.inputAt = time.Now()
			x.mu.Unlock()
			return sdk.TerminalInputResult{}
		})
		x.report(ctx, err)
		x.unsubscribe = unsub
	}
	x.signal(true, true)
	return nil, nil
}
func (x *extension) shutdown(ctx sdk.Context, _ map[string]any) (any, error) {
	err := x.remember(ctx)
	x.stop()
	x.mu.Lock()
	x.generation++
	x.cycle++
	x.mounted = false
	x.mu.Unlock()
	if x.unsubscribe != nil {
		x.unsubscribe()
		x.unsubscribe = nil
	}
	if x.unsubscribeStatus != nil {
		x.unsubscribeStatus()
		x.unsubscribeStatus = nil
	}
	select {
	case <-x.done:
	case <-time.After(2 * time.Second):
		log.Print("better-footer: worker did not stop before shutdown deadline")
	}
	select {
	case <-x.drawDone:
	case <-time.After(2 * time.Second):
		log.Print("better-footer: renderer did not stop before shutdown deadline")
	}
	return nil, err
}
func (x *extension) render(ctx sdk.Context) func(int) []string {
	return func(width int) []string {
		x.mu.Lock()
		state := x.state
		x.mu.Unlock()
		state.Statuses = footerstatus.Snapshot()
		theme := ctx.UITheme()
		return bf.RenderFooter(state, width, bf.Theme{Fg: theme.Fg, Name: theme.Name, Appearance: string(theme.Appearance()), ColorFGBG: os.Getenv("COLORFGBG")}, time.Now())
	}
}
func (x *extension) syncModel(ctx sdk.Context) {
	x.mu.Lock()
	gen, quotaGen := x.generation, x.quotaGeneration
	x.mu.Unlock()
	model := ctx.ModelRegistry().Find(ctx.ModelProvider(), ctx.Model())
	key, err := modelQuotaKey(ctx, model)
	x.report(ctx, err)
	level, err := ctx.GetThinkingLevel()
	x.report(ctx, err)
	x.mu.Lock()
	if gen != x.generation || quotaGen != x.quotaGeneration {
		x.mu.Unlock()
		return
	}
	x.state.Provider = ctx.ModelProvider()
	x.state.Model = ctx.Model()
	reasoning, _ := model["reasoning"].(bool)
	x.state.Reasoning = reasoning
	x.state.ContextWindow = int(number(model, "contextWindow"))
	x.state.Thinking = level
	x.setQuotaKeyLocked(key)
	x.state.Quota, _ = x.store.Get(key)
	x.mu.Unlock()
}
func (x *extension) modelSelect(ctx sdk.Context, data map[string]any) (any, error) {
	x.mu.Lock()
	old := x.state.Provider
	x.ctx = ctx
	x.lastActivity = time.Now()
	if !x.restoring {
		x.lastKnownAt = x.lastActivity.UnixMilli()
	}
	x.cycle++
	if text(data, "source") == "cycle" && ctx.Mode() == "tui" && x.settings.SkipExhaustedScopedModels && !x.restoring {
		copy := make(map[string]any, len(data)+1)
		for k, v := range data {
			copy[k] = v
		}
		if time.Since(x.inputAt) < time.Second {
			copy["preCycleThinking"] = x.inputThinking
		}
		x.pendingCycle = copy
	}
	x.mu.Unlock()
	x.syncModel(ctx)
	x.signal(false, old != ctx.ModelProvider())
	return nil, x.remember(ctx)
}
func (x *extension) response(ctx sdk.Context, data map[string]any) (any, error) {
	headers := map[string]string{}
	switch raw := data["headers"].(type) {
	case map[string]any:
		for k, v := range raw {
			if s, ok := v.(string); ok {
				headers[k] = s
			}
		}
	case map[string]string:
		headers = raw
	}
	// Selection can change without model_select. Resolve before accepting headers.
	x.syncModel(ctx)
	if x.publishHeaders(headers, int(number(data, "status")), time.Now()) {
		x.signal(false, false)
	}
	return nil, nil
}

func (x *extension) publishHeaders(headers map[string]string, status int, now time.Time) bool {
	x.mu.Lock()
	key := x.state.QuotaKey
	valid := key != "" && key == x.requestKey && x.requestGeneration == x.generation && x.requestQuotaGeneration == x.quotaGeneration
	if !valid {
		x.mu.Unlock()
		return false
	}
	source := bf.QuotaSource(key)
	x.store.Update(key, now, func(q *bf.ProviderQuota) {
		if source == bf.CodexProvider {
			for _, w := range bf.ParseCodexUsageHeaders(headers, status, q.Windows, now) {
				q.Windows = bf.UpsertRateWindow(q.Windows, w)
			}
		} else if source != bf.ChatGPTQuotaKey && !bf.IsZaiProvider(source) && !bf.IsOpenCodeGoProvider(source) {
			if windows := bf.ToRateWindows(bf.DetectRateWindows(headers), now); len(windows) > 0 {
				q.Windows = windows
			}
		}
	})
	x.mu.Unlock()
	return true
}
func (x *extension) recordError(ctx sdk.Context, msg map[string]any) {
	provider, modelID := text(msg, "provider"), text(msg, "model")
	key, err := modelQuotaKey(ctx, ctx.ModelRegistry().Find(provider, modelID))
	x.report(ctx, err)
	if key == "" {
		return
	}
	if _, native := bf.NativeQuotaAccount(key); native {
		x.mu.Lock()
		defer x.mu.Unlock()
		if key != x.requestKey || x.requestGeneration != x.generation || x.requestQuotaGeneration != x.quotaGeneration {
			return // Messages have no account ID; only this live request can attribute them.
		}
	}
	now := time.Now()
	message := text(msg, "errorMessage")
	if bf.QuotaSource(key) == bf.ChatGPTQuotaKey {
		if bf.IsChatGPTLimitError(message) {
			x.store.Update(key, now, func(q *bf.ProviderQuota) { q.ChatGPTLimitAt = now })
		} else if message == "" && text(msg, "stopReason") != "error" && text(msg, "stopReason") != "aborted" {
			x.store.Update(key, now, func(q *bf.ProviderQuota) { q.ChatGPTLimitAt = time.Time{} })
		}
	}
	if bf.IsZaiProvider(provider) && message != "" {
		if w, ok := bf.ParseLimitError(message, now); ok {
			x.store.Update(key, now, func(q *bf.ProviderQuota) { q.Windows = bf.UpsertRateWindow(q.Windows, w) })
		}
	}
}
func (x *extension) seedErrors(ctx sdk.Context, entries []map[string]any) {
	for _, entry := range entries {
		msg := object(entry, "message")
		if text(msg, "role") == "assistant" && bf.IsZaiProvider(text(msg, "provider")) {
			x.recordError(ctx, msg)
		}
	}
}
func (x *extension) messageEnd(ctx sdk.Context, data map[string]any) (any, error) {
	msg := object(data, "message")
	if text(msg, "role") == "assistant" {
		x.recordError(ctx, msg)
		usage := object(msg, "usage")
		x.mu.Lock()
		x.speed.MessageEnd(bf.StreamUsage{Output: int(number(usage, "output")), Reasoning: int(number(usage, "reasoning"))}, time.Since(x.origin))
		x.state.Speed = x.speed.Speed
		x.state.Estimated = x.speed.Estimated
		x.mu.Unlock()
	}
	x.mu.Lock()
	x.lastActivity = time.Now()
	x.mu.Unlock()
	x.signal(false, false)
	return nil, nil
}
func (x *extension) configure(ctx sdk.Context, _ string) error {
	if !ctx.HasUI() {
		return nil
	}
	for {
		shown, err := bf.LoadSettings(x.settingsPath)
		if err != nil {
			x.report(ctx, err)
		}
		recent, skip := shown.MenuItems()
		choice, ok, err := ctx.Select("Better footer settings", []string{recent, skip})
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		next, err := bf.LoadSettings(x.settingsPath)
		if err != nil {
			return err
		}
		switch choice {
		case recent:
			next.KeepRecentModel = !shown.KeepRecentModel
		case skip:
			next.SkipExhaustedScopedModels = !shown.SkipExhaustedScopedModels
		default:
			return fmt.Errorf("unknown better-footer setting")
		}
		if err := bf.SaveSettings(x.settingsPath, next); err != nil {
			return err
		}
		x.mu.Lock()
		x.settings = next
		x.mu.Unlock()
	}
}
