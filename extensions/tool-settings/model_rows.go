package toolsettingsext

import (
	"encoding/json"
	"fmt"
	"slices"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/toolsettings"
)

func modelToolRow(tool sdk.ToolInfo, cfg toolsettings.Config, active, hidden []string, registered map[string]bool) (toolsettings.Row, bool, error) {
	if slices.Contains(hidden, tool.Name) && !cfg.DisabledTools[tool.Name] {
		return toolsettings.Row{}, false, nil
	}
	exposure := string(tool.Exposure)
	if exposure == "" || exposure == string(sdk.ToolExposureModelOnly) {
		exposure = "direct"
	}
	if tool.Name == "goal_finish" {
		exposure = "conditional"
	}
	if owner := toolsettings.BackendOwner(tool.Name); owner != "" && registered[owner] {
		if !cfg.DisabledTools[tool.Name] {
			return toolsettings.Row{}, false, nil
		}
		exposure = "backend"
	}
	// This is a schema estimate, not the provider's serialized request size.
	raw, err := json.Marshal(map[string]any{"name": tool.Name, "description": tool.Description, "parameters": tool.Parameters})
	if err != nil {
		return toolsettings.Row{}, false, fmt.Errorf("count tool %s: %w", tool.Name, err)
	}
	source := tool.SourceInfo.Path
	if source == "" {
		source = tool.SourceInfo.Origin
	}
	if source == "" {
		source = tool.SourceInfo.Source
	}
	if source == "" {
		source = "builtin"
	}
	return toolsettings.Row{Name: tool.Name, ID: tool.Name, Source: source, Context: string(raw), Enabled: cfg.ToolEnabled(tool.Name), Visible: slices.Contains(active, tool.Name) && !slices.Contains(hidden, tool.Name), DefaultEnabled: cfg.Defaults[tool.Name], Group: toolsettings.GroupFor(tool.Name, source), Exposure: exposure}, true, nil
}
