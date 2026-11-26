//go:build !unix

package harness

import "os"

// linkCount is not known on this platform, so it reports one.
func linkCount(os.FileInfo) uint64 { return 1 }
