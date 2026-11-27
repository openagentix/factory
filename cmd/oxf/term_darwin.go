//go:build darwin

package main

import "syscall"

const ioctlGetAttrs = syscall.TIOCGETA
