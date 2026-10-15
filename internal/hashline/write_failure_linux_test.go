//go:build linux

package hashline

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A real kernel write failure (file-size limit => EFBIG) during preparation
// must surface with its errno, leave the target and directory untouched, and
// never be reported as committed.
func TestPreparationWriteFailureIsObservableAndLeavesTargetUntouched(t *testing.T) {
	// RLIMIT_FSIZE is process-wide. Isolate it from Go's buffered test-cache log,
	// which can otherwise flush during the limit and retain a sticky EFBIG error.
	const childFlag = "PIG_HASHLINE_FILE_LIMIT_CHILD"
	if os.Getenv(childFlag) != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPreparationWriteFailureIsObservableAndLeavesTargetUntouched$")
		cmd.Env = append(os.Environ(), childFlag+"=1")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated preparation failure test: %v\n%s", err, output)
		}
		return
	}
	dir := realTempDir(t)
	path := filepath.Join(dir, "f.txt")
	writeFile(t, path, "original\n", 0600)
	snapshot, err := TakeSnapshot(path, dir, MaxEditBytes)
	if err != nil {
		t.Fatal(err)
	}
	var previous syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &previous); err != nil {
		t.Fatal(err)
	}
	signal.Ignore(syscall.SIGXFSZ)
	t.Cleanup(func() { signal.Reset(syscall.SIGXFSZ) })
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &syscall.Rlimit{Cur: 16, Max: previous.Max}); err != nil {
		t.Skipf("cannot lower file-size limit: %v", err)
	}
	err = ReplaceAtomically(snapshot, strings.Repeat("x", 4096)+"\n", nil)
	if restoreErr := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &previous); restoreErr != nil {
		t.Fatal(restoreErr)
	}
	var committed *CommittedError
	if err == nil || !errors.Is(err, syscall.EFBIG) || errors.As(err, &committed) || !strings.Contains(err.Error(), "write temporary file") {
		t.Fatalf("write failure hidden or misreported: %v", err)
	}
	if got, readErr := os.ReadFile(path); readErr != nil || string(got) != "original\n" {
		t.Fatalf("target changed: %q %v", got, readErr)
	}
	if leftovers := tempFiles(t, dir); len(leftovers) != 0 {
		t.Fatalf("leaked preparation files: %v", leftovers)
	}
}
