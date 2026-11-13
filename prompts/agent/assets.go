// Package agentprompts contains the default instructions built into the binary.
package agentprompts

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed default/SYSTEM.md
var System string

// Apply fills an absent custom prompt. User and project overrides stay intact.
// Call this before handlers that append instructions to a rendered prompt.
func Apply(options map[string]any) (bool, error) {
	if custom, _ := options["customPrompt"].(string); strings.TrimSpace(custom) != "" {
		return false, nil
	}
	if _, forced := options["forceSystemPrompt"]; forced {
		return false, fmt.Errorf("cannot initialize default instructions after a forced system prompt")
	}
	options["customPrompt"] = strings.TrimSpace(System)
	return true, nil
}
