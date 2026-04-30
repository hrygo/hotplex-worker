//go:build !windows

package stt

// closeJob is a no-op on non-Windows platforms where process groups handle cleanup.
func (s *PersistentSTT) closeJob() {}
