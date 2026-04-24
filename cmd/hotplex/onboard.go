package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/cli/onboard"
	"github.com/hrygo/hotplex/internal/config"
)

func newOnboardCmd() *cobra.Command {
	var nonInteractive, force bool
	var configPath string
	var enableSlack, enableFeishu bool
	var slackAllowFrom, feishuAllowFrom []string
	var slackDMPolicy, slackGroupPolicy string
	var feishuDMPolicy, feishuGroupPolicy string

	cmd := &cobra.Command{
		Use:   "onboard",
		Short: "Interactive configuration wizard",
		Long: `Interactive configuration wizard for first-time setup.
Guides you through creating config.yaml and .env with sensible defaults.
Supports non-interactive mode for automated deployments.`,
		Example: `  hotplex onboard                    # Interactive setup
  hotplex onboard --non-interactive   # Use defaults, no prompts
  hotplex onboard --enable-slack --enable-feishu  # Enable all platforms
  hotplex onboard --force             # Overwrite existing config`,
		RunE: func(cmd *cobra.Command, args []string) error {
			var err error
			configPath, err = config.ExpandAndAbs(configPath)
			if err != nil {
				return fmt.Errorf("resolve config path: %w", err)
			}

			result, err := onboard.Run(context.Background(), onboard.WizardOptions{
				ConfigPath:        configPath,
				NonInteractive:    nonInteractive,
				Force:             force,
				EnableSlack:       enableSlack,
				EnableFeishu:      enableFeishu,
				SlackAllowFrom:    slackAllowFrom,
				SlackDMPolicy:     slackDMPolicy,
				SlackGroupPolicy:  slackGroupPolicy,
				FeishuAllowFrom:   feishuAllowFrom,
				FeishuDMPolicy:    feishuDMPolicy,
				FeishuGroupPolicy: feishuGroupPolicy,
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
	configFlag(cmd, &configPath)

	cmd.Flags().BoolVar(&enableSlack, "enable-slack", false, "enable Slack in non-interactive mode (credentials in .env)")
	cmd.Flags().BoolVar(&enableFeishu, "enable-feishu", false, "enable Feishu in non-interactive mode (credentials in .env)")
	cmd.Flags().StringSliceVar(&slackAllowFrom, "slack-allow-from", nil, "Slack allowed user IDs")
	cmd.Flags().StringVar(&slackDMPolicy, "slack-dm-policy", "allowlist", "Slack DM policy: open, allowlist, disabled")
	cmd.Flags().StringVar(&slackGroupPolicy, "slack-group-policy", "allowlist", "Slack group policy: open, allowlist, disabled")
	cmd.Flags().StringSliceVar(&feishuAllowFrom, "feishu-allow-from", nil, "Feishu allowed user IDs")
	cmd.Flags().StringVar(&feishuDMPolicy, "feishu-dm-policy", "allowlist", "Feishu DM policy: open, allowlist, disabled")
	cmd.Flags().StringVar(&feishuGroupPolicy, "feishu-group-policy", "allowlist", "Feishu group policy: open, allowlist, disabled")

	return cmd
}
