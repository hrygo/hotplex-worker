package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/hotplex/hotplex-worker/internal/cli/onboard"
)

func newOnboardCmd() *cobra.Command {
	var nonInteractive, force bool
	var configPath string

	cmd := &cobra.Command{
		Use:   "onboard",
		Short: "Interactive configuration wizard",
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" {
				configPath = "~/.hotplex/config.yaml"
			}

			result, err := onboard.Run(context.Background(), onboard.WizardOptions{
				ConfigPath:     configPath,
				NonInteractive: nonInteractive,
				Force:          force,
			})
			if err != nil {
				return err
			}

			fmt.Fprintln(os.Stderr)
			fmt.Fprintf(os.Stderr, "HotPlex Onboard %s\n\n", versionString())

			for _, step := range result.Steps {
				symbol := "?"
				switch step.Status {
				case "pass":
					symbol = "✓"
				case "skip":
					symbol = "○"
				case "fail":
					symbol = "✗"
				}
				fmt.Fprintf(os.Stderr, "  %s %-20s %s\n", symbol, step.Name, step.Detail)
			}

			fmt.Fprintln(os.Stderr)

			var hasFail bool
			for _, step := range result.Steps {
				if step.Status == "fail" {
					hasFail = true
					break
				}
			}
			if hasFail {
				fmt.Fprintln(os.Stderr, "  Some steps failed. Review errors above.")
				os.Exit(1)
			}

			fmt.Fprintln(os.Stderr, "  Configuration complete. Run 'hotplex gateway' to start.")
			return nil
		},
	}

	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "use defaults, no prompts")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing configuration")
	cmd.Flags().StringVarP(&configPath, "config", "c", "~/.hotplex/config.yaml", "config file path")
	return cmd
}
