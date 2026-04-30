//go:build linux

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type linuxManager struct{}

func NewManager() Manager {
	return &linuxManager{}
}

func (m *linuxManager) Install(opts InstallOptions) error {
	unitPath, err := m.unitPath(opts.Name, opts.Level)
	if err != nil {
		return err
	}

	if _, err := os.Stat(unitPath); err == nil {
		return fmt.Errorf("service already installed at %s (uninstall first)", unitPath)
	}

	if _, err := exec.LookPath("systemctl"); err != nil {
		return fmt.Errorf("systemctl not found (systemd may not be available)")
	}

	dir := filepath.Dir(unitPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	homeDir, _ := os.UserHomeDir()
	content := BuildSystemdUnit(opts, homeDir)

	if err := os.WriteFile(unitPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write unit: %w", err)
	}

	if out, err := systemctl(opts.Level, "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %s: %w", out, err)
	}

	if out, err := systemctl(opts.Level, "enable", opts.Name); err != nil {
		return fmt.Errorf("systemctl enable: %s: %w", out, err)
	}

	if opts.Level == LevelUser {
		_ = enableLinger()
	}

	return nil
}

func (m *linuxManager) Uninstall(name string, level Level) error {
	unitPath, err := m.unitPath(name, level)
	if err != nil {
		return err
	}

	if _, err := os.Stat(unitPath); os.IsNotExist(err) {
		return fmt.Errorf("service not installed at %s", unitPath)
	}

	_, _ = systemctl(level, "stop", name)
	_, _ = systemctl(level, "disable", name)

	if err := os.Remove(unitPath); err != nil {
		return fmt.Errorf("remove unit: %w", err)
	}

	_, _ = systemctl(level, "daemon-reload")
	return nil
}

func (m *linuxManager) Status(name string, level Level) (*Status, error) {
	unitPath, err := m.unitPath(name, level)
	if err != nil {
		return nil, err
	}

	s := &Status{Level: level, UnitPath: unitPath}

	if _, err := os.Stat(unitPath); os.IsNotExist(err) {
		s.Installed = false
		s.StatusText = "not installed"
		return s, nil
	}
	s.Installed = true

	out, _ := systemctl(level, "is-active", name)
	activeText := strings.TrimSpace(out)
	s.Running = activeText == "active"
	s.StatusText = activeText

	if s.Running {
		pidOut, _ := systemctl(level, "show", name, "--property=MainPID", "--value")
		if pid, err := strconv.Atoi(strings.TrimSpace(pidOut)); err == nil && pid > 0 {
			s.PID = pid
		}
	}

	return s, nil
}

func (m *linuxManager) unitPath(name string, level Level) (string, error) {
	switch level {
	case LevelSystem:
		return "/etc/systemd/system/" + name + ".service", nil
	case LevelUser:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("home directory: %w", err)
		}
		return filepath.Join(home, ".config", "systemd", "user", name+".service"), nil
	default:
		return "", fmt.Errorf("unknown level: %s", level)
	}
}

func systemctl(level Level, args ...string) (string, error) {
	var cmdArgs []string
	if level == LevelUser {
		cmdArgs = append(cmdArgs, "--user")
	}
	cmdArgs = append(cmdArgs, args...)
	out, err := exec.Command("systemctl", cmdArgs...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func enableLinger() error {
	uid := os.Getuid()
	return exec.Command("loginctl", "enable-linger", strconv.Itoa(uid)).Run()
}
