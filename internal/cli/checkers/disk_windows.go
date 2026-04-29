//go:build windows

package checkers

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// GetDiskFreeMB returns the available disk space in MB for the given path.
func GetDiskFreeMB(path string) (uint64, error) {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	var freeBytes uint64
	if err := windows.GetDiskFreeSpaceEx(pathPtr, &freeBytes, nil, nil); err != nil {
		return 0, err
	}
	return freeBytes / 1024 / 1024, nil
}
