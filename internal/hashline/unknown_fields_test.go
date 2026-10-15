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

func TestUnknownRequestFieldsAreRejected(t *testing.T) {
	dir := realTempDir(t)
	writeFile(t, filepath.Join(dir, "f.txt"), "one\n", 0644)
	for name, tool := range map[string]func(string, map[string]any) (Result, error){"read": Read, "edit": Edit, "write": Write} {
		t.Run(name, func(t *testing.T) {
			params := map[string]any{"path": "f.txt", "unexpected": "private-request-value"}
			if name == "write" {
				params["content"] = "changed"
			}
			if name == "edit" {
				params["version"] = readVersion(t, dir, "f.txt")
				params["edits"] = []any{map[string]any{"op": "replace", "anchor": FormatAnchor(1, "one"), "lines": []any{"changed"}}}
			}
			result, err := tool(dir, params)
			if err == nil || !strings.Contains(err.Error(), "unknown fields") || strings.Contains(err.Error(), "private-request-value") || result.Text != "" {
				t.Fatalf("unknown request fields were accepted or exposed: %v", err)
			}
			content, err := os.ReadFile(filepath.Join(dir, "f.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != "one\n" {
				t.Fatal("unknown request fields changed target content")
			}
		})
	}
}
