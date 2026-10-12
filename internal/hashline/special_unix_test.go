//go:build unix

package hashline

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestFIFOReplacementAfterStatDoesNotBlockRead(t *testing.T) {
	dir := realTempDir(t)
	path := filepath.Join(dir, "f.txt")
	writeFile(t, path, "regular\n", 0644)
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("initial stat did not observe a regular target: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(path, 0600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := readBoundedFile(path, 32)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("expected descriptor-type rejection, got %v", err)
		}
	case <-time.After(time.Second):
		// Release an incorrectly blocked reader so the regression does not leak
		// a goroutine. This writer does not send any content.
		writer, err := os.OpenFile(path, os.O_WRONLY|syscall.O_NONBLOCK, 0)
		if err == nil {
			if err := writer.Close(); err != nil {
				t.Errorf("close FIFO cleanup writer: %v", err)
			}
		} else {
			t.Errorf("release blocked FIFO reader: %v", err)
		}
		t.Fatal("a target swapped to a FIFO blocked before descriptor validation")
	}
}

func TestDeviceDescriptorCannotBeReadAsText(t *testing.T) {
	if _, err := readBoundedFile(os.DevNull, 32); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("expected device rejection, got %v", err)
	}
}
