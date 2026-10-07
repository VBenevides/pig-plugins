package pigtest

import (
	"os"
	"path/filepath"
	"testing"
)

// Golden compares got with testdata/<name> next to the test. The files are captured from the original
// TypeScript implementation (see testdata/README.md in each extension), so a mismatch is a parity failure.
// There is deliberately no update flag: regenerating a golden from the Go port would erase the parity check.
func Golden(t testing.TB, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v", path, err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch for %s\n--- want\n%s\n--- got\n%s", path, want, got)
	}
}
