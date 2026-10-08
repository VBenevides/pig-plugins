// Package projectprompt appends bundled project rules without replacing host guidance.
package projectprompt

import (
	"fmt"
	"strings"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"

	"github.com/VBenevides/pig-plugins/prompts"
)

// Extension adds no tools and preserves the base prompt on each agent start.
func Extension() *sdk.Extension {
	e := sdk.New("project-prompt")
	e.OnEvent(sdk.EventBeforeAgentStart, appendRules)
	return e
}

func appendRules(_ sdk.Context, data map[string]any) (any, error) {
	base, ok := data["systemPrompt"].(string)
	if !ok {
		return nil, fmt.Errorf("project-prompt: before_agent_start has no string systemPrompt")
	}
	if strings.Contains(base, prompts.ProjectSystem) {
		return nil, nil
	}
	return map[string]any{"systemPrompt": base + "\n\n" + prompts.ProjectSystem}, nil
}
