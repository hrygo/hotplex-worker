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

type linuxManager struct {
	run CommandRunner
}

func NewManager() Manager {
	return &linuxManager{run: realRunner{}}
}

func (m *linuxManager) Install(opts InstallOptions) error {
	unitPath, err := m.unitPath(opts.Name, opts.Level)
	if err != nil {
		return err
	}

	if _, err := os.Stat(unitPath); err == nil {
		return fmt.Errorf("service already installed at %s (uninstall first)", unitPath)
	}

	if _, err := m.run.LookPath("systemctl"); err != nil {
		return fmt.Errorf("systemctl not found (systemd may not be available)")
	}

	homeDir, _ := os.UserHomeDir()
	if err := writeServiceFile(unitPath, BuildSystemdUnit(opts, homeDir)); err != nil {
		return err
	}

	if out, err := m.systemctl(opts.Level, "daemon-reload"); err != nil {
		return fmt.Errorf("systemctl daemon-reload: %s: %w", out, err)
	}

	if out, err := m.systemctl(opts.Level, "enable", opts.Name); err != nil {
		return fmt.Errorf("systemctl enable: %s: %w", out, err)
	}

	if opts.Level == LevelUser {
		_ = m.enableLinger()
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

	_, _ = m.systemctl(level, "stop", name)
	_, _ = m.systemctl(level, "disable", name)

	if err := os.Remove(unitPath); err != nil {
		return fmt.Errorf("remove unit: %w", err)
	}

	_, _ = m.systemctl(level, "daemon-reload")
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

	out, _ := m.systemctl(level, "is-active", name)
	activeText := strings.TrimSpace(out)
	s.Running = activeText == "active"
	s.StatusText = activeText

	if s.Running {
		pidOut, _ := m.systemctl(level, "show", name, "--property=MainPID", "--value")
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

func (m *linuxManager) systemctl(level Level, args ...string) (string, error) {
	var cmdArgs []string
	if level == LevelUser {
		cmdArgs = append(cmdArgs, "--user")
	}
	cmdArgs = append(cmdArgs, args...)
	out, err := m.run.CombinedOutput("systemctl", cmdArgs...)
	return strings.TrimSpace(string(out)), err
}

func (m *linuxManager) Start(name string, level Level) error {
	out, err := m.systemctl(level, "start", name)
	if err != nil {
		return fmt.Errorf("systemctl start: %s: %w", out, err)
	}
	return nil
}

func (m *linuxManager) Stop(name string, level Level) error {
	out, err := m.systemctl(level, "stop", name)
	if err != nil {
		return fmt.Errorf("systemctl stop: %s: %w", out, err)
	}
	return nil
}

func (m *linuxManager) Restart(name string, level Level) error {
	out, err := m.systemctl(level, "restart", name)
	if err != nil {
		return fmt.Errorf("systemctl restart: %s: %w", out, err)
	}
	return nil
}

func (m *linuxManager) Logs(name string, level Level, follow bool, lines int) error {
	var args []string
	if level == LevelUser {
		args = append(args, "--user")
	}
	args = append(args, "-u", name)
	args = append(args, "--no-pager", "-n", strconv.Itoa(lines))
	if follow {
		args = append(args, "-f")
	}

	cmd := exec.Command("journalctl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func (m *linuxManager) enableLinger() error {
	uid := os.Getuid()
	return m.run.Run("loginctl", "enable-linger", strconv.Itoa(uid))
}
