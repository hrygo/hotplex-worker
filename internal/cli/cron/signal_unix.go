//go:build darwin || linux

package croncli

import (
	"fmt"
	"syscall"
)

func sendReloadSignal(pid int) error {
	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		return fmt.Errorf("send SIGHUP to gateway (PID %d): %w", pid, err)
	}
	return nil
}
