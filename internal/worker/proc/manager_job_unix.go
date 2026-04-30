//go:build darwin || linux

package proc

// createJobAndAssign is a no-op on Unix where process groups handle tree cleanup.
func (m *Manager) createJobAndAssign(_ int) {}

// killJob is a no-op on Unix where ForceKill uses process group signals.
func (m *Manager) killJob() {}

// closeJobHandle is a no-op on Unix.
func (m *Manager) closeJobHandle() {}
