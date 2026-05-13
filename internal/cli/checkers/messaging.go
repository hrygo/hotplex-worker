package checkers

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/hrygo/hotplex/internal/cli"
	"github.com/hrygo/hotplex/internal/config"
)

type slackCredsChecker struct{}

func (c slackCredsChecker) Name() string     { return "messaging.slack_creds" }
func (c slackCredsChecker) Category() string { return "messaging" }
func (c slackCredsChecker) Check(ctx context.Context) cli.Diagnostic {
	botToken := os.Getenv("SLACK_BOT_TOKEN")
	appToken := os.Getenv("SLACK_APP_TOKEN")

	if botToken == "" && appToken == "" {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  "Slack not configured (no tokens set)",
		}
	}

	var issues []string
	if botToken != "" && !strings.HasPrefix(botToken, "xoxb-") {
		issues = append(issues, "SLACK_BOT_TOKEN has invalid prefix (expected xoxb-)")
	}
	if appToken != "" && !strings.HasPrefix(appToken, "xapp-") {
		issues = append(issues, "SLACK_APP_TOKEN has invalid prefix (expected xapp-)")
	}

	if len(issues) > 0 {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "Invalid Slack token format: " + strings.Join(issues, "; "),
			FixHint:  "Check token values in .env — bot tokens start with xoxb-, app tokens with xapp-",
		}
	}

	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusPass,
		Message:  "Slack token format valid",
	}
}

type feishuCredsChecker struct{}

func (c feishuCredsChecker) Name() string     { return "messaging.feishu_creds" }
func (c feishuCredsChecker) Category() string { return "messaging" }
func (c feishuCredsChecker) Check(ctx context.Context) cli.Diagnostic {
	appID := os.Getenv("FEISHU_APP_ID")
	appSecret := os.Getenv("FEISHU_APP_SECRET")

	if appID == "" && appSecret == "" {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  "Feishu not configured (no credentials set)",
		}
	}

	var issues []string
	if appID != "" && strings.TrimSpace(appID) == "" {
		issues = append(issues, "FEISHU_APP_ID is whitespace-only")
	}
	if appSecret != "" && strings.TrimSpace(appSecret) == "" {
		issues = append(issues, "FEISHU_APP_SECRET is whitespace-only")
	}

	if len(issues) > 0 {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusFail,
			Message:  "Invalid Feishu credentials: " + strings.Join(issues, "; "),
			FixHint:  "Check FEISHU_APP_ID and FEISHU_APP_SECRET values in .env",
		}
	}

	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusPass,
		Message:  "Feishu credentials present",
	}
}

func init() {
	cli.DefaultRegistry.Register(slackCredsChecker{})
	cli.DefaultRegistry.Register(feishuCredsChecker{})
	cli.DefaultRegistry.Register(multiBotConfigChecker{})
}

// ─── messaging.multi_bot_config ─────────────────────────────────────────────

type multiBotConfigChecker struct{}

func (c multiBotConfigChecker) Name() string     { return "messaging.multi_bot_config" }
func (c multiBotConfigChecker) Category() string { return "messaging" }
func (c multiBotConfigChecker) Check(ctx context.Context) cli.Diagnostic {
	if configPath == "" {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  "Config path not set, skipping multi-bot check",
		}
	}

	cfg, err := config.Load(configPath, config.LoadOptions{})
	if err != nil {
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusWarn,
			Message:  "Cannot load config: " + err.Error(),
			FixHint:  "Fix config syntax errors first",
		}
	}

	var issues []string
	issues = append(issues, checkBotEntries("slack", mapSlackBots(cfg.Messaging.Slack.Bots))...)
	issues = append(issues, checkBotEntries("feishu", mapFeishuBots(cfg.Messaging.Feishu.Bots))...)

	if len(issues) == 0 {
		totalBots := len(cfg.Messaging.Slack.Bots) + len(cfg.Messaging.Feishu.Bots)
		if totalBots == 0 {
			return cli.Diagnostic{
				Name:     c.Name(),
				Category: c.Category(),
				Status:   cli.StatusPass,
				Message:  "No bots configured",
			}
		}
		return cli.Diagnostic{
			Name:     c.Name(),
			Category: c.Category(),
			Status:   cli.StatusPass,
			Message:  fmt.Sprintf("Multi-bot config valid (%d bot(s))", totalBots),
		}
	}

	return cli.Diagnostic{
		Name:     c.Name(),
		Category: c.Category(),
		Status:   cli.StatusFail,
		Message:  strings.Join(issues, "; "),
		FixHint:  "Fix multi-bot config: ensure unique names, non-empty credentials, max 10 bots per platform",
	}
}

type botCheck struct {
	Name  string
	Cred1 string
	Cred2 string
}

func checkBotEntries(platform string, bots []botCheck) []string {
	var issues []string
	if len(bots) > 10 {
		issues = append(issues, fmt.Sprintf("%s: %d bots exceed limit (max 10)", platform, len(bots)))
	}
	seen := make(map[string]bool, len(bots))
	for _, b := range bots {
		if b.Name == "" {
			issues = append(issues, fmt.Sprintf("%s: bot missing name", platform))
			continue
		}
		if seen[b.Name] {
			issues = append(issues, fmt.Sprintf("%s: duplicate bot name %q", platform, b.Name))
		}
		seen[b.Name] = true
		if strings.TrimSpace(b.Cred1) == "" && strings.TrimSpace(b.Cred2) == "" {
			issues = append(issues, fmt.Sprintf("%s: bot %q has no credentials", platform, b.Name))
		}
	}
	return issues
}

func mapSlackBots(bots []config.SlackBotConfig) []botCheck {
	result := make([]botCheck, len(bots))
	for i, b := range bots {
		result[i] = botCheck{Name: b.Name, Cred1: b.BotToken, Cred2: b.AppToken}
	}
	return result
}

func mapFeishuBots(bots []config.FeishuBotConfig) []botCheck {
	result := make([]botCheck, len(bots))
	for i, b := range bots {
		result[i] = botCheck{Name: b.Name, Cred1: b.AppID, Cred2: b.AppSecret}
	}
	return result
}
