package hashline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvalidReplacementTextRejectsWithoutMutation(t *testing.T) {
	payloads := map[string]string{
		"NUL":           "new\x00text",
		"late-NUL":      strings.Repeat("x", 8193) + "\x00",
		"invalid-UTF8":  string([]byte{'x', 0xff}),
		"binary-result": "GIF89a",
	}
	for name, payload := range payloads {
		t.Run(name, func(t *testing.T) {
			dir := realTempDir(t)
			path := filepath.Join(dir, "f.txt")
			writeFile(t, path, "original\n", 0644)
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			_, err = Edit(dir, map[string]any{"path": "f.txt", "version": readVersion(t, dir, "f.txt"), "edits": []any{
				map[string]any{"op": "replace", "anchor": FormatAnchor(1, "original"), "lines": []any{payload}},
			}})
			if err == nil {
				t.Fatal("non-text replacement was accepted")
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(before, after) || string(content) != "original\n" {
				t.Fatal("invalid text changed the target")
			}
		})
	}
}
