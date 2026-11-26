//go:build unix

package repo

import (
	"io/fs"
	"syscall"
)

// Flags to open a file for reading without following a link at the last
// component, and without blocking on a named pipe.
const (
	openFileFlags = syscall.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
	openDirFlags  = syscall.O_RDONLY | syscall.O_NOFOLLOW | syscall.O_DIRECTORY
)

// linkCount returns the number of hard links to a file.
func linkCount(fi fs.FileInfo) (uint64, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return uint64(st.Nlink), true
}
