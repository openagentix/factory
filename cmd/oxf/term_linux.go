//go:build linux

package main

import "syscall"

const ioctlGetAttrs = syscall.TCGETS
