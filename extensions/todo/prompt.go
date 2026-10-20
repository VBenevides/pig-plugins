package todoext

import (
	_ "embed"
	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
	"github.com/VBenevides/pig-plugins/internal/eventprompt"
	"strings"
)

//go:embed guidance.md
var taskGuidance string

func appendGuidance(_ sdk.Context, data map[string]any) (any, error) {
	base, err := eventprompt.Read("todo", data)
	if err != nil {
		return nil, err
	}
	if strings.Contains(base, "<Task_Management>") {
		return nil, nil
	}
	return map[string]any{"systemPrompt": base + "\n\n" + taskGuidance}, nil
}
