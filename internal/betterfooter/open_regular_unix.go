//go:build unix

package betterfooter

import (
	"os"
	"syscall"
)

// Nonblocking open prevents a FIFO substituted for the path from waiting for a
// writer. readCapped validates the opened descriptor before reading any bytes.
func openRegularFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK|syscall.O_NOCTTY, 0)
}
