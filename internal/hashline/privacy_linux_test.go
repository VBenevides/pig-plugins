//go:build linux

package hashline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPreparationPrivateUntilFinalPublicationStage(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		dir := realTempDir(t)
		path := filepath.Join(dir, "f.txt")
		writeFile(t, path, "old\n", 0644)
		snapshot, err := TakeSnapshot(path, dir, MaxEditBytes)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		err = ReplaceAtomicallyContext(ctx, snapshot, "sensitive replacement\n", func() {
			matches, err := filepath.Glob(filepath.Join(dir, ".pig-plugins-edit-*.tmp"))
			if err != nil || len(matches) != 1 {
				t.Fatalf("temporary missing: %v %v", matches, err)
			}
			info, err := os.Stat(matches[0])
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0600 {
				t.Fatalf("preparation exposed with %o", info.Mode().Perm())
			}
			if cancelled {
				cancel()
			}
		})
		cancel()
		if cancelled && !errors.Is(err, context.Canceled) || !cancelled && err != nil {
			t.Fatalf("unexpected outcome: %v", err)
		}
		content, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		expected := "sensitive replacement\n"
		if cancelled {
			expected = "old\n"
		}
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatal(statErr)
		}
		if string(content) != expected || info.Mode().Perm() != 0644 {
			t.Fatal("content/final permissions changed incorrectly")
		}
		if len(tempFiles(t, dir)) != 0 {
			t.Fatal("preparation file leaked")
		}
	}
}
