package hashline

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestOutlineByteAndNameBounds(t *testing.T) {
	symbols := make([]Symbol, 300)
	for i := range symbols {
		symbols[i] = Symbol{Start: i + 1, End: i + 1, Kind: "func", Name: strings.Repeat("x", 10000)}
	}
	output := RenderOutline(symbols)
	if len(output) > LargeFileBytes || !strings.Contains(output, "truncated") || !strings.Contains(output, "read") {
		t.Fatalf("outline lacks bounds or guidance: %d bytes", len(output))
	}
	dir := realTempDir(t)
	var content strings.Builder
	for i := 0; i < 500; i++ {
		fmt.Fprintf(&content, "func f%d%s() {}\n", i, strings.Repeat("x", 1000))
	}
	writeFile(t, filepath.Join(dir, "f.go"), content.String(), 0644)
	result, err := Read(dir, map[string]any{"path": "f.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Text) > MaxRangeBytes || !strings.Contains(result.Text, "byte cap") || !strings.Contains(result.Text, "offset") {
		t.Fatalf("bare outline read is not bounded/actionable: %d bytes", len(result.Text))
	}
}
