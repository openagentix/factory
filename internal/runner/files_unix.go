//go:build unix

package runner

import (
	"io/fs"
	"syscall"
)

const (
	openNoFollow = syscall.O_NOFOLLOW
	openNonBlock = syscall.O_NONBLOCK
)

// linkCount returns the number of hard links to a file.
func linkCount(fi fs.FileInfo) (uint64, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(st.Nlink), true
}
