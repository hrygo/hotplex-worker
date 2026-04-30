package main

import (
	"github.com/spf13/cobra"
)

func newServiceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Manage system service",
		Long: `Install, uninstall, or check the HotPlex gateway as a system service.

Supports both user-level and system-level service installation.
  Linux:  systemd (user or system)
  macOS:  launchd (LaunchAgents or LaunchDaemons)`,
	}
	cmd.AddCommand(
		newServiceInstallCmd(),
		newServiceUninstallCmd(),
		newServiceStatusCmd(),
		newServiceStartCmd(),
		newServiceStopCmd(),
		newServiceRestartCmd(),
		newServiceLogsCmd(),
	)
	return cmd
}
