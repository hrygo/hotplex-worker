//go:build windows

package proc

import (
	"fmt"
	"os/exec"
	"strconv"

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

// ForceKill terminates the process tree using taskkill.
func ForceKill(pgid int) error {
	return exec.Command("taskkill", "/PID", strconv.Itoa(pgid), "/T", "/F").Run()
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
	return err != nil
}
