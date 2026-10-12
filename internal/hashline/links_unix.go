//go:build unix

package hashline

import (
	"fmt"
	"io/fs"
	"syscall"
)

func linkCount(info fs.FileInfo) (uint64, error) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("cannot inspect link count for %s", info.Name())
	}
	return uint64(stat.Nlink), nil
}
