//go:build linux

package proc

import (
	"log/slog"
	"syscall"
)

func setMemoryLimit(pid int, log *slog.Logger) {
	const memLimit = 512 * 1024 * 1024 // 512 MB
	if err := syscall.Setrlimit(syscall.RLIMIT_AS, &syscall.Rlimit{
		Cur: memLimit,
		Max: memLimit,
	}); err != nil {
		log.Warn("proc: setrlimit RLIMIT_AS failed", "err", err)
	}
}
