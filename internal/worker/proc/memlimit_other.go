//go:build !linux

package proc

import "log/slog"

func setMemoryLimit(_ int, _ *slog.Logger) {
	// RLIMIT_AS is not available on macOS or Windows.
}
