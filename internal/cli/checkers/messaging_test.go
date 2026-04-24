package checkers

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/cli"
)

func TestSlackCreds_NoTokens(t *testing.T) {
	t.Parallel()

	c := slackCredsChecker{}
	d := c.Check(context.Background())

	require.Equal(t, "messaging.slack_creds", d.Name)
	require.Equal(t, "messaging", d.Category)
	require.Equal(t, cli.StatusPass, d.Status)
}

func TestSlackCreds_ValidTokens(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel in Go 1.26

	t.Setenv("SLACK_BOT_TOKEN", "xoxb-test-token")
	t.Setenv("SLACK_APP_TOKEN", "xapp-test-token")

	c := slackCredsChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusPass, d.Status)
}

func TestSlackCreds_InvalidPrefix(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel in Go 1.26

	t.Setenv("SLACK_BOT_TOKEN", "invalid-token")

	c := slackCredsChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusFail, d.Status)
}

func TestFeishuCreds_NoCreds(t *testing.T) {
	t.Parallel()

	c := feishuCredsChecker{}
	d := c.Check(context.Background())

	require.Equal(t, "messaging.feishu_creds", d.Name)
	require.Equal(t, "messaging", d.Category)
	require.Equal(t, cli.StatusPass, d.Status)
}

func TestFeishuCreds_ValidCreds(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel in Go 1.26

	t.Setenv("FEISHU_APP_ID", "cli_test123")
	t.Setenv("FEISHU_APP_SECRET", "secret123")

	c := feishuCredsChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusPass, d.Status)
}

func TestFeishuCreds_WhitespaceOnly(t *testing.T) {
	// t.Setenv is incompatible with t.Parallel in Go 1.26

	t.Setenv("FEISHU_APP_ID", "   ")

	c := feishuCredsChecker{}
	d := c.Check(context.Background())

	require.Equal(t, cli.StatusFail, d.Status)
}
