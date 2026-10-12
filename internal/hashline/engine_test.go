package hashline

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func engineRead(t *testing.T, engine *Engine, dir, path string) Result {
	t.Helper()
	result, err := engine.ReadContext(context.Background(), dir, map[string]any{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func engineDelete(engine *Engine, dir, path, version string) (Result, error) {
	return engine.EditContext(context.Background(), dir, map[string]any{"path": path, "version": version, "edits": []any{map[string]any{"op": "delete", "anchor": FormatAnchor(1, "one")}}})
}

func TestRetainedReadsAreBoundedAndEvictionRevokesAuthority(t *testing.T) {
	for _, test := range []struct {
		name         string
		files, bytes int
	}{
		{"file-count", 2, 4096}, {"byte-budget", 64, 180}, {"oversized-snapshot", 64, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := realTempDir(t)
			engine := newEngine(test.files, test.bytes)
			for _, name := range []string{"a", "b", "c"} {
				writeFile(t, filepath.Join(dir, name), "one\n", 0644)
				engineRead(t, engine, dir, name)
			}
			if len(engine.reads) > test.files || engine.bytes > test.bytes {
				t.Fatalf("retention exceeded limits: %d files, %d bytes", len(engine.reads), engine.bytes)
			}
			_, err := engineDelete(engine, dir, "a", ContentVersion([]byte("one\n")))
			if err == nil || !strings.Contains(err.Error(), "expired read authority") {
				t.Fatalf("evicted read authorized mutation: %v", err)
			}
			content, err := os.ReadFile(filepath.Join(dir, "a"))
			if err != nil || string(content) != "one\n" {
				t.Fatal("eviction rejection changed the file")
			}
			if test.bytes > 1 {
				engineRead(t, engine, dir, "a")
				if _, err := engineDelete(engine, dir, "a", ContentVersion([]byte("one\n"))); err != nil {
					t.Fatalf("reread did not restore authority: %v", err)
				}
			}
		})
	}
}

func TestRetainedReadsKeepOnlyLatestVersionPerFile(t *testing.T) {
	dir := realTempDir(t)
	engine := NewEngine()
	path := filepath.Join(dir, "f")
	writeFile(t, path, "one\n", 0644)
	old := engineRead(t, engine, dir, "f")
	writeFile(t, path, "one\ntwo\n", 0644)
	current := engineRead(t, engine, dir, "f")
	if len(engine.reads) != 1 {
		t.Fatal("old versions accumulated")
	}
	if _, err := engineDelete(engine, dir, "f", old.Details["version"].(string)); err == nil {
		t.Fatal("old version retained authority")
	}
	if _, err := engineDelete(engine, dir, "f", current.Details["version"].(string)); err != nil {
		t.Fatal(err)
	}
}
