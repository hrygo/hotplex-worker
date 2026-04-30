//go:build windows

package stt

import "golang.org/x/sys/windows"

// closeJob closes the Job Object handle, killing the entire process tree.
func (s *PersistentSTT) closeJob() {
	if s.jobHandle == 0 {
		return
	}
	_ = windows.CloseHandle(windows.Handle(s.jobHandle))
	s.jobHandle = 0
}
