package contextprune

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func argumentHistory(t *testing.T, count int) ([]any, Settings) {
	t.Helper()
	messages := []any{}
	for index := range count {
		id := fmt.Sprintf("w%d", index)
		call := assistantCall(id, "write", map[string]any{"path": fmt.Sprintf("f%d.go", index), "content": strings.Repeat("é line\n", 800)})
		messages = append(messages, call, result(id, "write", "Wrote file"))
	}
	entries := []map[string]any{}
	parent := ""
	for index, message := range messages {
		id := fmt.Sprintf("event-%d", index)
		entries = append(entries, map[string]any{"type": "message", "id": id, "parentId": parent, "message": message})
		parent = id
	}
	file, _ := recoverySession(t, entries)
	refs := RecoveryReferences(file, entries)
	settings := DefaultSettings
	settings.KeepRecent, settings.KeepRecentGroups = 0, 2
	settings.HistoricalArgumentBytes = 8192
	settings.Pressure = true
	settings.SessionFile = file
	settings.RecoveryHandles, settings.RecoverySources = RecoveryMaps(refs)
	settings.ArgumentHandles, settings.ArgumentSources = ArgumentRecovery(refs)
	return messages, settings
}

func TestHistoricalArgumentsKeepStructureAndNewestPayloads(t *testing.T) {
	messages, settings := argumentHistory(t, 12)
	before := fmt.Sprint(messages)
	decisions := Decide(messages, settings)
	view, count := Apply(messages, decisions)
	if count == 0 || fmt.Sprint(messages) != before {
		t.Fatalf("count=%d or source changed", count)
	}
	reduced := 0
	for index, value := range view {
		message := value.(map[string]any)
		if message["role"] != "assistant" {
			continue
		}
		block := message["content"].([]any)[0].(map[string]any)
		arguments := block["arguments"].(map[string]any)
		content := arguments["content"].(string)
		if !strings.HasPrefix(content, "[pruned:") {
			continue
		}
		reduced++
		original := messages[index].(map[string]any)["content"].([]any)[0].(map[string]any)
		if arguments["path"] != original["arguments"].(map[string]any)["path"] || block["id"] != original["id"] || block["name"] != "write" || block["type"] != "toolCall" {
			t.Fatal("structure or short fields changed")
		}
		if !strings.Contains(content, "context_recover") || !strings.Contains(content, "cr1_") {
			t.Fatalf("no handle: %q", content)
		}
	}
	if reduced == 0 || reduced >= 12 {
		t.Fatalf("reduced %d of 12", reduced)
	}
	// The newest groups stay whole.
	last := view[len(view)-2].(map[string]any)["content"].([]any)[0].(map[string]any)["arguments"].(map[string]any)
	if strings.HasPrefix(last["content"].(string), "[pruned:") {
		t.Fatal("protected payload changed")
	}
}

func TestHistoricalArgumentsRequireSuccessAndVerifiedSources(t *testing.T) {
	messages, settings := argumentHistory(t, 12)
	messages[1].(map[string]any)["isError"] = true
	decisions := Decide(messages, settings)
	if _, exists := decisions[ArgumentKey("w0")]; exists {
		t.Fatal("a failed call lost its payload")
	}
	settings.ArgumentSources = nil
	if len(Decide(messages, settings)) != 0 {
		t.Fatal("unverified arguments were reduced")
	}
	messages, settings = argumentHistory(t, 12)
	settings.ArgumentHandles = nil
	if len(Decide(messages, settings)) != 0 {
		t.Fatal("arguments without a handle were reduced")
	}
	messages, settings = argumentHistory(t, 12)
	messages[0].(map[string]any)["thinkingSignature"] = "sig"
	if _, exists := Decide(messages, settings)[ArgumentKey("w0")]; exists {
		t.Fatal("signed assistant message was rewritten")
	}
}

func TestChangedArgumentsInvalidateCachedDecisions(t *testing.T) {
	messages, settings := argumentHistory(t, 12)
	decisions := Decide(messages, settings)
	if _, exists := decisions[ArgumentKey("w0")]; !exists {
		t.Fatal("missing decision")
	}
	messages[0].(map[string]any)["content"].([]any)[0].(map[string]any)["arguments"].(map[string]any)["content"] = strings.Repeat("different ", 100)
	if _, changed := Apply(messages, decisions); changed != len(decisions)-1 {
		t.Fatalf("changed arguments were replaced: %d of %d", changed, len(decisions))
	}
	decisions.DiscardChangedSources(messages)
	if _, exists := decisions[ArgumentKey("w0")]; exists {
		t.Fatal("stale decision remained")
	}
}

func TestSmallArgumentsAndOtherToolsStay(t *testing.T) {
	arguments := map[string]any{"path": "a.go", "oldText": "short", "newText": strings.Repeat("x", 600), "nested": []any{strings.Repeat("y", 700), "ok"}}
	reduced, _ := reduceArgument(arguments, "", "edit", "cr1_x").(map[string]any)
	if reduced["path"] != "a.go" || reduced["oldText"] != "short" || !strings.HasPrefix(reduced["newText"].(string), "[pruned:") {
		t.Fatalf("wrong reduction: %v", reduced)
	}
	nested := reduced["nested"].([]any)
	if !strings.HasPrefix(nested[0].(string), "[pruned:") || nested[1] != "ok" {
		t.Fatal("nested payload mishandled")
	}
	if reduceArgument(map[string]any{"path": "a.go"}, "", "edit", "h") != nil {
		t.Fatal("small arguments changed")
	}
	if !reflect.DeepEqual(arguments["oldText"], "short") {
		t.Fatal("input changed")
	}
}

func TestArgumentPolicyValidation(t *testing.T) {
	if budget, err := ArgumentPolicy(map[string]any{}); err != nil || budget != DefaultArgumentBudgetBytes {
		t.Fatal("bad default")
	}
	file := t.TempDir() + "/missing.json"
	if budget, err := ArgumentBudget(file); err != nil || budget != DefaultArgumentBudgetBytes {
		t.Fatal("missing file should give the default")
	}
}

func TestArgumentPolicyRejectsInvalidValues(t *testing.T) {
	for _, value := range []any{json.Number("4095"), json.Number("16777217"), json.Number("1.5"), "x", true, nil} {
		if _, err := ArgumentPolicy(map[string]any{"historicalArgumentBytes": value}); err == nil {
			t.Fatalf("accepted %v", value)
		}
	}
	if budget, err := ArgumentPolicy(map[string]any{"historicalArgumentBytes": json.Number("8192")}); err != nil || budget != 8192 {
		t.Fatal("valid value rejected")
	}
}
