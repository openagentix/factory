//go:build !unix

package runner

import "io/fs"

const (
	openNoFollow = 0
	openNonBlock = 0
)

// linkCount cannot be known here, so no result file is accepted.
func linkCount(fi fs.FileInfo) (uint64, bool) { return 0, false }
