package guard

import (
	"fmt"
	"strings"
	"unicode"
)

// ScopeExplanation is advisory text, never a source of authorization or grant keys.
type ScopeExplanation struct {
	Risk          string   `json:"risk"`
	AffectedItems []string `json:"affectedItems"`
}

func unknownScopeAdvice(call Call, command string) string {
	explanation, err := call.ExplainUnknown(command, call.Cwd)
	if err != nil {
		// Do not expose provider errors, which can contain credentials or request payloads.
		return "\n\nModel assessment unavailable; scope remains unknown."
	}
	if strings.TrimSpace(explanation.Risk) == "" || len(explanation.AffectedItems) == 0 {
		return "\n\nModel assessment unavailable (incomplete response); scope remains unknown."
	}
	text := "\n\nModel assessment (advisory; not verified):\nRisk: " + advisoryLine(explanation.Risk)
	text += "\nPossible affected items:"
	for i, item := range explanation.AffectedItems {
		if i == 12 {
			text += "\n- additional items omitted"
			break
		}
		text += fmt.Sprintf("\n- %s", advisoryLine(item))
	}
	return text
}

func advisoryLine(text string) string {
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)
	runes := []rune(strings.TrimSpace(text))
	if len(runes) > 500 {
		return string(runes[:500]) + "…"
	}
	return string(runes)
}
