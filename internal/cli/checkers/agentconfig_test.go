package checkers

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/cli"
)

func TestAgentConfigSuffixChecker(t *testing.T) {
	t.Parallel()

	t.Run("nonexistent directory", func(t *testing.T) {
		t.Parallel()
		c := agentConfigSuffixChecker{dir: filepath.Join(t.TempDir(), "nonexistent")}
		d := c.Check(context.Background())
		require.Equal(t, cli.StatusWarn, d.Status)
		require.Contains(t, d.Message, "does not exist")
		require.NotEmpty(t, d.FixHint)
	})

	t.Run("clean directory no suffix files", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		cfgDir := filepath.Join(dir, "agent-configs")
		require.NoError(t, os.MkdirAll(cfgDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "SOUL.md"), []byte("global soul"), 0o644))

		c := agentConfigSuffixChecker{dir: cfgDir}
		d := c.Check(context.Background())
		require.Equal(t, cli.StatusPass, d.Status)
	})

	t.Run("deprecated suffix files detected", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		cfgDir := filepath.Join(dir, "agent-configs")
		require.NoError(t, os.MkdirAll(cfgDir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "SOUL.slack.md"), []byte("slack soul"), 0o644))
		require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "USER.feishu.md"), []byte("feishu user"), 0o644))

		c := agentConfigSuffixChecker{dir: cfgDir}
		d := c.Check(context.Background())
		require.Equal(t, cli.StatusWarn, d.Status)
		require.Contains(t, d.Message, "SOUL.slack.md")
		require.Contains(t, d.Message, "USER.feishu.md")
		require.NotEmpty(t, d.FixHint)
	})

	t.Run("directory entries ignored", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		cfgDir := filepath.Join(dir, "agent-configs")
		require.NoError(t, os.MkdirAll(filepath.Join(cfgDir, "slack"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "slack", "SOUL.md"), []byte("slack soul"), 0o644))

		c := agentConfigSuffixChecker{dir: cfgDir}
		d := c.Check(context.Background())
		require.Equal(t, cli.StatusPass, d.Status)
	})
}
