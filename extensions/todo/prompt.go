package todoext

import (
	_ "embed"
	"fmt"
	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"strings"
)

//go:embed guidance.md
var taskGuidance string

func appendGuidance(_ sdk.Context, data map[string]any) (any, error) {
	base, ok := data["systemPrompt"].(string)
	if !ok {
		return nil, fmt.Errorf("todo: before_agent_start has no string systemPrompt")
	}
	if strings.Contains(base, "<Task_Management>") {
		return nil, nil
	}
	return map[string]any{"systemPrompt": base + "\n\n" + taskGuidance}, nil
}
