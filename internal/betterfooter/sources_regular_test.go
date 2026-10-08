//go:build unix || windows

package betterfooter

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadCappedRegularAndMissingManifest(t *testing.T) {
	dir := t.TempDir()
	if version, err := ReadProjectVersion(dir); err != nil || version != "" {
		t.Fatalf("missing manifest: version=%q err=%v", version, err)
	}
	path := filepath.Join(dir, "package.json")
	if _, err := readCapped(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing-file identity lost: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"version":"1.2.3"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if version, err := ReadProjectVersion(dir); err != nil || version != "v1.2.3" {
		t.Fatalf("regular manifest: version=%q err=%v", version, err)
	}
	if _, err := readCapped(dir); !errors.Is(err, errNotRegularFile) {
		t.Fatalf("directory accepted: %v", err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat(" ", MaxResponseBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, err := readCapped(path); data != nil || err == nil || !strings.Contains(err.Error(), "exceeds 1 MiB") {
		t.Fatalf("oversize regular file accepted: data=%d err=%v", len(data), err)
	}
}
