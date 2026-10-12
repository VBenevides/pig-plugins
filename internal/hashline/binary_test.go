package hashline

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestNULAtEverySamplingBoundaryIsBinary(t *testing.T) {
	for _, offset := range []int{0, 8191, 8192, 8193, 20000} {
		t.Run(fmt.Sprint(offset), func(t *testing.T) {
			content := strings.Repeat("x", offset) + "\x00\ntail\n"
			if !IsBinary([]byte(content)) {
				t.Fatal("late NUL was classified as text")
			}
			dir := realTempDir(t)
			path := filepath.Join(dir, "f.txt")
			writeFile(t, path, content, 0644)
			if _, err := Read(dir, map[string]any{"path": "f.txt"}); err == nil || !strings.Contains(err.Error(), "binary") {
				t.Fatalf("NUL-containing read must reject: %v", err)
			}
			if _, err := TakeSnapshot(path, dir, MaxEditBytes); err == nil || !strings.Contains(err.Error(), "binary") {
				t.Fatalf("NUL-containing edit snapshot must reject: %v", err)
			}
		})
	}
}
