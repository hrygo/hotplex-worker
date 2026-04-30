//go:build linux

package proc

import (
	"log/slog"
	"syscall"
)

func setMemoryLimit(_ int, log *slog.Logger) {
	// NOTE: syscall.Setrlimit applies to the calling process (the gateway),
	// not to an already-started child. The RLIMIT_AS limit IS inherited by
	// children forked after this call, so this sets a ceiling for the gateway
	// and any worker processes it spawns subsequently.
	// For production per-worker isolation, prefer cgroups.
	const memLimit = 512 * 1024 * 1024 // 512 MB
	if err := syscall.Setrlimit(syscall.RLIMIT_AS, &syscall.Rlimit{
		Cur: memLimit,
		Max: memLimit,
	}); err != nil {
		log.Warn("proc: setrlimit RLIMIT_AS failed", "err", err)
	}
}
