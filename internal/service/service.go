package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

type Level string

const (
	LevelUser   Level = "user"
	LevelSystem Level = "system"
)

func ParseLevel(s string) (Level, error) {
	switch s {
	case "user", "":
		return LevelUser, nil
	case "system":
		return LevelSystem, nil
	default:
		return "", fmt.Errorf("invalid service level %q (use \"user\" or \"system\")", s)
	}
}

type InstallOptions struct {
	BinaryPath string
	ConfigPath string
	Level      Level
	Name       string
	WorkDir    string // resolved worker default_work_dir for WorkingDirectory
}

type Status struct {
	Installed  bool
	Running    bool
	Level      Level
	UnitPath   string
	PID        int
	StatusText string
}

type Manager interface {
	Install(opts InstallOptions) error
	Uninstall(name string, level Level) error
	Status(name string, level Level) (*Status, error)
	Start(name string, level Level) error
	Stop(name string, level Level) error
	Restart(name string, level Level) error
	Logs(name string, level Level, follow bool, lines int) error
}

func ResolveBinaryPath() (string, error) {
	// Prefer the binary found in PATH (standard deployment location).
	// This avoids capturing a build-directory path when the user runs
	// ./bin/hotplex-linux-amd64 service install.
	if p, err := exec.LookPath("hotplex"); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			if resolved, err := filepath.EvalSymlinks(abs); err == nil {
				return resolved, nil
			}
			return abs, nil
		}
		return p, nil
	}

	// Fallback: resolve the currently running executable.
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve binary: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("eval symlinks: %w", err)
	}
	return exe, nil
}

func LogDir(level Level) string {
	if level == LevelSystem {
		return systemLogDir()
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".hotplex", "logs")
}

var ErrNotInstalled = fmt.Errorf("service not installed (run 'hotplex service install' first)")
