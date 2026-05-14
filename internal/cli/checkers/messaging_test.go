package checkers

import (
	"context"
	"fmt"
	"os"
	"strings"
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

func TestMultiBotConfig_NoConfigPath(t *testing.T) {
	t.Parallel()

	orig := configPath
	configPath = ""
	defer func() { configPath = orig }()

	c := multiBotConfigChecker{}
	d := c.Check(context.Background())
	require.Equal(t, cli.StatusPass, d.Status)
}

func TestMultiBotConfig_ValidSingleBot(t *testing.T) {
	dir := t.TempDir()
	configPath = dir + "/config.yaml"
	require.NoError(t, os.WriteFile(configPath, []byte(`
messaging:
  slack:
    enabled: true
    bots:
      - name: default
        bot_token: xoxb-test
        app_token: xapp-test
`), 0o644))

	c := multiBotConfigChecker{}
	d := c.Check(context.Background())
	require.Equal(t, cli.StatusPass, d.Status)
	require.Contains(t, d.Message, "1 bot")
}

func TestMultiBotConfig_DuplicateName(t *testing.T) {
	dir := t.TempDir()
	configPath = dir + "/config.yaml"
	require.NoError(t, os.WriteFile(configPath, []byte(`
messaging:
  slack:
    enabled: true
    bots:
      - name: bot1
        bot_token: xoxb-aaa
        app_token: xapp-aaa
      - name: bot1
        bot_token: xoxb-bbb
        app_token: xapp-bbb
`), 0o644))

	c := multiBotConfigChecker{}
	d := c.Check(context.Background())
	require.Equal(t, cli.StatusFail, d.Status)
	require.Contains(t, d.Message, `duplicate bot name "bot1"`)
}

func TestMultiBotConfig_MissingCredentials(t *testing.T) {
	dir := t.TempDir()
	configPath = dir + "/config.yaml"
	require.NoError(t, os.WriteFile(configPath, []byte(`
messaging:
  feishu:
    enabled: true
    bots:
      - name: empty-bot
        app_id: ""
        app_secret: ""
`), 0o644))

	c := multiBotConfigChecker{}
	d := c.Check(context.Background())
	require.Equal(t, cli.StatusFail, d.Status)
	require.Contains(t, d.Message, `no credentials`)
}

func TestMultiBotConfig_ExceedsLimit(t *testing.T) {
	dir := t.TempDir()
	configPath = dir + "/config.yaml"

	var botLines strings.Builder
	for i := range 11 {
		fmt.Fprintf(&botLines, "      - name: bot-%d\n        bot_token: xoxb-%d\n        app_token: xapp-%d\n", i, i, i)
	}
	require.NoError(t, os.WriteFile(configPath, []byte("messaging:\n  slack:\n    enabled: true\n    bots:\n"+botLines.String()), 0o644))

	c := multiBotConfigChecker{}
	d := c.Check(context.Background())
	require.Equal(t, cli.StatusFail, d.Status)
	require.Contains(t, d.Message, "exceed limit")
}
