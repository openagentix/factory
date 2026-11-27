//go:build !darwin && !linux

package main

// ttyAttrs is not built here, so nothing is a terminal.
func ttyAttrs(uintptr) bool { return false }
