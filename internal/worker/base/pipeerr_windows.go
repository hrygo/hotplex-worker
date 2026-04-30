//go:build windows

package base

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// IsDeadProcessError checks if the error indicates the worker process is gone.
// Uses errno comparison to avoid locale-dependent string matching.
func IsDeadProcessError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, windows.ERROR_BROKEN_PIPE) ||
		errors.Is(err, windows.ERROR_NO_DATA) ||
		errors.Is(err, os.ErrClosed)
}
