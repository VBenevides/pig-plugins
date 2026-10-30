package contextprune

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func historicalErrors(t *testing.T, count, size int) ([]any, Settings) {
	t.Helper()
	messages := []any{}
	entries := []map[string]any{}
	parent := ""
	for index := range count {
		id := fmt.Sprintf("error-%d", index)
		output := result(id, "bash", strings.Repeat("diagnostic é 😀 ", size/20+1))
		output["isError"] = true
		output["details"] = map[string]any{"exitCode": 7, "unresolved": true}
		output["content"].([]any)[0].(map[string]any)["annotations"] = map[string]any{"priority": 1}
		messages = append(messages, assistantCall(id, "bash", map[string]any{"command": "compiler"}), output)
	}
	for index, message := range messages {
		id := fmt.Sprintf("event-%d", index)
		entries = append(entries, map[string]any{"type": "message", "id": id, "parentId": parent, "message": message})
		parent = id
	}
	file, _ := recoverySession(t, entries)
	settings := DefaultSettings
	settings.KeepRecent, settings.KeepRecentGroups = 0, 0
	settings.HistoricalErrorBytes = 4096
	settings.SessionFile = file
	settings.RecoveryHandles, settings.RecoverySources = RecoveryMaps(RecoveryReferences(file, entries))
	return messages, settings
}

func TestHistoricalErrorBudgetKeepsFailureEvidenceAndMetadata(t *testing.T) {
	messages, settings := historicalErrors(t, 5, 6000)
	original := fmt.Sprint(messages)
	decisions := Decide(messages, settings)
	if len(decisions) != 5 || !decisions.BudgetPressure() || decisions.Saved() >= settings.MinSavingChars {
		t.Fatalf("budget did not permit a useful smaller batch: %d decisions, %d saved", len(decisions), decisions.Saved())
	}
	view, count := Apply(messages, decisions)
	retained := 0
	for _, value := range view {
		message := value.(map[string]any)
		if message["role"] != "toolResult" {
			continue
		}
		text, ok := resultPlainBody(message)
		if !ok || !utf8.ValidString(text) || !strings.Contains(text, "failure; status unchanged; no resolution inferred") || !strings.Contains(text, "context_recover") {
			t.Fatalf("missing historical failure evidence: %q", text)
		}
		retained += len(text)
		if message["isError"] != true || !reflect.DeepEqual(message["details"], map[string]any{"exitCode": 7, "unresolved": true}) {
			t.Fatal("failure metadata changed")
		}
		block := message["content"].([]any)[0].(map[string]any)
		if !reflect.DeepEqual(block["annotations"], map[string]any{"priority": 1}) {
			t.Fatal("text-block metadata changed")
		}
	}
	if count != 5 || retained > settings.HistoricalErrorBytes || fmt.Sprint(messages) != original {
		t.Fatalf("wrong error budget or source mutation: count=%d retained=%d", count, retained)
	}
}

func TestHistoricalErrorsRequireVerifiedSourcesAndProtectRecentFailures(t *testing.T) {
	messages, settings := historicalErrors(t, 2, 50000)
	settings.KeepRecent = 1
	decisions := Decide(messages, settings)
	if len(decisions) != 1 {
		t.Fatalf("recent failure was eligible: %v", decisions)
	}
	view, count := Apply(messages, decisions)
	if count != 1 || !reflect.DeepEqual(view[len(view)-1], messages[len(messages)-1]) {
		t.Fatal("recent failure changed")
	}
	settings.RecoverySources = nil
	if len(Decide(messages, settings)) != 0 {
		t.Fatal("error reduction used an unverified recovery source")
	}
	settings.RecoveryHandles = nil
	if len(Decide(messages, settings)) != 0 {
		t.Fatal("error reduction used a path without a short handle")
	}
}

func TestCachedErrorDecisionRejectsChangedTextAndStatus(t *testing.T) {
	messages, settings := historicalErrors(t, 1, 50000)
	decisions := Decide(messages, settings)
	if len(decisions) != 1 {
		t.Fatal("missing error decision")
	}
	message := messages[1].(map[string]any)
	message["content"] = []any{map[string]any{"type": "text", "text": "a different unresolved failure"}}
	if _, changed := Apply(messages, decisions); changed != 0 {
		t.Fatal("cached decision hid changed diagnostics")
	}
	if len(Decide(messages, settings)) != 0 {
		t.Fatal("changed text reused the saved source hash")
	}
	message["isError"] = false
	if _, changed := Apply(messages, decisions); changed != 0 {
		t.Fatal("cached failure decision changed a successful result")
	}
	decisions.DiscardChangedSources(messages)
	if len(decisions) != 0 || decisions.Saved() != 0 {
		t.Fatal("stale failure savings remained after source invalidation")
	}
}

func TestErrorBudgetsRebaseIncrementalViewsWithoutLosingSourceIdentity(t *testing.T) {
	messages, settings := historicalErrors(t, 4, 50000)
	settings.HistoricalErrorBytes = 16384
	first := Decide(messages, settings)
	view, _ := Apply(messages, first)
	settings.HistoricalErrorBytes = 4096
	settings.RecoverySources = first.ViewSources(messages, settings.RecoverySources)
	more := Decide(view, settings)
	if len(more) != 4 {
		t.Fatalf("could not tighten prior error excerpts: %v", more)
	}
	first.MergeView(more, messages)
	view, count := Apply(messages, first)
	retained := 0
	for _, value := range view {
		message := value.(map[string]any)
		if message["role"] == "toolResult" {
			text, _ := resultPlainBody(message)
			retained += len(text)
		}
	}
	if count != 4 || retained > 4096 {
		t.Fatalf("rebased view lost reductions: count=%d bytes=%d", count, retained)
	}
	for id, decision := range first {
		if decision.SourceHash == "" || !decision.Failure {
			t.Fatalf("lost original source for %s", id)
		}
	}
}

func TestErrorTextBlocksDoNotLoseMetadataOrCreateEmptyProviderBlocks(t *testing.T) {
	message := result("call", "bash", "first diagnostic")
	message["isError"] = true
	message["content"] = []any{
		map[string]any{"type": "text", "text": "first diagnostic"},
		map[string]any{"type": "text", "text": "second diagnostic"},
	}
	body, ok := resultPlainBody(message)
	if !ok || body != "first diagnostic\nsecond diagnostic" {
		t.Fatal("failure text blocks lost diagnostic boundaries")
	}
	blocks := replaceResultText(message, "historical excerpt").([]any)
	if len(blocks) != 1 || blocks[0].(map[string]any)["text"] != "historical excerpt" {
		t.Fatal("replacement introduced an empty provider text block")
	}
	message["content"].([]any)[1].(map[string]any)["annotations"] = map[string]any{"audience": "user"}
	if _, ok := resultPlainBody(message); ok {
		t.Fatal("metadata-bearing text groups were treated as safely mergeable")
	}
}
