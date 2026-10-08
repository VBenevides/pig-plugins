//go:build !windows

package repoexcludes

import (
	"os"
	"syscall"
)

// Do not follow a substituted link or block on a substituted FIFO.
func openExclude(root *os.Root) (*os.File, error) {
	return root.OpenFile("exclude", os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_NOCTTY, 0)
}
