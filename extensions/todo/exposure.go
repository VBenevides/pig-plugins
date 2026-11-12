package todoext

import (
	"fmt"
	"slices"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/toolsettings"
)

func preserveWithoutDiscovery(ctx sdk.Context, _ map[string]any) (any, error) {
	active, err := ctx.GetActiveTools()
	if err != nil {
		return nil, fmt.Errorf("todo: read active tools: %w", err)
	}
	if slices.Contains(active, "tool_search") || slices.Contains(active, "codemode") || slices.Contains(active, "todo") {
		return nil, nil
	}
	cfg, err := toolsettings.LoadEffective(toolsettings.Path())
	if err != nil {
		return nil, fmt.Errorf("todo: read permissions: %w", err)
	}
	if cfg.ToolEnabled("todo") {
		ctx.SetActiveTools(append(active, "todo"))
		ctx.Notify("todo remains directly available because tool discovery is inactive.", "warn")
	}
	return nil, nil
}
