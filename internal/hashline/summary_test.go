package hashline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditSummaryBoundsLongLinesAndBytes(t *testing.T) {
	for _, count := range []int{1, 80} {
		dir := realTempDir(t)
		path := filepath.Join(dir, "f.txt")
		writeFile(t, path, "short\n", 0644)
		version := readVersion(t, dir, "f.txt")
		lines := make([]any, count)
		for i := range lines {
			lines[i] = strings.Repeat("😀", 2500)
		}
		result, err := Edit(dir, map[string]any{"path": "f.txt", "version": version, "edits": []any{
			map[string]any{"op": "replace", "anchor": FormatAnchor(1, "short"), "lines": lines},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Text) > MaxRangeBytes || !strings.Contains(result.Text, "line truncated") {
			t.Fatalf("edit output not bounded/marked: %d bytes", len(result.Text))
		}
		if count == 80 && !strings.Contains(result.Text, "read again") {
			t.Fatal("byte cap lacks reread guidance")
		}
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if result.Details["version"] != ContentVersion(content) || strings.Contains(string(content), "line truncated") {
			t.Fatal("display truncation altered committed content or its version")
		}
	}
}
