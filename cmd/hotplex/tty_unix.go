//go:build darwin || linux

package main

import "syscall"

func isTTY(fd uintptr) bool {
	var st syscall.Stat_t
	if err := syscall.Fstat(int(fd), &st); err != nil {
		return false
	}
	return st.Mode&syscall.S_IFMT == syscall.S_IFCHR
}
