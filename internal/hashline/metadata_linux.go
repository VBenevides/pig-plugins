//go:build linux

package hashline

import (
	"errors"
	"fmt"
	"io/fs"
	"syscall"
)

// ACLs and capabilities are extended attributes on Linux. Reject metadata we
// cannot preserve. An inherited SELinux label is permitted only if the new
// inode's exact label matches the snapshot; labels are never copied or logged.
func checkSupportedMetadata(path string) ([]byte, error) {
	count, err := syscall.Listxattr(path, nil)
	if errors.Is(err, syscall.ENOTSUP) {
		return nil, nil // This filesystem cannot store extended attributes.
	}
	if err != nil {
		return nil, fmt.Errorf("inspect extended attributes of %s: %w", path, err)
	}
	if count == 0 {
		return nil, nil
	}
	const allowed = "security.selinux\x00"
	if count != len(allowed) {
		return nil, editErrorf("Refusing to edit %s: extended attributes or ACLs cannot be safely preserved by replacement.", path)
	}
	names := make([]byte, count)
	n, err := syscall.Listxattr(path, names)
	if err != nil {
		return nil, fmt.Errorf("read extended-attribute names of %s: %w", path, err)
	}
	if string(names[:n]) != allowed {
		return nil, editErrorf("Refusing to edit %s: extended attributes or ACLs cannot be safely preserved by replacement.", path)
	}
	label := make([]byte, 4096)
	n, err = syscall.Getxattr(path, "security.selinux", label)
	if err != nil {
		return nil, fmt.Errorf("inspect inherited security label of %s: %w", path, err)
	}
	return label[:n], nil
}

func sameOwnership(original, replacement fs.FileInfo) bool {
	before, beforeOK := original.Sys().(*syscall.Stat_t)
	after, afterOK := replacement.Sys().(*syscall.Stat_t)
	return beforeOK && afterOK && before.Uid == after.Uid && before.Gid == after.Gid
}
