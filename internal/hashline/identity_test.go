package hashline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSameContentTargetReplacementRejectsCommit(t *testing.T) {
	for _, kind := range []string{"file", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := realTempDir(t)
			path := filepath.Join(dir, "f.txt")
			other := filepath.Join(dir, "other.txt")
			writeFile(t, path, "original\n", 0644)
			writeFile(t, other, "original\n", 0644)
			snapshot, err := TakeSnapshot(path, dir, MaxEditBytes)
			if err != nil {
				t.Fatal(err)
			}
			err = ReplaceAtomically(snapshot, "updated\n", func() {
				if kind == "file" {
					if err := os.Rename(other, path); err != nil {
						t.Fatal(err)
					}
				} else {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(other, path); err != nil {
						t.Fatal(err)
					}
				}
			})
			if err == nil || !strings.Contains(err.Error(), "identity") {
				t.Fatalf("want identity rejection, got %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != "original\n" {
				t.Fatalf("same-content replacement was overwritten: %q", got)
			}
			if kind == "symlink" {
				info, err := os.Lstat(path)
				if err != nil || info.Mode()&os.ModeSymlink == 0 {
					t.Fatalf("replacement symlink was destroyed: %v", err)
				}
			}
			if len(tempFiles(t, dir)) != 0 {
				t.Fatal("rejected commit left a temporary file")
			}
		})
	}
}
