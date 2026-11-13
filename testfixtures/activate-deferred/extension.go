// This fixture explicitly activates deferred tools for tests of tool execution.
// Discovery behavior is tested without this fixture.
package activatedeferred

import (
	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/toolsettings"
	"slices"
	"strings"
)

func Extension() *sdk.Extension {
	e := sdk.New("activate-deferred")
	e.OnEvent(sdk.EventBeforeAgentStart, func(ctx sdk.Context, _ map[string]any) (any, error) {
		cfg, err := toolsettings.LoadEffective(toolsettings.Path())
		if err != nil {
			return nil, err
		}
		tools, err := ctx.GetAllTools()
		if err != nil {
			return nil, err
		}
		active, err := ctx.GetActiveTools()
		if err != nil {
			return nil, err
		}
		for _, tool := range tools {
			// Execution tests explicitly opt into browser tools. Explicit denials and allowlists still apply.
			if strings.HasPrefix(tool.Name, "browser_") {
				cfg.EnabledTools[tool.Name] = true
			}
			if tool.Exposure == sdk.ToolExposureDeferred && cfg.ToolEnabled(tool.Name) && !slices.Contains(active, tool.Name) {
				active = append(active, tool.Name)
			}
		}
		ctx.SetActiveTools(active)
		return nil, nil
	})
	return e
}
