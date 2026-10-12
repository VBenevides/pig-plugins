//go:build unix

package hashline

import "syscall"

// A regular file swapped for a FIFO must not block open before fstat can reject
// it. Non-regular descriptors are never passed to the content reader.
const nonBlockingReadFlag = syscall.O_NONBLOCK
