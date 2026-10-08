package repoexcludes

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func git(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func repository(t *testing.T) (string, string) {
	t.Helper()
	cwd := t.TempDir()
	git(t, cwd, "init", "--quiet")
	return cwd, filepath.Join(cwd, ".git", "info", "exclude")
}

func read(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func write(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestRepeatedStartupPreservesBytesAndMode(t *testing.T) {
	for _, initial := range []string{"", "# prior\r\ncustom/\r\n.ouro/\r\n", "# no newline", ".agent-work/\n.ouro/\n.curator/"} {
		t.Run(initial, func(t *testing.T) {
			cwd, path := repository(t)
			write(t, path, []byte(initial), 0o640)
			ignore := filepath.Join(cwd, ".gitignore")
			write(t, ignore, []byte("tracked-ignore-sentinel\n"), 0o644)
			git(t, cwd, "add", ".gitignore")
			git(t, cwd, "commit", "--quiet", "-m", "fixture")
			firstChanged, err := Ensure(cwd)
			if err != nil {
				t.Fatal(err)
			}
			first := read(t, path)
			if !bytes.HasPrefix(first, []byte(initial)) {
				t.Fatalf("prior bytes changed: %q", first)
			}
			if firstChanged != (initial != ".agent-work/\n.ouro/\n.curator/") {
				t.Fatalf("wrong changed result: %v", firstChanged)
			}
			for range 3 {
				changed, err := Ensure(cwd)
				if err != nil || changed || !bytes.Equal(first, read(t, path)) {
					t.Fatalf("repeat startup: changed=%v error=%v", changed, err)
				}
			}
			for _, pattern := range patterns {
				count := 0
				for line := range strings.SplitSeq(string(first), "\n") {
					if strings.TrimSuffix(line, "\r") == pattern {
						count++
					}
				}
				if count != 1 {
					t.Fatalf("%s appears %d times in %q", pattern, count, first)
				}
			}
			stat, err := os.Stat(path)
			if err != nil || stat.Mode().Perm() != 0o640 {
				t.Fatalf("permissions: %v, %v", stat, err)
			}
			if string(read(t, ignore)) != "tracked-ignore-sentinel\n" || git(t, cwd, "diff", "HEAD", "--", ".gitignore") != "" {
				t.Fatal("tracked ignore changed")
			}
		})
	}
}

func TestMissingFileAndInfoDirectory(t *testing.T) {
	cwd, path := repository(t)
	if err := os.RemoveAll(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	changed, err := Ensure(cwd)
	if err != nil || !changed {
		t.Fatalf("missing info: %v, %v", changed, err)
	}
	stat, err := os.Stat(path)
	if err != nil || stat.Mode().Perm() != 0o600 {
		t.Fatalf("new permissions: %v, %v", stat, err)
	}
	if string(read(t, path)) != ".agent-work/\n.ouro/\n.curator/\n" {
		t.Fatal("wrong new exclude entries")
	}
}

func TestNestedAndLinkedWorktree(t *testing.T) {
	cwd, path := repository(t)
	git(t, cwd, "commit", "--quiet", "--allow-empty", "-m", "fixture")
	linked := filepath.Join(t.TempDir(), "linked")
	git(t, cwd, "worktree", "add", "--quiet", "-b", "linked", linked)
	nested := filepath.Join(linked, "sub", "dir")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, path, []byte("existing-no-newline"), 0o600)
	if changed, err := Ensure(nested); err != nil || !changed {
		t.Fatalf("linked startup: %v, %v", changed, err)
	}
	if string(read(t, path)) != "existing-no-newline\n.agent-work/\n.ouro/\n.curator/\n" {
		t.Fatal("common Git exclude not updated")
	}
	if changed, err := Ensure(cwd); err != nil || changed {
		t.Fatalf("common startup: %v, %v", changed, err)
	}
	if _, err := os.Stat(filepath.Join(cwd, ".git", "worktrees", "linked", "info")); !os.IsNotExist(err) {
		t.Fatalf("private worktree info created: %v", err)
	}
	for _, pattern := range patterns {
		git(t, linked, "check-ignore", "--", pattern+"probe")
	}
}

func TestExternalGitDirectory(t *testing.T) {
	cwd := t.TempDir()
	external := filepath.Join(t.TempDir(), "external-git")
	git(t, cwd, "init", "--quiet", "--separate-git-dir", external)
	if changed, err := Ensure(cwd); err != nil || !changed {
		t.Fatalf("external startup: %v, %v", changed, err)
	}
	if !bytes.Contains(read(t, filepath.Join(external, "info", "exclude")), []byte(".curator/\n")) {
		t.Fatal("external Git exclude not updated")
	}
}

func TestNonRepositoryIsQuiet(t *testing.T) {
	cwd := t.TempDir()
	changed, err := Ensure(cwd)
	if err != nil || changed {
		t.Fatalf("nonrepository: %v, %v", changed, err)
	}
	entries, err := os.ReadDir(cwd)
	if err != nil || len(entries) != 0 {
		t.Fatalf("nonrepository created files: %v, %v", entries, err)
	}
}

func TestInvalidGitAndFileFailures(t *testing.T) {
	t.Run("invalid Git metadata", func(t *testing.T) {
		cwd := t.TempDir()
		write(t, filepath.Join(cwd, ".git"), []byte("gitdir: /no/such/git/directory"), 0o600)
		if changed, err := Ensure(cwd); err == nil || changed {
			t.Fatalf("invalid metadata silently accepted: %v, %v", changed, err)
		}
	})
	for _, kind := range []string{"symlink", "info symlink", "directory", "oversize"} {
		t.Run(kind, func(t *testing.T) {
			cwd, path := repository(t)
			outside := filepath.Join(t.TempDir(), "untouched")
			write(t, outside, []byte("sentinel"), 0o600)
			switch kind {
			case "symlink":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(outside, path); err != nil {
					t.Fatal(err)
				}
			case "info symlink":
				if err := os.RemoveAll(filepath.Dir(path)); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Dir(outside), filepath.Dir(path)); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			case "oversize":
				write(t, path, bytes.Repeat([]byte{'x'}, maxExcludeBytes+1), 0o600)
			}
			if changed, err := Ensure(cwd); err == nil || changed {
				t.Fatalf("bad file silently accepted: %v, %v", changed, err)
			}
			if string(read(t, outside)) != "sentinel" {
				t.Fatal("outside file changed")
			}
			if _, err := os.Lstat(filepath.Join(filepath.Dir(path), "exclude.lock")); !os.IsNotExist(err) {
				t.Fatalf("failed update left a lock: %v", err)
			}
		})
	}
}

func TestLockTimeoutPreservesExistingLockAndExclude(t *testing.T) {
	_, path := repository(t)
	write(t, path, []byte("original"), 0o600)
	lockPath := path + ".lock"
	write(t, lockPath, []byte("another writer"), 0o600)
	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if changed, err := appendLocked(ctx, root, dir); err == nil || changed {
		t.Fatalf("lock timeout silently accepted: %v, %v", changed, err)
	}
	if string(read(t, lockPath)) != "another writer" || string(read(t, path)) != "original" {
		t.Fatal("lock timeout modified another writer's files")
	}
}

func TestConcurrentWriters(t *testing.T) {
	cwd, path := repository(t)
	write(t, path, []byte("original"), 0o600)
	var wg sync.WaitGroup
	results := make(chan error, 16)
	for range 16 {
		wg.Go(func() { _, err := Ensure(cwd); results <- err })
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	if string(read(t, path)) != "original\n.agent-work/\n.ouro/\n.curator/\n" {
		t.Fatalf("concurrent writers lost or duplicated entries: %q", read(t, path))
	}
}

func TestOnlyExactEntriesCount(t *testing.T) {
	initial := "# .agent-work/\n!.ouro/\n .curator/\n.agent-work/nested\n"
	want := initial + ".agent-work/\n.ouro/\n.curator/\n"
	if got := string(appendMissing([]byte(initial))); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWritePermissionFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions")
	}
	cwd, path := repository(t)
	write(t, path, []byte("original"), 0o600)
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755)
	if changed, err := Ensure(cwd); err == nil || changed {
		t.Fatalf("write permission failure silently accepted: %v, %v", changed, err)
	}
	if string(read(t, path)) != "original" {
		t.Fatal("failed write changed prior bytes")
	}
}
