// Package toolsearch enables built-in tool discovery by default.
package toolsearch

import (
	"fmt"
	"slices"
	"strings"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

const Name = "tool-search"
const toolName = "tool_search"

func Extension() *sdk.Extension {
	e := sdk.New(Name)
	e.OnSessionStart(func(ctx sdk.Context, _ map[string]any) (any, error) {
		return nil, setEnabled(ctx, true)
	})
	e.RegisterCommand(Name, sdk.CommandOptions{
		Description: "Enable or disable tool discovery (/tool-search on|off; no argument shows status)",
		GetArgumentCompletions: func(prefix string) ([]sdk.AutocompleteItem, error) {
			var items []sdk.AutocompleteItem
			for _, value := range []string{"on", "off"} {
				if strings.HasPrefix(value, prefix) {
					items = append(items, sdk.AutocompleteItem{Value: value, Label: value})
				}
			}
			return items, nil
		},
		Handler: func(ctx sdk.Context, args string) error {
			switch strings.ToLower(strings.TrimSpace(args)) {
			case "on":
				if err := setEnabled(ctx, true); err != nil {
					return err
				}
			case "off":
				if err := setEnabled(ctx, false); err != nil {
					return err
				}
			case "":
			default:
				ctx.Notify("usage: /tool-search [on|off]", "warning")
				return nil
			}
			active, err := ctx.GetActiveTools()
			if err != nil {
				return fmt.Errorf("tool-search: read active tools: %w", err)
			}
			state := "off"
			if slices.Contains(active, toolName) {
				state = "on"
			}
			ctx.Notify("Tool search: "+state, "info")
			return nil
		},
	})
	return e
}

func setEnabled(ctx sdk.Context, enabled bool) error {
	active, err := ctx.GetActiveTools()
	if err != nil {
		return fmt.Errorf("tool-search: read active tools: %w", err)
	}
	if slices.Contains(active, toolName) == enabled {
		return nil
	}
	if enabled {
		available, err := ctx.GetAllTools()
		if err != nil {
			return fmt.Errorf("tool-search: read available tools: %w", err)
		}
		if !slices.ContainsFunc(available, func(tool sdk.ToolInfo) bool { return tool.Name == toolName }) {
			return fmt.Errorf("tool-search: tool_search is unavailable; enable the built-in tool-search extension in /config (remove -builtin:tool-search from settings), then /reload")
		}
	}
	if enabled {
		active = append(active, toolName)
	} else {
		active = slices.DeleteFunc(active, func(name string) bool { return name == toolName })
	}
	ctx.SetActiveTools(active)
	updated, err := ctx.GetActiveTools()
	if err != nil {
		return fmt.Errorf("tool-search: verify active tools: %w", err)
	}
	if slices.Contains(updated, toolName) != enabled {
		return fmt.Errorf("tool-search: could not set discovery enabled=%t", enabled)
	}
	return nil
}
