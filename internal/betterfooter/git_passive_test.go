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
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, mode := range []string{"clean", "process", "unused-clean", "unused-process", "untracked-filter", "fsmonitor", "split-index", "unborn", "worktree"} {
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
			blocked := mode == "clean" || mode == "process" || mode == "untracked-filter"
			if blocked || mode == "unused-clean" || mode == "unused-process" {
				pattern, filterMode := "*", mode
				switch mode {
				case "unused-clean":
					pattern, filterMode = "*.lfs", "clean"
				case "unused-process":
					pattern, filterMode = "*.lfs", "process"
				case "untracked-filter":
					pattern, filterMode = "untracked", "clean"
				}
				write(".gitattributes", pattern+" filter=sentinel\n")
				run("config", "filter.sentinel."+filterMode, "echo invoked > sentinel; cat")
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
			changes, ok, err := ReadGitChanges(t.Context(), dir)
			if blocked {
				if ok || !errors.Is(err, ErrPassiveGitCountsUnavailable) {
					t.Fatalf("filter conversion must be suppressed: ok=%v err=%v", ok, err)
				}
			} else if !ok || err != nil {
				t.Fatalf("passive counts: ok=%v err=%v", ok, err)
			}
			if mode == "unused-clean" || mode == "unused-process" {
				if changes != (GitChanges{Added: 3, Removed: 1, Dirty: true}) {
					t.Fatalf("unused filter changed counts: %+v", changes)
				}
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
