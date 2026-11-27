//go:build !unix

package main

import "os"

// ownedByMe is false where the owner cannot be read, so nothing is removed.
func ownedByMe(os.FileInfo) bool { return false }
