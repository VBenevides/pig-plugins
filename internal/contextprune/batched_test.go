package contextprune

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestAgeMaskingKeepsWholeRecentParallelGroupsAndRecoveryReferences(t *testing.T) {
	body := strings.Repeat("original content\n", 3000)
	messages := []any{
		user("Preserve the task and its constraints"),
		assistantCall("old", "read", map[string]any{"path": "old.go", "offset": 20, "limit": 30}), result("old", "read", body),
		map[string]any{"role": "assistant", "content": []any{
			map[string]any{"type": "toolCall", "id": "parallel-a", "name": "read", "arguments": map[string]any{"path": "a.go"}},
			map[string]any{"type": "toolCall", "id": "parallel-b", "name": "read", "arguments": map[string]any{"path": "b.go"}},
		}},
		result("parallel-a", "read", body), result("parallel-b", "read", body),
		assistantCall("last", "read", map[string]any{"path": "last.go"}), result("last", "read", body),
	}
	before, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	settings := DefaultSettings
	settings.KeepRecent = 0
	settings.KeepRecentGroups = 2
	settings.SessionFile = "/archive/session.jsonl"
	decisions := Decide(messages, settings)
	if len(decisions) != 1 {
		t.Fatalf("only the old group is eligible: %v", decisions)
	}
	text := decisions["old"].Text
	for _, want := range []string{"old.go", "20:30", `toolCallId "old"`, settings.SessionFile} {
		if !strings.Contains(text, want) {
			t.Fatalf("recovery reference lacks %q: %s", want, text)
		}
	}
	out, changed := Apply(messages, decisions)
	if changed != 1 || !reflect.DeepEqual(out[0], messages[0]) {
		t.Fatal("user constraints must stay unchanged")
	}
	for _, index := range []int{4, 5, 7} {
		if resultText(t, out[index]) != body {
			t.Fatal("every result in the recent parallel groups must stay intact")
		}
	}
	after, err := json.Marshal(messages)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("masking changed the original history")
	}
	if got := Decide(out, settings); len(got) != 0 {
		t.Fatalf("masking must be idempotent: %v", got)
	}
}

func TestAgeMaskingRequiresDurableRecoveryAndProtectsErrorsImagesAndUnknownTools(t *testing.T) {
	body := strings.Repeat("output\n", 7000)
	failed := result("error", "read", body)
	failed["isError"] = true
	image := result("image", "read", body)
	image["content"] = []any{map[string]any{"type": "text", "text": body}, map[string]any{"type": "image", "data": "..."}}
	messages := []any{
		assistantCall("unique", "read", map[string]any{"path": "unique.go"}), result("unique", "read", body),
		assistantCall("search", "code_search", nil), result("search", "code_search", body),
		assistantCall("error", "read", nil), failed,
		assistantCall("image", "read", nil), image,
		assistantCall("unknown", "send_message", nil), result("unknown", "send_message", body),
	}
	settings := DefaultSettings
	settings.KeepRecent, settings.KeepRecentGroups = 0, 0
	if got := Decide(messages, settings); len(got) != 0 {
		t.Fatalf("unique observations without an archive must stay: %v", got)
	}
	settings.SessionFile = "/archive/session.jsonl"
	got := Decide(messages, settings)
	if len(got) != 2 || got["unique"].Text == "" || got["search"].Text == "" {
		t.Fatalf("only successful, recoverable reads and searches may be age-masked: %v", got)
	}
}

func TestTenRecentGroupsStayProtectedEvenWithManyUserMessages(t *testing.T) {
	body := strings.Repeat("output\n", 7000)
	messages := []any{assistantCall("only", "read", map[string]any{"path": "only.go"}), result("only", "read", body)}
	messages = append(messages, filler(30)...)
	settings := DefaultSettings
	settings.SessionFile = "/archive/session.jsonl"
	if got := Decide(messages, settings); len(got) != 0 {
		t.Fatalf("user messages must not age a recent tool group out: %v", got)
	}
}
