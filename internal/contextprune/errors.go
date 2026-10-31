package contextprune

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"unicode/utf8"
)

const (
	DefaultErrorBudgetBytes  = 32768
	DefaultErrorExcerptBytes = 3072
	minimumFailureBytes      = 512
)

// ResultSourceHash binds visible text and failure status. It is not edit authority.
func ResultSourceHash(message map[string]any) (string, bool) {
	text, ok := resultPlainBody(message)
	if !ok {
		return "", false
	}
	failed, _ := message["isError"].(bool)
	return resultBodyHash(text, failed), true
}

func resultBodyHash(text string, failed bool) string {
	prefix := "success\x00"
	if failed {
		prefix = "failure\x00"
	}
	hash := sha256.Sum256([]byte(prefix + text))
	return hex.EncodeToString(hash[:])
}

func (decision Decision) MatchesSource(message map[string]any) bool {
	failed, _ := message["isError"].(bool)
	if failed != decision.Failure {
		return false
	}
	text, ok := resultPlainBody(message)
	if !ok {
		return false
	}
	return decision.SourceHash == "" || decision.SourceHash == resultBodyHash(text, failed)
}

// DiscardChangedSources removes stale hashed decisions before counts or savings are reported.
func (decisions Decisions) DiscardChangedSources(messages []any) {
	pending := map[string]bool{}
	for id, decision := range decisions {
		if decision.SourceHash != "" && decision.Arguments == nil {
			pending[id] = true
		}
	}
	for _, value := range messages {
		message, _ := value.(map[string]any)
		if message["role"] != "toolResult" {
			continue
		}
		id, _ := message["toolCallId"].(string)
		if pending[id] && decisions[id].MatchesSource(message) {
			delete(pending, id)
		}
	}
	for id := range pending {
		delete(decisions, id)
	}
	decisions.discardChangedArguments(messages)
}

func (decisions Decisions) BudgetPressure() bool {
	for _, decision := range decisions {
		if decision.BudgetRequired {
			return true
		}
	}
	return false
}

// ViewSources proves that an already reduced view came from our own verified
// decision, not from an omission marker supplied by untrusted tool text.
func (decisions Decisions) ViewSources(messages []any, originals map[string]string) map[string]string {
	out := make(map[string]string, len(originals))
	for id, hash := range originals {
		out[id] = hash
	}
	for _, value := range messages {
		message, _ := value.(map[string]any)
		id, _ := message["toolCallId"].(string)
		decision, exists := decisions[id]
		if exists && decision.SourceHash != "" && decision.MatchesSource(message) {
			out[id] = resultBodyHash(decision.Text, decision.Failure)
		}
	}
	return out
}

// MergeView rebases incremental reductions onto immutable source messages.
// A changed source never inherits stale savings or a stale source hash.
func (decisions Decisions) MergeView(more Decisions, messages []any) {
	originals := map[string]map[string]any{}
	for _, value := range messages {
		message, _ := value.(map[string]any)
		if message["role"] == "toolResult" {
			id, _ := message["toolCallId"].(string)
			originals[id] = message
		}
	}
	for key, decision := range more {
		previous := decisions[key]
		if previous.SourceHash == "" {
			decision.Saved += previous.Saved
		} else if previous.MatchesSource(originals[key]) {
			decision.SourceHash = previous.SourceHash
			decision.Saved += previous.Saved
		}
		decisions[key] = decision
	}
}

type historicalFailure struct{ id, name, text string }

func decideHistoricalErrors(messages []any, calls map[string]toolCall, protectedFrom int, settings Settings, decisions Decisions) {
	if settings.HistoricalErrorBytes <= 0 || settings.ErrorExcerptBytes < minimumFailureBytes {
		return
	}
	failures := []historicalFailure{}
	retainedBytes, totalBytes := 0, 0
	for index := protectedFrom - 1; index >= 0; index-- {
		message, _ := messages[index].(map[string]any)
		if message["role"] != "toolResult" || message["isError"] != true {
			continue
		}
		id, _ := message["toolCallId"].(string)
		call, known := calls[id]
		text, plain := resultPlainBody(message)
		if !known || !plain {
			continue
		}
		totalBytes += len(text)
		hash, _ := ResultSourceHash(message)
		if settings.RecoveryHandles[id] == "" || settings.RecoverySources[id] != hash {
			retainedBytes += len(text)
			continue
		}
		failures = append(failures, historicalFailure{id: id, name: call.name, text: text})
	}
	if totalBytes <= settings.HistoricalErrorBytes {
		return
	}
	remaining := max(0, settings.HistoricalErrorBytes*postReductionPercent/100-retainedBytes)
	for index, failure := range failures {
		target := max(minimumFailureBytes, min(settings.ErrorExcerptBytes, remaining/(len(failures)-index)))
		replacement := failureExcerpt(failure.name, failure.text, settings.RecoveryHandles[failure.id], target)
		if len(replacement) >= len(failure.text) {
			remaining -= len(failure.text)
			continue
		}
		decisions[failure.id] = Decision{Text: replacement, Saved: len(failure.text) - len(replacement), Failure: true,
			SourceHash: resultBodyHash(failure.text, true), BudgetRequired: true}
		remaining = max(0, remaining-len(replacement))
	}
}

func failureExcerpt(name, text, handle string, limit int) string {
	if len(text) <= limit {
		return text
	}
	if len(name) > 64 {
		name = name[:validCut(name, 64)]
	}
	head := fmt.Sprintf("[historical %q failure; status unchanged; no resolution inferred]\n", name)
	marker := "\n[... old failure body omitted ...]\n"
	tail := fmt.Sprintf("\n[recovery: %s; use context_recover]", handle)
	bodyBytes := max(0, limit-len(head)-len(marker)-len(tail))
	headEnd := validCut(text, bodyBytes*2/3)
	tailStart := len(text) - bodyBytes/3
	for tailStart < len(text) && !utf8.RuneStart(text[tailStart]) {
		tailStart++
	}
	return head + text[:headEnd] + marker + text[tailStart:] + tail
}

// replaceResultText preserves text-block metadata. Non-text blocks never reach it.
func replaceResultText(message map[string]any, text string) any {
	if _, ok := message["content"].(string); ok {
		return text
	}
	blocks, _ := message["content"].([]any)
	if len(blocks) != 1 {
		// Multiple metadata-bearing blocks are protected by resultPlainBody.
		return []any{map[string]any{"type": "text", "text": text}}
	}
	original, _ := blocks[0].(map[string]any)
	block := make(map[string]any, len(original))
	for key, value := range original {
		block[key] = value
	}
	block["text"] = text
	return []any{block}
}
