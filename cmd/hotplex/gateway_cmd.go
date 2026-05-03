package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/config"
)

func newGatewayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gateway",
		Short: "Manage the gateway server",
		Long:  `Manage the gateway server lifecycle — start, stop, or restart.`,
		Example: `  hotplex gateway start              # Start with default config
  hotplex gateway start -d           # Start as daemon (background)
  hotplex gateway start -c /path/to/config.yaml
  hotplex gateway start --dev        # Development mode (no auth)
  hotplex gateway stop
  hotplex gateway restart
  hotplex gateway restart -d         # Restart as daemon`,
	}
	cmd.AddCommand(
		newGatewayStartCmd(),
		newGatewayStopCmd(),
		newGatewayRestartCmd(),
	)
	return cmd
}

func newGatewayStartCmd() *cobra.Command {
	var configPath string
	var devMode, daemon bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the gateway server",
		Long: `Start the gateway server. Loads configuration from the specified config file (default: ~/.hotplex/config.yaml).
In dev mode (--dev), API key authentication and admin tokens are disabled.
Use -d to run as a background daemon.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if daemon {
				return startDaemon(configPath, devMode)
			}
			if err := writeGatewayState(configPath, devMode); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not write PID file: %s\n", err)
			}
			return runGateway(configPath, devMode, nil)
		},
	}
	configFlag(cmd, &configPath)
	cmd.Flags().BoolVar(&devMode, "dev", false, "development mode")
	cmd.Flags().BoolVarP(&daemon, "daemon", "d", false, "run as background daemon")
	return cmd
}

func newGatewayStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running gateway server",
		Long:  `Stop the running gateway server. Detects both PID-file-managed and service-managed instances.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			inst, err := findRunningGateway()
			if err != nil {
				return err
			}
			if err := stopGateway(inst); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "gateway: stopped (%s, PID %d)\n", inst.Source, inst.PID)
			return nil
		},
	}
}

func newGatewayRestartCmd() *cobra.Command {
	var configPath string
	var devMode, daemon bool

	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Restart the gateway server",
		Long: `Restart the gateway server by stopping the current instance and starting a new one.
Preserves the same configuration file and mode.
Use -d to restart as a background daemon.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			inst, err := findRunningGateway()
			if err != nil {
				fmt.Fprintf(os.Stderr, "gateway: %s (proceeding with start)\n", err)
			} else {
				if stopErr := stopGateway(inst); stopErr != nil {
					fmt.Fprintf(os.Stderr, "gateway: stop failed: %s (proceeding with start)\n", stopErr)
				} else {
					fmt.Fprintf(os.Stderr, "gateway: stopped (%s, PID %d)\n", inst.Source, inst.PID)
				}

				if inst.Source == sourcePID {
					waitForProcessExit(inst.PID, 5*time.Second)
				} else {
					time.Sleep(2 * time.Second)
				}

				// Use the running instance's config if user didn't specify one.
				if configPath == defaultConfigPath && inst.ConfigPath != "" {
					configPath = inst.ConfigPath
				}
				if !devMode && inst.DevMode {
					devMode = true
				}
			}

			if daemon {
				return startDaemon(configPath, devMode)
			}
			if err := writeGatewayState(configPath, devMode); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not write PID file: %s\n", err)
			}
			return runGateway(configPath, devMode, nil)
		},
	}
	configFlag(cmd, &configPath)
	cmd.Flags().BoolVar(&devMode, "dev", false, "development mode")
	cmd.Flags().BoolVarP(&daemon, "daemon", "d", false, "run as background daemon")
	return cmd
}

// startDaemon re-executes the current binary in the background without -d,
// writes the child PID, and redirects output to a log file.
func startDaemon(configPath string, devMode bool) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable: %w", err)
	}

	// Build args: "gateway" "start" [+ config flag] [+ --dev] — no -d
	daemonArgs := []string{"gateway", "start"}
	if configPath != "" {
		daemonArgs = append(daemonArgs, "-c", configPath)
	}
	if devMode {
		daemonArgs = append(daemonArgs, "--dev")
	}

	// Redirect stdout+stderr to log file
	logDir := filepath.Join(config.HotplexHome(), "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return fmt.Errorf("create log dir: %w", err)
	}
	logPath := filepath.Join(logDir, "gateway.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer func() { _ = logFile.Close() }()

	daemonCmd := exec.Command(self, daemonArgs...)
	daemonCmd.Stdout = logFile
	daemonCmd.Stderr = logFile
	daemonCmd.Stdin = nil
	daemonCmd.SysProcAttr = daemonSysProcAttr()

	if err := daemonCmd.Start(); err != nil {
		return fmt.Errorf("start daemon: %w", err)
	}

	// Write child state (PID + config path + dev mode)
	childPID := daemonCmd.Process.Pid
	pidPath := gatewayPIDPath()
	if err := os.MkdirAll(filepath.Dir(pidPath), 0o755); err == nil {
		state := gatewayState{PID: childPID, ConfigPath: configPath, DevMode: devMode}
		data, _ := json.Marshal(state)
		_ = os.WriteFile(pidPath, data, 0o644)
	}

	// Release the child so it survives parent exit
	_ = daemonCmd.Process.Release()

	fmt.Fprintf(os.Stderr, "gateway: started as daemon (PID %d, log: %s)\n", childPID, logPath)
	return nil
}
