package betterfooter

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestGitChangesPreservesAddFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test Git executable uses a POSIX shell")
	}
	for _, scenario := range []string{"exit", "deadline"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			add := "echo SECRET-PAYLOAD >&2; exit 42"
			if scenario == "deadline" {
				add = "exec sleep 10"
			}
			// Simulate a repository with no HEAD, then fail only the add operation.
			script := "#!/bin/sh\ncase \"$1\" in\nrev-parse) if [ \"$2\" = --show-toplevel ]; then pwd; echo .git/index; else exit 1; fi ;;\nread-tree) exit 0 ;;\nadd) " + add + " ;;\nesac\n"
			if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			ctx := t.Context()
			if scenario == "deadline" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, time.Second)
				defer cancel()
			}
			_, ok, err := ReadGitChanges(ctx, dir)
			if ok || err == nil || !strings.Contains(err.Error(), "temporary index") {
				t.Fatalf("ok=%v err=%v", ok, err)
			}
			if strings.Contains(err.Error(), "SECRET-PAYLOAD") {
				t.Fatal("Git stderr leaked into diagnostic")
			}
			if scenario == "deadline" {
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("deadline cause discarded: %v", err)
				}
			} else {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 42 {
					t.Fatalf("Git exit status discarded: %v", err)
				}
			}
		})
	}
}
