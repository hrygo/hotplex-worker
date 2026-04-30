package main

import (
	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/service"
)

func newServiceLogsCmd() *cobra.Command {
	var levelStr string
	var follow bool
	var lines int

	cmd := &cobra.Command{
		Use:   "logs",
		Short: "View service logs",
		Long: `View HotPlex service logs.

  Linux:  journalctl (with --user for user-level services)
  macOS:  tail the launchd log files`,
		RunE: func(cmd *cobra.Command, args []string) error {
			level, err := service.ParseLevel(levelStr)
			if err != nil {
				return err
			}

			mgr := service.NewManager()
			return mgr.Logs("hotplex", level, follow, lines)
		},
	}

	cmd.Flags().StringVar(&levelStr, "level", "user", "service level: user or system")
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "follow log output")
	cmd.Flags().IntVarP(&lines, "lines", "n", 100, "number of recent lines to show")

	return cmd
}
