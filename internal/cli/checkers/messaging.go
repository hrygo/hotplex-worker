package checkers

import (
	"context"
	"os"
	"strings"

	"github.com/hrygo/hotplex/internal/cli"
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
}
