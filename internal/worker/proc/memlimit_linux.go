//go:build linux

package proc

import (
	"log/slog"

	"golang.org/x/sys/unix"
)

func setMemoryLimit(pid int, log *slog.Logger) {
	// Use unix.Prlimit to set RLIMIT_AS on the child process specifically.
	// syscall.Setrlimit applies to the CALLING process (the gateway) — that
	// was a bug: it capped the gateway's own virtual address space, causing
	// Bun bmalloc mmap failures AND gateway pthread_create failures.
	// Prlimit targets a specific PID, so only the worker is constrained.
	const memLimit = 2 * 1024 * 1024 * 1024 // 2 GB
	if err := unix.Prlimit(pid, unix.RLIMIT_AS, &unix.Rlimit{
		Cur: memLimit,
		Max: memLimit,
	}, nil); err != nil {
		log.Warn("proc: prlimit RLIMIT_AS failed", "pid", pid, "err", err)
	}
}
