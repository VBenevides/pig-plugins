// Package echo is a test-only extension: the harness loads it to prove that a Go extension builds, registers,
// and runs through the real pig binary.
package echo

import (
	"fmt"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

func Extension() *sdk.Extension {
	e := sdk.New("echo")
	e.Tool("echo", "Return the given text prefixed with 'echo: '.",
		sdk.Schema{"type": "object", "properties": map[string]any{"text": map[string]any{"type": "string"}},
			"required": []string{"text"}},
		func(ctx sdk.Context, params map[string]any) (any, error) {
			text, ok := params["text"].(string)
			if !ok {
				return nil, fmt.Errorf("echo: text must be a string")
			}
			return "echo: " + text, nil
		})
	return e
}
