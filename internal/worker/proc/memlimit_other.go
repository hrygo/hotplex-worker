//go:build !linux

package proc

import "log/slog"

func setMemoryLimit(_ int, _ *slog.Logger) {
	// RLIMIT_AS is not available on macOS or Windows.
	// For production memory isolation, use cgroups (Linux) or
	// Job Object memory limits (Windows).
}
