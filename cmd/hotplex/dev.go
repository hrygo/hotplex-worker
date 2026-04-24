package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newDevCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Quick start in development mode",
		Long: "Start the gateway in development mode — a shortcut for 'hotplex gateway start --dev'.\n" +
			"In dev mode, API key authentication and admin tokens are disabled for easier local testing.",
		Example: `  hotplex dev                        # Start in dev mode
  hotplex dev -c /path/to/config.yaml`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := writeGatewayPID(); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not write PID file: %s\n", err)
			}
			return runGateway(configPath, true)
		},
	}
	configFlag(cmd, &configPath)
	return cmd
}
