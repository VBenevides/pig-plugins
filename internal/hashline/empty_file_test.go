package hashline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The empty-file edit error points at write; that workflow must actually work
// on an existing empty file (explicit replacement) and keep its mode.
func TestEmptyFileGuidanceMatchesWritePolicy(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "empty.txt")
	writeFile(t, path, "", 0640)
	read, err := Read(dir, map[string]any{"path": "empty.txt"})
	if err != nil || read.Text != "(empty file)\nVersion: "+ContentVersion(nil) {
		t.Fatalf("empty read: %q %v", read.Text, err)
	}
	_, err = Edit(dir, map[string]any{"path": "empty.txt", "version": ContentVersion(nil), "edits": []any{
		map[string]any{"op": "insert_after", "anchor": "1#0000", "lines": []any{"x"}}}})
	if err == nil || !strings.Contains(err.Error(), "Use write") {
		t.Fatalf("edit guidance missing: %v", err)
	}
	if !strings.Contains(WriteDescription, "explicitly replaces") {
		t.Fatal("write description no longer states replacement policy")
	}
	if _, err := Write(dir, map[string]any{"path": "empty.txt", "content": "filled\n"}); err != nil {
		t.Fatalf("recommended workflow failed: %v", err)
	}
	info, err := os.Stat(path)
	got, readErr := os.ReadFile(path)
	if err != nil || readErr != nil || string(got) != "filled\n" || info.Mode().Perm() != 0640 {
		t.Fatalf("empty file not filled correctly: %q %v %v", got, info.Mode(), err)
	}
}
