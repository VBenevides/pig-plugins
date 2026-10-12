package hashline

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func readVersion(t *testing.T, dir, path string) string {
	t.Helper()
	result, err := Read(dir, map[string]any{"path": path})
	if err != nil {
		t.Fatal(err)
	}
	version, ok := result.Details["version"].(string)
	if !ok || len(version) != 64 || !strings.Contains(result.Text, "Version: "+version) {
		t.Fatalf("read did not expose a full version token: %+v", result)
	}
	return version
}

func TestEditRequiresReadVersion(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "f.txt")
	writeFile(t, path, "one\ntwo\n", 0644)
	ops := []any{map[string]any{"op": "replace", "anchor": FormatAnchor(1, "one"), "lines": []any{"ONE"}}}
	for _, version := range []any{nil, 123, "", "not-a-version", strings.Repeat("z", 64)} {
		_, err := Edit(dir, map[string]any{"path": "f.txt", "version": version, "edits": ops})
		if err == nil || !strings.Contains(err.Error(), "version") {
			t.Fatalf("version %v: expected rejection, got %v", version, err)
		}
	}
	version := readVersion(t, dir, "f.txt")
	result, err := Edit(dir, map[string]any{"path": "f.txt", "version": version, "edits": ops})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(updated) != "ONE\ntwo\n" || result.Details["version"] != ContentVersion(updated) {
		t.Fatalf("committed version/content mismatch: %q %+v", updated, result)
	}
	if result.Details["version"] == version {
		t.Fatal("successful edit retained the old version")
	}
}

func TestMultipleReadVersionsRemainUnambiguous(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "f.txt")
	writeFile(t, path, "one\ntwo\n", 0644)
	oldVersion := readVersion(t, dir, "f.txt")
	writeFile(t, path, "one\nexternal\n", 0644)
	currentVersion := readVersion(t, dir, "f.txt")
	ops := []any{map[string]any{"op": "replace", "anchor": FormatAnchor(1, "one"), "lines": []any{"ONE"}}}
	_, err := Edit(dir, map[string]any{"path": "f.txt", "version": oldVersion, "edits": ops})
	if err == nil || !strings.Contains(err.Error(), "stale version") {
		t.Fatalf("old read must reject despite a newer read: %v", err)
	}
	if _, err := Edit(dir, map[string]any{"path": "f.txt", "version": currentVersion, "edits": ops}); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentCallersCannotOverwriteStaleVersion(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "f.txt")
	writeFile(t, path, "one\ntwo\n", 0644)
	version := readVersion(t, dir, "f.txt")
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, text := range []string{"ONE", "OTHER"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = Edit(dir, map[string]any{"path": "f.txt", "version": version, "edits": []any{
				map[string]any{"op": "replace", "anchor": FormatAnchor(1, "one"), "lines": []any{text}},
			}})
		}()
	}
	wg.Wait()
	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
		} else if !strings.Contains(err.Error(), "stale version") {
			t.Fatalf("unexpected rejection: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("want exactly one committed writer, got %d", successes)
	}
}
