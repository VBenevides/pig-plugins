//go:build unix

package hashline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHardLinkedTargetsAreRejectedWithoutBreakingLinks(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "f.txt")
	alias := filepath.Join(dir, "alias.txt")
	writeFile(t, path, "original\n", 0644)
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	version := readVersion(t, dir, "f.txt")
	_, err := Edit(dir, map[string]any{"path": "f.txt", "version": version, "edits": []any{
		map[string]any{"op": "replace", "anchor": FormatAnchor(1, "original"), "lines": []any{"changed"}},
	}})
	if err == nil || !strings.Contains(err.Error(), "hard link") {
		t.Fatalf("expected hard-link rejection, got %v", err)
	}
	originalInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	aliasInfo, err := os.Stat(alias)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{path, alias} {
		content, err := os.ReadFile(target)
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != "original\n" {
			t.Fatal("rejection changed target content")
		}
	}
	if !os.SameFile(originalInfo, aliasInfo) {
		t.Fatal("rejection changed content or broke the link relationship")
	}
}
