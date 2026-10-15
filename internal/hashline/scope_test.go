package hashline

import (
	"context"
	"path/filepath"
	"testing"
)

func TestReadAuthorityDoesNotCrossFilesOrSessions(t *testing.T) {
	dir := realTempDir(t)
	engine := NewEngine()
	for _, name := range []string{"a", "b"} {
		writeFile(t, filepath.Join(dir, name), "one\n", 0644)
	}
	first := WithScope(context.Background(), "session-a")
	second := WithScope(context.Background(), "session-b")
	read, err := engine.ReadContext(first, dir, map[string]any{"path": "a"})
	if err != nil {
		t.Fatal(err)
	}
	version := read.Details["version"]
	params := map[string]any{"path": "b", "version": version, "edits": []any{map[string]any{"op": "delete", "anchor": FormatAnchor(1, "one")}}}
	if _, err := engine.EditContext(first, dir, params); err == nil {
		t.Fatal("identical unread file accepted another file's authority")
	}
	params["path"] = "a"
	if _, err := engine.EditContext(second, dir, params); err == nil {
		t.Fatal("unread session accepted another session's authority")
	}
	if _, err := engine.EditContext(first, dir, params); err == nil {
		t.Fatal("switching back restored revoked session authority")
	}
	if _, err := engine.ReadContext(first, dir, map[string]any{"path": "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.EditContext(first, dir, params); err != nil {
		t.Fatal(err)
	}
}
