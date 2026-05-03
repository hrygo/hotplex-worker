package checkers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/cli"
	"github.com/hrygo/hotplex/internal/config"
)

func TestAgentConfigSuffixChecker(t *testing.T) {
	c := agentConfigSuffixChecker{}

	t.Run("deprecated suffix files detected", func(t *testing.T) {
		cfgDir := filepath.Join(config.HotplexHome(), "agent-configs")
		require.NoError(t, os.MkdirAll(cfgDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "SOUL.slack.md"), []byte("slack soul"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "USER.feishu.md"), []byte("feishu user"), 0o644))
		t.Cleanup(func() {
			_ = os.Remove(filepath.Join(cfgDir, "SOUL.slack.md"))
			_ = os.Remove(filepath.Join(cfgDir, "USER.feishu.md"))
		})

		d := c.Check(context.Background())
		require.Equal(t, cli.StatusWarn, d.Status)
		require.Contains(t, d.Message, "SOUL.slack.md")
		require.Contains(t, d.Message, "USER.feishu.md")
		require.NotEmpty(t, d.FixHint)
	})

	t.Run("directory entries ignored", func(t *testing.T) {
		cfgDir := filepath.Join(config.HotplexHome(), "agent-configs")
		require.NoError(t, os.MkdirAll(filepath.Join(cfgDir, "slack"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "slack", "SOUL.md"), []byte("slack soul"), 0o644))
		t.Cleanup(func() { _ = os.RemoveAll(filepath.Join(cfgDir, "slack")) })

		d := c.Check(context.Background())
		require.Equal(t, cli.StatusPass, d.Status)
	})

	t.Run("clean directory passes", func(t *testing.T) {
		cfgDir := filepath.Join(config.HotplexHome(), "agent-configs")
		require.NoError(t, os.MkdirAll(cfgDir, 0o755))

		d := c.Check(context.Background())
		require.Equal(t, cli.StatusPass, d.Status)
	})
}
