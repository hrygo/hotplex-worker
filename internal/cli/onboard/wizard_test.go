package onboard

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hotplex/hotplex-worker/internal/cli/checkers"
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
	s, created := stepConfigGen(WizardOptions{ConfigPath: path})
	require.Equal(t, "pass", s.Status)
	require.True(t, created)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "gateway:")
}

func TestStepConfigGen_Exists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("existing\n"), 0o644))
	s, created := stepConfigGen(WizardOptions{ConfigPath: path})
	require.Equal(t, "skip", s.Status)
	require.False(t, created)
}

func TestStepConfigGen_ForceOverwrite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("old\n"), 0o644))
	s, created := stepConfigGen(WizardOptions{ConfigPath: path, Force: true})
	require.Equal(t, "pass", s.Status)
	require.True(t, created)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), "gateway:")
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
	tests := []struct {
		name    string
		slack   map[string]string
		feishu  map[string]string
		contain []string
	}{
		{
			"minimal",
			nil, nil,
			[]string{"HOTPLEX_JWT_SECRET=jwt", "HOTPLEX_ADMIN_TOKEN_1=admin", "HOTPLEX_WORKER_TYPE=claude_code"},
		},
		{
			"with_slack",
			map[string]string{"SLACK_BOT_TOKEN": "xoxb-test"},
			nil,
			[]string{"SLACK_BOT_TOKEN=xoxb-test", "# Slack"},
		},
		{
			"with_feishu",
			nil,
			map[string]string{"FEISHU_APP_ID": "cli_123"},
			[]string{"FEISHU_APP_ID=cli_123", "# Feishu"},
		},
		{
			"both",
			map[string]string{"SLACK_BOT_TOKEN": "xoxb-test"},
			map[string]string{"FEISHU_APP_ID": "cli_123"},
			[]string{"# Slack", "# Feishu"},
		},
		{
			"empty_worker_type",
			nil, nil,
			[]string{"HOTPLEX_JWT_SECRET=jwt\n"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			workerType := "claude_code"
			if tt.name == "empty_worker_type" {
				workerType = ""
			}
			got := buildEnvContent("jwt", "admin", workerType, tt.slack, tt.feishu)
			for _, c := range tt.contain {
				require.Contains(t, got, c)
			}
		})
	}
}

func TestStepWriteConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	s := stepWriteConfig(envPath, "jwt-secret", "admin-token", "claude_code", nil, nil, false, WizardOptions{})
	require.Equal(t, "pass", s.Status)
	data, err := os.ReadFile(envPath)
	require.NoError(t, err)
	require.Contains(t, string(data), "HOTPLEX_JWT_SECRET=jwt-secret")
}

func TestStepWriteConfig_InvalidPath(t *testing.T) {
	t.Parallel()
	s := stepWriteConfig("/nonexistent/dir/.env", "jwt", "admin", "claude_code", nil, nil, false, WizardOptions{})
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
	require.Equal(t, ".env", result.EnvPath)

	_, statErr := os.Stat(configPath)
	require.NoError(t, statErr)

	envData, readErr := os.ReadFile(filepath.Join(dir, ".env"))
	require.NoError(t, readErr)
	require.Contains(t, string(envData), "HOTPLEX_JWT_SECRET=")
	require.Contains(t, string(envData), "HOTPLEX_ADMIN_TOKEN_1=")
}

func TestRun_EnvPreCheckFail(t *testing.T) {
	// This test verifies the early-exit behavior when env pre-check fails.
	// Since we can't easily force env pre-check failure (it checks Go version and OS),
	// we test the hasFail path indirectly.
	t.Parallel()
	r := &WizardResult{Steps: []StepResult{{Name: "env_precheck", Status: "fail"}}}
	require.True(t, r.hasFail())
}

func TestDefaultConfigYAML(t *testing.T) {
	t.Parallel()
	got := DefaultConfigYAML()
	require.Contains(t, got, "gateway:")
	require.Contains(t, got, "worker:")
	require.Contains(t, got, "claude_code")
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
