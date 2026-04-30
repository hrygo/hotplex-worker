//go:build windows

package stt

import "github.com/hrygo/hotplex/internal/worker/proc"

// closeJob closes the Job Object handle, killing the entire process tree.
func (s *PersistentSTT) closeJob() {
	proc.CloseJobHandle(s.jobHandle)
	s.jobHandle = 0
}
