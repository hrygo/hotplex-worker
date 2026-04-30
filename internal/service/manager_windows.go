//go:build windows

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

type windowsManager struct{}

func NewManager() Manager {
	return &windowsManager{}
}

func connectSCM() (*mgr.Mgr, error) {
	return mgr.Connect()
}

func (m *windowsManager) Install(opts InstallOptions) error {
	mgrConn, err := connectSCM()
	if err != nil {
		return fmt.Errorf("connect to service control manager: %w", err)
	}
	defer mgrConn.Disconnect()

	s, err := mgrConn.OpenService(opts.Name)
	if err == nil {
		s.Close()
		return fmt.Errorf("service already installed (uninstall first)")
	}

	logDir := LogDir(opts.Level)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("create log dir %s: %w", logDir, err)
	}

	s, err = mgrConn.CreateService(
		opts.Name,
		opts.BinaryPath,
		mgr.Config{
			DisplayName: "HotPlex Worker Gateway",
			Description: "Unified access layer for AI Coding Agent sessions",
			StartType:   mgr.StartAutomatic,
		},
		"--service-run",
		"--service-config", opts.ConfigPath,
	)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}
	defer s.Close()

	recovery := []mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: 60 * time.Second},
	}
	if err := s.SetRecoveryActions(recovery, 86400); err != nil {
		return fmt.Errorf("set recovery actions: %w", err)
	}

	return nil
}

func (m *windowsManager) Uninstall(name string, level Level) error {
	mgrConn, err := connectSCM()
	if err != nil {
		return fmt.Errorf("connect to service control manager: %w", err)
	}
	defer mgrConn.Disconnect()

	s, err := mgrConn.OpenService(name)
	if err != nil {
		return fmt.Errorf("service not found: %w", err)
	}
	defer s.Close()

	// Best-effort stop: service may already be stopped.
	_, _ = s.Control(svc.Stop)

	if err := s.Delete(); err != nil {
		return fmt.Errorf("delete service: %w", err)
	}
	return nil
}

func (m *windowsManager) Status(name string, level Level) (*Status, error) {
	mgrConn, err := connectSCM()
	if err != nil {
		return nil, fmt.Errorf("connect to service control manager: %w", err)
	}
	defer mgrConn.Disconnect()

	s, err := mgrConn.OpenService(name)
	if err != nil {
		return &Status{
			Level:      level,
			Installed:  false,
			StatusText: "not installed",
		}, nil
	}
	defer s.Close()

	status, err := s.Query()
	if err != nil {
		return &Status{
			Level:      level,
			Installed:  true,
			Running:    false,
			StatusText: "unknown",
			UnitPath:   name,
		}, nil
	}

	running := status.State == svc.Running

	return &Status{
		Level:      level,
		Installed:  true,
		Running:    running,
		PID:        int(status.ProcessId),
		StatusText: stateString(status.State),
		UnitPath:   name,
	}, nil
}

func (m *windowsManager) Start(name string, level Level) error {
	mgrConn, err := connectSCM()
	if err != nil {
		return fmt.Errorf("connect to service control manager: %w", err)
	}
	defer mgrConn.Disconnect()

	s, err := mgrConn.OpenService(name)
	if err != nil {
		return ErrNotInstalled
	}
	defer s.Close()

	if err := s.Start(); err != nil {
		return fmt.Errorf("start service: %w", err)
	}
	return nil
}

func (m *windowsManager) Stop(name string, level Level) error {
	mgrConn, err := connectSCM()
	if err != nil {
		return fmt.Errorf("connect to service control manager: %w", err)
	}
	defer mgrConn.Disconnect()

	s, err := mgrConn.OpenService(name)
	if err != nil {
		return ErrNotInstalled
	}
	defer s.Close()

	_, err = s.Control(svc.Stop)
	if err != nil {
		return fmt.Errorf("stop service: %w", err)
	}
	return nil
}

func (m *windowsManager) Restart(name string, level Level) error {
	if err := m.Stop(name, level); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	return m.Start(name, level)
}

func (m *windowsManager) Logs(name string, level Level, follow bool, lines int) error {
	logDir := LogDir(level)
	logFile := filepath.Join(logDir, "service.log")
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		return fmt.Errorf("log file not found: %s", logFile)
	}

	psCmd := fmt.Sprintf("Get-Content -Path '%s' -Tail %d", logFile, lines)
	if follow {
		psCmd += " -Wait"
	}

	cmd := exec.Command("powershell", "-NoProfile", "-Command", psCmd)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func stateString(state svc.State) string {
	switch state {
	case svc.Stopped:
		return "stopped"
	case svc.StartPending:
		return "start-pending"
	case svc.StopPending:
		return "stop-pending"
	case svc.Running:
		return "running"
	case svc.ContinuePending:
		return "continue-pending"
	case svc.PausePending:
		return "pause-pending"
	case svc.Paused:
		return "paused"
	default:
		return "unknown"
	}
}
