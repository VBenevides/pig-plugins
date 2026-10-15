package hashline

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExplicitOutsidePathsAndInternalAliasesPolicy(t *testing.T) {
	root := realTempDir(t)
	cwd := filepath.Join(root, "work")
	if err := os.Mkdir(cwd, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(root, "outside.txt")
	inside := filepath.Join(cwd, "inside.txt")
	writeFile(t, outside, "outside\n", 0600)
	writeFile(t, inside, "inside\n", 0600)
	if err := os.Symlink(inside, filepath.Join(cwd, "internal-alias")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(cwd, "external-alias")); err != nil {
		t.Fatal(err)
	}
	for _, it := range []struct {
		path    string
		allowed bool
	}{{outside, true}, {"../outside.txt", true}, {"inside.txt", true}, {"internal-alias", true}, {"external-alias", false}} {
		t.Run(it.path, func(t *testing.T) {
			_, err := TakeSnapshot(ResolveToolPath(it.path, cwd), cwd, MaxEditBytes)
			if (err == nil) != it.allowed {
				t.Fatalf("snapshot policy: allowed=%v err=%v", it.allowed, err)
			}
			_, err = Write(cwd, map[string]any{"path": it.path, "content": "same explicit write\n"})
			if (err == nil) != it.allowed {
				t.Fatalf("write policy: allowed=%v err=%v", it.allowed, err)
			}
		})
	}
}
