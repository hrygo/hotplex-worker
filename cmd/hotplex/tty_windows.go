//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

func isTTY(fd uintptr) bool {
	ft, err := windows.GetFileType(windows.Handle(os.NewFile(fd, "").Fd()))
	if err != nil {
		return false
	}
	return ft == windows.FILE_TYPE_CHAR
}
