//go:build windows

package proc

// createJobAndAssign creates a Job Object and assigns the process to it.
func (m *Manager) createJobAndAssign(pid int) {
	m.jobHandle = CreateAndAssignJob(pid, m.log)
}

// closeJobHandle closes the Job Object handle. KILL_ON_JOB_CLOSE ensures
// all processes in the job are terminated when the handle is closed.
func (m *Manager) closeJobHandle() {
	CloseJobHandle(m.jobHandle)
	m.jobHandle = 0
}
