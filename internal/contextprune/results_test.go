package contextprune

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func historicalResults(t *testing.T, tools []string, size int) ([]any, Settings) {
	t.Helper()
	messages := []any{}
	for index, tool := range tools {
		id := fmt.Sprintf("result-%d", index)
		output := result(id, tool, strings.Repeat("é", size/2))
		output["details"] = map[string]any{"status": "ok", "index": index}
		messages = append(messages, assistantCall(id, tool, map[string]any{"query": "x"}), output)
	}
	entries := []map[string]any{}
	parent := ""
	for index, message := range messages {
		id := fmt.Sprintf("event-%d", index)
		entries = append(entries, map[string]any{"type": "message", "id": id, "parentId": parent, "message": message})
		parent = id
	}
	file, _ := recoverySession(t, entries)
	settings := DefaultSettings
	settings.KeepRecent, settings.KeepRecentGroups = 0, 2
	settings.HistoricalResultBytes = 4096
	settings.ResultExcerptBytes = 64
	settings.SessionFile = file
	settings.RecoveryHandles, settings.RecoverySources = RecoveryMaps(RecoveryReferences(file, entries))
	return messages, settings
}

func TestHistoricalResultBudgetCoversUnknownToolsAndKeepsNewestWhole(t *testing.T) {
	tools := []string{}
	for index := range 40 {
		tools = append(tools, fmt.Sprintf("mcp_unknown_%d", index%5))
	}
	messages, settings := historicalResults(t, tools, 600)
	original := fmt.Sprint(messages)
	decisions := Decide(messages, settings)
	if !decisions.BudgetPressure() || decisions.Saved() >= settings.MinSavingChars {
		t.Fatalf("aggregate budget should allow a small useful batch: %d", decisions.Saved())
	}
	view, count := Apply(messages, decisions)
	retained, whole := 0, 0
	for _, value := range view {
		message := value.(map[string]any)
		if message["role"] != "toolResult" {
			continue
		}
		text, _ := resultPlainBody(message)
		retained += len(text)
		if strings.Contains(text, "status unchanged") {
			if !strings.Contains(text, "context_recover") || message["details"] == nil || message["isError"] == true {
				t.Fatalf("bad marker: %q", text)
			}
		} else {
			whole++
		}
	}
	if count == 0 || whole == 0 || fmt.Sprint(messages) != original {
		t.Fatalf("count=%d whole=%d or source changed", count, whole)
	}
	newest := view[len(view)-1].(map[string]any)
	if text, _ := resultPlainBody(newest); strings.Contains(text, "status unchanged") {
		t.Fatal("protected newest result changed")
	}
	// Retained size is within the budget plus the protected tail and rewritten markers.
	if retained > settings.HistoricalResultBytes+2*1200+count*400 {
		t.Fatalf("retained %d bytes exceeds the bounded view", retained)
	}
}

func TestHistoricalResultsNeverGrowAndRequireVerifiedRecovery(t *testing.T) {
	messages, settings := historicalResults(t, []string{"web_fetch", "web_fetch", "web_fetch", "web_fetch", "web_fetch"}, 20)
	settings.HistoricalResultBytes = 1
	if len(Decide(messages, settings)) != 0 {
		t.Fatal("tiny results were replaced by larger markers")
	}
	messages, settings = historicalResults(t, []string{"web_fetch", "web_fetch", "web_fetch", "web_fetch", "web_fetch"}, 6000)
	settings.RecoverySources = nil
	if len(Decide(messages, settings)) != 0 {
		t.Fatal("unverified source was reduced")
	}
	settings.ResultExcerptBytes = 0
	settings.RecoveryHandles = nil
	if len(Decide(messages, settings)) != 0 {
		t.Fatal("result without a handle was reduced")
	}
}

func TestHistoricalResultsKeepFailuresAndSkipNonText(t *testing.T) {
	messages, settings := historicalResults(t, []string{"web_fetch", "web_fetch", "web_fetch", "web_fetch"}, 6000)
	messages[1].(map[string]any)["isError"] = true
	messages[3].(map[string]any)["content"] = []any{map[string]any{"type": "image", "data": "abc"}}
	settings.HistoricalErrorBytes = 0
	decisions := Decide(messages, settings)
	for _, id := range []string{"result-0", "result-1"} {
		if _, exists := decisions[id]; exists {
			t.Fatalf("%s changed", id)
		}
	}
}

func TestResultPolicyValidation(t *testing.T) {
	file := filepath.Join(t.TempDir(), "settings.json")
	budget, excerpt, err := ResultPolicy(file)
	if err != nil || budget != DefaultResultBudgetBytes || excerpt != DefaultResultExcerptBytes {
		t.Fatalf("defaults: %d %d %v", budget, excerpt, err)
	}
	for _, text := range []string{`{"historicalResultBytes":4095}`, `{"historicalResultBytes":"x"}`, `{"resultExcerptBytes":-1}`, `{"resultExcerptBytes":4097}`, `{"resultExcerptBytes":1.5}`} {
		if err := os.WriteFile(file, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := ResultPolicy(file); err == nil {
			t.Fatalf("accepted %s", text)
		}
		if enabled, err := Enabled(file); err == nil || enabled {
			t.Fatalf("did not fail closed: %s", text)
		}
	}
	if err := os.WriteFile(file, []byte(`{"historicalResultBytes":8192,"resultExcerptBytes":0}`), 0600); err != nil {
		t.Fatal(err)
	}
	if budget, excerpt, err = ResultPolicy(file); err != nil || budget != 8192 || excerpt != 0 {
		t.Fatalf("valid: %d %d %v", budget, excerpt, err)
	}
}

func resultBytes(t *testing.T, view []any) int {
	t.Helper()
	total := 0
	for _, value := range view {
		if message := value.(map[string]any); message["role"] == "toolResult" {
			text, _ := resultPlainBody(message)
			total += len(text)
		}
	}
	return total
}

func TestResultBatchesReduceToALowerTargetAndStayStable(t *testing.T) {
	tools := make([]string, 12)
	for index := range tools {
		tools[index] = "web_fetch"
	}
	messages, settings := historicalResults(t, tools, 1000)
	settings.ResultExcerptBytes = 0
	settings.HistoricalResultBytes = 12500
	protected := 2 * 1000 // Two protected groups stay whole.
	if decisions := Decide(messages, settings); len(decisions) != 0 {
		t.Fatalf("a result set below its budget changed: %d", len(decisions))
	}
	settings.HistoricalResultBytes = 9000
	first := Decide(messages, settings)
	view, _ := Apply(messages, first)
	if got := resultBytes(t, view); got > 9000*75/100+protected+len(first)*130 {
		t.Fatalf("batch kept %d bytes", got)
	}
	// A later refresh sees the reduced view and makes no new tiny rewrite.
	settings.RecoverySources = first.ViewSources(messages, settings.RecoverySources)
	if more := Decide(view, settings); len(more) != 0 {
		t.Fatalf("stable view was rewritten again: %d", len(more))
	}
}
