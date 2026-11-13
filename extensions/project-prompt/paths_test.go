package projectprompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAllInstructionLocations(t *testing.T) {
	root := t.TempDir()
	var expected []string
	for _, dir := range []string{".local", "local", ".pig", "pig"} {
		for _, name := range []string{"SYSTEM.md", "APPEND_SYSTEM.md", "AGENTS.md", "RULES.md"} {
			path := dir + "/" + name
			expected = append(expected, path)
			if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, path), []byte("UNIQUE:"+path+":END"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	if got := strings.Join(instructionPaths(), "|"); got != strings.Join(expected, "|") {
		t.Fatalf("unexpected order: %s", got)
	}
	result, err := appendPrompt("HOST", root, true)
	if err != nil {
		t.Fatal(err)
	}
	prompt := result.(map[string]any)["systemPrompt"].(string)
	previous := -1
	for _, path := range expected {
		marker := "UNIQUE:" + path + ":END"
		position := strings.Index(prompt, marker)
		if position <= previous || strings.Count(prompt, marker) != 1 {
			t.Fatalf("missing, duplicated or out-of-order %s", path)
		}
		previous = position
	}
	if !strings.HasPrefix(prompt, "HOST") {
		t.Fatal("base replaced")
	}
	if result, err := appendPrompt("HOST", root, false); result != nil || err != nil {
		t.Fatalf("untrusted project read: %v %v", result, err)
	}
}
