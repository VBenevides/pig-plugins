package toolsettingsext

import (
	"fmt"
	"path/filepath"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/toolsettings"
	agentprompts "github.com/VBenevides/pig-plugins/prompts/agent"
)

func selectPromptStyle(_ sdk.Context, data map[string]any) (any, error) {
	cfg, err := toolsettings.LoadEffective(toolsettings.Path())
	if err != nil {
		return nil, err
	}
	options, ok := data["systemPromptOptions"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("host omitted systemPromptOptions for prompt-style selection")
	}
	changed, err := agentprompts.Select(options, cfg.Style(), filepath.Dir(toolsettings.Path()))
	if err != nil {
		return nil, err
	}
	if !changed {
		return nil, nil
	}
	return map[string]any{"systemPromptOptions": options}, nil
}

// A launch tool profile is an overlay. Saving a prompt style must not save its temporary permissions.
func settingsToSave(menu *toolsettings.Menu, previous, effective toolsettings.Config) toolsettings.Config {
	if menu.ProfileOverride != "" {
		effective = previous
	}
	menu.ApplyTo(&effective)
	return effective
}
