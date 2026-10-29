package contextprune

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func recoverySession(t *testing.T, entries []map[string]any) (string, []byte) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "session.jsonl")
	var data []byte
	for _, entry := range entries {
		line, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		data = append(data, line...)
		data = append(data, ' ', '\n')
	}
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	return file, data
}

func recoveryEvent(id, parent, call, text string, failed bool) map[string]any {
	return map[string]any{"id": id, "parentId": parent, "type": "message", "message": map[string]any{"role": "toolResult", "toolCallId": call, "isError": failed, "content": []any{map[string]any{"type": "text", "text": text}}}}
}

func TestRecoveryPagesReconstructExactSavedErrorsAndRemainStable(t *testing.T) {
	entries := []map[string]any{recoveryEvent("first", "", "call", "diagnostic é 😀 "+strings.Repeat("failure", 200), true)}
	file, original := recoverySession(t, entries)
	refs := RecoveryReferences(file, entries)
	var handle string
	for key := range refs {
		handle = key
	}
	if handle == "" || len(handle) != 20 {
		t.Fatal("missing short handle")
	}
	var recovered strings.Builder
	for offset := 0; ; {
		page, err := ReadRecoveryPage(file, refs, handle, offset, 39)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Content) > 39 || !utf8.ValidString(page.Content) || page.Offset != offset {
			t.Fatalf("invalid page: %+v", page)
		}
		hash := sha256.Sum256(original)
		if page.SHA256 != hex.EncodeToString(hash[:]) {
			t.Fatal("raw record checksum changed")
		}
		recovered.WriteString(page.Content)
		if page.Next == -1 {
			break
		}
		offset = page.Next
	}
	if recovered.String() != string(original) {
		t.Fatal("recovery did not preserve exact saved bytes")
	}
	fresh, err := BranchRecoveryReferences(file, "first")
	if err != nil || fresh[handle] != refs[handle] {
		t.Fatalf("restart changed recovery: %v %v", fresh, err)
	}
	if _, err := ReadRecoveryPage(file, fresh, handle, 0, RecoveryPageLimit); err != nil {
		t.Fatal(err)
	}
	if len(RecoveryReferences(file+".other", entries)[handle].Handle) != 0 {
		t.Fatal("handle crossed sessions")
	}
}

func TestRecoveryHonorsBranchesAndCompactionAncestors(t *testing.T) {
	entries := []map[string]any{
		{"id": "root", "type": "message"},
		recoveryEvent("a", "root", "call-a", "branch A", false),
		recoveryEvent("b", "root", "call-b", "branch B", false),
		{"id": "compact", "parentId": "a", "type": "compaction"},
	}
	file, _ := recoverySession(t, entries)
	all := RecoveryReferences(file, entries)
	a, err := BranchRecoveryReferences(file, "compact")
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 1 {
		t.Fatalf("wrong branch members: %v", a)
	}
	for handle, ref := range all {
		_, err := ReadRecoveryPage(file, a, handle, 0, 1024)
		if (ref.CallID == "call-a") != (err == nil) {
			t.Fatalf("wrong branch authorization: %v %v", ref, err)
		}
	}
	if _, err := BranchRecoveryReferences(file, "missing"); err == nil {
		t.Fatal("accepted a non-durable leaf")
	}
	cycle := []map[string]any{{"id": "a", "parentId": "b"}, {"id": "b", "parentId": "a"}}
	file, _ = recoverySession(t, cycle)
	if _, err := BranchRecoveryReferences(file, "a"); err == nil {
		t.Fatal("accepted a branch cycle")
	}
}

func TestRecoveryRejectsChangedMissingAndOversizedSources(t *testing.T) {
	entries := []map[string]any{recoveryEvent("first", "", "call", "original", false)}
	file, _ := recoverySession(t, entries)
	refs := RecoveryReferences(file, entries)
	var handle string
	for key := range refs {
		handle = key
	}
	for _, args := range []struct {
		handle        string
		offset, limit int
	}{
		{"../../secret", 0, 10}, {handle, -1, 10}, {handle, 0, 0}, {handle, 0, RecoveryPageLimit + 1}, {handle, 1000000, 1},
	} {
		if _, err := ReadRecoveryPage(file, refs, args.handle, args.offset, args.limit); err == nil {
			t.Fatalf("accepted invalid query: %+v", args)
		}
	}
	changed := recoveryEvent("first", "", "call", "changed", false)
	line, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, append(line, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRecoveryPage(file, refs, handle, 0, 100); err == nil {
		t.Fatal("accepted changed original")
	}
	if err := os.WriteFile(file, []byte(strings.Repeat("x", RecoveryRecordLimit+1)), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRecoveryPage(file, refs, handle, 0, 100); err == nil {
		t.Fatal("accepted oversized record")
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRecoveryPage(file, refs, handle, 0, 100); err == nil {
		t.Fatal("accepted missing file")
	}
}

func TestShortRecoveryMarkersPreserveOriginals(t *testing.T) {
	original := result("r", "read", strings.Repeat("source", 200))
	entries := []map[string]any{{"id": "entry", "type": "message", "message": original}}
	file, _ := recoverySession(t, entries)
	settings := DefaultSettings
	settings.KeepRecent, settings.KeepRecentGroups, settings.MinSavingChars = 0, 0, 1
	settings.SessionFile = file
	messages := []any{assistantCall("r", "read", map[string]any{"path": "file"}), original}
	long := Decide(messages, settings)
	settings.RecoveryHandles = RecoveryHandles(RecoveryReferences(file, entries))
	short := Decide(messages, settings)
	if len(short) != 1 || len(short["r"].Text) >= len(long["r"].Text) || !strings.Contains(short["r"].Text, "context_recover") {
		t.Fatalf("short recovery marker missing: %v", short)
	}
	view, _ := Apply(messages, short)
	text, _ := plainText(view[1].(map[string]any))
	if !strings.Contains(text, "cr1_") || strings.Contains(text, file) {
		t.Fatal("marker retained a full session path")
	}
	before, _ := plainText(original)
	if before != strings.Repeat("source", 200) {
		t.Fatal("original changed")
	}
}
