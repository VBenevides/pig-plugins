//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd

package betterfooter

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestReadCappedRejectsSpecialFiles(t *testing.T) {
	// Isolate each read in a killable child: a regression to blocking FIFO open
	// or device read must fail the test, not hang the test process or worker.
	if path := os.Getenv("BETTER_FOOTER_SPECIAL_FILE_TEST"); path != "" {
		data, err := readCapped(path)
		if data != nil || !errors.Is(err, errNotRegularFile) {
			t.Fatalf("special file accepted: data=%d err=%v", len(data), err)
		}
		if filepath.Base(path) == "package.json" {
			version, err := ReadProjectVersion(filepath.Dir(path))
			if version != "" || !errors.Is(err, errNotRegularFile) {
				t.Fatalf("special manifest accepted: version=%q err=%v", version, err)
			}
		}
		return
	}
	fifo := filepath.Join(t.TempDir(), "package.json")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	fifoLink := filepath.Join(t.TempDir(), "package.json")
	if err := os.Symlink(fifo, fifoLink); err != nil {
		t.Fatal(err)
	}
	deviceLink := filepath.Join(t.TempDir(), "package.json")
	if err := os.Symlink("/dev/zero", deviceLink); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"fifo": fifo, "fifo-symlink": fifoLink, "device": "/dev/null", "device-symlink": deviceLink} {
		t.Run(name, func(t *testing.T) {
			bounded, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			cmd := exec.CommandContext(bounded, os.Args[0], "-test.run=^TestReadCappedRejectsSpecialFiles$")
			cmd.Env = append(os.Environ(), "BETTER_FOOTER_SPECIAL_FILE_TEST="+path)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("special-file rejection failed (timeout=%v): %v\n%s", bounded.Err(), err, output)
			}
		})
	}
}
