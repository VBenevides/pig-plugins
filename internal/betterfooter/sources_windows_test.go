package betterfooter

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestReadCappedRejectsWindowsDevices(t *testing.T) {
	if path := os.Getenv("BETTER_FOOTER_WINDOWS_DEVICE_TEST"); path != "" {
		if data, err := readCapped(path); data != nil || !errors.Is(err, errNotRegularFile) {
			t.Fatalf("device accepted: data=%d err=%v", len(data), err)
		}
		return
	}
	check := func(t *testing.T, path string) {
		t.Helper()
		bounded, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(bounded, os.Args[0], "-test.run=^TestReadCappedRejectsWindowsDevices$")
		cmd.Env = append(os.Environ(), "BETTER_FOOTER_WINDOWS_DEVICE_TEST="+path)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("device rejection failed (timeout=%v): %v\n%s", bounded.Err(), err, output)
		}
	}
	t.Run("device", func(t *testing.T) { check(t, "NUL") })
	t.Run("device-namespace", func(t *testing.T) { check(t, `\\.\NUL`) })
	t.Run("device-symlink", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "package.json")
		if err := os.Symlink(`\\.\NUL`, path); err != nil {
			t.Skipf("symlink creation unavailable: %v", err)
		}
		check(t, path)
	})
}
