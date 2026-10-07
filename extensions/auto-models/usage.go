package automodels

import (
	"fmt"
	"strings"
	"time"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	quota "github.com/VBenevides/pig-plugins/internal/automodels"
	"github.com/VBenevides/pig-plugins/internal/sdkctx"
)

func (x *extension) usage(ctx sdk.Context, _ string) error {
	ctx.SetWorkingMessage("Checking quota…")
	ctx.SetWorkingVisible(true)
	ctx.SetStatus("auto-model-usage", ctx.UITheme().Fg("warning", "⏳ Checking quota…"))
	defer func() { ctx.SetWorkingMessage(""); ctx.SetWorkingVisible(true); ctx.SetStatus("auto-model-usage", "") }()
	run, cancel := sdkctx.Request(ctx)
	defer cancel()
	auth, err := x.store.LoadAuth()
	if err != nil {
		return err
	}
	primary, fallback := x.slots()
	providers := []string{primary.Provider}
	if fallback.Provider != primary.Provider {
		providers = append(providers, fallback.Provider)
	}
	lines := []string{}
	for _, provider := range providers {
		if err := run.Err(); err != nil {
			return err
		}
		now := time.Now()
		entry, present := auth[provider]
		lines = append(lines, "── Account ("+clean(provider)+") ──")
		switch {
		case !present:
			lines = append(lines, "  🔑 Not logged in")
		case entry.Type != "oauth":
			lines = append(lines, "  🔑 API key account (no subscription quota)")
		case entry.Expires < float64(now.UnixMilli()):
			lines = append(lines, "  🔑 Token expired, please /login again")
		default:
			lines = append(lines, "  🔑 Logged in (token valid until "+time.UnixMilli(int64(entry.Expires)).Format("1/2/2006")+")")
		}
		var claude *quota.ClaudeUsage
		var codex *quota.CodexUsage
		var fetchErr error
		if present && validOAuth(entry, now) {
			switch provider {
			case "anthropic":
				claude, fetchErr = x.client.FetchClaude(run, entry)
			case "openai-codex", "openai":
				codex, fetchErr = x.client.FetchCodex(run, entry)
			}
		}
		if run.Err() != nil {
			return run.Err()
		}
		info := x.rate(provider)
		left, err := x.cooldown(provider, now)
		if err != nil {
			return err
		}
		if passive := quota.PassiveCooldown(info, now); passive > 0 {
			if err := x.setCooldown(provider, now.Add(passive)); err != nil {
				return err
			}
			if passive > left {
				left = passive
			}
		}
		windows := []quota.CodexWindow{}
		var governing *quota.CodexWindow
		if codex != nil && codex.RateLimit != nil {
			for _, window := range []*quota.CodexWindow{codex.RateLimit.PrimaryWindow, codex.RateLimit.SecondaryWindow} {
				if window != nil {
					windows = append(windows, *window)
					if governing == nil || window.ResetAt > governing.ResetAt {
						governing = window
					}
				}
			}
		}
		stale := quota.IsStale(info, now)
		switch {
		case codex != nil && codex.RateLimit != nil && codex.RateLimit.LimitReached && governing != nil:
			lines = append(lines, "  📊 ❌ Rate-limited, recovers in "+quota.FormatTimeLeft(time.Duration(governing.ResetAfterSeconds*float64(time.Second))))
			if err := x.setCooldown(provider, time.UnixMilli(int64(governing.ResetAt*1000))); err != nil {
				return err
			}
		case codex != nil && codex.RateLimit != nil && codex.RateLimit.Allowed:
			lines = append(lines, "  📊 ✅ Quota available")
		case left > 0:
			lines = append(lines, "  📊 ❌ Rate-limited, recovers in "+quota.FormatTimeLeft(left))
		case stale || !present || entry.Type != "oauth" || fetchErr != nil || (provider == "anthropic" && info == nil):
			lines = append(lines, "  📊 Quota unknown")
		default:
			lines = append(lines, "  📊 ✅ Quota available")
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
		case stale:
			lines = append(lines, "  📈 Stale data, quota details fetched automatically after use")
		case info != nil:
			lines = append(lines, quota.FormatPassiveRateLimitLines(*info)...)
			captured := now
			if info.CapturedAt != 0 {
				captured = time.UnixMilli(int64(info.CapturedAt))
			}
			lines = append(lines, "  ⏰ Data age: "+quota.FormatAge(now.Sub(captured)))
		default:
			lines = append(lines, "  📈 Quota details fetched automatically after use")
		}
		lines = append(lines, "")
	}
	text := clean(strings.Join(lines, "\n"))
	if ctx.HasUI() && ctx.Mode() != "rpc" {
		ctx.SetStatus("auto-model-usage", "")
		_, err := ctx.Custom(&dashboard{title: "Usage", lines: strings.Split(text, "\n"), theme: ctx.UITheme()}, sdk.RemoteOverlayOptions{Title: "Usage"})
		return err
	}
	ctx.Notify(text, "info")
	return nil
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
	models, err := ctx.ModelRegistry().GetAvailable()
	if err != nil {
		return err
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
