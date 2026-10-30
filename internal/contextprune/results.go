package contextprune

import (
	"fmt"
)

const (
	DefaultResultBudgetBytes  = 65536
	DefaultResultExcerptBytes = 128
	maxResultExcerptBytes     = 4096
	minimumResultBudgetBytes  = 4096
	maximumResultBudgetBytes  = 16 << 20
)

type historicalResult struct {
	id, name, text string
}

// decideHistoricalResults bounds successful results that no tool-specific rule reduced.
// It keeps the newest results whole and replaces older verified results with a short
// marker. The rule uses no tool names, so unknown MCP and web tools are covered.
func decideHistoricalResults(messages []any, calls map[string]toolCall, protectedFrom int, settings Settings, decisions Decisions) {
	if settings.HistoricalResultBytes <= 0 || settings.ResultExcerptBytes < 0 {
		return
	}
	retained := 0
	candidates := []historicalResult{}
	for index := protectedFrom - 1; index >= 0; index-- {
		message, _ := messages[index].(map[string]any)
		if message["role"] != "toolResult" || message["isError"] == true {
			continue
		}
		id, _ := message["toolCallId"].(string)
		call, known := calls[id]
		text, plain := resultPlainBody(message)
		if !known || !plain {
			continue
		}
		if decision, decided := decisions[id]; decided {
			retained += len(decision.Text)
			continue
		}
		hash, _ := ResultSourceHash(message)
		if settings.RecoveryHandles[id] == "" || settings.RecoverySources[id] != hash {
			retained += len(text)
			continue
		}
		candidates = append(candidates, historicalResult{id: id, name: call.name, text: text})
	}
	// Candidates are newest first. Walking in that order keeps the newest results whole.
	for _, result := range candidates {
		if retained+len(result.text) <= settings.HistoricalResultBytes {
			retained += len(result.text)
			continue
		}
		replacement := resultMarker(result, settings.RecoveryHandles[result.id], settings.ResultExcerptBytes)
		if len(replacement) >= len(result.text) {
			retained += len(result.text)
			continue
		}
		decisions[result.id] = Decision{
			Text: replacement, Saved: len(result.text) - len(replacement),
			SourceHash: resultBodyHash(result.text, false), BudgetRequired: true,
		}
		retained += len(replacement)
	}
}

func resultMarker(result historicalResult, handle string, excerptBytes int) string {
	name := result.name
	if len(name) > 64 {
		name = name[:validCut(name, 64)]
	}
	marker := fmt.Sprintf("[pruned: old %q result, %d bytes; status unchanged; recovery: %s; use context_recover]", name, len(result.text), handle)
	if excerptBytes == 0 {
		return marker
	}
	return marker + "\n" + result.text[:validCut(result.text, min(excerptBytes, len(result.text)-1))] + "\n[... omitted ...]"
}
