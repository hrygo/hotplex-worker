package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/service"
)

func newServiceStatusCmd() *cobra.Command {
	var levelStr string
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status",
		Short: "Check service status",
		RunE: func(cmd *cobra.Command, args []string) error {
			level, err := service.ParseLevel(levelStr)
			if err != nil {
				return err
			}

			mgr := service.NewManager()
			s, err := mgr.Status("hotplex", level)
			if err != nil {
				return err
			}

			if jsonOutput {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(s)
			}

			if !s.Installed {
				fmt.Fprintf(os.Stderr, "  Service not installed (%s)\n", level)
				return nil
			}

			statusIcon := "●"
			if s.Running {
				statusIcon = "✓"
			}
			fmt.Fprintf(os.Stderr, "  %s hotplex (%s): %s\n", statusIcon, level, s.StatusText)
			if s.PID > 0 {
				fmt.Fprintf(os.Stderr, "    PID: %d\n", s.PID)
			}
			fmt.Fprintf(os.Stderr, "    Unit: %s\n", s.UnitPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&levelStr, "level", "user", "service level: user or system")
	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output in JSON format")

	return cmd
}
