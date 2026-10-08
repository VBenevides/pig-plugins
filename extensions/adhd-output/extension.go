// Package adhdoutput provides a native, session-aware presentation toggle.
package adhdoutput

import (
	_ "embed"
	"errors"
	"fmt"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	mode "github.com/VBenevides/pig-plugins/internal/adhdoutput"
	"github.com/VBenevides/pig-plugins/internal/footerstatus"
)

//go:embed rules.md
var defaultRules string

// Extension adds no tools, model turns, footer replacement, or prompt replacement.
func Extension() *sdk.Extension {
	ext := sdk.New("adhd-output")
	controller := mode.New(func() (mode.Config, error) { return mode.LoadConfig(mode.ConfigPath(), defaultRules) })
	controller.PersistDefault(func(enabled bool) error { return mode.SaveDefaultEnabled(mode.ConfigPath(), enabled) })
	ext.Flag("adhd", sdk.FlagOptions{Description: "Start new sessions with ADHD-friendly output", Type: sdk.FlagBoolean, Default: false})
	ext.Command("adhd", "Toggle ADHD output: /adhd [on|off|status]", func(ctx sdk.Context, args string) error {
		return controller.Command(host{ctx}, args)
	})
	restore := func(ctx sdk.Context, _ map[string]any) (any, error) { return nil, controller.Restore(host{ctx}) }
	ext.OnSessionStart(restore)
	ext.OnEvent(sdk.EventSessionTree, restore)
	ext.OnEvent(sdk.EventSessionCompact, func(ctx sdk.Context, _ map[string]any) (any, error) {
		return nil, controller.Sync(host{ctx})
	})
	// Explicit triggerTurn=false messages can wait for the active turn's tool results.
	// Only a pending message needs these callbacks. Normal turns do no plugin work.
	settle := func(ctx sdk.Context, _ map[string]any) (any, error) {
		if !controller.NeedsSync() {
			return nil, nil
		}
		return nil, controller.Sync(host{ctx})
	}
	ext.OnEvent(sdk.EventTurnEnd, settle)
	ext.OnEvent(sdk.EventAgentSettled, settle)
	ext.OnEvent(sdk.EventSessionShutdown, func(ctx sdk.Context, _ map[string]any) (any, error) {
		return nil, controller.Shutdown(host{ctx})
	})
	return ext
}

type host struct{ ctx sdk.Context }

func (h host) Identity() (string, string, error) {
	manager := h.ctx.SessionManager()
	id, err := manager.GetSessionID()
	if err != nil {
		return "", "", err
	}
	leaf, err := manager.GetLeafID()
	if err != nil {
		return "", "", err
	}
	if leaf == nil {
		return id, "", nil
	}
	return id, *leaf, nil
}

func (h host) Snapshot() (mode.Snapshot, error) {
	// The SDK has no atomic combined read. Retry once if a host append overlaps a read.
	for range 2 {
		id, leaf, err := h.Identity()
		if err != nil {
			return mode.Snapshot{}, err
		}
		var from *string
		if leaf != "" {
			from = &leaf
		}
		branch, err := h.ctx.SessionManager().GetBranch(from)
		if err != nil {
			return mode.Snapshot{}, fmt.Errorf("raw active branch: %w", err)
		}
		context, err := h.ctx.SessionManager().BuildSessionContext()
		if err != nil {
			return mode.Snapshot{}, fmt.Errorf("resolved model context: %w", err)
		}
		idle, err := h.ctx.IsIdle()
		if err != nil {
			return mode.Snapshot{}, err
		}
		afterID, afterLeaf, err := h.Identity()
		if err != nil {
			return mode.Snapshot{}, err
		}
		if id == afterID && leaf == afterLeaf {
			return mode.Snapshot{SessionID: id, LeafID: leaf, Branch: branch, Messages: context.Messages, Idle: idle}, nil
		}
	}
	return mode.Snapshot{}, errors.New("active session changed during both context reads")
}

func (h host) DefaultFlag() (bool, error) {
	flag, err := h.ctx.GetFlag("adhd")
	if err != nil {
		return false, err
	}
	return parseDefaultFlag(flag)
}

// PiG's core CLI parser passes explicit flag values as strings, while a bare
// boolean flag and the SDK's registered default are bools.
func parseDefaultFlag(flag any) (bool, error) {
	switch value := flag.(type) {
	case nil:
		return false, nil
	case bool:
		return value, nil
	case string:
		switch value {
		case "true":
			return true, nil
		case "false":
			return false, nil
		default:
			return false, errors.New("--adhd requires true or false")
		}
	default:
		return false, fmt.Errorf("--adhd has non-boolean value %T", flag)
	}
}
func (h host) AppendEntry(customType string, state mode.State) error {
	return h.ctx.AppendEntry(customType, state)
}
func (h host) SendMessage(customType, content string) error {
	return h.ctx.SendMessage(customType, content, false, sdk.SendMessageOptions{TriggerTurn: new(false)})
}
func (h host) SetStatus(key, text string)   { footerstatus.Set(h.ctx, key, text) }
func (h host) Notify(message, level string) { h.ctx.Notify(message, level) }
