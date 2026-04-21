package security

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

// ─── IsProtectedEnvVar ─────────────────────────────────────────────────────────

func TestIsProtectedEnvVar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
		want bool
	}{
		// Protected system variables
		{"HOME protected", "HOME", true},
		{"USER protected", "USER", true},
		{"SHELL protected", "SHELL", true},
		{"PATH protected", "PATH", true},
		{"TERM protected", "TERM", true},
		{"LANG protected", "LANG", true},
		{"LC_ALL protected", "LC_ALL", true},
		{"PWD protected", "PWD", true},
		{"GID protected", "GID", true},
		{"UID protected", "UID", true},
		{"SHLVL protected", "SHLVL", true},
		{"GOROOT protected", "GOROOT", true},
		{"GOPATH protected", "GOPATH", true},
		{"GOPROXY protected", "GOPROXY", true},
		{"GOSUMDB protected", "GOSUMDB", true},
		{"BASH protected", "BASH", true},
		{"BASH_VERSION protected", "BASH_VERSION", true},
		{"ZSH_VERSION protected", "ZSH_VERSION", true},
		{"ZDOTDIR protected", "ZDOTDIR", true},

		// Not protected
		{"CUSTOM_VAR not protected", "CUSTOM_VAR", false},
		{"API_KEY not protected", "API_KEY", false},
		{"MY_SECRET not protected", "MY_SECRET", false},
		{"HOTPLEX_SESSION_ID not protected", "HOTPLEX_SESSION_ID", false},
		{"CLAUDE_API_KEY not protected", "CLAUDE_API_KEY", false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := IsProtectedEnvVar(tt.key)
			require.Equal(t, tt.want, got, "IsProtectedEnvVar(%q)", tt.key)
		})
	}
}

// ─── SafeEnvBuilder ───────────────────────────────────────────────────────────

func TestNewSafeEnvBuilder(t *testing.T) {
	t.Parallel()

	builder := NewSafeEnvBuilder()
	require.NotNil(t, builder)
	require.NotNil(t, builder.whitelist)
	require.NotNil(t, builder.hotplexVars)
	require.NotNil(t, builder.secrets)
	require.Nil(t, builder.lastErr)

	// Should include base + Go whitelists
	require.Contains(t, builder.whitelist, "HOME")
	require.Contains(t, builder.whitelist, "PATH")
	require.Contains(t, builder.whitelist, "GOPROXY")
}

func TestSafeEnvBuilder_AddWorkerType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		workerType string
		wantInList []string
	}{
		{
			name:       "claude-code worker",
			workerType: "claude-code",
			wantInList: []string{"CLAUDE_API_KEY", "CLAUDE_MODEL", "CLAUDE_BASE_URL"},
		},
		{
			name:       "opencode-server worker",
			workerType: "opencode-server",
			wantInList: []string{}, // no extra vars
		},
		{
			name:       "unknown worker",
			workerType: "unknown-worker",
			wantInList: []string{}, // no extra vars
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := NewSafeEnvBuilder()
			result := builder.AddWorkerType(tt.workerType)

			// Should return same builder for chaining
			require.Equal(t, builder, result)

			// Check whitelist contains expected vars
			for _, key := range tt.wantInList {
				require.Contains(t, builder.whitelist, key, "whitelist should contain %s", key)
			}
		})
	}
}

func TestSafeEnvBuilder_AddHotPlexVar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		key     string
		value   string
		wantErr bool
	}{
		{
			name:    "valid hotplex var",
			key:     "HOTPLEX_SESSION_ID",
			value:   "sess_123",
			wantErr: false,
		},
		{
			name:    "protected var rejected",
			key:     "HOME",
			value:   "/malicious",
			wantErr: true,
		},
		{
			name:    "protected PATH rejected",
			key:     "PATH",
			value:   "/evil/bin",
			wantErr: true,
		},
		{
			name:    "custom var allowed",
			key:     "CUSTOM_CONFIG",
			value:   "value",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := NewSafeEnvBuilder()
			err := builder.AddHotPlexVar(tt.key, tt.value)

			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "protected system variable")
				require.Equal(t, err, builder.lastErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.value, builder.hotplexVars[tt.key])
			}
		})
	}
}

func TestSafeEnvBuilder_AddSecret(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		key     string
		value   string
		wantErr bool
	}{
		{
			name:    "valid secret",
			key:     "CLAUDE_API_KEY",
			value:   "sk-secret",
			wantErr: false,
		},
		{
			name:    "protected var rejected",
			key:     "PATH",
			value:   "/evil",
			wantErr: true,
		},
		{
			name:    "protected HOME rejected",
			key:     "HOME",
			value:   "/evil",
			wantErr: true,
		},
		{
			name:    "custom secret allowed",
			key:     "MY_API_KEY",
			value:   "secret123",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := NewSafeEnvBuilder()
			err := builder.AddSecret(tt.key, tt.value)

			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "protected system variable")
				require.Equal(t, err, builder.lastErr)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.value, builder.secrets[tt.key])
			}
		})
	}
}

func TestSafeEnvBuilder_LastError(t *testing.T) {
	t.Parallel()

	t.Run("no error initially", func(t *testing.T) {
		t.Parallel()
		builder := NewSafeEnvBuilder()
		require.Nil(t, builder.LastError())
	})

	t.Run("error from AddHotPlexVar", func(t *testing.T) {
		t.Parallel()
		builder := NewSafeEnvBuilder()
		_ = builder.AddHotPlexVar("HOME", "/evil")
		require.Error(t, builder.LastError())
	})

	t.Run("error from AddSecret", func(t *testing.T) {
		t.Parallel()
		builder := NewSafeEnvBuilder()
		_ = builder.AddSecret("PATH", "/evil")
		require.Error(t, builder.LastError())
	})

	t.Run("first error retained", func(t *testing.T) {
		t.Parallel()
		builder := NewSafeEnvBuilder()
		_ = builder.AddHotPlexVar("HOME", "/evil1")
		secondErr := builder.AddHotPlexVar("PATH", "/evil2")
		// Both should fail, but we check that an error is present
		require.Error(t, builder.LastError())
		require.Error(t, secondErr)
	})
}

func TestSafeEnvBuilder_Build(t *testing.T) {
	t.Parallel()

	// Set up test environment variables
	os.Setenv("TEST_HOME", "/test/home")
	os.Setenv("TEST_PATH", "/test/bin")
	defer func() {
		os.Unsetenv("TEST_HOME")
		os.Unsetenv("TEST_PATH")
	}()

	tests := []struct {
		name     string
		setup    func(b *SafeEnvBuilder)
		validate func(t *testing.T, env []string)
	}{
		{
			name:  "empty builder",
			setup: func(b *SafeEnvBuilder) {},
			validate: func(t *testing.T, env []string) {
				// Should only have whitelisted vars that exist in environment
				// (base whitelist vars that are set on the system)
				require.NotNil(t, env)
			},
		},
		{
			name: "hotplex vars only",
			setup: func(b *SafeEnvBuilder) {
				_ = b.AddHotPlexVar("HOTPLEX_SESSION_ID", "sess_123")
				_ = b.AddHotPlexVar("HOTPLEX_WORKER_TYPE", "claude-code")
			},
			validate: func(t *testing.T, env []string) {
				envMap := envSliceToMap(env)
				require.Equal(t, "sess_123", envMap["HOTPLEX_SESSION_ID"])
				require.Equal(t, "claude-code", envMap["HOTPLEX_WORKER_TYPE"])
			},
		},
		{
			name: "secrets only",
			setup: func(b *SafeEnvBuilder) {
				_ = b.AddSecret("CLAUDE_API_KEY", "sk-secret")
				_ = b.AddSecret("OPENAI_API_KEY", "sk-openai")
			},
			validate: func(t *testing.T, env []string) {
				envMap := envSliceToMap(env)
				require.Equal(t, "sk-secret", envMap["CLAUDE_API_KEY"])
				require.Equal(t, "sk-openai", envMap["OPENAI_API_KEY"])
			},
		},
		{
			name: "mixed vars hotplex takes precedence",
			setup: func(b *SafeEnvBuilder) {
				_ = b.AddHotPlexVar("CUSTOM_VAR", "hotplex-value")
				_ = b.AddSecret("CUSTOM_VAR", "secret-value")
			},
			validate: func(t *testing.T, env []string) {
				envMap := envSliceToMap(env)
				// HotPlex vars take precedence in the builder's logic
				require.Equal(t, "hotplex-value", envMap["CUSTOM_VAR"])
			},
		},
		{
			name: "with worker type",
			setup: func(b *SafeEnvBuilder) {
				b.AddWorkerType("claude-code")
				_ = b.AddHotPlexVar("HOTPLEX_SESSION_ID", "sess_abc")
			},
			validate: func(t *testing.T, env []string) {
				envMap := envSliceToMap(env)
				require.Equal(t, "sess_abc", envMap["HOTPLEX_SESSION_ID"])
				// Whitelist should include claude-code vars, but they may not be in env
				// if not set in the test environment
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			builder := NewSafeEnvBuilder()
			tt.setup(builder)
			env := builder.Build()

			tt.validate(t, env)

			// All entries should be in KEY=VALUE format
			for _, entry := range env {
				require.Contains(t, entry, "=", "env entry should contain =: %s", entry)
			}
		})
	}
}

func TestSafeEnvBuilder_Integration(t *testing.T) {
	t.Parallel()

	// Real-world scenario: build env for claude-code worker
	builder := NewSafeEnvBuilder()
	builder.AddWorkerType("claude-code")
	err := builder.AddHotPlexVar("HOTPLEX_SESSION_ID", "sess_12345")
	require.NoError(t, err)
	err = builder.AddHotPlexVar("HOTPLEX_WORK_DIR", "/var/hotplex/projects/proj_1")
	require.NoError(t, err)
	err = builder.AddSecret("CLAUDE_API_KEY", "sk-ant-secret")
	require.NoError(t, err)

	env := builder.Build()
	envMap := envSliceToMap(env)

	// HotPlex vars
	require.Equal(t, "sess_12345", envMap["HOTPLEX_SESSION_ID"])
	require.Equal(t, "/var/hotplex/projects/proj_1", envMap["HOTPLEX_WORK_DIR"])

	// Secret
	require.Equal(t, "sk-ant-secret", envMap["CLAUDE_API_KEY"])

	// System vars should be present (from whitelist)
	// (HOME, PATH, etc. if they exist in the environment)
}

// Helper function to convert env slice to map
func envSliceToMap(env []string) map[string]string {
	result := make(map[string]string)
	for _, entry := range env {
		for i := 0; i < len(entry); i++ {
			if entry[i] == '=' {
				key := entry[:i]
				value := entry[i+1:]
				result[key] = value
				break
			}
		}
	}
	return result
}
