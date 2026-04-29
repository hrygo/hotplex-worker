//go:build darwin || linux

package proc

import (
	"errors"
	"os/exec"
	"syscall"
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
