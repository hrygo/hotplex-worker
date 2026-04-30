package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/service"
)

func newServiceUninstallCmd() *cobra.Command {
	var levelStr string

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Uninstall system service",
		RunE: func(cmd *cobra.Command, args []string) error {
			level, err := service.ParseLevel(levelStr)
			if err != nil {
				return err
			}

			mgr := service.NewManager()
			if err := mgr.Uninstall("hotplex", level); err != nil {
				return fmt.Errorf("uninstall service: %w", err)
			}

			fmt.Fprintf(os.Stderr, "  ✓ Service uninstalled (%s)\n", level)
			return nil
		},
	}

	cmd.Flags().StringVar(&levelStr, "level", "user", "service level: user or system")

	return cmd
}
