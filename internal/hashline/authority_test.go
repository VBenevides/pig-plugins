package hashline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDestructiveRangeRequiresEveryInteriorLineDisplayed(t *testing.T) {
	for _, op := range []string{"replace", "delete"} {
		t.Run(op, func(t *testing.T) {
			dir := realTempDir(t)
			engine := NewEngine()
			path := filepath.Join(dir, "f")
			original := "one\ntwo\nthree\n"
			writeFile(t, path, original, 0751)
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, offset := range []int{1, 3} {
				if _, err := engine.ReadContext(context.Background(), dir, map[string]any{"path": "f", "offset": offset, "limit": 1}); err != nil {
					t.Fatal(err)
				}
			}
			edit := map[string]any{"op": op, "anchor": FormatAnchor(1, "one"), "end": FormatAnchor(3, "three")}
			if op == "replace" {
				edit["lines"] = []any{"changed"}
			}
			params := map[string]any{"path": "f", "version": ContentVersion([]byte(original)), "edits": []any{edit}}
			if _, err := engine.EditContext(context.Background(), dir, params); err == nil || !strings.Contains(err.Error(), "not fully displayed") {
				t.Fatalf("unseen interior was accepted: %v", err)
			}
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != original || !os.SameFile(before, after) || before.Mode() != after.Mode() || len(tempFiles(t, dir)) != 0 {
				t.Fatal("unseen-range rejection modified original")
			}
			if _, err := engine.ReadContext(context.Background(), dir, map[string]any{"path": "f", "offset": 2, "limit": 1}); err != nil {
				t.Fatal(err)
			}
			if _, err := engine.EditContext(context.Background(), dir, params); err != nil {
				t.Fatalf("complete accumulated coverage was rejected: %v", err)
			}
		})
	}
}

func TestOutlineDoesNotAuthorizeDestructiveEdits(t *testing.T) {
	dir := realTempDir(t)
	engine := NewEngine()
	path := filepath.Join(dir, "f.go")
	content := "package example\nfunc Example() {\n" + strings.Repeat("// padding\n", LargeFileLines+1) + "}\n"
	writeFile(t, path, content, 0644)
	result := engineRead(t, engine, dir, "f.go")
	if result.Details["mode"] != "outline" {
		t.Fatal("test did not produce an outline")
	}
	_, err := engine.EditContext(context.Background(), dir, map[string]any{"path": "f.go", "version": result.Details["version"], "edits": []any{map[string]any{"op": "delete", "anchor": FormatAnchor(1, "package example")}}})
	if err == nil || !strings.Contains(err.Error(), "not fully displayed") {
		t.Fatalf("outline authorized destruction: %v", err)
	}
}
