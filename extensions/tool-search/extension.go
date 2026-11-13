// Package toolsearch enables built-in tool discovery by default.
package toolsearch

import (
	"fmt"
	"slices"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/toolsettings"
)

const Name = "tool-search"
const toolName = "tool_search"

func Extension() *sdk.Extension {
	e := sdk.New(Name)
	state := &discoveries{}
	e.OnEvent(sdk.EventToolCall, boundSearch)
	e.OnEvent(sdk.EventToolResult, state.result)
	e.OnEvent(sdk.EventAgentSettled, state.settle)
	e.OnSessionStart(func(ctx sdk.Context, _ map[string]any) (any, error) {
		tools, err := ctx.GetAllTools()
		if err != nil {
			return nil, err
		}
		cfg, err := toolsettings.LoadEffective(toolsettings.Path())
		if err != nil {
			return nil, err
		}
		for _, tool := range tools {
			if tool.Name == toolName {
				return nil, setEnabled(ctx, cfg.ToolEnabled(toolName))
			}
		}
		ctx.Notify("Tool discovery is unavailable. Only the four core tools remain exposed.", "warning")
		return nil, nil
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
