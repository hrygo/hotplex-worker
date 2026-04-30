package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/service"
)

func newServiceInstallCmd() *cobra.Command {
	var configPath string
	var levelStr string

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install as system service",
		Long: `Install HotPlex gateway as a system service.

  --level user   Install for the current user (default, no root required)
  --level system Install system-wide (requires root/sudo)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			level, err := service.ParseLevel(levelStr)
			if err != nil {
				return err
			}

			configPath, err = config.ExpandAndAbs(configPath)
			if err != nil {
				return fmt.Errorf("resolve config path: %w", err)
			}

			if _, err := os.Stat(configPath); err != nil {
				return fmt.Errorf("config not found: %s (run 'hotplex onboard' first)", configPath)
			}

			if level == service.LevelSystem && os.Getuid() != 0 {
				return fmt.Errorf("system-level service requires root (use sudo or --level user)")
			}

			binaryPath, err := service.ResolveBinaryPath()
			if err != nil {
				return err
			}

			envPath := filepath.Join(filepath.Dir(configPath), ".env")

			mgr := service.NewManager()
			opts := service.InstallOptions{
				BinaryPath: binaryPath,
				ConfigPath: configPath,
				EnvPath:    envPath,
				Level:      level,
				Name:       "hotplex",
			}

			if err := mgr.Install(opts); err != nil {
				return fmt.Errorf("install service: %w", err)
			}

			s, _ := mgr.Status("hotplex", level)
			fmt.Fprintf(os.Stderr, "  ✓ Service installed (%s)\n", level)
			if s != nil && s.UnitPath != "" {
				fmt.Fprintf(os.Stderr, "    %s\n", s.UnitPath)
			}
			fmt.Fprintf(os.Stderr, "\n  Manage with: hotplex service status / uninstall\n")
			return nil
		},
	}

	configFlag(cmd, &configPath)
	cmd.Flags().StringVar(&levelStr, "level", "user", "service level: user or system")

	return cmd
}
