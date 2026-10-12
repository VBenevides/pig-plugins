package hashline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnknownOperationFieldsRejectWholeBatch(t *testing.T) {
	for _, kind := range []string{"replace", "delete", "insert_before", "insert_after"} {
		t.Run(kind, func(t *testing.T) {
			dir := realTempDir(t)
			path := filepath.Join(dir, "f.txt")
			writeFile(t, path, "one\ntwo\n", 0644)
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			bad := map[string]any{"op": kind, "anchor": FormatAnchor(2, "two"), "surprise": "private-payload"}
			if kind != "delete" {
				bad["lines"] = []any{"updated"}
			}
			_, err = Edit(dir, map[string]any{"path": "f.txt", "version": readVersion(t, dir, "f.txt"), "edits": []any{
				map[string]any{"op": "replace", "anchor": FormatAnchor(1, "one"), "lines": []any{"changed"}}, bad,
			}})
			if err == nil || !strings.Contains(err.Error(), "edits[1]") || !strings.Contains(err.Error(), "unknown fields") || strings.Contains(err.Error(), "private-payload") {
				t.Fatalf("expected bounded unknown-field rejection, got %v", err)
			}
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) || string(content) != "one\ntwo\n" {
				t.Fatal("invalid neighboring operation changed the target")
			}
		})
	}
}
