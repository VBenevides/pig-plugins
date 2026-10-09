package todo

import (
	"regexp"
	"strings"
)

// Remove terminal escape sequences before rendering untrusted task text.
var ansiSequence = regexp.MustCompile(`(?:\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b\[[0-?]*[ -/]*[@-~]|\x1b[@-_]|\x9b[0-?]*[ -/]*[@-~])`)

func SanitizeText(text string) string {
	text = ansiSequence.ReplaceAllString(text, "")
	text = strings.Map(func(r rune) rune {
		if r < 32 || r >= 127 && r <= 159 {
			return ' '
		}
		return r
	}, text)
	return strings.Join(strings.Fields(text), " ")
}
func Marker(status string) string {
	switch status {
	case "completed":
		return "[✓]"
	case "in_progress":
		return "[•]"
	case "abandoned", "cancelled":
		return "[×]"
	default:
		return "[ ]"
	}
}

// ActivePhase prefers in-progress work over earlier pending work, as upstream.
func ActivePhase(phases []Phase) int {
	pending := -1
	for i, p := range phases {
		for _, task := range p.Tasks {
			if task.Status == "in_progress" {
				return i
			}
			if pending < 0 && task.Status == "pending" {
				pending = i
			}
		}
	}
	return pending
}
func WidgetLines(phases []Phase) []string {
	i := ActivePhase(phases)
	if i < 0 {
		return nil
	}
	lines := []string{"Todo", SanitizeText(phases[i].Name)}
	for _, task := range phases[i].Tasks {
		lines = append(lines, Marker(task.Status)+" "+SanitizeText(task.Content))
	}
	return lines
}
