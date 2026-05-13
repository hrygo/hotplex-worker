//go:build !windows

package main

import "syscall"

func restartHelperSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}
