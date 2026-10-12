//go:build linux

package hashline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReplacementSupportsLongDestinationNames(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, strings.Repeat("f", 240))
	writeFile(t, path, "original\n", 0644)
	snapshot, err := TakeSnapshot(path, dir, MaxEditBytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := ReplaceAtomically(snapshot, "changed\n", nil); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil || string(content) != "changed\n" {
		t.Fatalf("replacement failed: %v", err)
	}
	if len(tempFiles(t, dir)) != 0 {
		t.Fatal("replacement left temporary artifacts")
	}
}
