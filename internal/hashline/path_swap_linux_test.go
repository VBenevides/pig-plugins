//go:build linux

package hashline

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParentDirectorySwapDoesNotRedirectMutation(t *testing.T) {
	root := realTempDir(t)
	parent := filepath.Join(root, "parent")
	moved := filepath.Join(root, "moved")
	decoy := filepath.Join(root, "decoy")
	for _, directory := range []string{parent, decoy} {
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(parent, "f.txt")
	writeFile(t, target, "original\n", 0600)
	writeFile(t, filepath.Join(decoy, "f.txt"), "decoy\n", 0600)
	snapshot, err := TakeSnapshot(target, root, MaxEditBytes)
	if err != nil {
		t.Fatal(err)
	}
	err = ReplaceAtomically(snapshot, "replacement\n", func() {
		if err := os.Rename(parent, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(decoy, parent); err != nil {
			t.Fatal(err)
		}
	})
	if err == nil {
		t.Fatal("swapped parent was accepted")
	}
	for path, want := range map[string]string{filepath.Join(decoy, "f.txt"): "decoy\n", filepath.Join(moved, "f.txt"): "original\n"} {
		got, readErr := os.ReadFile(path)
		if readErr != nil || string(got) != want {
			t.Fatalf("%s changed: %q %v", path, got, readErr)
		}
	}
	for _, directory := range []string{decoy, moved} {
		if leftovers := tempFiles(t, directory); len(leftovers) != 0 {
			t.Fatalf("temporary files left in %s: %v", directory, leftovers)
		}
	}
}
