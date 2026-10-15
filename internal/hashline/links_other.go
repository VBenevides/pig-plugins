//go:build !unix

package hashline

import (
	"fmt"
	"io/fs"
)

func linkCount(info fs.FileInfo) (uint64, error) {
	return 0, fmt.Errorf("cannot safely inspect hard links on this platform: %s", info.Name())
}
