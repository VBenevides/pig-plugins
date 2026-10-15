//go:build linux

package hashline

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// After rename a durability failure is a committed result: the response must
// say so (never "nothing was written"), the target holds the new bytes, and
// the retained read authority is revoked.
func TestEnginePostReplacementFailureIsReportedAsCommitted(t *testing.T) {
	cause := errors.New("injected sync failure")
	original := directorySyncer
	directorySyncer = func(string) error { return cause }
	t.Cleanup(func() { directorySyncer = original })

	dir := realTempDir(t)
	path := filepath.Join(dir, "f.txt")
	writeFile(t, path, "one\ntwo\n", 0600)
	engine := NewEngine()
	read, err := engine.ReadContext(context.Background(), dir, map[string]any{"path": "f.txt"})
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"path": "f.txt", "version": read.Details["version"], "edits": []any{
		map[string]any{"op": "replace", "anchor": FormatAnchor(2, "two"), "lines": []any{"TWO"}}}}
	result, err := engine.EditContext(context.Background(), dir, params)
	var committed *CommittedError
	if !errors.As(err, &committed) || !errors.Is(err, cause) || ErrorCategoryOf(err) != CategoryCommitted ||
		strings.Contains(err.Error(), "nothing was") || result.Text != "" {
		t.Fatalf("post-replacement failure misreported: %+v %v", result, err)
	}
	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != "one\nTWO\n" {
		t.Fatalf("target does not hold committed bytes: %q %v", got, readErr)
	}
	if _, err := engine.EditContext(context.Background(), dir, params); ErrorCategoryOf(err) != CategoryStale {
		t.Fatalf("authority survived a committed mutation: %v", err)
	}
	_, err = engine.WriteContext(context.Background(), dir, map[string]any{"path": "new.txt", "content": "x\n"})
	if !errors.As(err, &committed) || !errors.Is(err, cause) {
		t.Fatalf("write failure misreported: %v", err)
	}
	if got, readErr := os.ReadFile(filepath.Join(dir, "new.txt")); readErr != nil || string(got) != "x\n" {
		t.Fatalf("created file missing: %q %v", got, readErr)
	}
}
