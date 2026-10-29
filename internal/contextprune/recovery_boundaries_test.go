package contextprune

import (
	"os"
	"strings"
	"testing"
)

func TestRecoveryCanRetrieveHistoricalCallArguments(t *testing.T) {
	message := assistantCall("write-call", "write", map[string]any{"path": "file.go", "content": "historical code"})
	entries := []map[string]any{{"id": "assistant", "message": message}}
	file, original := recoverySession(t, entries)
	refs, err := BranchRecoveryReferences(file, "assistant")
	if err != nil || len(refs) != 1 {
		t.Fatalf("missing call reference: %v %v", refs, err)
	}
	if len(RecoveryHandles(refs)) != 0 {
		t.Fatal("assistant reference became a tool-result mask")
	}
	for handle, ref := range refs {
		if ref.Role != "assistant" || ref.EventID != "assistant" {
			t.Fatalf("missing provenance: %+v", ref)
		}
		page, err := ReadRecoveryPage(file, refs, handle, 0, RecoveryPageLimit)
		if err != nil || page.Content != string(original) {
			t.Fatalf("historical arguments changed: %v %v", page, err)
		}
	}
}

func TestRecoveryRejectsAmbiguousCallsAndUnsafeStorage(t *testing.T) {
	entries := []map[string]any{recoveryEvent("a", "", "same-call", "a", false), recoveryEvent("b", "a", "same-call", "b", false)}
	file, _ := recoverySession(t, entries)
	if len(RecoveryHandles(RecoveryReferences(file, entries))) != 0 {
		t.Fatal("ambiguous call ID received an arbitrary handle")
	}
	if err := os.Truncate(file, RecoveryScanLimit+1); err != nil {
		t.Fatal(err)
	}
	if _, err := BranchRecoveryReferences(file, "a"); err == nil {
		t.Fatal("accepted a session above the physical read budget")
	}
	if _, err := BranchRecoveryReferences(t.TempDir(), "a"); err == nil {
		t.Fatal("accepted a directory")
	}
}

func TestRecoveryHashPreservesLargeJSONNumbers(t *testing.T) {
	file, _ := recoverySession(t, nil)
	original := `{"id":"a","message":{"role":"assistant","content":[{"type":"toolCall","arguments":{"n":9007199254740992}}]}}` + "\n"
	if err := os.WriteFile(file, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	refs, err := BranchRecoveryReferences(file, "a")
	if err != nil {
		t.Fatal(err)
	}
	var handle string
	for key := range refs {
		handle = key
	}
	changed := strings.ReplaceAll(original, "9007199254740992", "9007199254740993")
	if err := os.WriteFile(file, []byte(changed), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRecoveryPage(file, refs, handle, 0, 1024); err == nil {
		t.Fatal("numeric precision hid a source change")
	}
	if _, err := decodeRecoveryRecord([]byte(original + `{"trailing":true}`)); err == nil {
		t.Fatal("accepted trailing event data")
	}
	if _, err := decodeRecoveryRecord([]byte{'"', 0xff, '"'}); err == nil {
		t.Fatal("accepted invalid UTF-8")
	}
}
