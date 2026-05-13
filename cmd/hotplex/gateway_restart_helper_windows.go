//go:build windows

package main

import (
	"syscall"

	"golang.org/x/sys/windows"
)

func restartHelperSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
}
