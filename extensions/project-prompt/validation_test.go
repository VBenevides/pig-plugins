package projectprompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEveryInstructionPathChecksInputAndDeduplicates(t *testing.T) {
	for _, path := range instructionPaths() {
		t.Run(path, func(t *testing.T) {
			root := t.TempDir()
			full := filepath.Join(root, path)
			if err := os.MkdirAll(filepath.Dir(full), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte{0xff}, 0600); err != nil {
				t.Fatal(err)
			}
			if result, err := appendPrompt("base", root, true); result != nil || err == nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("invalid input accepted: %v %v", result, err)
			}
			if err := os.WriteFile(full, []byte("SAME_RULE"), 0600); err != nil {
				t.Fatal(err)
			}
			if result, err := appendPrompt("base SAME_RULE", root, true); result != nil || err != nil {
				t.Fatalf("host rule duplicated: %v %v", result, err)
			}
			if err := os.Remove(full); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(outside, []byte("OUTSIDE"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, full); err != nil {
				t.Fatal(err)
			}
			if result, err := appendPrompt("base", root, true); result != nil || err == nil {
				t.Fatalf("workspace escape accepted: %v %v", result, err)
			}
		})
	}
}

func TestAbsentProjectInstructions(t *testing.T) {
	if result, err := appendPrompt("base", t.TempDir(), true); result != nil || err != nil {
		t.Fatalf("missing optional files: %v %v", result, err)
	}
}
