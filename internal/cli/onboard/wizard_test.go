package onboard

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/cli/checkers"
)

func TestStepEnvPreCheck(t *testing.T) {
	t.Parallel()
	s := stepEnvPreCheck()
	require.Equal(t, "env_precheck", s.Name)
	require.Contains(t, []string{"pass", "fail"}, s.Status)
}

func TestStepConfigGen_Create(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	s, created := stepConfigGen(WizardOptions{ConfigPath: path}, ConfigTemplateOptions{WorkerType: "claude_code"})
	require.Equal(t, "pass", s.Status)
	require.True(t, created)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "gateway:")
	require.Contains(t, string(data), "messaging:")
	require.Contains(t, string(data), "worker:")
	require.Contains(t, string(data), "auto_retry:")
}

func TestStepConfigGen_Exists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("existing\n"), 0o644))
	s, created := stepConfigGen(WizardOptions{ConfigPath: path}, ConfigTemplateOptions{})
	require.Equal(t, "skip", s.Status)
	require.False(t, created)
}

func TestStepConfigGen_ForceOverwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("old\n"), 0o644))
	s, created := stepConfigGen(WizardOptions{ConfigPath: path, Force: true}, ConfigTemplateOptions{WorkerType: "claude_code"})
	require.Equal(t, "pass", s.Status)
	require.True(t, created)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "gateway:")
	require.Contains(t, string(data), "messaging:")
}

func TestStepConfigGen_SlackEnabled(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	s, created := stepConfigGen(WizardOptions{ConfigPath: path}, ConfigTemplateOptions{
		WorkerType:     "claude_code",
		SlackEnabled:   true,
		SlackDMPolicy:  "open",
		SlackAllowFrom: []string{"U123", "U456"},
	})
	require.Equal(t, "pass", s.Status)
	require.True(t, created)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)
	require.Contains(t, content, "messaging:")
	require.Contains(t, content, "enabled: true")
	require.Contains(t, content, "dm_policy: \"open\"")
	require.Contains(t, content, "U123")
	require.Contains(t, content, "U456")
}

func TestStepWorkerDep(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		workerType string
		wantSkip   bool
	}{
		{"claude_code", "claude_code", false},
		{"opencode_server", "opencode_server", false},
		{"pi", "pi", true},
		{"unknown", "unknown", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := stepWorkerDep(tt.workerType)
			require.Equal(t, "worker_dep", s.Name)
			if tt.wantSkip {
				require.Equal(t, "skip", s.Status)
			} else {
				require.Equal(t, "pass", s.Status)
			}
		})
	}
}

func TestBuildEnvContent(t *testing.T) {
	t.Parallel()
	t.Run("minimal", func(t *testing.T) {
		t.Parallel()
		got := buildEnvContent("jwt", "admin", messagingPlatformConfig{}, messagingPlatformConfig{}, "")
		require.Contains(t, got, "HOTPLEX_JWT_SECRET=jwt")
		require.Contains(t, got, "HOTPLEX_ADMIN_TOKEN_1=admin")
		require.NotContains(t, got, "HOTPLEX_WORKER_TYPE")
		require.NotContains(t, got, "# ── Slack ──")
	})

	t.Run("with_slack_enabled", func(t *testing.T) {
		t.Parallel()
		slack := messagingPlatformConfig{
			enabled: true,
			credentials: map[string]string{
				"HOTPLEX_MESSAGING_SLACK_BOT_TOKEN": "xoxb-test",
				"HOTPLEX_MESSAGING_SLACK_APP_TOKEN": "xapp-test",
			},
		}
		got := buildEnvContent("jwt", "admin", slack, messagingPlatformConfig{}, "")
		require.Contains(t, got, "HOTPLEX_MESSAGING_SLACK_ENABLED=true")
		require.Contains(t, got, "HOTPLEX_MESSAGING_SLACK_BOT_TOKEN=xoxb-test")
		require.Contains(t, got, "HOTPLEX_MESSAGING_SLACK_APP_TOKEN=xapp-test")
		require.NotContains(t, got, "\nSLACK_BOT_TOKEN=")
		require.NotContains(t, got, "\nSLACK_APP_TOKEN=")
	})

	t.Run("with_feishu_enabled", func(t *testing.T) {
		t.Parallel()
		feishu := messagingPlatformConfig{
			enabled: true,
			credentials: map[string]string{
				"HOTPLEX_MESSAGING_FEISHU_APP_ID":     "cli_123",
				"HOTPLEX_MESSAGING_FEISHU_APP_SECRET": "secret456",
			},
		}
		got := buildEnvContent("jwt", "admin", messagingPlatformConfig{}, feishu, "")
		require.Contains(t, got, "HOTPLEX_MESSAGING_FEISHU_ENABLED=true")
		require.Contains(t, got, "HOTPLEX_MESSAGING_FEISHU_APP_ID=cli_123")
		require.Contains(t, got, "HOTPLEX_MESSAGING_FEISHU_APP_SECRET=secret456")
		require.NotContains(t, got, "\nFEISHU_APP_ID=")
	})

	t.Run("both_platforms", func(t *testing.T) {
		t.Parallel()
		slack := messagingPlatformConfig{
			enabled: true,
			credentials: map[string]string{
				"HOTPLEX_MESSAGING_SLACK_BOT_TOKEN": "xoxb-test",
			},
		}
		feishu := messagingPlatformConfig{
			enabled: true,
			credentials: map[string]string{
				"HOTPLEX_MESSAGING_FEISHU_APP_ID": "cli_789",
			},
		}
		got := buildEnvContent("jwt", "admin", slack, feishu, "")
		require.Contains(t, got, "# ── Slack ──")
		require.Contains(t, got, "# ── Feishu ──")
	})

	t.Run("slack_no_credentials", func(t *testing.T) {
		t.Parallel()
		slack := messagingPlatformConfig{
			enabled:     true,
			credentials: map[string]string{},
		}
		got := buildEnvContent("jwt", "admin", slack, messagingPlatformConfig{}, "")
		require.Contains(t, got, "HOTPLEX_MESSAGING_SLACK_ENABLED=true")
		require.NotContains(t, got, "HOTPLEX_MESSAGING_SLACK_BOT_TOKEN=")
	})
}

func TestStepWriteConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	s := stepWriteConfig(envPath, "jwt-secret", "admin-token", messagingPlatformConfig{}, messagingPlatformConfig{}, false, WizardOptions{})
	require.Equal(t, "pass", s.Status)
	data, err := os.ReadFile(envPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "HOTPLEX_JWT_SECRET=jwt-secret")
}

func TestStepWriteConfig_InvalidPath(t *testing.T) {
	t.Parallel()
	s := stepWriteConfig("/nonexistent/dir/.env", "jwt", "admin", messagingPlatformConfig{}, messagingPlatformConfig{}, false, WizardOptions{})
	require.Equal(t, "fail", s.Status)
}

func TestWizardResult_Add(t *testing.T) {
	t.Parallel()
	r := &WizardResult{}
	r.add(StepResult{Name: "step1", Status: "pass"})
	r.add(StepResult{Name: "step2", Status: "fail"})
	require.Len(t, r.Steps, 2)
	require.Equal(t, "step1", r.Steps[0].Name)
	require.Equal(t, "step2", r.Steps[1].Name)
}

func TestWizardResult_HasFail(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		steps []StepResult
		want  bool
	}{
		{"no_steps", nil, false},
		{"all_pass", []StepResult{{Status: "pass"}}, false},
		{"has_fail", []StepResult{{Status: "pass"}, {Status: "fail"}}, true},
		{"has_warn_only", []StepResult{{Status: "warn"}, {Status: "skip"}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := &WizardResult{Steps: tt.steps}
			require.Equal(t, tt.want, r.hasFail())
		})
	}
}

func TestPrompt(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("hello\n"))
	got := prompt(reader, "test")
	require.Equal(t, "hello", got)
}

func TestPromptChoice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		choices []string
		want    string
	}{
		{"empty_default", "\n", []string{"a", "b", "c"}, "a"},
		{"select_2", "2\n", []string{"a", "b", "c"}, "b"},
		{"select_3", "3\n", []string{"a", "b", "c"}, "c"},
		{"invalid_number", "99\n", []string{"a", "b"}, "a"},
		{"non_number", "abc\n", []string{"a", "b"}, "a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bufio.NewReader(strings.NewReader(tt.input))
			got := promptChoice(reader, "pick", tt.choices)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestPromptYesNo(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"y", "y\n", true},
		{"yes", "yes\n", true},
		{"n", "n\n", false},
		{"empty", "\n", false},
		{"Y_uppercase", "Y\n", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := bufio.NewReader(strings.NewReader(tt.input))
			got := promptYesNo(reader, "confirm")
			require.Equal(t, tt.want, got)
		})
	}
}

func TestPromptWithDefault(t *testing.T) {
	t.Parallel()
	t.Run("empty_returns_default", func(t *testing.T) {
		t.Parallel()
		reader := bufio.NewReader(strings.NewReader("\n"))
		got := promptWithDefault(reader, "policy", "allowlist")
		require.Equal(t, "allowlist", got)
	})
	t.Run("non_empty_returns_input", func(t *testing.T) {
		t.Parallel()
		reader := bufio.NewReader(strings.NewReader("open\n"))
		got := promptWithDefault(reader, "policy", "allowlist")
		require.Equal(t, "open", got)
	})
}

func TestPromptCommaList(t *testing.T) {
	t.Parallel()
	t.Run("empty_returns_nil", func(t *testing.T) {
		t.Parallel()
		reader := bufio.NewReader(strings.NewReader("\n"))
		got := promptCommaList(reader, "users")
		require.Nil(t, got)
	})
	t.Run("comma_separated", func(t *testing.T) {
		t.Parallel()
		reader := bufio.NewReader(strings.NewReader("a, b , c\n"))
		got := promptCommaList(reader, "users")
		require.Equal(t, []string{"a", "b", "c"}, got)
	})
	t.Run("single_value", func(t *testing.T) {
		t.Parallel()
		reader := bufio.NewReader(strings.NewReader("U123\n"))
		got := promptCommaList(reader, "users")
		require.Equal(t, []string{"U123"}, got)
	})
}

func TestRun_NonInteractive(t *testing.T) {
	s := stepEnvPreCheck()
	if s.Status == "fail" {
		t.Skip("skipping: environment pre-check fails on this system: " + s.Detail)
	}

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() {
		_ = os.Chdir(origDir)
		checkers.SetConfigPath("")
	})

	result, err := Run(context.Background(), WizardOptions{
		ConfigPath:     configPath,
		NonInteractive: true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, configPath, result.ConfigPath)
	require.Equal(t, filepath.Join(dir, ".env"), result.EnvPath)

	_, statErr := os.Stat(configPath)
	require.NoError(t, statErr)

	envData, readErr := os.ReadFile(filepath.Join(dir, ".env"))
	require.NoError(t, readErr)
	require.Contains(t, string(envData), "HOTPLEX_JWT_SECRET=")
	require.Contains(t, string(envData), "HOTPLEX_ADMIN_TOKEN_1=")

	configData, configErr := os.ReadFile(configPath)
	require.NoError(t, configErr)
	configContent := string(configData)
	require.Contains(t, configContent, "gateway:")
	require.Contains(t, configContent, "messaging:")
	require.Contains(t, configContent, "worker:")
	require.Contains(t, configContent, "auto_retry:")
	require.Contains(t, configContent, "security:")
	require.Contains(t, configContent, "session:")
	require.Contains(t, configContent, "pool:")
}

func TestRun_NonInteractive_WithSlack(t *testing.T) {
	s := stepEnvPreCheck()
	if s.Status == "fail" {
		t.Skip("skipping: environment pre-check fails on this system: " + s.Detail)
	}

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	origDir, _ := os.Getwd()
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() {
		_ = os.Chdir(origDir)
		checkers.SetConfigPath("")
	})

	result, err := Run(context.Background(), WizardOptions{
		ConfigPath:     configPath,
		NonInteractive: true,
		EnableSlack:    true,
		SlackAllowFrom: []string{"U123", "U456"},
		SlackDMPolicy:  "open",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	configData, configErr := os.ReadFile(configPath)
	require.NoError(t, configErr)
	configContent := string(configData)
	require.Contains(t, configContent, "enabled: true")
	require.Contains(t, configContent, "dm_policy: \"open\"")
	require.Contains(t, configContent, "U123")
	require.Contains(t, configContent, "U456")

	envData, envErr := os.ReadFile(filepath.Join(dir, ".env"))
	require.NoError(t, envErr)
	require.Contains(t, string(envData), "HOTPLEX_MESSAGING_SLACK_ENABLED=true")
}

func TestRun_EnvPreCheckFail(t *testing.T) {
	t.Parallel()
	r := &WizardResult{Steps: []StepResult{{Name: "env_precheck", Status: "fail"}}}
	require.True(t, r.hasFail())
}

func TestDefaultConfigYAML(t *testing.T) {
	t.Parallel()
	got := DefaultConfigYAML()
	require.Contains(t, got, "gateway:")
	require.Contains(t, got, "admin:")
	require.Contains(t, got, "db:")
	require.Contains(t, got, "security:")
	require.Contains(t, got, "session:")
	require.Contains(t, got, "pool:")
	require.Contains(t, got, "worker:")
	require.Contains(t, got, "messaging:")
	require.Contains(t, got, "auto_retry:")
	require.Contains(t, got, "claude_code")
	require.Contains(t, got, "enabled: false")
}

func TestBuildConfigYAML_SlackEnabled(t *testing.T) {
	t.Parallel()
	got := BuildConfigYAML(ConfigTemplateOptions{
		SlackEnabled:  true,
		SlackDMPolicy: "open",
		WorkerType:    "claude_code",
	})
	require.Contains(t, got, "enabled: true")
	require.Contains(t, got, "dm_policy: \"open\"")
	require.Contains(t, got, "feishu:")
	require.Contains(t, got, "slack:")
}

func TestBuildConfigYAML_AllowFrom(t *testing.T) {
	t.Parallel()
	got := BuildConfigYAML(ConfigTemplateOptions{
		FeishuEnabled:   true,
		FeishuAllowFrom: []string{"ou_abc123", "ou_def456"},
		WorkerType:      "claude_code",
	})
	require.Contains(t, got, "ou_abc123")
	require.Contains(t, got, "ou_def456")
	require.Contains(t, got, "allow_from:")
}

func TestGenerateSecret(t *testing.T) {
	t.Parallel()
	s1 := GenerateSecret()
	s2 := GenerateSecret()
	require.NotEmpty(t, s1)
	require.NotEmpty(t, s2)
	require.NotEqual(t, s1, s2)
	require.Len(t, s1, 64) // base64 of 48 bytes
}
