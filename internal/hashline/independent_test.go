package hashline

import (
	"crypto/sha1"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Expected tokens come from the format specification, not golden fixtures or
// production helpers. Identical repeated lines must never permit relocation.
func TestIndependentVersionBindingRejectsChangedInteriorAndRepeatedLines(t *testing.T) {
	original := "same\ninterior\nsame\n"
	version := fmt.Sprintf("%x", sha256.Sum256([]byte(original)))
	digest := sha1.Sum([]byte("same"))
	hash := fmt.Sprintf("%x", digest)[:4]
	for _, current := range []string{"same\nchanged\nsame\n", "same\nsame\ninterior\nsame\n"} {
		dir := realTempDir(t)
		path := filepath.Join(dir, "f.txt")
		writeFile(t, path, current, 0600)
		before, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		_, err = Edit(dir, map[string]any{"path": "f.txt", "version": version, "edits": []any{map[string]any{"op": "delete", "anchor": "1#" + hash, "end": "3#" + hash}}})
		if err == nil || ErrorCategoryOf(err) != CategoryStale {
			t.Fatalf("unsafe request accepted: %v", err)
		}
		bytes, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		after, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if string(bytes) != current || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
			t.Fatal("stale rejection changed target")
		}
	}
}
