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

func ensureNotRunning() error {
	inst, err := findRunningGateway()
	if err != nil {
		return nil
	}
	if inst.PID == os.Getpid() {
		return nil
	}
	return fmt.Errorf("gateway already running (PID %d, via %s); use 'hotplex gateway stop' first",
		inst.PID, inst.Source)
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

// ─── Restart Cooldown Marker ──────────────────────────────────────────────────

type restartMarker struct {
	HelperPID int       `json:"helper_pid"`
	CreatedAt time.Time `json:"created_at"`
}

func restartMarkerPath() string {
	return filepath.Join(config.HotplexHome(), ".pids", "gateway.restart")
}

func writeRestartMarker(helperPID int) error {
	p := restartMarkerPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, _ := json.Marshal(restartMarker{HelperPID: helperPID, CreatedAt: time.Now()})
	return os.WriteFile(p, data, 0o644)
}

func readRestartMarker() (*restartMarker, error) {
	data, err := os.ReadFile(restartMarkerPath())
	if err != nil {
		return nil, err
	}
	var m restartMarker
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("invalid restart marker: %w", err)
	}
	return &m, nil
}

func removeRestartMarker() {
	_ = os.Remove(restartMarkerPath())
}

const restartCooldownPeriod = 60 * time.Second

func checkRestartCooldown() error {
	m, err := readRestartMarker()
	if err != nil {
		return nil // no marker = no cooldown
	}
	if proc.IsProcessAlive(m.HelperPID) == nil {
		return fmt.Errorf("gateway: restart in progress (helper PID %d)", m.HelperPID)
	}
	elapsed := time.Since(m.CreatedAt)
	if elapsed < restartCooldownPeriod {
		remaining := restartCooldownPeriod - elapsed
		return fmt.Errorf("gateway: restart cooldown active (try again in %s)", remaining.Round(time.Second))
	}
	removeRestartMarker()
	return nil
}
