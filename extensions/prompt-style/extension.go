// Package promptstyle puts the built-in instructions of the selected prompt style into each request.
// It must load before every extension that appends to the system prompt (see piglet.yaml), because those
// extensions build on the prompt that exists when they run.
package promptstyle

import (
	"fmt"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/toolsettings"
	agentprompts "github.com/VBenevides/pig-plugins/prompts/agent"
)

const Name = "prompt-style"

func Extension() *sdk.Extension {
	e := sdk.New(Name)
	e.OnEvent(sdk.EventBeforeAgentStart, selectPromptStyle)
	return e
}

func selectPromptStyle(_ sdk.Context, data map[string]any) (any, error) {
	cfg, err := toolsettings.LoadEffective(toolsettings.Path())
	if err != nil {
		return nil, err
	}
	options, ok := data["systemPromptOptions"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("host omitted systemPromptOptions for prompt-style selection")
	}
	changed, err := agentprompts.Select(options, cfg.Style())
	if err != nil {
		return nil, err
	}
	if !changed {
		return nil, nil
	}
	return map[string]any{"systemPromptOptions": options}, nil
}
