package hashline

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Every output cap must say that it truncated and how to continue. The
// non-UTF-8 path used to emit "lines 1-0 ... offset=1" for an over-cap line,
// an endless continuation loop.
func TestNonUTF8ReadsMakeProgressAndMarkTruncation(t *testing.T) {
	dir := realTempDir(t)
	var content strings.Builder
	content.WriteString("\xff" + strings.Repeat("a", 100000) + "\n")
	for i := 0; i < 3000; i++ {
		fmt.Fprintf(&content, "row \xff %d\n", i)
	}
	writeFile(t, filepath.Join(dir, "n.txt"), content.String(), 0600)
	next, requests := 1, 0
	for next <= 3001 {
		requests++
		if requests > 10 {
			t.Fatalf("continuation does not converge at offset %d", next)
		}
		result, err := Read(dir, map[string]any{"path": "n.txt", "offset": next})
		if err != nil {
			t.Fatal(err)
		}
		if next == 1 && (!strings.Contains(result.Text, "line truncated") || !strings.Contains(result.Text, "shortened for display")) {
			t.Fatal("shortened line is not marked")
		}
		to := result.Details["to"].(int)
		if to < next {
			t.Fatalf("response made no progress: %v", result.Details)
		}
		match := continuationPattern.FindStringSubmatch(result.Text)
		if to == 3001 {
			if match != nil {
				t.Fatal("complete delivery advertises continuation")
			}
			return
		}
		if match == nil || match[1] != strconv.Itoa(to+1) {
			t.Fatalf("continuation does not name line %d: %v", to+1, match)
		}
		next = to + 1
	}
}
