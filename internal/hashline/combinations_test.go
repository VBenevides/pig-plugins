package hashline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInvalidOperationCombinationsPreserveWholeBatch(t *testing.T) {
	for _, test := range []struct {
		name    string
		invalid map[string]any
		message string
	}{
		{"replace-missing-lines", map[string]any{"op": "replace"}, "lines"},
		{"insert-before-missing-lines", map[string]any{"op": "insert_before"}, "lines"},
		{"insert-after-missing-lines", map[string]any{"op": "insert_after"}, "lines"},
		{"insert-before-end", map[string]any{"op": "insert_before", "lines": []any{"new"}, "end": FormatAnchor(2, "two")}, "end"},
		{"insert-after-null-end", map[string]any{"op": "insert_after", "lines": []any{"new"}, "end": nil}, "end"},
		{"delete-lines", map[string]any{"op": "delete", "lines": []any{"new"}}, "lines"},
		{"delete-null-lines", map[string]any{"op": "delete", "lines": nil}, "lines"},
		{"missing-operation", map[string]any{}, "op must"},
		{"wrong-operation-type", map[string]any{"op": 7}, "op must"},
		{"reversed-range", map[string]any{"op": "delete", "anchor": FormatAnchor(3, "three"), "end": FormatAnchor(2, "two")}, "before anchor"},
		{"conflicting-range", map[string]any{"op": "delete"}, "cover the same line"},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := realTempDir(t)
			path := filepath.Join(dir, "f.txt")
			original := "one\ntwo\nthree\n"
			writeFile(t, path, original, 0751)
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := test.invalid["anchor"]; !ok {
				test.invalid["anchor"] = FormatAnchor(2, "two")
			}
			_, err = Edit(dir, map[string]any{"path": "f.txt", "version": ContentVersion([]byte(original)), "edits": []any{
				map[string]any{"op": "replace", "anchor": FormatAnchor(2, "two"), "lines": []any{"valid-neighbor"}}, test.invalid,
			}})
			if err == nil || !strings.Contains(err.Error(), test.message) {
				t.Fatalf("invalid combination was not diagnosed: %v", err)
			}
			after, statErr := os.Stat(path)
			content, readErr := os.ReadFile(path)
			if statErr != nil || readErr != nil {
				t.Fatalf("inspect rejected batch: %v, %v", statErr, readErr)
			}
			if string(content) != original || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) || len(tempFiles(t, dir)) != 0 {
				t.Fatal("invalid neighbor caused partial mutation or changed metadata")
			}
		})
	}
}
