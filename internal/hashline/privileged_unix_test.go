//go:build unix

package hashline

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrivilegedPermissionTargetsAreRejected(t *testing.T) {
	for _, bit := range []fs.FileMode{fs.ModeSetuid, fs.ModeSetgid, fs.ModeSticky} {
		t.Run(bit.String(), func(t *testing.T) {
			dir := realTempDir(t)
			path := filepath.Join(dir, "f.txt")
			writeFile(t, path, "original\n", 0755|bit)
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if before.Mode()&bit == 0 {
				t.Fatal("filesystem did not retain the permission bit needed for this test")
			}
			version := readVersion(t, dir, "f.txt")
			_, err = Edit(dir, map[string]any{"path": "f.txt", "version": version, "edits": []any{
				map[string]any{"op": "replace", "anchor": FormatAnchor(1, "original"), "lines": []any{"changed"}},
			}})
			if err == nil || !strings.Contains(err.Error(), "privileged permission") {
				t.Fatalf("expected privileged-mode rejection, got %v", err)
			}
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(before, after) || before.Mode() != after.Mode() || string(content) != "original\n" {
				t.Fatal("rejection changed target content or metadata")
			}
		})
	}
}
