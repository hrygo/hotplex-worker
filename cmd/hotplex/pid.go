package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/service"
	"github.com/hrygo/hotplex/internal/worker/proc"
)

func gatewayPIDPath() string {
	return filepath.Join(config.HotplexHome(), ".pids", "gateway.pid")
}

type gatewayState struct {
	PID        int    `json:"pid"`
	ConfigPath string `json:"config,omitempty"`
	DevMode    bool   `json:"dev,omitempty"`
}

func writeGatewayState(configPath string, devMode bool) error {
	pidPath := gatewayPIDPath()
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o755); err != nil {
		return err
	}
	state := gatewayState{
		PID:        os.Getpid(),
		ConfigPath: configPath,
		DevMode:    devMode,
	}
	data, _ := json.Marshal(state)
	return os.WriteFile(pidPath, data, 0o644)
}

func readGatewayState() (*gatewayState, error) {
	data, err := os.ReadFile(gatewayPIDPath())
	if err != nil {
		return nil, fmt.Errorf("gateway not running (no PID file)")
	}

	var state gatewayState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("invalid PID file content")
	}

	if err := proc.IsProcessAlive(state.PID); err != nil {
		removeGatewayState()
		if proc.IsProcessNotExist(err) {
			return nil, fmt.Errorf("gateway not running (PID %d stale)", state.PID)
		}
		return nil, fmt.Errorf("gateway not running (PID %d: %w)", state.PID, err)
	}

	return &state, nil
}

func removeGatewayState() {
	_ = os.Remove(gatewayPIDPath())
}

type discoverySource string

const (
	sourcePID     discoverySource = "pid"
	sourceService discoverySource = "service"
)

type gatewayInstance struct {
	PID        int
	Source     discoverySource
	Level      service.Level
	ConfigPath string
	DevMode    bool
}

func findRunningGateway() (*gatewayInstance, error) {
	if state, err := readGatewayState(); err == nil {
		return &gatewayInstance{
			PID:        state.PID,
			Source:     sourcePID,
			ConfigPath: state.ConfigPath,
			DevMode:    state.DevMode,
		}, nil
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

func stopGateway(inst *gatewayInstance) error {
	switch inst.Source {
	case sourcePID:
		if err := proc.GracefulTerminate(inst.PID); err != nil {
			return fmt.Errorf("stop PID %d: %w", inst.PID, err)
		}
		removeGatewayState()
	case sourceService:
		if err := service.NewManager().Stop("hotplex", inst.Level); err != nil {
			return fmt.Errorf("stop service: %w", err)
		}
	}
	return nil
}

func waitForProcessExit(pid int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if err := proc.IsProcessAlive(pid); err != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
}
