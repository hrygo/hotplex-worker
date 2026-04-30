//go:build darwin || linux

package base

import (
	"errors"
	"os"
	"syscall"
)

// IsDeadProcessError checks if the error indicates the worker process is gone.
func IsDeadProcessError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, os.ErrClosed)
}
