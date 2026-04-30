//go:build darwin

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

type darwinManager struct{}

func NewManager() Manager {
	return &darwinManager{}
}

func (m *darwinManager) Install(opts InstallOptions) error {
	plistPath, err := m.plistPath(opts.Name, opts.Level)
	if err != nil {
		return err
	}

	if _, err := os.Stat(plistPath); err == nil {
		return fmt.Errorf("service already installed at %s (uninstall first)", plistPath)
	}

	dir := filepath.Dir(plistPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	homeDir, _ := os.UserHomeDir()
	content := BuildLaunchdPlist(opts, homeDir)

	if err := os.WriteFile(plistPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	if _, err := exec.LookPath("launchctl"); err != nil {
		return fmt.Errorf("launchctl not found")
	}

	label := launchdLabel(opts.Name, opts.Level)
	out, err := exec.Command("launchctl", "load", "-w", plistPath).CombinedOutput()
	if err != nil {
		_ = os.Remove(plistPath)
		return fmt.Errorf("launchctl load: %s: %w", strings.TrimSpace(string(out)), err)
	}

	_ = exec.Command("launchctl", "start", label).Run()
	return nil
}

func (m *darwinManager) Uninstall(name string, level Level) error {
	plistPath, err := m.plistPath(name, level)
	if err != nil {
		return err
	}

	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		return fmt.Errorf("service not installed at %s", plistPath)
	}

	label := launchdLabel(name, level)
	_ = exec.Command("launchctl", "stop", label).Run()

	out, err := exec.Command("launchctl", "unload", plistPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl unload: %s: %w", strings.TrimSpace(string(out)), err)
	}

	return os.Remove(plistPath)
}

func (m *darwinManager) Status(name string, level Level) (*Status, error) {
	plistPath, err := m.plistPath(name, level)
	if err != nil {
		return nil, err
	}

	s := &Status{Level: level, UnitPath: plistPath}

	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		s.Installed = false
		s.StatusText = "not installed"
		return s, nil
	}
	s.Installed = true

	label := launchdLabel(name, level)
	out, err := exec.Command("launchctl", "list", label).CombinedOutput()
	if err != nil {
		s.Running = false
		s.StatusText = "stopped"
		return s, nil
	}

	output := string(out)
	s.Running = true
	s.StatusText = "running"

	if pidStr := parseLaunchctlPID(output); pidStr != "" {
		if pid, err := strconv.Atoi(pidStr); err == nil {
			s.PID = pid
		}
	}

	return s, nil
}

func (m *darwinManager) plistPath(name string, level Level) (string, error) {
	switch level {
	case LevelSystem:
		return "/Library/LaunchDaemons/" + launchdLabel(name, level) + ".plist", nil
	case LevelUser:
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("home directory: %w", err)
		}
		return filepath.Join(home, "Library", "LaunchAgents", launchdLabel(name, level)+".plist"), nil
	default:
		return "", fmt.Errorf("unknown level: %s", level)
	}
}

func parseLaunchctlPID(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		pidPrefix := `"PID" = `
		if strings.HasPrefix(line, pidPrefix) {
			return strings.TrimRight(strings.TrimPrefix(line, pidPrefix), `";`)
		}
	}
	return ""
}
