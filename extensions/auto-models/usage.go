package automodels

import (
	"context"
	"fmt"
	"strings"
	"time"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	quota "github.com/VBenevides/pig-plugins/internal/automodels"
	"github.com/VBenevides/pig-plugins/internal/footerstatus"
	"github.com/VBenevides/pig-plugins/internal/sdkctx"
)

func (x *extension) usage(ctx sdk.Context, _ string) error {
	ctx.SetWorkingMessage("Checking quota…")
	ctx.SetWorkingVisible(true)
	footerstatus.Set(ctx, "auto-model-usage", ctx.UITheme().Fg("warning", "⏳ Checking quota…"))
	defer func() {
		ctx.SetWorkingMessage("")
		ctx.SetWorkingVisible(true)
		footerstatus.Set(ctx, "auto-model-usage", "")
	}()
	run, cancel := sdkctx.Request(ctx)
	defer cancel()
	lines, err := x.usageLines(run, ctx, ctx.ModelProvider(), ctx.Model())
	if err != nil {
		return err
	}
	text := clean(strings.Join(lines, "\n"))
	if ctx.HasUI() && ctx.Mode() != "rpc" {
		footerstatus.Set(ctx, "auto-model-usage", "")
		_, err := ctx.Custom(&dashboard{title: "Usage", lines: strings.Split(text, "\n"), theme: ctx.UITheme()}, sdk.RemoteOverlayOptions{Title: "Usage"})
		return err
	}
	ctx.Notify(text, "info")
	return nil
}

// Fetch sequentially: each HTTP request has a 15-second deadline and cancellation
// stops the dashboard without refreshing credentials or changing native selection.
func (x *extension) usageLines(run context.Context, source accountSource, activeProvider, model string) ([]string, error) {
	accounts, err := source.OAuthAccounts()
	if err != nil {
		return nil, fmt.Errorf("native OAuth account enumeration failed")
	}
	lines := []string{"Active model: " + clean(activeProvider+"/"+model), "Credential source: native PiG OAuth accounts", ""}
	if len(accounts) == 0 {
		return append(lines, "No native OAuth accounts. Use /login to add an account.", "Quota unknown"), nil
	}
	for _, account := range accounts {
		if err := run.Err(); err != nil {
			return nil, err
		}
		now := time.Now()
		label := account.Label
		if label == "" {
			label = account.ID
		}
		marker := ""
		if account.Active {
			marker = " [active]"
		}
		lines = append(lines, "── Account ("+clean(account.Provider)+") "+clean(label)+marker+" ──")
		entry, authErr := accountAuth(source, account.ID)
		switch {
		case authErr != nil:
			lines = append(lines, "  🔑 Credential lookup failed")
		case !validOAuth(entry, now):
			lines = append(lines, "  🔑 OAuth credentials unavailable or expired; please /login again")
		default:
			lines = append(lines, "  🔑 Logged in (token valid until "+time.UnixMilli(int64(entry.Expires)).Format("1/2/2006")+")")
		}
		var claude *quota.ClaudeUsage
		var codex *quota.CodexUsage
		fetchErr := authErr
		if authErr == nil && validOAuth(entry, now) {
			switch account.Provider {
			case "anthropic":
				claude, fetchErr = x.client.FetchClaude(run, entry)
			case "openai-codex":
				codex, fetchErr = x.client.FetchCodex(run, entry)
			}
		}
		if err := run.Err(); err != nil {
			return nil, err
		}
		// Passive state belongs to a provider, not a credential. It is eligible
		// only for the selected account of the currently running provider.
		var info *quota.RateLimitInfo
		var left time.Duration
		if account.Active && account.Provider == activeProvider {
			info = x.rate(account.Provider)
			left, err = x.cooldown(account.Provider, now)
			if err != nil {
				lines = append(lines, "  📈 Failed to read provider cooldown")
			}
			if passive := quota.PassiveCooldown(info, now); passive > left {
				left = passive
			}
		}
		windows := []quota.CodexWindow{}
		if codex != nil && codex.RateLimit != nil {
			for _, window := range []*quota.CodexWindow{codex.RateLimit.PrimaryWindow, codex.RateLimit.SecondaryWindow} {
				if window != nil {
					windows = append(windows, *window)
				}
			}
		}
		available := quota.ClaudeAvailable(claude)
		switch {
		case codex != nil && codex.RateLimit != nil && codex.RateLimit.LimitReached:
			lines = append(lines, "  📊 ❌ Rate-limited")
		case codex != nil && codex.RateLimit != nil && codex.RateLimit.Allowed:
			lines = append(lines, "  📊 ✅ Quota available")
		case available != nil && *available:
			lines = append(lines, "  📊 ✅ Quota available")
		case available != nil && !*available:
			lines = append(lines, "  📊 ❌ Rate-limited")
		case left > 0:
			lines = append(lines, "  📊 ❌ Provider rate-limited, recovers in "+quota.FormatTimeLeft(left))
		default:
			lines = append(lines, "  📊 Quota unknown")
		}
		if fetchErr != nil {
			lines = append(lines, "  📈 Failed to fetch quota: "+fetchErr.Error())
		}
		switch {
		case len(windows) > 0:
			lines = append(lines, quota.FormatCodexUsageLines(windows, codex.PlanType, codex.AdditionalRateLimits, now)...)
			lines = append(lines, "  ⏰ Real-time")
		case claude != nil && len(claude.Limits) > 0:
			lines = append(lines, quota.FormatClaudeUsageLines(claude.Limits, now)...)
			lines = append(lines, "  ⏰ Real-time")
		case account.Provider == "openai":
			lines = append(lines, "  📈 Quota unavailable: direct OpenAI login has no numeric subscription balance; Codex quota is separate (/login openai-codex)")
		case info != nil && !quota.IsStale(info, now):
			lines = append(lines, "  Provider response data (not account quota):")
			lines = append(lines, quota.FormatPassiveRateLimitLines(*info)...)
		default:
			lines = append(lines, "  📈 No live quota details available for this account")
		}
		lines = append(lines, "")
	}
	return lines, nil
}

func (x *extension) configure(ctx sdk.Context, _ string) error {
	if !ctx.HasUI() {
		return fmt.Errorf("/auto-model requires an interactive UI")
	}
	primary, fallback := x.slots()
	slot, ok, err := pick(ctx, "Configure Auto Model", []choice{
		{"primary", "Primary model", primary.Provider + "/" + primary.Model + " (" + primary.Thinking + ")"},
		{"fallback", "Fallback model", fallback.Provider + "/" + fallback.Model + " (" + fallback.Thinking + ")"},
	}, false)
	if err != nil || !ok {
		return err
	}
	// A configured scope (--models or enabledModels) limits the choices; with no scope, offer every authenticated model.
	scoped, err := ctx.ScopedModels()
	if err != nil {
		return err
	}
	models := make([]map[string]any, 0, len(scoped))
	for _, entry := range scoped {
		models = append(models, entry.Model)
	}
	if len(models) == 0 {
		if models, err = ctx.ModelRegistry().GetAvailable(); err != nil {
			return err
		}
	}
	items := make([]choice, 0, len(models))
	for _, model := range models {
		provider, _ := model["provider"].(string)
		id, _ := model["id"].(string)
		if provider != "" && id != "" {
			value := provider + "/" + id
			items = append(items, choice{value, value, ""})
		}
	}
	if len(items) == 0 {
		return fmt.Errorf("no authenticated models available")
	}
	label := "Primary"
	if slot == "fallback" {
		label = "Fallback"
	}
	model, ok, err := pick(ctx, "Select "+label+" model", items, true)
	if err != nil || !ok {
		return err
	}
	levels := []choice{}
	for _, level := range []string{"off", "minimal", "low", "medium", "high", "xhigh"} {
		levels = append(levels, choice{level, level, ""})
	}
	thinking, ok, err := pick(ctx, "Select Thinking Level", levels, false)
	if err != nil || !ok {
		return err
	}
	provider, id, _ := strings.Cut(model, "/")
	selected := quota.Slot{Provider: provider, Model: id, Thinking: thinking}
	x.mu.Lock()
	// Read again so configuring one slot preserves changes from another process.
	config, err := x.store.LoadConfig()
	if err == nil {
		if slot == "primary" {
			config.Primary = &selected
		} else {
			config.Fallback = &selected
		}
		err = x.store.SaveConfig(config)
		if err == nil {
			x.config = config
			x.primary, x.fallback = quota.Defaults(config)
		}
	}
	x.mu.Unlock()
	if err != nil {
		return err
	}
	ctx.Notify(fmt.Sprintf("%s set to %s (%s)", label, clean(model), thinking), "info")
	x.queueRefresh(ctx, true)
	return nil
}
