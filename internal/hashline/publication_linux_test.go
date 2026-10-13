//go:build linux

package hashline

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Success output (fresh anchors, version, "changed") must exist only after the
// rename; every failure before it yields an empty Result and unchanged bytes.
func TestFreshAnchorsPublishedOnlyAfterCommit(t *testing.T) {
	for _, scenario := range []string{"concurrent-write", "rename-denied"} {
		t.Run(scenario, func(t *testing.T) {
			dir := realTempDir(t)
			path := filepath.Join(dir, "f.txt")
			writeFile(t, path, "one\ntwo\n", 0600)
			version := ContentVersion([]byte("one\ntwo\n"))
			edits := []any{map[string]any{"op": "replace", "anchor": FormatAnchor(2, "two"), "lines": []any{"TWO"}}}
			expected := "one\ntwo\n"
			hook := func() {
				switch scenario {
				case "concurrent-write":
					expected = "one\nexternal\n"
					writeFile(t, path, expected, 0600)
				case "rename-denied":
					if err := os.Chmod(dir, 0500); err != nil {
						t.Fatal(err)
					}
				}
			}
			t.Cleanup(func() { _ = os.Chmod(dir, 0700) })
			result, err := editLocked(dir, path, version, edits, hook)
			if err == nil || result.Text != "" || result.Details != nil {
				t.Fatalf("failure published a result: %+v %v", result, err)
			}
			if strings.Contains(err.Error(), "Applied") {
				t.Fatalf("failure claimed application: %v", err)
			}
			_ = os.Chmod(dir, 0700)
			got, readErr := os.ReadFile(path)
			if readErr != nil || string(got) != expected {
				t.Fatalf("target changed: %q %v", got, readErr)
			}
			leftovers := tempFiles(t, dir)
			if scenario == "rename-denied" {
				// The same denied directory blocks cleanup; that failure must be observable.
				if len(leftovers) != 1 || !strings.Contains(err.Error(), "remove temporary file") || !errors.Is(err, os.ErrPermission) {
					t.Fatalf("cleanup failure hidden: %v %v", leftovers, err)
				}
			} else if len(leftovers) != 0 {
				t.Fatalf("leaked preparation files: %v", leftovers)
			}
			var committed *CommittedError
			if errors.As(err, &committed) {
				t.Fatalf("pre-commit failure reported as committed: %v", err)
			}
		})
	}
}
