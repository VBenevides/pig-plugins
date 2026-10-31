package contextprune

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func foldScope() ReductionScope {
	return ReductionScope{Session: "s", Anchor: "a1", Provider: "openai", Model: "m", API: "openai-completions", Endpoint: "https://api.example.test/v1", Account: "acct"}
}

type foldFixture struct {
	messages []any
	refs     map[string]RecoveryRef
	request  FoldRequest
}

// newFold builds: user, assistant{text, write call}, result, assistant{parallel read, bash}, 2 results, user, assistant text.
func newFold(t *testing.T) foldFixture {
	t.Helper()
	big := strings.Repeat("payload é\n", 400)
	first := assistantCall("w", "write", map[string]any{"path": "a.go", "content": big})
	first["content"] = append([]any{map[string]any{"type": "text", "text": "Writing the file."}}, first["content"].([]any)...)
	second := map[string]any{"role": "assistant", "content": []any{
		map[string]any{"type": "toolCall", "id": "r", "name": "read", "arguments": map[string]any{"path": "a.go"}},
		map[string]any{"type": "toolCall", "id": "b", "name": "bash", "arguments": map[string]any{"command": "echo secret-token"}},
	}}
	messages := []any{user("Build a thing; keep tests."), first, result("w", "write", "wrote"),
		second, result("r", "read", big), result("b", "bash", big),
		user("Next: continue"), map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "ok"}}}}
	entries := []map[string]any{}
	parent := ""
	for index, message := range messages {
		id := fmt.Sprintf("e%d", index)
		entries = append(entries, map[string]any{"type": "message", "id": id, "parentId": parent, "message": message})
		parent = id
	}
	scope := foldScope()
	checkpoint, err := scope.Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	return foldFixture{messages, RecoveryReferences("/session.jsonl", entries), FoldRequest{Scope: scope, Checkpoint: checkpoint, CallIDs: []string{"w", "r", "b"}}}
}

func TestFoldReplacesBothSidesAndKeepsSourceHistory(t *testing.T) {
	fixture := newFold(t)
	before := fmt.Sprint(fixture.messages)
	view, receipt, err := Fold(fixture.messages, fixture.request, fixture.refs, 6)
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(fixture.messages) != before {
		t.Fatal("fold changed its input")
	}
	text := fmt.Sprint(view)
	for _, banned := range []string{"payload", "secret-token", `"toolCall"`, "toolResult"} {
		if strings.Contains(text, banned) {
			t.Fatalf("view still holds %q", banned)
		}
	}
	for _, kept := range []string{"Build a thing; keep tests.", "Next: continue", "Writing the file.", "write ok a.go", "read ok a.go", "Read file contents again before editing", "context_recover"} {
		if !strings.Contains(text, kept) {
			t.Fatalf("view lost %q: %s", kept, text)
		}
	}
	if receipt.Removed != 5 || receipt.Saved < 8000 || len(receipt.Handles) != 5 || len(view) != 5 {
		t.Fatalf("unexpected receipt %+v with %d messages", receipt, len(view))
	}
	// Folding is idempotent: a second request finds nothing and changes nothing.
	if _, _, err := Fold(view, fixture.request, fixture.refs, 3); !errors.Is(err, ErrNothingToFold) {
		t.Fatalf("second fold = %v", err)
	}
	// The receipt hash still verifies against the untouched source range.
	fixture.request.RangeHash = receipt.RangeHash
	again, second, err := Fold(fixture.messages, fixture.request, fixture.refs, 6)
	if err != nil || second.RangeHash != receipt.RangeHash || !reflect.DeepEqual(again, view) {
		t.Fatalf("fold is not deterministic: %v", err)
	}
}

func TestFoldRejectsUnsafeRanges(t *testing.T) {
	cases := map[string]func(*foldFixture){
		"split parallel group": func(f *foldFixture) { f.request.CallIDs = []string{"r"} },
		"missing result":       func(f *foldFixture) { f.messages = append(f.messages[:4:4], f.messages[5:]...) },
		"failure":              func(f *foldFixture) { f.messages[2].(map[string]any)["isError"] = true },
		"changed range":        func(f *foldFixture) { f.request.RangeHash = "00" },
		"scope changed":        func(f *foldFixture) { f.request.Scope.Model = "other" },
		"unsupported api":      func(f *foldFixture) { f.request.Scope.API = "openai-responses" },
		"workflow tool": func(f *foldFixture) {
			f.messages[1].(map[string]any)["content"].([]any)[1].(map[string]any)["name"] = "todo"
		},
		"signed reasoning":   func(f *foldFixture) { f.messages[1].(map[string]any)["thinkingSignature"] = "x" },
		"no recovery handle": func(f *foldFixture) { f.refs = map[string]RecoveryRef{} },
		"protected window":   func(f *foldFixture) {},
		"repeated id":        func(f *foldFixture) { f.request.CallIDs = []string{"w", "w"} },
		"media": func(f *foldFixture) {
			f.messages[2].(map[string]any)["content"] = []any{map[string]any{"type": "image", "data": "x"}}
		},
	}
	for name, mutate := range cases {
		fixture := newFold(t)
		mutate(&fixture)
		protected := 6
		if name == "protected window" {
			protected = 5
		}
		before := fmt.Sprint(fixture.messages)
		if view, _, err := Fold(fixture.messages, fixture.request, fixture.refs, protected); err == nil || view != nil {
			t.Errorf("%s: fold was accepted", name)
		}
		if fmt.Sprint(fixture.messages) != before {
			t.Errorf("%s: input changed", name)
		}
	}
}

func TestFoldDeduplicatesAndRefusesGrowth(t *testing.T) {
	fixture := newFold(t)
	view, _, err := Fold(fixture.messages, fixture.request, fixture.refs, 6)
	if err != nil || !strings.Contains(fmt.Sprint(view), "same result as cr1_") {
		t.Fatalf("duplicate bodies were not noted: %v", err)
	}
	small := []any{user("go"), assistantCall("x", "read", map[string]any{"path": "a"}), result("x", "read", "1"), user("end")}
	entries := []map[string]any{}
	parent := ""
	for index, message := range small {
		id := fmt.Sprintf("s%d", index)
		entries = append(entries, map[string]any{"type": "message", "id": id, "parentId": parent, "message": message})
		parent = id
	}
	request := fixture.request
	request.CallIDs = []string{"x"}
	if _, _, err := Fold(small, request, RecoveryReferences("/s.jsonl", entries), 3); err == nil {
		t.Fatal("fold accepted a range that would grow")
	}
}

func TestFoldHandlesRestoreExactOriginalsWithoutExecution(t *testing.T) {
	fixture := newFold(t)
	entries := []map[string]any{}
	parent := ""
	for index, message := range fixture.messages {
		id := fmt.Sprintf("e%d", index)
		entries = append(entries, map[string]any{"type": "message", "id": id, "parentId": parent, "message": message})
		parent = id
	}
	file, _ := recoverySession(t, entries)
	refs := RecoveryReferences(file, entries)
	_, receipt, err := Fold(fixture.messages, fixture.request, refs, 6)
	if err != nil {
		t.Fatal(err)
	}
	restored := 0
	for _, handle := range receipt.Handles {
		content := ""
		for offset, done := 0, false; !done; {
			page, err := ReadRecoveryPage(file, refs, handle, offset, RecoveryPageLimit)
			if err != nil {
				t.Fatal(err)
			}
			content += page.Content
			done = page.Next < 0 || page.Next >= page.Total
			offset = page.Next
		}
		if !strings.Contains(content, `"role"`) {
			t.Fatalf("handle %s did not restore a message", handle)
		}
		restored++
	}
	if restored != 5 {
		t.Fatal("restoration incomplete")
	}
}
