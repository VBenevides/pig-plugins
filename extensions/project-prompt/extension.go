// Package projectprompt appends trusted project instructions without replacing host guidance.
package projectprompt

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	sdk "github.com/MichaelKinsy/PiG/extensions/sdk"
)

// Extension adds no tools and preserves the base prompt on each agent start.
func Extension() *sdk.Extension {
	e := sdk.New("project-prompt")
	e.OnEvent(sdk.EventBeforeAgentStart, appendRules)
	return e
}

func appendRules(ctx sdk.Context, data map[string]any) (any, error) {
	base, ok := data["systemPrompt"].(string)
	if !ok {
		return nil, fmt.Errorf("project-prompt: before_agent_start has no string systemPrompt")
	}
	trusted, err := ctx.IsProjectTrusted()
	if err != nil {
		return nil, fmt.Errorf("project-prompt: determine project trust: %w", err)
	}
	return appendPrompt(base, ctx.Cwd(), trusted)
}

const maxLocalInstructionBytes = 256 << 10

// appendPrompt reads the literal defaults on every request. It does not cache
// instructions or substitute local/APPEND_SYSTEM.md for .local/APPEND_SYSTEM.md.
func appendPrompt(base, cwd string, trusted bool) (any, error) {
	if !trusted {
		return nil, nil
	}
	root, err := os.OpenRoot(cwd)
	if err != nil {
		return nil, fmt.Errorf("project-prompt: open workspace: %w", err)
	}
	defer root.Close()
	var additions strings.Builder
	for _, path := range []string{".local/APPEND_SYSTEM.md", "local/AGENTS.md"} {
		content, err := readLocalInstructions(root, path)
		if err != nil {
			return nil, fmt.Errorf("project-prompt: read %s: %w", path, err)
		}
		if strings.TrimSpace(content) == "" || strings.Contains(base, content) || strings.Contains(additions.String(), content) {
			continue
		}
		additions.WriteString("\n\n# Trusted project instructions: ")
		additions.WriteString(path)
		additions.WriteString("\n\n")
		additions.WriteString(content)
	}
	if additions.Len() == 0 {
		return nil, nil
	}
	return map[string]any{"systemPrompt": base + additions.String()}, nil
}

func readLocalInstructions(root *os.Root, path string) (string, error) {
	info, err := root.Stat(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("instruction source is not a regular file")
	}
	if info.Size() > maxLocalInstructionBytes {
		return "", fmt.Errorf("instruction source exceeds %d bytes", maxLocalInstructionBytes)
	}
	file, err := root.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err = file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("instruction source is not a regular file")
	}
	content, err := io.ReadAll(io.LimitReader(file, maxLocalInstructionBytes+1))
	if err != nil {
		return "", err
	}
	if len(content) > maxLocalInstructionBytes {
		return "", fmt.Errorf("instruction source exceeds %d bytes", maxLocalInstructionBytes)
	}
	if !utf8.Valid(content) {
		return "", fmt.Errorf("instruction source is not valid UTF-8")
	}
	return string(content), nil
}
