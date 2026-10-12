package hashline

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestReadCoverageIncludesOnlyCompleteSourceLines(t *testing.T) {
	for _, test := range []struct {
		name, file, content string
		params              map[string]any
		want                [][2]int
	}{
		{"full", "f.txt", "one\ntwo\n", nil, [][2]int{{1, 2}}},
		{"empty", "f.txt", "", nil, [][2]int{}},
		{"explicit-range", "f.txt", "one\ntwo\nthree\n", map[string]any{"offset": 2, "limit": 1}, [][2]int{{2, 2}}},
		{"shortened-line", "f.txt", "one\n" + strings.Repeat("x", MaxLineChars+1) + "\nthree\n", nil, [][2]int{{1, 1}, {3, 3}}},
		{"shortened-unicode", "f.txt", strings.Repeat("😀", MaxLineChars/2+1) + "\ntwo\n", nil, [][2]int{{2, 2}}},
		{"outline", "f.go", "package example\nfunc Example() {\n" + strings.Repeat("// padding\n", LargeFileLines+1) + "}\n", nil, [][2]int{}},
		{"head-cap", "f.txt", strings.Repeat("line\n", LargeFileLines+1), nil, [][2]int{{1, LargeFileLines}}},
		{"range-line-cap", "f.txt", strings.Repeat("line\n", MaxRangeLines+1), map[string]any{"offset": 1, "limit": MaxRangeLines + 1}, [][2]int{{1, MaxRangeLines}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := realTempDir(t)
			writeFile(t, filepath.Join(dir, test.file), test.content, 0644)
			params := map[string]any{"path": test.file}
			for key, value := range test.params {
				params[key] = value
			}
			result, err := Read(dir, params)
			if err != nil {
				t.Fatal(err)
			}
			if got := result.Details["fullyDisplayedRanges"]; !reflect.DeepEqual(got, test.want) {
				t.Fatalf("fully displayed coverage = %v, want %v", got, test.want)
			}
		})
	}
}

func TestByteCappedCoverageExcludesOmittedAndShortenedLines(t *testing.T) {
	file := ParseFile("one\n" + strings.Repeat("x", MaxLineChars+1) + "\nthree\nfour\n")
	result := rangeResult(file, 1, 4, 4, MaxLineChars+70, "range")
	if got := result.Details["fullyDisplayedRanges"]; !reflect.DeepEqual(got, [][2]int{{1, 1}, {3, 3}}) {
		t.Fatalf("byte-capped coverage = %v", got)
	}
}
