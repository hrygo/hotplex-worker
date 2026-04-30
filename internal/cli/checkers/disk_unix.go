//go:build darwin || linux

package checkers

import "syscall"

// GetDiskFreeMB returns the available disk space in MB for the given path.
func GetDiskFreeMB(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize) / 1024 / 1024, nil
}
