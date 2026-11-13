// Package defaultprompt initializes the built-in instructions before other prompt handlers.
package defaultprompt

import (
	"fmt"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	agentprompts "github.com/VBenevides/pig-plugins/prompts/agent"
)

const Name = "default-prompt"

func Extension() *sdk.Extension {
	e := sdk.New(Name)
	e.OnEvent(sdk.EventBeforeAgentStart, initializePrompt)
	return e
}

func initializePrompt(_ sdk.Context, data map[string]any) (any, error) {
	options, ok := data["systemPromptOptions"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("host omitted systemPromptOptions for default prompt initialization")
	}
	changed, err := agentprompts.Apply(options)
	if err != nil {
		return nil, err
	}
	if !changed {
		return nil, nil
	}
	return map[string]any{"systemPromptOptions": options}, nil
}
