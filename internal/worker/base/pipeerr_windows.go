//go:build windows

package base

import "strings"

// IsDeadProcessError checks if the error indicates the worker process is gone.
func IsDeadProcessError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "pipe has been ended") ||
		strings.Contains(s, "pipe is being closed") ||
		strings.Contains(s, "file already closed") ||
		strings.Contains(s, "broken pipe")
}
