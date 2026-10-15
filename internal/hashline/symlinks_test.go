package hashline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParentDirectorySymlinkPolicy(t *testing.T) {
	dir := realTempDir(t)
	outside := realTempDir(t)
	inside := filepath.Join(dir, "real")
	if err := os.Mkdir(inside, 0755); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{outside, inside} {
		writeFile(t, filepath.Join(target, "f.txt"), "original\n", 0644)
	}
	for name, target := range map[string]string{"escape": outside, "internal": inside} {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(name, filepath.Join(dir, name+"-nested")); err != nil {
			t.Fatal(err)
		}
	}
	for _, alias := range []string{"escape", "escape-nested", "internal", "internal-nested"} {
		t.Run(alias, func(t *testing.T) {
			target := inside
			if strings.HasPrefix(alias, "escape") {
				target = outside
			}
			path := filepath.Join(target, "f.txt")
			writeFile(t, path, "original\n", 0644)
			version := readVersion(t, dir, filepath.Join(alias, "f.txt"))
			_, err := Edit(dir, map[string]any{"path": filepath.Join(alias, "f.txt"), "version": version, "edits": []any{
				map[string]any{"op": "replace", "anchor": FormatAnchor(1, "original"), "lines": []any{"updated"}},
			}})
			want := "updated\n"
			if target == outside {
				want = "original\n"
				if err == nil || !strings.Contains(err.Error(), "outside the working directory") {
					t.Fatalf("want symlink escape rejection: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			got, readErr := os.ReadFile(path)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if string(got) != want {
				t.Fatalf("target content %q, want %q", got, want)
			}
			info, statErr := os.Lstat(filepath.Join(dir, alias))
			if statErr != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Fatalf("alias was replaced: %v", statErr)
			}
		})
	}
}
