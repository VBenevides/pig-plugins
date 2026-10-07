package fsutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileAtomicReplacesContentAndLeavesNoTemporaryFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	for _, content := range []string{"first", "second, longer than first"} {
		if err := WriteFileAtomic(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(path)
		if err != nil || string(got) != content {
			t.Fatalf("read back %q, %v; want %q", got, err, content)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", info.Mode().Perm())
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("directory holds %d entries, want only the target file", len(entries))
	}
}

func TestWriteFileAtomicKeepsOldContentWhenTargetIsADirectory(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFileAtomic(target, []byte("x"), 0o600); err == nil {
		t.Fatal("expected an error when the target is a directory")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temporary file leaked after failure: %v", entries)
	}
}

func TestReadJSONFileDistinguishesMissingFromInvalid(t *testing.T) {
	dir := t.TempDir()
	var v struct{ A int }

	found, err := ReadJSONFile(filepath.Join(dir, "missing.json"), &v)
	if found || err != nil {
		t.Fatalf("missing file: found=%v err=%v, want false, nil", found, err)
	}

	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	found, err = ReadJSONFile(bad, &v)
	if found || err == nil || !strings.Contains(err.Error(), bad) {
		t.Fatalf("invalid file: found=%v err=%v, want an error naming %s", found, err, bad)
	}

	good := filepath.Join(dir, "good.json")
	if err := WriteJSONFileAtomic(good, map[string]int{"A": 7}, 0o600); err != nil {
		t.Fatal(err)
	}
	found, err = ReadJSONFile(good, &v)
	if !found || err != nil || v.A != 7 {
		t.Fatalf("good file: found=%v err=%v v=%+v", found, err, v)
	}
}
