package contextprune

import (
	"fmt"
	"strings"
	"testing"
)

// policyHistory is a synthetic session that follows the measured content mix:
// large write arguments, small shell results, web results, and a few old failures.
// It holds no private data.
func policyHistory(t *testing.T) ([]any, Settings, map[string]RecoveryRef) {
	t.Helper()
	messages := []any{user("Constraint: MUST keep the tests passing and never edit vendor files.")}
	for index := range 60 {
		id := fmt.Sprintf("c%02d", index)
		switch index % 5 {
		case 0:
			messages = append(messages, assistantCall(id, "write", map[string]any{"path": fmt.Sprintf("pkg/f%d.go", index), "content": strings.Repeat("code line\n", 300)}), result(id, "write", "Wrote file"))
		case 1:
			messages = append(messages, assistantCall(id, "bash", map[string]any{"command": "go test ./..."}), result(id, "bash", strings.Repeat("ok pkg 0.1s\n", 120)))
		case 2:
			messages = append(messages, assistantCall(id, "web_fetch", map[string]any{"url": "https://example.test"}), result(id, "web_fetch", strings.Repeat("page text ", 400)))
		case 3:
			failure := result(id, "bash", strings.Repeat("undefined: Symbol\n", 150))
			failure["isError"] = true
			messages = append(messages, assistantCall(id, "bash", map[string]any{"command": "go build"}), failure)
		default:
			messages = append(messages, assistantCall(id, "read", map[string]any{"path": fmt.Sprintf("pkg/g%d.go", index)}), result(id, "read", strings.Repeat("read line\n", 250)))
		}
	}
	entries := []map[string]any{}
	parent := ""
	for index, message := range messages {
		id := fmt.Sprintf("p%d", index)
		entries = append(entries, map[string]any{"type": "message", "id": id, "parentId": parent, "message": message})
		parent = id
	}
	file, _ := recoverySession(t, entries)
	refs := RecoveryReferences(file, entries)
	settings := DefaultSettings
	settings.SessionFile = file
	settings.RecoveryHandles, settings.RecoverySources = RecoveryMaps(refs)
	return messages, settings, refs
}

func viewBytes(view []any) int {
	c := Compose(view, len(view))
	return c.UserText + c.AssistantText + c.CallArguments + c.ResultText + c.FailureText + c.RecoveryNotices
}

func balanced(t *testing.T, view []any) {
	t.Helper()
	calls, results := map[string]int{}, map[string]int{}
	for _, value := range view {
		message := value.(map[string]any)
		if message["role"] == "toolResult" {
			results[message["toolCallId"].(string)]++
		}
		blocks, _ := message["content"].([]any)
		for _, raw := range blocks {
			if block, _ := raw.(map[string]any); block["type"] == "toolCall" {
				calls[block["id"].(string)]++
			}
		}
	}
	for id, count := range calls {
		if count != 1 || results[id] != 1 {
			t.Fatalf("unbalanced pair for %s", id)
		}
	}
	for id := range results {
		if calls[id] != 1 {
			t.Fatalf("orphan result %s", id)
		}
	}
}

func TestPolicyComparisonOnSyntheticSession(t *testing.T) {
	messages, settings, refs := policyHistory(t)
	source := fmt.Sprint(messages)
	original := viewBytes(messages)

	legacy := settings
	legacy.HistoricalErrorBytes, legacy.HistoricalResultBytes = 0, 0
	legacyView, _ := Apply(messages, Decide(messages, legacy))

	budgeted := settings
	budgeted.HistoricalErrorBytes, budgeted.HistoricalResultBytes = 16<<10, 16<<10
	budgetedView, _ := Apply(messages, Decide(messages, budgeted))

	// Fold the oldest 25 completed groups, which sit before the protected window.
	scope := foldScope()
	checkpoint, err := scope.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	ids := []string{}
	for index := 0; index < 60 && len(ids) < 25; index++ {
		if index%5 != 3 { // Failures stay visible.
			ids = append(ids, fmt.Sprintf("c%02d", index))
		}
	}
	folded, receipt, err := Fold(messages, FoldRequest{Scope: scope, Checkpoint: checkpoint, CallIDs: ids}, refs, len(messages)-16)
	if err != nil {
		t.Fatalf("fold: %v", err)
	}

	// Fold verifies original digests, so it runs on the saved view. Budgeted pruning then reduces the rest.
	folded, _ = Apply(folded, Decide(folded, budgeted))
	sizes := map[string]int{"none": original, "legacy pruning": viewBytes(legacyView), "budgeted pruning": viewBytes(budgetedView), "budgeted + fold": viewBytes(folded)}
	t.Logf("message bytes: %v; fold saved %d", sizes, receipt.Saved)
	if !(sizes["none"] >= sizes["legacy pruning"] && sizes["legacy pruning"] > sizes["budgeted pruning"] && sizes["budgeted pruning"] > sizes["budgeted + fold"]) {
		t.Fatalf("policies do not reduce in order: %v", sizes)
	}
	for name, view := range map[string][]any{"legacy": legacyView, "budgeted": budgetedView, "folded": folded} {
		balanced(t, view)
		text := fmt.Sprint(view)
		for _, required := range []string{"MUST keep the tests passing and never edit vendor files.", "undefined: Symbol"} {
			if !strings.Contains(text, required) {
				t.Fatalf("%s lost %q", name, required)
			}
		}
	}
	if fmt.Sprint(messages) != source {
		t.Fatal("a policy changed the saved history")
	}
}
