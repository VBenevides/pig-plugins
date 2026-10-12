package hashline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaleVersionsNeverRelocateOrMerge(t *testing.T) {
	for name, current := range map[string]string{
		"insertion": "prefix\nfirst\ntarget\nlast\n",
		"movement":  "target\nfirst\nlast\n",
		"change":    "first\ntarget\nexternal\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := realTempDir(t)
			path := filepath.Join(dir, "f.txt")
			writeFile(t, path, "first\ntarget\nlast\n", 0644)
			version := readVersion(t, dir, "f.txt")
			writeFile(t, path, current, 0640)
			before, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			result, err := Edit(dir, map[string]any{"path": "f.txt", "version": version, "edits": []any{
				map[string]any{"op": "replace", "anchor": FormatAnchor(2, "target"), "lines": []any{"replacement"}},
			}})
			if err == nil || !strings.Contains(err.Error(), "stale version") || result.Text != "" {
				t.Fatalf("want strict rejection without a success result: %+v %v", result, err)
			}
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != current || !os.SameFile(before, after) || after.Mode() != before.Mode() {
				t.Fatal("stale request modified content, inode, or permissions")
			}
			if len(tempFiles(t, dir)) != 0 {
				t.Fatal("stale request created a preparation artifact")
			}
		})
	}
}
