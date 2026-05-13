package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/service"
	"github.com/hrygo/hotplex/internal/worker/proc"
)

func newRestartHelperCmd() *cobra.Command {
	var oldPID int
	var source string
	var configPath string
	var level string
	var devMode, daemon bool

	cmd := &cobra.Command{
		Use:    "_restart-helper",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRestartHelper(oldPID, source, configPath, level, devMode, daemon)
		},
	}
	cmd.Flags().IntVar(&oldPID, "old-pid", 0, "PID of the old gateway process")
	cmd.Flags().StringVar(&source, "source", "pid", "discovery source (pid|service)")
	cmd.Flags().StringVar(&configPath, "config", "", "config file path")
	cmd.Flags().StringVar(&level, "level", "", "service level (user|system)")
	cmd.Flags().BoolVar(&devMode, "dev", false, "development mode")
	cmd.Flags().BoolVarP(&daemon, "daemon", "d", false, "restart as daemon")
	return cmd
}

func forkRestartHelper(inst *gatewayInstance, configPath string, devMode, daemon bool) error {
	if err := checkRestartCooldown(); err != nil {
		return err
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	args := []string{
		"gateway", "_restart-helper",
		"--old-pid", fmt.Sprintf("%d", inst.PID),
		"--source", string(inst.Source),
	}
	if configPath != "" {
		args = append(args, "--config", configPath)
	}
	if inst.Level != "" {
		args = append(args, "--level", string(inst.Level))
	}
	if devMode {
		args = append(args, "--dev")
	}
	if daemon {
		args = append(args, "-d")
	}

	logDir := filepath.Join(config.HotplexHome(), "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	logPath := filepath.Join(logDir, "gateway-restart.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open restart log: %w", err)
	}
	defer func() { _ = logFile.Close() }()

	helperCmd := exec.Command(self, args...)
	helperCmd.Stdout = logFile
	helperCmd.Stderr = logFile
	helperCmd.Stdin = nil
	helperCmd.SysProcAttr = restartHelperSysProcAttr()

	if err := helperCmd.Start(); err != nil {
		return fmt.Errorf("spawn restart helper: %w", err)
	}

	helperPID := helperCmd.Process.Pid
	if err := writeRestartMarker(helperPID); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not write restart marker: %s\n", err)
	}

	_ = helperCmd.Process.Release()

	time.Sleep(500 * time.Millisecond)
	if err := proc.IsProcessAlive(helperPID); err != nil {
		removeRestartMarker()
		return fmt.Errorf("restart helper exited unexpectedly; check %s", logPath)
	}

	fmt.Fprintf(os.Stderr, "gateway: restart helper spawned (PID %d, log: %s)\n", helperPID, logPath)
	return nil
}

func runRestartHelper(oldPID int, source, configPath, levelStr string, devMode, daemon bool) error {
	defer removeRestartMarker()

	logDir := filepath.Join(config.HotplexHome(), "logs")
	logPath := filepath.Join(logDir, "gateway-restart.log")

	switch source {
	case "service":
		var lvl service.Level
		switch levelStr {
		case "system":
			lvl = service.LevelSystem
		default:
			lvl = service.LevelUser
		}
		if err := service.NewManager().Restart("hotplex", lvl); err != nil {
			appendRestartLog(logPath, "service restart failed: %s\n", err)
			return fmt.Errorf("service restart: %w", err)
		}
		appendRestartLog(logPath, "service restart completed\n")

	default: // "pid"
		appendRestartLog(logPath, "stopping old gateway (PID %d)\n", oldPID)

		if err := proc.GracefulTerminate(oldPID); err != nil {
			appendRestartLog(logPath, "graceful terminate failed: %s, force killing\n", err)
			_ = proc.ForceKill(oldPID)
		}
		waitForProcessExit(oldPID, 30*time.Second)

		if proc.IsProcessAlive(oldPID) == nil {
			appendRestartLog(logPath, "process %d still alive after timeout, force killing\n", oldPID)
			_ = proc.ForceKill(oldPID)
			time.Sleep(500 * time.Millisecond)
		}

		removeGatewayState()
		appendRestartLog(logPath, "old gateway stopped, starting new instance\n")

		if daemon {
			if err := startDaemon(configPath, devMode); err != nil {
				appendRestartLog(logPath, "daemon start failed: %s\n", err)
				return err
			}
			appendRestartLog(logPath, "new gateway started as daemon\n")
			return nil
		}

		if err := writeGatewayState(configPath, devMode); err != nil {
			appendRestartLog(logPath, "warning: could not write PID file: %s\n", err)
		}
		if err := runGateway(configPath, devMode, nil); err != nil {
			removeGatewayState()
			appendRestartLog(logPath, "gateway run failed: %s\n", err)
			return err
		}
		appendRestartLog(logPath, "new gateway started\n")
	}

	return nil
}

func appendRestartLog(path, format string, args ...interface{}) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	_, _ = fmt.Fprintf(f, "[%s] ", time.Now().Format(time.RFC3339))
	_, _ = fmt.Fprintf(f, format, args...)
}
