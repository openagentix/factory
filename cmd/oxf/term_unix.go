//go:build darwin || linux

package main

import (
	"syscall"
	"unsafe"
)

// ttyAttrs asks for the terminal attributes of fd. Only a terminal answers.
func ttyAttrs(fd uintptr) bool {
	var t syscall.Termios
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, ioctlGetAttrs, uintptr(unsafe.Pointer(&t)))
	return e == 0
}
