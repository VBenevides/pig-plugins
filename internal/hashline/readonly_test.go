package hashline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadOnlyTargetIsRejectedEvenWithWritableDirectory(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "f.txt")
	writeFile(t, path, "original\n", 0444)
	version := readVersion(t, dir, "f.txt")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = Edit(dir, map[string]any{"path": "f.txt", "version": version, "edits": []any{
		map[string]any{"op": "replace", "anchor": FormatAnchor(1, "original"), "lines": []any{"changed"}},
	}})
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("expected read-only rejection, got %v", err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || after.Mode() != before.Mode() || string(content) != "original\n" {
		t.Fatal("rejection changed the read-only target")
	}
}
