//go:build linux || darwin || freebsd || netbsd || openbsd

package tui

import (
	"os"
	"syscall"
	"unsafe"
)

// TerminalRows returns the height of the controlling terminal, or 0 when it cannot be read. An extension runs as a
// child process without a terminal on its standard streams, so it asks the controlling terminal directly. Callers
// must treat 0 as "unknown" and use a conservative default.
func TerminalRows() int {
	tty, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0)
	if err != nil {
		return 0
	}
	defer tty.Close()
	var size struct{ rows, cols, x, y uint16 }
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, tty.Fd(), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&size))); errno != 0 {
		return 0
	}
	return int(size.rows)
}
