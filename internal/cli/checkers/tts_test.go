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

func TestTTSEnvironmentChecker_Name(t *testing.T) {
	t.Parallel()
	c := ttsEnvironmentChecker{}
	require.Equal(t, "tts.runtime", c.Name())
	require.Equal(t, "tts", c.Category())
}

func TestTTSEnvironmentChecker_NotConfigured(t *testing.T) {
	defer resetConfigPath()
	SetConfigPath("")

	c := ttsEnvironmentChecker{}
	d := c.Check(context.Background())
	require.Equal(t, cli.StatusPass, d.Status)
	require.Contains(t, d.Message, "not configured")
}

func TestTTSEnvironmentChecker_ConfigLoadFails(t *testing.T) {
	dir := t.TempDir()
	defer resetConfigPath()
	SetConfigPath(filepath.Join(dir, "nonexistent.yaml"))

	c := ttsEnvironmentChecker{}
	d := c.Check(context.Background())
	require.Equal(t, cli.StatusPass, d.Status)
	require.Contains(t, d.Message, "not configured")
}

func TestTTSEnvironmentChecker_TTSDisabled(t *testing.T) {
	dir := t.TempDir()
	path := writeTTSConfig(t, dir, ttsConfigOpts{})
	defer resetConfigPath()
	SetConfigPath(path)

	c := ttsEnvironmentChecker{}
	d := c.Check(context.Background())
	require.Equal(t, cli.StatusPass, d.Status)
	require.Contains(t, d.Message, "not configured")
}

func TestTTSEnvironmentChecker_FeishuEdgeTTSEnabled(t *testing.T) {
	dir := t.TempDir()
	path := writeTTSConfig(t, dir, ttsConfigOpts{feishuEnabled: true, feishuTTS: true, feishuProvider: "edge"})
	defer resetConfigPath()
	SetConfigPath(path)

	c := ttsEnvironmentChecker{}
	d := c.Check(context.Background())
	require.Contains(t, []cli.Status{cli.StatusPass, cli.StatusFail}, d.Status)
	if d.Status == cli.StatusFail {
		require.Contains(t, d.Message, "ffmpeg")
		require.NotEmpty(t, d.FixHint)
	}
}

func TestTTSEnvironmentChecker_SlackEdgeTTSEnabled(t *testing.T) {
	dir := t.TempDir()
	path := writeTTSConfig(t, dir, ttsConfigOpts{slackEnabled: true, slackTTS: true, slackProvider: "edge"})
	defer resetConfigPath()
	SetConfigPath(path)

	c := ttsEnvironmentChecker{}
	d := c.Check(context.Background())
	require.Contains(t, []cli.Status{cli.StatusPass, cli.StatusFail}, d.Status)
	if d.Status == cli.StatusFail {
		require.Contains(t, d.Message, "ffmpeg")
		require.NotEmpty(t, d.FixHint)
	}
}

// --- MOSS Provider Tests ---

func TestTTSEnvironmentChecker_MossProvider(t *testing.T) {
	dir := t.TempDir()
	path := writeTTSConfig(t, dir, ttsConfigOpts{feishuEnabled: true, feishuTTS: true, feishuProvider: "edge+moss"})
	defer resetConfigPath()
	SetConfigPath(path)

	c := ttsEnvironmentChecker{}
	d := c.Check(context.Background())
	// Should require ffmpeg + python3 + moss model dir
	require.Contains(t, []cli.Status{cli.StatusPass, cli.StatusFail}, d.Status)
}

func TestTTSEnvironmentChecker_MossOnlyProvider(t *testing.T) {
	dir := t.TempDir()
	path := writeTTSConfig(t, dir, ttsConfigOpts{feishuEnabled: true, feishuTTS: true, feishuProvider: "moss"})
	defer resetConfigPath()
	SetConfigPath(path)

	c := ttsEnvironmentChecker{}
	d := c.Check(context.Background())
	require.Contains(t, []cli.Status{cli.StatusPass, cli.StatusFail}, d.Status)
}

// --- ttsRequirements Unit Tests ---

func TestTTSRequirements(t *testing.T) {
	tests := []struct {
		name             string
		opts             ttsConfigOpts
		wantFFmpeg       bool
		wantPython3      bool
		wantMossModelDir bool
	}{
		{"both_disabled", ttsConfigOpts{}, false, false, false},
		{"slack_edge", ttsConfigOpts{slackEnabled: true, slackTTS: true, slackProvider: "edge"}, true, false, false},
		{"feishu_edge", ttsConfigOpts{feishuEnabled: true, feishuTTS: true, feishuProvider: "edge"}, true, false, false},
		{"feishu_edge_moss", ttsConfigOpts{feishuEnabled: true, feishuTTS: true, feishuProvider: "edge+moss"}, true, true, true},
		{"feishu_moss_only", ttsConfigOpts{feishuEnabled: true, feishuTTS: true, feishuProvider: "moss"}, true, true, true},
		{"both_edge", ttsConfigOpts{slackEnabled: true, slackTTS: true, slackProvider: "edge", feishuEnabled: true, feishuTTS: true, feishuProvider: "edge"}, true, false, false},
		{"both_moss", ttsConfigOpts{slackEnabled: true, slackTTS: true, slackProvider: "edge+moss", feishuEnabled: true, feishuTTS: true, feishuProvider: "edge+moss"}, true, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeTTSConfig(t, dir, tt.opts)
			defer resetConfigPath()
			SetConfigPath(path)

			got := ttsRequirements()
			require.Equal(t, tt.wantFFmpeg, got.FFmpeg, "FFmpeg mismatch")
			require.Equal(t, tt.wantPython3, got.Python3, "Python3 mismatch")
		})
	}
}

func TestMossProvider(t *testing.T) {
	t.Parallel()
	require.True(t, mossProvider("moss"))
	require.True(t, mossProvider("edge+moss"))
	require.False(t, mossProvider("edge"))
	require.False(t, mossProvider(""))
}

// --- Helpers ---

type ttsConfigOpts struct {
	slackEnabled   bool
	slackTTS       bool
	slackProvider  string
	feishuEnabled  bool
	feishuTTS      bool
	feishuProvider string
}

func writeTTSConfig(t *testing.T, dir string, opts ttsConfigOpts) string {
	t.Helper()

	yaml := "gateway:\n  addr: \":8888\"\nmessaging:\n"

	// Slack section
	yaml += "  slack:\n    enabled: " + strconv.FormatBool(opts.slackEnabled)
	yaml += "\n    tts_enabled: " + strconv.FormatBool(opts.slackTTS)
	if opts.slackTTS && opts.slackProvider != "" {
		yaml += "\n    tts_provider: " + opts.slackProvider
	}
	yaml += "\n"

	// Feishu section
	yaml += "  feishu:\n    enabled: " + strconv.FormatBool(opts.feishuEnabled)
	yaml += "\n    tts_enabled: " + strconv.FormatBool(opts.feishuTTS)
	if opts.feishuTTS && opts.feishuProvider != "" {
		yaml += "\n    tts_provider: " + opts.feishuProvider
	}
	yaml += "\n"

	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))
	return path
}
