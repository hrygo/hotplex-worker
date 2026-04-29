//go:build windows

package proc

import (
	"fmt"
	"os/exec"

	"golang.org/x/sys/windows"
)

// SetSysProcAttr configures the command to create a new process group (Windows).
func SetSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &windows.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP,
	}
}

// GracefulTerminate sends CTRL_BREAK_EVENT to the process group.
func GracefulTerminate(pgid int) error {
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_BREAK_EVENT, uint32(pgid))
}

// ForceKill terminates the process and its descendants via TerminateProcess.
func ForceKill(pgid int) error {
	// Open with PROCESS_TERMINATE to call TerminateProcess.
	handle, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pgid))
	if err != nil {
		return fmt.Errorf("open process %d for termination: %w", pgid, err)
	}
	defer windows.CloseHandle(handle)
	return windows.TerminateProcess(handle, 1)
}

// IsProcessAlive checks if a process exists using OpenProcess.
func IsProcessAlive(pid int) error {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return fmt.Errorf("process %d not found: %w", pid, err)
	}
	_ = windows.CloseHandle(handle)
	return nil
}

// IsProcessGroupAlive checks if a process (group leader) is still running.
func IsProcessGroupAlive(pgid int) error {
	return IsProcessAlive(pgid)
}

// IsProcessNotExist returns true if the error indicates the process does not exist.
func IsProcessNotExist(err error) bool {
	if err == nil {
		return false
	}
	return isWindowsError(err, windows.ERROR_INVALID_PARAMETER)
}

// isWindowsError checks if err wraps the specified Windows error code.
func isWindowsError(err error, target error) bool {
	for ; err != nil; err = unwrap(err) {
		if err == target {
			return true
		}
	}
	return false
}

func unwrap(err error) error {
	type unwrapper interface{ Unwrap() error }
	if u, ok := err.(unwrapper); ok {
		return u.Unwrap()
	}
	return nil
}
