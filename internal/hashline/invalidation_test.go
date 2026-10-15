package hashline

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMutationsRevokeOldReadsAndRereadPublishesCurrentAuthority(t *testing.T) {
	for _, operation := range []string{"write", "edit"} {
		t.Run(operation, func(t *testing.T) {
			dir := realTempDir(t)
			engine := NewEngine()
			path := filepath.Join(dir, "f")
			writeFile(t, path, "one\n", 0644)
			old := engineRead(t, engine, dir, "f")
			var result Result
			var err error
			if operation == "write" {
				result, err = engine.WriteContext(context.Background(), dir, map[string]any{"path": "f", "content": "changed\n"})
			} else {
				result, err = engine.EditContext(context.Background(), dir, map[string]any{"path": "f", "version": old.Details["version"], "edits": []any{map[string]any{"op": "replace", "anchor": FormatAnchor(1, "one"), "lines": []any{"changed"}}}})
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(engine.reads) != 0 || engine.bytes != 0 {
				t.Fatal("successful mutation retained obsolete authority")
			}
			if _, err := engineDelete(engine, dir, "f", old.Details["version"].(string)); err == nil {
				t.Fatal("old read authorized another mutation")
			}
			current := engineRead(t, engine, dir, "f")
			if current.Details["version"] != result.Details["version"] {
				t.Fatal("reread and committed result disagree on current version")
			}
			if _, err := engine.EditContext(context.Background(), dir, map[string]any{"path": "f", "version": current.Details["version"], "edits": []any{map[string]any{"op": "delete", "anchor": FormatAnchor(1, "changed")}}}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNoOpsAndRejectedRequestsPreserveReadAuthority(t *testing.T) {
	dir := realTempDir(t)
	engine := NewEngine()
	path := filepath.Join(dir, "f")
	writeFile(t, path, "one\n", 0644)
	read := engineRead(t, engine, dir, "f")
	if _, err := engine.WriteContext(context.Background(), dir, map[string]any{"path": "f", "content": "one\n"}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.EditContext(context.Background(), dir, map[string]any{"path": "f", "version": read.Details["version"], "edits": []any{map[string]any{"op": "replace", "anchor": FormatAnchor(1, "one"), "lines": []any{"one"}}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.EditContext(context.Background(), dir, map[string]any{"path": "f", "version": read.Details["version"], "edits": []any{map[string]any{"op": "replace", "anchor": FormatAnchor(1, "one")}}}); err == nil {
		t.Fatal("invalid request accepted")
	}
	if _, err := engineDelete(engine, dir, "f", read.Details["version"].(string)); err != nil {
		t.Fatalf("no-op/rejection revoked valid authority: %v", err)
	}
}
