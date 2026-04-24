package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/hrygo/hotplex/internal/config"
)

func gatewayPIDPath() string {
	return filepath.Join(config.HotplexHome(), ".pids", "gateway.pid")
}

func writeGatewayPID() error {
	pidPath := gatewayPIDPath()
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(pidPath, []byte(fmt.Sprintf("%d", os.Getpid())), 0o644)
}

func readGatewayPID() (int, error) {
	data, err := os.ReadFile(gatewayPIDPath())
	if err != nil {
		return 0, fmt.Errorf("gateway not running (no PID file)")
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid PID file content")
	}

	if err := syscall.Kill(pid, 0); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			removeGatewayPID()
			return 0, fmt.Errorf("gateway not running (PID %d stale)", pid)
		}
	}

	return pid, nil
}

func removeGatewayPID() {
	_ = os.Remove(gatewayPIDPath())
}
