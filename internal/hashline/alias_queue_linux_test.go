//go:build linux

package hashline

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Equivalent spellings of one target must share one queue: every edit starts
// from the same read version, so exactly one may win and the rest are stale.
func TestAliasesShareOneMutationQueue(t *testing.T) {
	dir := realTempDir(t)
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0700); err != nil {
		t.Fatal(err)
	}
	canonical := filepath.Join(dir, "sub", "f.txt")
	writeFile(t, canonical, "base\n", 0600)
	if err := os.Symlink("sub/f.txt", filepath.Join(dir, "file-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("sub", filepath.Join(dir, "dir-link")); err != nil {
		t.Fatal(err)
	}
	spellings := []string{"sub/f.txt", canonical, "./sub/../sub/f.txt", "file-link", "dir-link/f.txt", "@sub/f.txt"}
	version := ContentVersion([]byte("base\n"))
	var group sync.WaitGroup
	var mutex sync.Mutex
	successes := 0
	for index, spelling := range spellings {
		group.Add(1)
		go func() {
			defer group.Done()
			_, err := Edit(dir, map[string]any{"path": spelling, "version": version, "edits": []any{
				map[string]any{"op": "replace", "anchor": FormatAnchor(1, "base"), "lines": []any{"writer-" + string(rune('a'+index))}}}})
			mutex.Lock()
			defer mutex.Unlock()
			if err == nil {
				successes++
			} else if !strings.Contains(err.Error(), "stale version") {
				t.Errorf("%s: unexpected failure: %v", spelling, err)
			}
		}()
	}
	group.Wait()
	content, err := os.ReadFile(canonical)
	if err != nil {
		t.Fatal(err)
	}
	if successes != 1 || !strings.HasPrefix(string(content), "writer-") || strings.Count(string(content), "\n") != 1 {
		t.Fatalf("lost or duplicated update: successes=%d content=%q", successes, content)
	}
	if leftovers := tempFiles(t, filepath.Join(dir, "sub")); len(leftovers) != 0 {
		t.Fatalf("leaked temporaries: %v", leftovers)
	}
}
