//go:build !linux

package proc

import "log/slog"

func setMemoryLimit(pid int, log *slog.Logger) {
	// RLIMIT_AS is not available on macOS or Windows.
}
