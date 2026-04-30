//go:build !windows

package proc

// CreateJobObject is a no-op on non-Windows platforms where process groups
// handle tree cleanup.
func CreateJobObject() (uintptr, error) { return 0, nil }

// AssignProcessToJob is a no-op on non-Windows platforms.
func AssignProcessToJob(_ uintptr, _ int) error { return nil }
