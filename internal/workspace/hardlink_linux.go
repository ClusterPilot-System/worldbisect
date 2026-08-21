//go:build linux

package workspace

import (
	"os"
	"syscall"
)

func hardlinkCount(info os.FileInfo) uint64 {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 1
	}
	return uint64(stat.Nlink)
}
