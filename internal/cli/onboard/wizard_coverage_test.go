package onboard

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// ─── ExistingConfig methods ──────────────────────────────────────────────────

func TestExistingConfig_SlackReady(t *testing.T) {
	t.Parallel()

	ec := &ExistingConfig{SlackEnabled: true, SlackCreds: true}
	require.True(t, ec.SlackReady())

	ec = &ExistingConfig{SlackEnabled: true, SlackCreds: false}
	require.False(t, ec.SlackReady())
}

func TestExistingConfig_FeishuReady(t *testing.T) {
	t.Parallel()

	ec := &ExistingConfig{FeishuEnabled: true, FeishuCreds: true}
	require.True(t, ec.FeishuReady())

	ec = &ExistingConfig{FeishuEnabled: true, FeishuCreds: false}
	require.False(t, ec.FeishuReady())
}

func TestExistingConfig_PlatformConfigured(t *testing.T) {
	t.Parallel()

	ec := &ExistingConfig{SlackEnabled: true, FeishuCreds: true}
	require.True(t, ec.PlatformConfigured("slack"))
	require.True(t, ec.PlatformConfigured("feishu"))
	require.False(t, ec.PlatformConfigured("discord"))
}

// ─── detectExistingConfig ────────────────────────────────────────────────────

func TestDetectExistingConfig_BothFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	envPath := filepath.Join(dir, ".env")

	require.NoError(t, os.WriteFile(cfgPath, []byte("messaging:\n  slack:\n    enabled: true\n"), 0o644))
	require.NoError(t, os.WriteFile(envPath, []byte("HOTPLEX_MESSAGING_SLACK_BOT_TOKEN=xoxb-123\n"), 0o600))

	ec := detectExistingConfig(cfgPath, envPath)
	require.True(t, ec.ConfigExists)
	require.True(t, ec.EnvExists)
	require.True(t, ec.SlackEnabled)
	require.True(t, ec.SlackCreds)
}

func TestDetectExistingConfig_NoFiles(t *testing.T) {
	t.Parallel()

	ec := detectExistingConfig("/nonexistent/config.yaml", "/nonexistent/.env")
	require.False(t, ec.ConfigExists)
	require.False(t, ec.EnvExists)
	require.False(t, ec.HasAny())
}

// ─── isPlatformEnabled ───────────────────────────────────────────────────────

func TestIsPlatformEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		yaml     string
		platform string
		want     bool
	}{
		{"slack enabled", "messaging:\n  slack:\n    enabled: true\n", "slack", true},
		{"slack indented", "slack:\n  enabled: true\n", "slack", true},
		{"feishu enabled", "messaging:\n  feishu:\n    enabled: true\n", "feishu", true},
		{"not enabled", "messaging:\n  slack:\n    enabled: false\n", "slack", false},
		{"missing", "messaging:\n", "slack", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, isPlatformEnabled(tt.yaml, tt.platform))
		})
	}
}

// ─── hasEnvValue ─────────────────────────────────────────────────────────────

func TestHasEnvValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		content string
		key     string
		want    bool
	}{
		{"present", "MY_KEY=value\n", "MY_KEY", true},
		{"missing", "OTHER=val\n", "MY_KEY", false},
		{"empty value", "MY_KEY=\n", "MY_KEY", false},
		{"multiline", "A=1\nMY_KEY=hello\nB=2\n", "MY_KEY", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, hasEnvValue(tt.content, tt.key))
		})
	}
}

// ─── displayExistingConfig (just ensure no panic) ────────────────────────────

func TestDisplayExistingConfig(t *testing.T) {
	ec := &ExistingConfig{
		ConfigPath:    "/tmp/config.yaml",
		EnvPath:       "/tmp/.env",
		ConfigExists:  true,
		SlackEnabled:  true,
		SlackCreds:    true,
		FeishuEnabled: true,
		FeishuCreds:   false,
	}
	require.NotPanics(t, func() { displayExistingConfig(ec) })

	ec2 := &ExistingConfig{EnvExists: true, ConfigExists: false, EnvPath: "/tmp/.env"}
	require.NotPanics(t, func() { displayExistingConfig(ec2) })

	ec3 := &ExistingConfig{ConfigExists: true}
	require.NotPanics(t, func() { displayExistingConfig(ec3) })
}

// ─── stepEnvPreCheck ─────────────────────────────────────────────────────────

func TestStepEnvPreCheck_Coverage(t *testing.T) {
	result := stepEnvPreCheck()
	require.Equal(t, "env_precheck", result.Name)
	require.NotEmpty(t, result.Detail)
}

// ─── stepWorkerDep ───────────────────────────────────────────────────────────

func TestStepWorkerDep_Unknown(t *testing.T) {
	t.Parallel()
	result := stepWorkerDep("unknown_type")
	require.Equal(t, "skip", result.Status)
}

// ─── stepConfigGen ───────────────────────────────────────────────────────────

func TestStepConfigGen_NewFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	result, created := stepConfigGen(WizardOptions{ConfigPath: cfgPath}, ConfigTemplateOptions{
		WorkerType: "claude_code",
	})

	require.Equal(t, "pass", result.Status)
	require.True(t, created)

	data, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "gateway:")
}

func TestStepConfigGen_ExistingNoForce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("existing"), 0o600))

	result, created := stepConfigGen(WizardOptions{ConfigPath: cfgPath}, ConfigTemplateOptions{})
	require.Equal(t, "skip", result.Status)
	require.False(t, created)
}

func TestStepConfigGen_Force(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("existing"), 0o600))

	result, created := stepConfigGen(WizardOptions{ConfigPath: cfgPath, Force: true}, ConfigTemplateOptions{
		WorkerType: "claude_code",
	})

	require.Equal(t, "pass", result.Status)
	require.True(t, created)
}

// ─── stepWriteEnv ────────────────────────────────────────────────────────────

func TestStepWriteEnv(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	result := stepWriteConfig(envPath, "jwt-secret", "admin-token",
		messagingPlatformConfig{}, messagingPlatformConfig{}, false, WizardOptions{})

	require.Equal(t, "pass", result.Status)

	data, err := os.ReadFile(envPath)
	require.NoError(t, err)
	content := string(data)
	require.Contains(t, content, "HOTPLEX_JWT_SECRET=jwt-secret")
	require.Contains(t, content, "HOTPLEX_ADMIN_TOKEN_1=admin-token")
}

// ─── buildEnvContent ─────────────────────────────────────────────────────────

func TestBuildEnvContent_NoPlatforms(t *testing.T) {
	content := buildEnvContent("secret", "token", messagingPlatformConfig{}, messagingPlatformConfig{}, "")
	require.Contains(t, content, "HOTPLEX_JWT_SECRET=secret")
	require.Contains(t, content, "HOTPLEX_ADMIN_TOKEN_1=token")
	require.NotContains(t, content, "SLACK_ENABLED")
	require.NotContains(t, content, "FEISHU_ENABLED")
}

func TestBuildEnvContent_WithSlack(t *testing.T) {
	slackCfg := messagingPlatformConfig{
		enabled:     true,
		credentials: map[string]string{"HOTPLEX_MESSAGING_SLACK_BOT_TOKEN": "xoxb-123"},
	}
	content := buildEnvContent("secret", "token", slackCfg, messagingPlatformConfig{}, "")
	require.Contains(t, content, "HOTPLEX_MESSAGING_SLACK_ENABLED=true")
	require.Contains(t, content, "HOTPLEX_MESSAGING_SLACK_BOT_TOKEN=xoxb-123")
}

func TestBuildEnvContent_KeptPlatform(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(envPath, []byte("HOTPLEX_MESSAGING_SLACK_BOT_TOKEN=xoxb-existing\n"), 0o600))

	slackCfg := messagingPlatformConfig{enabled: true, kept: true, credentials: map[string]string{}}
	content := buildEnvContent("secret", "token", slackCfg, messagingPlatformConfig{}, envPath)
	require.Contains(t, content, "HOTPLEX_MESSAGING_SLACK_BOT_TOKEN=xoxb-existing")
}

// ─── readExistingEnvCredentials ──────────────────────────────────────────────

func TestReadExistingEnvCredentials(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(envPath, []byte("KEY_A=val_a\nKEY_B=val_b\nKEY_C=val_c\n"), 0o600))

	result := readExistingEnvCredentials(envPath, []string{"KEY_A", "KEY_C"})
	require.Equal(t, "val_a", result["KEY_A"])
	require.Equal(t, "val_c", result["KEY_C"])
	_, hasB := result["KEY_B"]
	require.False(t, hasB) // KEY_B not requested
}

func TestReadExistingEnvCredentials_NoFile(t *testing.T) {
	t.Parallel()

	result := readExistingEnvCredentials("/nonexistent/.env", []string{"KEY_A"})
	require.Empty(t, result)
}

// ─── extractPlatformBlock ────────────────────────────────────────────────────

func TestExtractPlatformBlock(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("messaging:\n  slack:\n    enabled: true\n    token: xoxb\n  feishu:\n    enabled: false\n"), 0o644))

	block := extractPlatformBlock(cfgPath, "slack")
	require.Contains(t, block, "enabled: true")
	require.Contains(t, block, "token: xoxb")
	require.NotContains(t, block, "feishu")
}

func TestExtractPlatformBlock_NotFound(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(cfgPath, []byte("messaging:\n  slack:\n    enabled: true\n"), 0o644))

	block := extractPlatformBlock(cfgPath, "discord")
	require.Empty(t, block)
}

// ─── stepAgentConfig ─────────────────────────────────────────────────────────

func TestStepAgentConfig(t *testing.T) {
	result, files := stepAgentConfig()
	require.Equal(t, "agent_config", result.Name)
	_ = files // may be empty
}
