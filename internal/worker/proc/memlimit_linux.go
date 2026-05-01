//go:build linux

package proc

import (
	"log/slog"
)

func setMemoryLimit(pid int, log *slog.Logger) {
	// DISABLED: Modern JIT runtimes (Bun v1.3.x, Node.js v20+) reserve large
	// virtual address spaces for JIT code caches, heap pre-allocation, and
	// WebAssembly linear memory. Claude Code built-in Bun requires ~70GB+ VA
	// space despite using only ~350MB RSS. RLIMIT_AS limits address space, not
	// physical RAM, causing immediate "mmap: cannot allocate memory" crashes.
	//
	// Alternatives for production memory isolation:
	// - Linux: cgroups v2 (memory.max) for precise RSS control
	// - Containers: Docker/Kubernetes memory limits
	// - Monitoring: Prometheus alerts on hotplex_worker_memory_bytes
	//
	// System memory is sufficient (7GB total, 3GB available). The previous
	// 2GB limit was causing all Claude Code workers to crash on startup.
	//
	// Re-enable only if targeting embedded systems with <512MB RAM.
	// For most servers, let the OS page scanner manage memory pressure.
	log.Debug("proc: RLIMIT_AS disabled (modern JIT requires large VA space)")
}
