package betterfooter

import (
	"fmt"
	"os"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	bf "github.com/VBenevides/pig-plugins/internal/betterfooter"
)

func (x *extension) remember(ctx sdk.Context) error {
	x.mu.Lock()
	enabled := x.fused && x.settings.KeepRecentModel && !x.disabled && !x.restoring
	updatedAt := x.lastKnownAt
	x.mu.Unlock()
	if !enabled || !ctx.HasUI() {
		return nil
	}
	// Serialize snapshots with restoration and shutdown, without holding the event-state lock across host calls.
	x.saveMu.Lock()
	defer x.saveMu.Unlock()
	level, err := ctx.GetThinkingLevel()
	if err != nil {
		return err
	}
	if ctx.ModelProvider() == "" || ctx.Model() == "" {
		return nil
	}
	_, err = bf.SaveRecentState(bf.RecentStatePath(os.Getenv, ctx.Cwd()), bf.ModelRef{Provider: ctx.ModelProvider(), ModelID: ctx.Model()}, level, updatedAt)
	return err
}
func (x *extension) restore(ctx sdk.Context, reason string) {
	x.mu.Lock()
	disabled := !x.fused || !x.settings.KeepRecentModel || x.disabled
	x.mu.Unlock()
	if disabled {
		return
	}
	branch, err := ctx.SessionManager().GetBranch(nil)
	if err != nil {
		x.report(ctx, err)
		return
	}
	conversation := false
	for _, entry := range branch {
		if text(entry, "type") == "message" {
			conversation = true
			break
		}
	}
	if !bf.ShouldRestore(reason, x.args, conversation, disabled) {
		return
	}
	saved, found, err := bf.LoadRecentState(bf.RecentStatePath(os.Getenv, ctx.Cwd()))
	if err != nil {
		x.report(ctx, err)
		return
	}
	if !found {
		return
	}
	_, explicit := bf.StartupOption(x.args, "--models")
	explicit = explicit && reason == "startup"
	scopedLevel := ""
	if explicit {
		scope, err := ctx.ScopedModels()
		if err != nil {
			x.report(ctx, err)
			return
		}
		matched := false
		for _, item := range scope {
			if text(item.Model, "provider") == saved.Provider && text(item.Model, "id") == saved.ModelID {
				matched = true
				scopedLevel = item.ThinkingLevel
				break
			}
		}
		if !matched {
			return
		}
	}
	level := bf.RestoreThinkingLevel(reason, x.args, explicit, scopedLevel, saved.ThinkingLevel)
	x.mu.Lock()
	x.restoring = true
	x.mu.Unlock()
	defer func() { x.mu.Lock(); x.restoring = false; x.mu.Unlock() }()
	if ctx.ModelProvider() != saved.Provider || ctx.Model() != saved.ModelID {
		if ctx.ModelRegistry().Find(saved.Provider, saved.ModelID) == nil {
			x.report(ctx, fmt.Errorf("could not restore recent model %s/%s: model not found", saved.Provider, saved.ModelID))
			return
		}
		ok, err := ctx.SetModel(saved.Provider + "/" + saved.ModelID)
		if err != nil {
			x.report(ctx, fmt.Errorf("could not restore recent model: %w", err))
			return
		}
		if !ok {
			x.report(ctx, fmt.Errorf("could not restore recent model %s/%s: unavailable credentials", saved.Provider, saved.ModelID))
			return
		}
	}
	if level != "" {
		ctx.SetThinkingLevel(level)
	}
	actual, err := ctx.GetThinkingLevel()
	if err != nil {
		x.report(ctx, err)
		return
	}
	x.mu.Lock()
	updatedAt := x.lastKnownAt
	x.mu.Unlock()
	x.saveMu.Lock()
	_, err = bf.SaveRecentState(bf.RecentStatePath(os.Getenv, ctx.Cwd()), saved.Ref(), actual, updatedAt)
	x.saveMu.Unlock()
	x.report(ctx, err)
}
