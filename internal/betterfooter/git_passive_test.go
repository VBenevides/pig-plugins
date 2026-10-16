package betterfooter

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPassiveGitDoesNotExecuteHelpers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("helper fixtures use a POSIX shell")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable")
	}
	for _, mode := range []string{"clean", "process", "fsmonitor", "split-index", "unborn", "worktree"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			run := func(args ...string) {
				t.Helper()
				cmd := exec.Command("git", args...)
				cmd.Dir = dir
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("git %v: %v: %s", args, err, out)
				}
			}
			run("init", "--quiet")
			run("config", "user.email", "test@example.invalid")
			run("config", "user.name", "test")
			write := func(name, content string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			write("tracked", "before\n")
			run("add", ".")
			if mode != "unborn" {
				run("commit", "--quiet", "-m", "initial")
			}
			if mode == "worktree" {
				worktree := filepath.Join(t.TempDir(), "checkout")
				run("worktree", "add", "--quiet", "-b", "fixture", worktree)
				dir = worktree
			}
			if mode == "split-index" {
				run("update-index", "--split-index")
			}
			if mode == "clean" || mode == "process" {
				write(".gitattributes", "* filter=sentinel\n")
				run("config", "filter.sentinel."+mode, "echo invoked > sentinel; cat")
			} else {
				// Exercise both Git's fsmonitor and diff helper configuration.
				write("helper", "#!/bin/sh\necho invoked > sentinel\n")
				if err := os.Chmod(filepath.Join(dir, "helper"), 0o700); err != nil {
					t.Fatal(err)
				}
				run("config", "core.fsmonitor", filepath.Join(dir, "helper"))
				run("config", "diff.external", filepath.Join(dir, "helper"))
				run("config", "diff.sentinel.textconv", filepath.Join(dir, "helper"))
				write(".gitattributes", "* diff=sentinel\n")
			}
			indexCmd := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-path", "index")
			indexCmd.Dir = dir
			indexPath, err := indexCmd.Output()
			if err != nil {
				t.Fatal(err)
			}
			index := string(bytes.TrimSpace(indexPath))
			before, err := os.ReadFile(index)
			if err != nil {
				t.Fatal(err)
			}
			write("tracked", "after\n")
			write("untracked", "new\n")
			_, ok, err := ReadGitChanges(t.Context(), dir)
			if mode == "clean" || mode == "process" {
				if ok || !errors.Is(err, ErrPassiveGitCountsUnavailable) {
					t.Fatalf("filter conversion must be suppressed: ok=%v err=%v", ok, err)
				}
			} else if !ok || err != nil {
				t.Fatalf("passive counts: ok=%v err=%v", ok, err)
			}
			if _, err := os.Stat(filepath.Join(dir, "sentinel")); !os.IsNotExist(err) {
				t.Fatalf("configured helper executed: %v", err)
			}
			after, err := os.ReadFile(index)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("real index changed: %v", err)
			}
		})
	}
}
