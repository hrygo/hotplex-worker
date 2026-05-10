//go:build darwin || linux

package proc

import (
	"errors"
	"os/exec"
	"syscall"
	"time"
)

// SetSysProcAttr configures the command to create a new process group (POSIX).
func SetSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// GracefulTerminate sends SIGTERM to the entire process group.
func GracefulTerminate(pgid int) error {
	return syscall.Kill(-pgid, syscall.SIGTERM)
}

// ForceKill sends SIGKILL to the entire process group.
func ForceKill(pgid int) error {
	return syscall.Kill(-pgid, syscall.SIGKILL)
}

// DefaultGracePeriod is the default time to wait after SIGTERM before
// escalating to SIGKILL. Shared across proc/manager, proc/pidfile, and
// base/worker to avoid magic number duplication.
const DefaultGracePeriod = 5 * time.Second

// IsProcessAlive checks if a process exists by sending signal 0.
func IsProcessAlive(pid int) error {
	return syscall.Kill(pid, syscall.Signal(0))
}

// IsProcessGroupAlive checks if a process group leader is still running.
func IsProcessGroupAlive(pgid int) error {
	return syscall.Kill(-pgid, syscall.Signal(0))
}

// IsProcessNotExist returns true if the error indicates the process does not exist.
func IsProcessNotExist(err error) bool {
	return errors.Is(err, syscall.ESRCH)
}
