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
	path := writeTTSConfig(t, dir, false, false)
	defer resetConfigPath()
	SetConfigPath(path)

	c := ttsEnvironmentChecker{}
	d := c.Check(context.Background())
	require.Equal(t, cli.StatusPass, d.Status)
	require.Contains(t, d.Message, "not configured")
}

func TestTTSEnvironmentChecker_FeishuTTSEnabled(t *testing.T) {
	dir := t.TempDir()
	// Feishu enabled + TTS enabled → needs ffmpeg.
	path := writeTTSConfig(t, dir, false, true)
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

func TestTTSEnvironmentChecker_SlackTTSEnabled(t *testing.T) {
	dir := t.TempDir()
	// Slack TTS also uses Edge TTS → MP3 → ffmpeg Opus.
	path := writeTTSConfig(t, dir, true, false)
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

func TestTTSRequirements(t *testing.T) {
	tests := []struct {
		name        string
		slackTTS    bool
		feishuTTS   bool
		needsFFmpeg bool
	}{
		{"both_disabled", false, false, false},
		{"slack_only", true, false, true},
		{"feishu_only", false, true, true},
		{"both_enabled", true, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := writeTTSConfig(t, dir, tt.slackTTS, tt.feishuTTS)
			defer resetConfigPath()
			SetConfigPath(path)

			got := ttsRequirements()
			require.Equal(t, tt.needsFFmpeg, got)
		})
	}
}

// writeTTSConfig creates a minimal config YAML with TTS settings.
// Slack TTS is enabled when slackTTS=true, Feishu when feishuTTS=true.
func writeTTSConfig(t *testing.T, dir string, slackTTS, feishuTTS bool) string {
	t.Helper()
	yaml := "gateway:\n  addr: \":8888\"\nmessaging:\n"
	yaml += "  slack:\n    enabled: " + strconv.FormatBool(slackTTS || feishuTTS)
	yaml += "\n    tts_enabled: " + strconv.FormatBool(slackTTS)
	if slackTTS {
		yaml += "\n    tts_provider: edge"
	}
	yaml += "\n  feishu:\n    enabled: true"
	yaml += "\n    tts_enabled: " + strconv.FormatBool(feishuTTS)
	if feishuTTS {
		yaml += "\n    tts_provider: edge"
	}
	yaml += "\n"

	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o644))
	return path
}
