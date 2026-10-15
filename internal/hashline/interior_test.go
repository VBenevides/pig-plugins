package hashline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChangedInteriorRejectsReplaceAndDelete(t *testing.T) {
	for _, kind := range []string{"replace", "delete"} {
		t.Run(kind, func(t *testing.T) {
			dir := realTempDir(t)
			path := filepath.Join(dir, "f.txt")
			writeFile(t, path, "start\noriginal interior\nend\n", 0644)
			version := readVersion(t, dir, "f.txt")
			const current = "start\nexternal change\nend\n"
			writeFile(t, path, current, 0644)
			op := map[string]any{"op": kind, "anchor": FormatAnchor(1, "start"), "end": FormatAnchor(3, "end")}
			if kind == "replace" {
				op["lines"] = []any{"replacement"}
			}
			_, err := Edit(dir, map[string]any{"path": "f.txt", "version": version, "edits": []any{op}})
			if err == nil || !strings.Contains(err.Error(), "stale version") {
				t.Fatalf("changed interior was not rejected: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != current {
				t.Fatalf("external interior was lost: %q", got)
			}
		})
	}
}
