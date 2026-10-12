package hashline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShiftedRepeatedLinesCannotAcceptOldAnchors(t *testing.T) {
	for _, repeated := range []string{"", "}"} {
		t.Run("repeated-"+LineHash(repeated), func(t *testing.T) {
			dir := realTempDir(t)
			path := filepath.Join(dir, "f.txt")
			original := "first\n" + repeated + "\n" + repeated + "\nlast\n"
			writeFile(t, path, original, 0644)
			version := readVersion(t, dir, "f.txt")
			current := "inserted\n" + original
			writeFile(t, path, current, 0644)
			// Old line 3 and current line 3 intentionally have the same short anchor.
			if FormatAnchor(3, ParseFile(current).Lines[2].Text) != FormatAnchor(3, repeated) {
				t.Fatal("fixture does not reproduce the repeated-line ambiguity")
			}
			_, err := Edit(dir, map[string]any{"path": "f.txt", "version": version, "edits": []any{
				map[string]any{"op": "replace", "anchor": FormatAnchor(3, repeated), "lines": []any{"wrong target"}},
			}})
			if err == nil || !strings.Contains(err.Error(), "stale version") {
				t.Fatalf("old repeated-line anchor was accepted: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != current {
				t.Fatalf("wrong repeated occurrence was changed: %q", got)
			}
		})
	}
}
