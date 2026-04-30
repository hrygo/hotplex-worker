package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/hrygo/hotplex/internal/cli/onboard"
	"github.com/hrygo/hotplex/internal/cli/output"
	"github.com/hrygo/hotplex/internal/config"
)

func newOnboardCmd() *cobra.Command {
	var nonInteractive, force bool
	var configPath string
	var enableSlack, enableFeishu bool
	var slackAllowFrom, feishuAllowFrom []string
	var slackDMPolicy, slackGroupPolicy string
	var feishuDMPolicy, feishuGroupPolicy string
	var installService bool
	var serviceLevel string

	cmd := &cobra.Command{
		Use:   "onboard",
		Short: "Interactive configuration wizard",
		Long: `Interactive configuration wizard for first-time setup or reconfiguration.

Detects existing configuration and prompts to keep or reconfigure.
Guides you through creating config.yaml and .env with sensible defaults.
Supports non-interactive mode for automated deployments.`,
		Example: `  hotplex onboard                    # Interactive setup (detects existing config)
  hotplex onboard --force           # Overwrite existing config
  hotplex onboard --non-interactive # Auto-generate, no prompts
  hotplex onboard --enable-slack --enable-feishu  # Enable all platforms`,
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
				InstallService:    installService,
				ServiceLevel:      serviceLevel,
			})
			if err != nil {
				return err
			}

			fmt.Fprintln(os.Stderr)
			fmt.Fprintf(os.Stderr, "  %s %s\n\n", output.Bold("HotPlex Onboard"), output.Dim(versionString()))

			for _, step := range result.Steps {
				fmt.Fprintf(os.Stderr, "  %s %-20s %s\n",
					output.StatusSymbol(step.Status),
					output.Bold(step.Name),
					output.Dim(step.Detail))
			}

			fmt.Fprintln(os.Stderr)

			if len(result.AgentConfigNew) > 0 {
				displayAgentConfigPanel(result.AgentConfigNew)
			}

			var hasFail bool
			for _, step := range result.Steps {
				if step.Status == "fail" {
					hasFail = true
					break
				}
			}
			if hasFail {
				fmt.Fprintln(os.Stderr, "  "+output.Red("Some steps failed. Review errors above."))
				os.Exit(1)
			}

			switch result.Action {
			case "keep":
				fmt.Fprint(os.Stderr, output.CommandBox("hotplex doctor"))
			default:
				fmt.Fprint(os.Stderr, output.CommandBox("hotplex gateway start"))
			}
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

	cmd.Flags().BoolVar(&installService, "install-service", false, "install as system service in non-interactive mode")
	cmd.Flags().StringVar(&serviceLevel, "service-level", "user", "service level for --install-service (user or system)")

	return cmd
}

// displayAgentConfigPanel shows a description panel for newly created agent-config files.
func displayAgentConfigPanel(created []string) {
	dir := filepath.Join(config.HotplexHome(), "agent-configs")

	descriptions := map[string]string{
		"SOUL.md":   "Agent 人格定义 — 身份、沟通风格和核心价值观",
		"AGENTS.md": "工作区规则 — 自主行为边界、错误处理、输出风格",
		"SKILLS.md": "工具使用指南 — 平台能力和最佳实践",
		"USER.md":   "用户偏好 — 你的技术背景、工作习惯和沟通偏好",
		"MEMORY.md": "上下文记忆 — 跨会话持久化知识（自动管理）",
	}

	fmt.Fprint(os.Stderr, output.SectionHeader("Agent Configuration"))

	fmt.Fprintf(os.Stderr, "  以下文件已生成到 %s：\n", output.Cyan(dir))
	fmt.Fprintln(os.Stderr)

	// Compute max filename length for alignment.
	maxLen := 0
	for _, name := range created {
		if len(name) > maxLen {
			maxLen = len(name)
		}
	}
	for _, name := range created {
		desc := descriptions[name]
		padding := strings.Repeat(" ", maxLen-len(name)+2)
		fmt.Fprintf(os.Stderr, "    %s%s%s\n",
			output.Bold(name), padding, output.Dim(desc))
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "  %s\n", output.Dim("根据需要编辑这些文件来定制 Agent 行为。"))
	fmt.Fprintf(os.Stderr, "  %s\n\n", output.Dim("修改后自动生效（支持热重载），无需重启网关。"))
}
