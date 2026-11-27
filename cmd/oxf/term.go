package main

import "os"

// isTerminal says whether f is a terminal. A character device is not enough:
// /dev/null is one. The file must also answer the terminal attribute call.
// Every other case, and every error, is "not a terminal", which is the safe
// side: the link then goes to a file with mode 0600 and is never printed.
func isTerminal(f *os.File) bool { return isTerminalFile(f, ttyAttrs) }

func isTerminalFile(f *os.File, attrs func(fd uintptr) bool) bool {
	if f == nil || attrs == nil {
		return false
	}
	fi, err := f.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	rc, err := f.SyscallConn()
	if err != nil {
		return false
	}
	ok := false
	if err := rc.Control(func(fd uintptr) { ok = attrs(fd) }); err != nil {
		return false
	}
	return ok
}
