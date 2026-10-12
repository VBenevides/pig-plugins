//go:build !linux

package hashline

import (
	"fmt"
	"io/fs"
)

func checkSupportedMetadata(path string) ([]byte, error) {
	return nil, fmt.Errorf("refusing to edit %s: safe ownership/ACL/extended-attribute inspection is supported only on Linux", path)
}

func sameOwnership(original, replacement fs.FileInfo) bool {
	return false
}
