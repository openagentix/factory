//go:build unix

package harness

import (
	"os"
	"syscall"
)

// linkCount is the number of hard links to a file.
func linkCount(fi os.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Nlink)
	}
	return 1
}
