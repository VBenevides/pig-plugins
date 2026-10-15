package hashline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTruncatedSourceNeverAuthorizesReplacementOrDeletion(t *testing.T) {
	for _, op := range []string{"replace", "delete"} {
		t.Run(op, func(t *testing.T) {
			dir := realTempDir(t)
			engine := NewEngine()
			path := filepath.Join(dir, "f")
			line := strings.Repeat("😀", MaxLineChars/2+1)
			content := line + "\ntail\n"
			writeFile(t, path, content, 0644)
			read := engineRead(t, engine, dir, "f")
			if !strings.Contains(read.Text, "line truncated") {
				t.Fatal("test did not truncate source")
			}
			item := map[string]any{"op": op, "anchor": FormatAnchor(1, line)}
			if op == "replace" {
				item["lines"] = []any{"short"}
			}
			params := map[string]any{"path": "f", "version": read.Details["version"], "edits": []any{item}}
			for range 2 {
				if _, err := engine.EditContext(context.Background(), dir, params); err == nil || !strings.Contains(err.Error(), "not fully displayed") {
					t.Fatalf("truncated line authorized destructive edit: %v", err)
				}
				if _, err := engine.ReadContext(context.Background(), dir, map[string]any{"path": "f", "offset": 1, "limit": 1}); err != nil {
					t.Fatal(err)
				}
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != content {
				t.Fatalf("hidden source was changed: %v", err)
			}
			// Insertion is nondestructive and deliberately permitted at an anchored gap.
			params["edits"] = []any{map[string]any{"op": "insert_after", "anchor": FormatAnchor(1, line), "lines": []any{"new"}}}
			if _, err := engine.EditContext(context.Background(), dir, params); err != nil {
				t.Fatal(err)
			}
			after, err = os.ReadFile(path)
			if err != nil || string(after) != line+"\nnew\ntail\n" {
				t.Fatalf("insertion changed hidden source: %v", err)
			}
		})
	}
}
