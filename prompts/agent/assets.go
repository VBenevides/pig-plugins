// Package agentprompts contains the distribution's versioned instruction assets.
package agentprompts

import (
	"embed"
	"fmt"
	"path/filepath"
	"strings"
)

//go:embed SYSTEM.md APPEND_SYSTEM.md AGENTS.md legacy/*.md
var assets embed.FS

func Content(style, name string) (string, error) {
	if style != "default" && style != "legacy" {
		return "", fmt.Errorf("unknown prompt style %q", style)
	}
	switch name {
	case "SYSTEM.md", "APPEND_SYSTEM.md", "AGENTS.md":
	default:
		return "", fmt.Errorf("unknown prompt asset %q", name)
	}
	if style == "legacy" {
		name = "legacy/" + name
	}
	data, err := assets.ReadFile(name)
	return string(data), err
}

// Select changes only recognized shipped assets. Custom prompts and project instructions stay intact.
// Options are the per-request SDK snapshot, not persisted host settings.
func Select(options map[string]any, style, agentDir string) (bool, error) {
	if style != "default" && style != "legacy" {
		return false, fmt.Errorf("unknown prompt style %q", style)
	}
	texts := map[string]map[string]string{}
	for _, sourceStyle := range []string{"default", "legacy"} {
		texts[sourceStyle] = map[string]string{}
		for _, name := range []string{"SYSTEM.md", "APPEND_SYSTEM.md", "AGENTS.md"} {
			content, err := Content(sourceStyle, name)
			if err != nil {
				return false, err
			}
			texts[sourceStyle][name] = strings.TrimSpace(content)
		}
	}
	type replacement struct{ old, new string }
	var replacements []replacement
	replace := func(text, name string, prefix bool) string {
		target := texts[style][name]
		for _, sourceStyle := range []string{"default", "legacy"} {
			source := texts[sourceStyle][name]
			matches := strings.TrimSpace(text) == source
			if prefix {
				matches = text == source || strings.HasPrefix(text, source+"\n\n")
			}
			if !matches || source == target {
				continue
			}
			replacements = append(replacements, replacement{source, target})
			return strings.Replace(text, source, target, 1)
		}
		return text
	}
	if value, ok := options["customPrompt"].(string); ok {
		options["customPrompt"] = replace(value, "SYSTEM.md", false)
	}
	if value, ok := options["appendSystemPrompt"].(string); ok {
		options["appendSystemPrompt"] = replace(value, "APPEND_SYSTEM.md", true)
	}
	if files, ok := options["contextFiles"].([]any); ok {
		for _, raw := range files {
			file, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			path, _ := file["path"].(string)
			content, _ := file["content"].(string)
			if filepath.Clean(path) == filepath.Join(filepath.Clean(agentDir), "AGENTS.md") {
				file["content"] = replace(content, "AGENTS.md", false)
			}
		}
	}
	// Earlier handlers may have appended instructions to a forced prompt. Preserve those instructions.
	if forced, ok := options["forceSystemPrompt"].(string); ok {
		for _, pair := range replacements {
			if strings.Count(forced, pair.old) != 1 {
				return false, fmt.Errorf("cannot select prompt style: shipped instruction fragment is absent or duplicated in a forced prompt")
			}
			forced = strings.Replace(forced, pair.old, pair.new, 1)
		}
		options["forceSystemPrompt"] = forced
	}
	return len(replacements) > 0, nil
}
