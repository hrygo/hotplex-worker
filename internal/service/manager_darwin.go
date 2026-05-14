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

type darwinManager struct {
	run CommandRunner
}

func NewManager() Manager {
	return &darwinManager{run: realRunner{}}
}

func (m *darwinManager) Install(opts InstallOptions) error {
	plistPath, err := m.plistPath(opts.Name, opts.Level)
	if err != nil {
		return err
	}

	if _, err := os.Stat(plistPath); err == nil {
		return fmt.Errorf("service already installed at %s (uninstall first)", plistPath)
	}

	homeDir, _ := os.UserHomeDir()
	if err := writeServiceFile(plistPath, BuildLaunchdPlist(opts, homeDir)); err != nil {
		return err
	}

	if _, err := m.run.LookPath("launchctl"); err != nil {
		return fmt.Errorf("launchctl not found")
	}

	label := launchdLabel(opts.Name, opts.Level)
	out, err := m.run.CombinedOutput("launchctl", "load", "-w", plistPath)
	if err != nil {
		_ = os.Remove(plistPath)
		return fmt.Errorf("launchctl load: %s: %w", strings.TrimSpace(string(out)), err)
	}

	_ = m.run.Run("launchctl", "start", label)
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

	if err := m.stopAndUnload(plistPath, name, level); err != nil {
		return err
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
	out, err := m.run.CombinedOutput("launchctl", "list", label)
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

func (m *darwinManager) Start(name string, level Level) error {
	plistPath, err := m.plistPath(name, level)
	if err != nil {
		return err
	}
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		return ErrNotInstalled
	}

	label := launchdLabel(name, level)
	out, err := m.run.CombinedOutput("launchctl", "load", "-w", plistPath)
	if err != nil {
		if strings.Contains(string(out), "already loaded") {
			if err := m.run.Run("launchctl", "start", label); err != nil {
				return fmt.Errorf("launchctl start: %w", err)
			}
			return nil
		}
		return fmt.Errorf("launchctl load: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (m *darwinManager) Stop(name string, level Level) error {
	plistPath, err := m.plistPath(name, level)
	if err != nil {
		return err
	}
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		return ErrNotInstalled
	}
	return m.stopAndUnload(plistPath, name, level)
}

func (m *darwinManager) stopAndUnload(plistPath, name string, level Level) error {
	label := launchdLabel(name, level)
	_ = m.run.Run("launchctl", "stop", label)

	out, err := m.run.CombinedOutput("launchctl", "unload", plistPath)
	if err != nil {
		return fmt.Errorf("launchctl unload: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func (m *darwinManager) Restart(name string, level Level) error {
	if err := m.Stop(name, level); err != nil {
		return fmt.Errorf("stop: %w", err)
	}
	return m.Start(name, level)
}

func (m *darwinManager) Logs(name string, level Level, follow bool, lines int) error {
	dir := LogDir(level)
	stderrLog := filepath.Join(dir, "launchd.stderr.log")
	stdoutLog := filepath.Join(dir, "launchd.stdout.log")

	logFile := stderrLog
	if _, err := os.Stat(logFile); os.IsNotExist(err) {
		logFile = stdoutLog
		if _, err := os.Stat(logFile); os.IsNotExist(err) {
			return fmt.Errorf("log file not found in %s", dir)
		}
	}

	args := []string{}
	if follow {
		args = append(args, "-f")
	}
	args = append(args, "-n", strconv.Itoa(lines), logFile)

	cmd := exec.Command("tail", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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
