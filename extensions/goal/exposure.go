package goalext

import (
	"fmt"
	"slices"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/goal"
	"github.com/VBenevides/pig-plugins/internal/toolsettings"
)

// syncFinishExposure updates only this extension's tool. Permission remains independent of goal state.
func (x *extension) syncFinishExposure(ctx sdk.Context) error {
	cfg, err := toolsettings.LoadEffective(toolsettings.Path())
	if err != nil {
		return fmt.Errorf("goal: read tool permissions: %w", err)
	}
	x.mu.Lock()
	want := x.state != nil && x.state.Status == goal.Working && cfg.ToolEnabled("goal_finish")
	x.mu.Unlock()
	active, err := ctx.GetActiveTools()
	if err != nil {
		return fmt.Errorf("goal: read active tools: %w", err)
	}
	// A working goal permits discovery, not automatic declaration.
	if want || !slices.Contains(active, "goal_finish") {
		return nil
	}
	active = slices.DeleteFunc(active, func(name string) bool { return name == "goal_finish" })
	ctx.SetActiveTools(active)
	return nil
}
