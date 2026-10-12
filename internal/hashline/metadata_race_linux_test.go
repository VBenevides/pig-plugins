//go:build linux

package hashline

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestMetadataChangesDuringPreparationRejectCommit(t *testing.T) {
	changes := map[string]func(string) error{
		"read-only":  func(path string) error { return os.Chmod(path, 0444) },
		"privileged": func(path string) error { return os.Chmod(path, 0755|fs.ModeSetuid) },
		"executable": func(path string) error { return os.Chmod(path, 0755) },
		"hard-link":  func(path string) error { return os.Link(path, path+".alias") },
		"attributes": func(path string) error { return syscall.Setxattr(path, "user.concurrent", []byte("private"), 0) },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			dir := realTempDir(t)
			path := filepath.Join(dir, "f.txt")
			writeFile(t, path, "original\n", 0644)
			snapshot, err := TakeSnapshot(path, dir, MaxEditBytes)
			if err != nil {
				t.Fatal(err)
			}
			var changedInfo fs.FileInfo
			err = ReplaceAtomically(snapshot, "changed\n", func() {
				if err := change(path); err != nil {
					t.Fatal(err)
				}
				var statErr error
				changedInfo, statErr = os.Stat(path)
				if statErr != nil {
					t.Fatal(statErr)
				}
			})
			if err == nil || strings.Contains(err.Error(), "private") {
				t.Fatalf("expected safe metadata rejection, got %v", err)
			}
			after, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(changedInfo, after) || changedInfo.Mode() != after.Mode() || string(content) != "original\n" {
				t.Fatal("rejection overwrote externally changed metadata/content")
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.Contains(entry.Name(), ".pig-plugins-") {
					t.Fatal("rejection left temporary content")
				}
			}
		})
	}
}
