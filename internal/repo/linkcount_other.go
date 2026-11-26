//go:build !unix

package repo

import "io/fs"

// Import needs to open a file without following links and to count its hard
// links. Without that, it refuses every file.
const (
	openFileFlags = 0
	openDirFlags  = 0
)

func linkCount(fi fs.FileInfo) (uint64, bool) { return 0, false }
