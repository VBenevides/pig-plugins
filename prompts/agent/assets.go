// Package agentprompts contains the instructions built into the binary, one file for each prompt style.
// A SYSTEM.md in the agent directory or the project replaces the built-in text.
package agentprompts

import (
	"embed"
	"fmt"
	"strings"
)

//go:embed default/SYSTEM.md legacy/SYSTEM.md
var assets embed.FS

// Styles lists the built-in prompt styles.
var Styles = []string{"default", "legacy"}

// Content returns the built-in SYSTEM.md of a style.
func Content(style string) (string, error) {
	if style != "default" && style != "legacy" {
		return "", fmt.Errorf("unknown prompt style %q", style)
	}
	data, err := assets.ReadFile(style + "/SYSTEM.md")
	return string(data), err
}

// Select puts the built-in prompt of style into the per-request SDK snapshot of the options.
// The host fills customPrompt only when the user or the project supplies a SYSTEM.md (or --system-prompt),
// so any other non-empty custom prompt is user-owned and stays intact. Project instructions stay intact too.
// It reports whether the options changed.
func Select(options map[string]any, style string) (bool, error) {
	target, err := Content(style)
	if err != nil {
		return false, err
	}
	target = strings.TrimSpace(target)
	current, _ := options["customPrompt"].(string)
	trimmed := strings.TrimSpace(current)
	if trimmed == target {
		return false, nil
	}
	if trimmed != "" {
		replaced, err := shippedStyle(trimmed)
		if err != nil || !replaced {
			return false, err
		}
	}
	if forced, ok := options["forceSystemPrompt"].(string); ok {
		// An earlier handler already built a prompt on the previous base. Only an exact shipped fragment is safe to swap.
		if trimmed == "" || strings.Count(forced, trimmed) != 1 {
			return false, fmt.Errorf("cannot select prompt style: the built-in instructions are absent or duplicated in a forced prompt")
		}
		options["forceSystemPrompt"] = strings.Replace(forced, trimmed, target, 1)
	}
	options["customPrompt"] = target
	return true, nil
}

// shippedStyle reports whether text is exactly one of the built-in prompts.
func shippedStyle(text string) (bool, error) {
	for _, style := range Styles {
		content, err := Content(style)
		if err != nil {
			return false, err
		}
		if strings.TrimSpace(content) == text {
			return true, nil
		}
	}
	return false, nil
}
