package toolsettings

import (
	"regexp"
	"strings"
)

// The host expands skills into this exact block shape before model requests.
var skillBlock = regexp.MustCompile(`(?s)^<skill name="[^"]+" location="([^"]+)">\n(.*?)\n</skill>(?:\n\n.*)?$`)

// LoadedSkills counts the full expanded blocks in the most recent context event. It does not read skill
// files or save their contents. Matching only complete host-shaped blocks avoids counting ordinary mentions.
func LoadedSkills(messages []any) map[string]int {
	counts := map[string]int{}
	add := func(text string) {
		match := skillBlock.FindStringSubmatch(text)
		if match == nil {
			return
		}
		end := strings.Index(text, "\n</skill>") + len("\n</skill>")
		n, _ := Characters(text[:end])
		counts[Canonical(match[1])] += n
	}
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if text, ok := message["content"].(string); ok {
			add(text)
			continue
		}
		blocks, _ := message["content"].([]any)
		for _, raw := range blocks {
			if block, ok := raw.(map[string]any); ok {
				text, _ := block["text"].(string)
				add(text)
			}
		}
	}
	return counts
}
