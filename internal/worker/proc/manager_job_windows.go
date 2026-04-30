//go:build windows

package proc

import (
	"golang.org/x/sys/windows"
)

// createJobAndAssign creates a Job Object with KILL_ON_JOB_CLOSE and assigns
// the process to it. The handle is stored in m.jobHandle for later cleanup.
func (m *Manager) createJobAndAssign(pid int) {
	job, err := CreateJobObject()
	if err != nil {
		m.log.Warn("proc: failed to create job object, process tree cleanup disabled", "error", err)
		return
	}
	if err := AssignProcessToJob(job, pid); err != nil {
		windows.CloseHandle(windows.Handle(job))
		m.log.Warn("proc: failed to assign process to job object", "pid", pid, "error", err)
		return
	}
	m.jobHandle = uintptr(job)
	m.log.Info("proc: assigned process to job object", "pid", pid)
}

// killJob terminates the process tree by closing the Job Object handle.
// On Windows, JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE ensures all processes
// in the job are terminated when the handle is closed.
func (m *Manager) killJob() {
	if m.jobHandle == 0 {
		return
	}
	job := windows.Handle(m.jobHandle)
	// TerminateJobObject sends termination to all processes in the job.
	_ = windows.TerminateJobObject(job, 1)
}

// closeJobHandle closes the Job Object handle.
func (m *Manager) closeJobHandle() {
	if m.jobHandle == 0 {
		return
	}
	_ = windows.CloseHandle(windows.Handle(m.jobHandle))
	m.jobHandle = 0
}
