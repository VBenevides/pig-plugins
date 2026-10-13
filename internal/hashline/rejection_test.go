package hashline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaleRejectionHintsAreBoundedAndDoNotAuthorize(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "f.txt")
	writeFile(t, path, "old\n", 0600)
	engine := NewEngine()
	read, err := engine.ReadContext(context.Background(), dir, map[string]any{"path": "f.txt"})
	if err != nil {
		t.Fatal(err)
	}
	current := strings.Repeat("synthetic-source-secret\n", 1000)
	writeFile(t, path, current, 0600)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"path": "f.txt", "version": read.Details["version"], "edits": []any{map[string]any{"op": "replace", "anchor": FormatAnchor(1, "old"), "lines": []any{"synthetic-replacement-secret"}}}}
	result, err := engine.EditContext(context.Background(), dir, params)
	var hints *RejectionContext
	if !errors.As(err, &hints) || len(hints.Anchors) > 5 || len(err.Error()) > 600 || ErrorCategoryOf(err) != CategoryStale || result.Text != "" {
		t.Fatalf("unbounded/wrong rejection: %v", err)
	}
	if strings.Contains(err.Error(), "synthetic-") || !strings.Contains(err.Error(), FormatAnchor(1, "synthetic-source-secret")) {
		t.Fatalf("payload disclosure or missing fresh anchor: %v", err)
	}
	params["version"] = ContentVersion([]byte(current))
	params["edits"].([]any)[0].(map[string]any)["anchor"] = hints.Anchors[0]
	if _, err := engine.EditContext(context.Background(), dir, params); err == nil {
		t.Fatal("hints authorized a mutation")
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != current || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("rejection changed target")
	}
}
