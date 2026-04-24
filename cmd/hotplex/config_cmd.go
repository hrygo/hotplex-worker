package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage configuration",
		Long:  `Manage HotPlex configuration files.`,
	}
	cmd.AddCommand(newConfigValidateCmd())
	return cmd
}

func newConfigValidateCmd() *cobra.Command {
	var configPath string
	var strict bool

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate configuration file",
		Long: "Validate the configuration file without starting the gateway.\n" +
			"Checks YAML syntax, required fields, and value constraints.\n" +
			"Use --strict to also verify that required secrets (JWT, admin tokens) are set.",
		Example: `  hotplex config validate                     # Validate default config
  hotplex config validate -c /path/to/config.yaml
  hotplex config validate --strict             # Also check secrets`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig(configPath, false)
			if err != nil {
				return err
			}

			warns := cfg.Validate()
			for _, w := range warns {
				fmt.Fprintf(os.Stderr, "  ⚠ %s\n", w)
			}

			if strict {
				if err := cfg.RequireSecrets(); err != nil {
					return err
				}
			}

			if len(warns) > 0 {
				fmt.Fprintf(os.Stderr, "\nConfiguration loaded with %d warning(s).\n", len(warns))
			} else {
				fmt.Fprintln(os.Stderr, "Configuration is valid.")
			}
			return nil
		},
	}
	configFlag(cmd, &configPath)
	cmd.Flags().BoolVar(&strict, "strict", false, "also verify required secrets are set")
	return cmd
}
