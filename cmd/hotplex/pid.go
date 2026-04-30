package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/service"
	"github.com/hrygo/hotplex/internal/worker/proc"
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

	if err := proc.IsProcessAlive(pid); err != nil {
		if proc.IsProcessNotExist(err) {
			removeGatewayPID()
			return 0, fmt.Errorf("gateway not running (PID %d stale)", pid)
		}
	}

	return pid, nil
}

func removeGatewayPID() {
	_ = os.Remove(gatewayPIDPath())
}

type discoverySource string

const (
	sourcePID     discoverySource = "pid"
	sourceService discoverySource = "service"
)

type gatewayInstance struct {
	PID    int
	Source discoverySource
	Level  service.Level // only set when Source == sourceService
}

func findRunningGateway() (*gatewayInstance, error) {
	if pid, err := readGatewayPID(); err == nil {
		return &gatewayInstance{PID: pid, Source: sourcePID}, nil
	}

	mgr := service.NewManager()
	for _, level := range []service.Level{service.LevelUser, service.LevelSystem} {
		s, err := mgr.Status("hotplex", level)
		if err == nil && s.Running {
			return &gatewayInstance{PID: s.PID, Source: sourceService, Level: level}, nil
		}
	}

	return nil, fmt.Errorf("gateway not running (no PID file and no service found)")
}

// stopGateway terminates a discovered gateway instance via the appropriate mechanism.
func stopGateway(inst *gatewayInstance) error {
	switch inst.Source {
	case sourcePID:
		if err := proc.GracefulTerminate(inst.PID); err != nil {
			return fmt.Errorf("stop PID %d: %w", inst.PID, err)
		}
		removeGatewayPID()
	case sourceService:
		if err := service.NewManager().Stop("hotplex", inst.Level); err != nil {
			return fmt.Errorf("stop service: %w", err)
		}
	}
	return nil
}
