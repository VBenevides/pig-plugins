// Package askmodeext provides /mode [act|ask|status] to switch session permissions.
// The state lives in memory only and resets whenever a session starts.
package askmodeext

import (
	"fmt"
	"strings"
	"sync/atomic"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"

	"github.com/VBenevides/pig-plugins/internal/askmode"
	"github.com/VBenevides/pig-plugins/internal/footerstatus"
)

// Name is the extension identity and the footer status key.
const Name = "ask-mode"

// Extension returns the extension.
func Extension() *sdk.Extension {
	e := sdk.New(Name)
	var on atomic.Bool
	announce := func(ctx sdk.Context) {
		text := ""
		if on.Load() {
			text = "ASK"
		}
		footerstatus.Set(ctx, Name, text)
	}

	e.OnSessionStart(func(ctx sdk.Context, _ map[string]any) (any, error) {
		on.Store(false)
		announce(ctx)
		return nil, nil
	})

	e.RegisterCommand("mode", sdk.CommandOptions{
		Description: "Switch ACT/ASK mode (/mode, /mode ask, /mode act, /mode status)",
		GetArgumentCompletions: func(prefix string) ([]sdk.AutocompleteItem, error) {
			var items []sdk.AutocompleteItem
			for _, v := range []string{"ask", "act", "status"} {
				if strings.HasPrefix(v, prefix) {
					items = append(items, sdk.AutocompleteItem{Value: v, Label: v})
				}
			}
			return items, nil
		},
		Handler: func(ctx sdk.Context, args string) error {
			switch strings.ToLower(strings.TrimSpace(args)) {
			case "":
				on.Store(!on.Load())
			case "ask":
				on.Store(true)
			case "act":
				on.Store(false)
			case "status":
			default:
				ctx.Notify("usage: /mode [act|ask|status]", "warning")
				return nil
			}
			announce(ctx)
			state := "ACT"
			if on.Load() {
				state = "ASK (read-only)"
			}
			ctx.Notify("Mode: "+state, "info")
			return nil
		},
	})

	e.OnEvent(sdk.EventBeforeAgentStart, func(_ sdk.Context, data map[string]any) (any, error) {
		if !on.Load() {
			return nil, nil
		}
		base, ok := data["systemPrompt"].(string)
		if !ok {
			return nil, fmt.Errorf("%s: before_agent_start has no string systemPrompt", Name)
		}
		return map[string]any{"systemPrompt": base + askmode.Prompt}, nil
	})

	e.OnEvent(sdk.EventToolCall, func(ctx sdk.Context, data map[string]any) (any, error) {
		if !on.Load() {
			return nil, nil
		}
		tool, _ := data["toolName"].(string)
		input, _ := data["input"].(map[string]any)
		if reason := check(ctx, tool, input); reason != "" {
			return map[string]any{"block": true, "reason": reason}, nil
		}
		return nil, nil
	})
	return e
}

// check returns a block reason, or "" to let the call continue.
func check(ctx sdk.Context, tool string, input map[string]any) string {
	const prefix = "Ask mode is on (read-only): "
	switch {
	case askmode.ReadOnlyTool(tool):
		return ""
	case tool == "bash":
		command, _ := input["command"].(string)
		if ok, why := askmode.ReadOnlyBash(command); !ok {
			return prefix + "bash blocked: " + why + ". Ask the user to run /mode act."
		}
		return ""
	case askmode.WriteTool(tool):
		path, _ := input["path"].(string)
		inside, err := askmode.InAgentWork(ctx.Cwd(), path)
		if err != nil {
			return prefix + tool + " blocked: " + err.Error()
		}
		if !inside {
			return prefix + tool + " outside .agent-work/ is blocked. Ask the user to run /mode act."
		}
		if !ctx.HasUI() {
			return prefix + tool + " blocked: no UI to confirm the .agent-work write."
		}
		ok, err := ctx.Confirm("Ask mode", fmt.Sprintf("Allow %s to %s? (inside .agent-work/; only approve if you asked for it)", tool, path))
		if err != nil {
			return prefix + tool + " blocked: confirmation failed: " + err.Error()
		}
		if !ok {
			return prefix + "the user declined the " + tool + " on " + path
		}
		return ""
	}
	return prefix + "tool " + tool + " is not known to be read-only. Ask the user to run /mode act."
}
