package checkers

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/cli"
)

func TestSTTEnvironmentChecker_Name(t *testing.T) {
	t.Parallel()
	c := sttEnvironmentChecker{}
	require.Equal(t, "stt.runtime", c.Name())
	require.Equal(t, "stt", c.Category())
}

func TestSTTEnvironmentChecker_NotConfigured(t *testing.T) {
	defer resetConfigPath()
	SetConfigPath("")

	c := sttEnvironmentChecker{}
	d := c.Check(context.Background())
	require.Equal(t, cli.StatusPass, d.Status)
	require.Contains(t, d.Message, "not configured")
}

func TestSTTEnvironmentChecker_ConfigLoadFails(t *testing.T) {
	dir := t.TempDir()
	defer resetConfigPath()
	SetConfigPath(filepath.Join(dir, "nonexistent.yaml"))

	c := sttEnvironmentChecker{}
	d := c.Check(context.Background())
	require.Equal(t, cli.StatusPass, d.Status)
	require.Contains(t, d.Message, "not configured")
}

func TestSTTEnvironmentChecker_STTDisabled(t *testing.T) {
	dir := t.TempDir()
	// Both platforms disabled — no STT deps needed.
	path := writeSTTConfig(t, dir, false, "", false, "")
	defer resetConfigPath()
	SetConfigPath(path)

	c := sttEnvironmentChecker{}
	d := c.Check(context.Background())
	require.Equal(t, cli.StatusPass, d.Status)
	require.Contains(t, d.Message, "not configured")
}

func TestSTTEnvironmentChecker_LocalProvider(t *testing.T) {
	dir := t.TempDir()
	path := writeSTTConfig(t, dir, true, "local", false, "")
	defer resetConfigPath()
	SetConfigPath(path)

	c := sttEnvironmentChecker{}
	d := c.Check(context.Background())
	require.Equal(t, "stt.runtime", d.Name)
	require.Contains(t, []cli.Status{cli.StatusPass, cli.StatusFail}, d.Status)
	if d.Status == cli.StatusFail {
		require.NotEmpty(t, d.FixHint)
	}
}

func TestSTTEnvironmentChecker_FeishuProvider(t *testing.T) {
	dir := t.TempDir()
	path := writeSTTConfig(t, dir, false, "", true, "feishu")
	defer resetConfigPath()
	SetConfigPath(path)

	c := sttEnvironmentChecker{}
	d := c.Check(context.Background())
	require.Contains(t, []cli.Status{cli.StatusPass, cli.StatusFail}, d.Status)
	if d.Status == cli.StatusFail {
		require.Contains(t, d.Message, "ffmpeg")
	}
}

func TestSTTEnvironmentChecker_FeishuLocalProvider(t *testing.T) {
	dir := t.TempDir()
	path := writeSTTConfig(t, dir, false, "", true, "feishu+local")
	defer resetConfigPath()
	SetConfigPath(path)

	c := sttEnvironmentChecker{}
	d := c.Check(context.Background())
	require.Contains(t, []cli.Status{cli.StatusPass, cli.StatusFail}, d.Status)
}

func TestSTTRequirements(t *testing.T) {
	tests := []struct {
		name        string
		slackOn     bool
		slackProv   string
		feishuOn    bool
		feishuProv  string
		needsPython bool
		needsFFmpeg bool
	}{
		{"both_disabled", false, "", false, "", false, false},
		{"slack_local", true, "local", false, "", true, false},
		{"feishu_cloud", false, "", true, "feishu", false, true},
		{"feishu_local", false, "", true, "feishu+local", true, true},
		{"both_enabled", true, "local", true, "feishu", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeSTTConfig(t, dir, tt.slackOn, tt.slackProv, tt.feishuOn, tt.feishuProv)
			defer resetConfigPath()
			SetConfigPath(path)

			gotPy, gotFF := sttRequirements()
			require.Equal(t, tt.needsPython, gotPy, "needsPython")
			require.Equal(t, tt.needsFFmpeg, gotFF, "needsFFmpeg")
		})
	}
}

// writeSTTConfig creates a minimal config YAML with explicit platform STT settings.
func writeSTTConfig(t *testing.T, dir string, slackOn bool, slackProv string, feishuOn bool, feishuProv string) string {
	t.Helper()
	yaml := "gateway:\n  addr: \":8888\"\nmessaging:\n"
	yaml += "  slack:\n    enabled: " + strconv.FormatBool(slackOn)
	if slackProv != "" {
		yaml += "\n    stt_provider: \"" + slackProv + "\""
	}
	yaml += "\n  feishu:\n    enabled: " + strconv.FormatBool(feishuOn)
	if feishuProv != "" {
		yaml += "\n    stt_provider: \"" + feishuProv + "\""
	}
	yaml += "\n"

	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))
	return path
}
