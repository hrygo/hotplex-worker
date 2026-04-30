//go:build darwin || linux

package proc

// createJobAndAssign is a no-op on Unix where process groups handle tree cleanup.
func (m *Manager) createJobAndAssign(_ int) {}

// closeJobHandle is a no-op on Unix.
func (m *Manager) closeJobHandle() {}
