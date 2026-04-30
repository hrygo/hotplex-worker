package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/worker/proc"
)

func newGatewayCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gateway",
		Short: "Manage the gateway server",
		Long:  `Manage the gateway server lifecycle — start, stop, or restart.`,
		Example: `  hotplex gateway start              # Start with default config
  hotplex gateway start -c /path/to/config.yaml
  hotplex gateway start --dev          # Development mode (no auth)
  hotplex gateway stop
  hotplex gateway restart`,
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
	var devMode bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the gateway server",
		Long: `Start the gateway server. Loads configuration from the specified config file (default: ~/.hotplex/config.yaml).
In dev mode (--dev), API key authentication and admin tokens are disabled.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := writeGatewayPID(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not write PID file: %s\n", err)
			}
			return runGateway(configPath, devMode)
		},
	}
	configFlag(cmd, &configPath)
	cmd.Flags().BoolVar(&devMode, "dev", false, "development mode")
	return cmd
}

func newGatewayStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the running gateway server",
		Long:  `Stop the running gateway server by sending graceful termination to the process recorded in the PID file.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			pid, err := readGatewayPID()
			if err != nil {
				return err
			}
			if err := proc.GracefulTerminate(pid); err != nil {
				return fmt.Errorf("stop PID %d: %w", pid, err)
			}
			fmt.Fprintf(os.Stderr, "gateway: sent graceful termination to PID %d\n", pid)
			removeGatewayPID()
			return nil
		},
	}
}

func newGatewayRestartCmd() *cobra.Command {
	var configPath string
	var devMode bool

	cmd := &cobra.Command{
		Use:   "restart",
		Short: "Restart the gateway server",
		Long: `Restart the gateway server by stopping the current instance and starting a new one.
Preserves the same configuration file and mode.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			pid, err := readGatewayPID()
			if err != nil {
				return err
			}
			if err := proc.GracefulTerminate(pid); err != nil {
				return fmt.Errorf("stop PID %d: %w", pid, err)
			}
			fmt.Fprintf(os.Stderr, "gateway: stopped PID %d\n", pid)
			removeGatewayPID()

			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				if err := proc.IsProcessAlive(pid); err != nil {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}

			if err := writeGatewayPID(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not write PID file: %s\n", err)
			}
			return runGateway(configPath, devMode)
		},
	}
	configFlag(cmd, &configPath)
	cmd.Flags().BoolVar(&devMode, "dev", false, "development mode")
	return cmd
}
