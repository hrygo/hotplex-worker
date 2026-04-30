//go:build !windows

package service

import "os"

func IsPrivileged() bool {
	return os.Getuid() == 0
}
