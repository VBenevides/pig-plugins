package hashline

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type endlessText struct{ consumed int }

func (reader *endlessText) Read(buffer []byte) (int, error) {
	for i := range buffer {
		buffer[i] = 'x'
	}
	reader.consumed += len(buffer)
	return len(buffer), nil
}

func TestActualReadsStopAtByteLimit(t *testing.T) {
	reader := &endlessText{}
	content, err := readBounded(reader, 32)
	if !errors.Is(err, errReadLimit) || content != nil || reader.consumed != 33 {
		t.Fatalf("unbounded read: %d consumed, %d returned, %v", reader.consumed, len(content), err)
	}
	for _, size := range []int{0, 31, 32, 33, 100} {
		content, err := readBounded(strings.NewReader(strings.Repeat("x", size)), 32)
		if size <= 32 && (err != nil || len(content) != size) {
			t.Fatalf("valid boundary rejected: size %d, %v", size, err)
		}
		if size > 32 && (!errors.Is(err, errReadLimit) || content != nil) {
			t.Fatalf("oversized content accepted: size %d, %v", size, err)
		}
	}
	for _, limit := range []int64{-1, math.MaxInt64} {
		if _, err := readBounded(strings.NewReader(""), limit); err == nil {
			t.Fatalf("unsafe read limit accepted: %d", limit)
		}
	}
}

func TestFileGrowthAfterPreliminaryStatIsBounded(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "f.txt")
	writeFile(t, path, "small\n", 0644)
	before, err := os.Stat(path)
	if err != nil || before.Size() > 32 {
		t.Fatalf("initial stat should pass: %v", err)
	}
	writeFile(t, path, strings.Repeat("x", 100), 0644)
	content, err := readBoundedFile(path, 32)
	if !errors.Is(err, errReadLimit) || content != nil {
		t.Fatalf("growth after stat was accepted: %v", err)
	}
}
