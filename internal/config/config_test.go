package config

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDefault(t *testing.T) {
	t.Parallel()

	cfg := Default()
	require.NotNil(t, cfg)
	require.Equal(t, ":8888", cfg.Gateway.Addr)
	require.True(t, cfg.DB.WALMode)
	require.Equal(t, 100, cfg.Pool.MaxSize)
	require.Equal(t, 5, cfg.Pool.MaxIdlePerUser)
	require.Equal(t, 7*24*time.Hour, cfg.Session.RetentionPeriod)
	require.Equal(t, 1*time.Minute, cfg.Session.GCScanInterval)
	require.False(t, cfg.Security.TLSEnabled)
	require.True(t, cfg.Admin.Enabled)
	require.Equal(t, ":9999", cfg.Admin.Addr)
}

func TestConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		cfg    Config
		errCnt int
	}{
		{
			name:   "valid defaults",
			cfg:    *Default(),
			errCnt: 1, // TLS warning for non-local address :8888
		},
		{
			name: "missing gateway addr",
			cfg: func() Config {
				c := *Default()
				c.Gateway.Addr = ""
				return c
			}(),
			errCnt: 2, // missing addr + TLS warning
		},
		{
			name: "missing db path",
			cfg: func() Config {
				c := *Default()
				c.DB.Path = ""
				return c
			}(),
			errCnt: 2, // missing path + TLS warning
		},
		{
			name: "non-positive retention period",
			cfg: func() Config {
				c := *Default()
				c.Session.RetentionPeriod = 0
				return c
			}(),
			errCnt: 2, // invalid retention + TLS warning
		},
		{
			name: "non-positive pool max size",
			cfg: func() Config {
				c := *Default()
				c.Pool.MaxSize = 0
				return c
			}(),
			errCnt: 2, // invalid pool + TLS warning
		},
		{
			name: "multiple errors",
			cfg: func() Config {
				c := *Default()
				c.Gateway.Addr = ""
				c.DB.Path = ""
				return c
			}(),
			errCnt: 3, // missing addr + missing path + TLS warning
		},
		{
			name: "localhost TLS bypass",
			cfg: func() Config {
				c := *Default()
				c.Gateway.Addr = "127.0.0.1:8888"
				return c
			}(),
			errCnt: 0, // localhost bypasses TLS warning
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// NOT parallel — mutates global env vars
			errs := tt.cfg.Validate()
			require.Len(t, errs, tt.errCnt)
		})
	}
}

func TestExpandEnv(t *testing.T) {
	// NOT parallel — mutates global env vars (HOME, USER, etc.)
	tests := []struct {
		name   string
		input  string
		setup  func()
		verify func(string)
	}{
		{
			name:  "no variables",
			input: "hello world",
			setup: func() {},
			verify: func(got string) {
				require.Equal(t, "hello world", got)
			},
		},
		{
			name:  "simple variable",
			input: "path=${TEST_MY_HOME}",
			setup: func() { os.Setenv("TEST_MY_HOME", "/home/user") },
			verify: func(got string) {
				require.Equal(t, "path=/home/user", got)
			},
		},
		{
			name:  "variable with default",
			input: "path=${UNSET_VAR:-/default/path}",
			setup: func() {},
			verify: func(got string) {
				require.Equal(t, "path=/default/path", got)
			},
		},
		{
			name:  "variable with non-empty default",
			input: "token=${MY_TOKEN:-fallback}",
			setup: func() {},
			verify: func(got string) {
				require.Equal(t, "token=fallback", got)
			},
		},
		{
			name:  "multiple variables",
			input: "${HOME}/${USER}/${PWD}",
			setup: func() {},
			verify: func(got string) {
				// All three are typically set in a shell environment, so expect expansion
				require.NotContains(t, got, "${HOME}")
				require.NotContains(t, got, "${USER}")
				require.NotContains(t, got, "${PWD}")
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// NOT parallel — mutates global env vars
			os.Unsetenv("HOME")
			os.Unsetenv("USER")
			os.Unsetenv("PWD")
			os.Unsetenv("UNSET_VAR")
			os.Unsetenv("MY_TOKEN")
			os.Unsetenv("TEST_MY_HOME")
			tt.setup()
			got := ExpandEnv(tt.input)
			tt.verify(got)
		})
	}
}

func TestEnvSecretsProvider(t *testing.T) {
	t.Parallel()

	os.Setenv("TEST_SECRET", "secret123")
	defer os.Unsetenv("TEST_SECRET")

	p := NewEnvSecretsProvider()
	require.Equal(t, "secret123", p.Get("TEST_SECRET"))
	require.Empty(t, p.Get("NONEXISTENT"))
}

func TestChainedSecretsProvider(t *testing.T) {
	t.Parallel()

	p := NewChainedSecretsProvider(
		&staticProvider{data: map[string]string{"key1": "from-first"}},
		&staticProvider{data: map[string]string{"key1": "from-second", "key2": "from-second"}},
	)

	require.Equal(t, "from-first", p.Get("key1"))  // first provider wins
	require.Equal(t, "from-second", p.Get("key2")) // only in second
	require.Empty(t, p.Get("key3"))                // neither has it
}

type staticProvider struct {
	data map[string]string
}

func (p *staticProvider) Get(key string) string {
	return p.data[key]
}

func TestChainedSecretsProvider_Empty(t *testing.T) {
	t.Parallel()

	p := NewChainedSecretsProvider()
	require.Empty(t, p.Get("anything"))
}

func TestLoad_FileNotFound(t *testing.T) {
	t.Parallel()

	_, err := Load("/nonexistent/config.yaml", LoadOptions{})
	require.Error(t, err)
}

func TestMustLoad_Panic(t *testing.T) {
	t.Parallel()

	require.Panics(t, func() {
		MustLoad("/nonexistent/config.yaml", LoadOptions{})
	})
}

func TestReadFile(t *testing.T) {
	t.Parallel()

	// Create a temp file
	tmp, err := os.CreateTemp("", "test_config_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmp.Name())

	content := []byte("gateway:\n  addr: :9090\n")
	_, err = tmp.Write(content)
	require.NoError(t, err)
	require.NoError(t, tmp.Close())

	data, err := ReadFile(tmp.Name())
	require.NoError(t, err)
	require.Equal(t, content, data)
}

func TestReadFile_NotFound(t *testing.T) {
	t.Parallel()

	_, err := ReadFile("/nonexistent/file.yaml")
	require.Error(t, err)
}

// ─── Config inheritance tests ──────────────────────────────────────────────────

func TestLoad_Inheritance_CycleDetection(t *testing.T) {
	t.Parallel()

	// Create two config files that reference each other.
	baseDir := t.TempDir()

	baseCfg := baseDir + "/base.yaml"
	if err := os.WriteFile(baseCfg, []byte("gateway:\n  addr: :8888\ninherits: child.yaml\n"), 0644); err != nil {
		t.Fatal(err)
	}
	childCfg := baseDir + "/child.yaml"
	if err := os.WriteFile(childCfg, []byte("gateway:\n  addr: :9090\ninherits: base.yaml\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(baseCfg, LoadOptions{})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrConfigCycle)
}

func TestLoad_Inheritance_SelfReference(t *testing.T) {
	t.Parallel()

	tmp, err := os.CreateTemp("", "self_cycle_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString("gateway:\n  addr: :8888\ninherits: " + tmp.Name() + "\n"); err != nil {
		t.Fatal(err)
	}
	tmp.Close()

	_, err = Load(tmp.Name(), LoadOptions{})
	require.Error(t, err)
	require.ErrorIs(t, err, ErrConfigCycle)
}

func TestLoad_Inheritance_ThreeLevelChain(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()

	baseCfg := baseDir + "/base.yaml"
	if err := os.WriteFile(baseCfg, []byte("gateway:\n  addr: :8888\npool:\n  max_size: 10\n"), 0644); err != nil {
		t.Fatal(err)
	}
	midCfg := baseDir + "/mid.yaml"
	if err := os.WriteFile(midCfg, []byte("gateway:\n  addr: :9090\ninherits: base.yaml\npool:\n  max_size: 20\n"), 0644); err != nil {
		t.Fatal(err)
	}
	leafCfg := baseDir + "/leaf.yaml"
	if err := os.WriteFile(leafCfg, []byte("gateway:\n  addr: :7070\ninherits: mid.yaml\npool:\n  max_size: 30\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(leafCfg, LoadOptions{})
	require.NoError(t, err)
	// Leaf overrides mid, mid overrides base.
	require.Equal(t, ":7070", cfg.Gateway.Addr)
	require.Equal(t, 30, cfg.Pool.MaxSize)
}

func TestLoad_Inheritance_NoInherits(t *testing.T) {
	t.Parallel()

	tmp, err := os.CreateTemp("", "no_inherit_*.yaml")
	require.NoError(t, err)
	defer os.Remove(tmp.Name())

	if _, err := tmp.WriteString("gateway:\n  addr: :6060\npool:\n  max_size: 5\n"); err != nil {
		t.Fatal(err)
	}
	tmp.Close()

	cfg, err := Load(tmp.Name(), LoadOptions{})
	require.NoError(t, err)
	require.Equal(t, ":6060", cfg.Gateway.Addr)
	require.Equal(t, 5, cfg.Pool.MaxSize)
}

func TestLoad_Inheritance_PathExpansion(t *testing.T) {
	t.Parallel()

	baseDir := t.TempDir()

	// Use absolute path for base, relative for child.
	baseCfg := baseDir + "/base.yaml"
	if err := os.WriteFile(baseCfg, []byte("gateway:\n  addr: :8000\n"), 0644); err != nil {
		t.Fatal(err)
	}

	relChild := "child.yaml"
	childPath := baseDir + "/" + relChild
	if err := os.WriteFile(childPath, []byte("inherits: "+relChild+"\ngateway:\n  addr: :8001\n"), 0644); err != nil {
		t.Fatal(err)
	}
	// Fix: child inherits from base (the parent), not itself.
	if err := os.WriteFile(childPath, []byte("inherits: "+baseCfg+"\ngateway:\n  addr: :8001\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(childPath, LoadOptions{})
	require.NoError(t, err)
	require.Equal(t, ":8001", cfg.Gateway.Addr)
}

func TestLoad_NumberedEnv(t *testing.T) {
	os.Setenv("HOTPLEX_ADMIN_TOKEN_1", "token1")
	os.Setenv("HOTPLEX_ADMIN_TOKEN_2", "token2")
	os.Setenv("HOTPLEX_SECURITY_API_KEY_1", "key1")
	defer func() {
		os.Unsetenv("HOTPLEX_ADMIN_TOKEN_1")
		os.Unsetenv("HOTPLEX_ADMIN_TOKEN_2")
		os.Unsetenv("HOTPLEX_SECURITY_API_KEY_1")
	}()

	cfg, err := Load("", LoadOptions{})
	require.NoError(t, err)

	require.Contains(t, cfg.Admin.Tokens, "token1")
	require.Contains(t, cfg.Admin.Tokens, "token2")
	require.Contains(t, cfg.Security.APIKeys, "key1")
}

func TestConfig_RequireSecrets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		cfg         Config
		expectError bool
	}{
		{
			name: "JWT secret present",
			cfg: Config{
				Security: SecurityConfig{
					JWTSecret: decodeJWTSecret("c2VjcmV0LXNlY3JldC1zZWNyZXQtc2VjcmV0MTIzNDU="),
				},
			},
			expectError: false,
		},
		{
			name: "JWT secret missing",
			cfg: Config{
				Security: SecurityConfig{
					JWTSecret: []byte{},
				},
			},
			expectError: true,
		},
		{
			name: "JWT secret present but short",
			cfg: Config{
				Security: SecurityConfig{
					JWTSecret: decodeJWTSecret("c2hvcnQ="), // "short" in base64
				},
			},
			expectError: false, // Only checks for empty, not minimum length
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.cfg.RequireSecrets()
			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestAutoRetryConfig_Defaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    AutoRetryConfig
		expected AutoRetryConfig
	}{
		{
			name:  "zero values get defaults",
			input: AutoRetryConfig{},
			expected: AutoRetryConfig{
				MaxRetries: 9,
				BaseDelay:  5 * time.Second,
				MaxDelay:   120 * time.Second,
				RetryInput: "继续",
			},
		},
		{
			name: "non-zero values preserved",
			input: AutoRetryConfig{
				MaxRetries: 5,
				BaseDelay:  1 * time.Second,
				MaxDelay:   30 * time.Second,
				Enabled:    true,
				RetryInput: "retry",
				NotifyUser: true,
				Patterns:   []string{"429", "5xx"},
			},
			expected: AutoRetryConfig{
				MaxRetries: 5,
				BaseDelay:  1 * time.Second,
				MaxDelay:   30 * time.Second,
				Enabled:    true,
				RetryInput: "retry",
				NotifyUser: true,
				Patterns:   []string{"429", "5xx"},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := tt.input.Defaults()
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestDecodeJWTSecret(t *testing.T) {
	t.Parallel()

	// Valid 32-byte secret
	validSecret32 := make([]byte, 32)
	for i := range validSecret32 {
		validSecret32[i] = byte(i)
	}
	validSecretB64 := base64.StdEncoding.EncodeToString(validSecret32)
	validSecretURLB64 := base64.URLEncoding.EncodeToString(validSecret32)

	tests := []struct {
		name     string
		input    string
		expected []byte
	}{
		{
			name:     "standard base64",
			input:    validSecretB64,
			expected: validSecret32,
		},
		{
			name:     "URL-safe base64",
			input:    validSecretURLB64,
			expected: validSecret32,
		},
		{
			name:     "raw 32-byte string",
			input:    string(validSecret32),
			expected: validSecret32,
		},
		{
			name:     "other string (not 32 bytes)",
			input:    "short",
			expected: []byte("short"),
		},
		{
			name:     "empty string",
			input:    "",
			expected: []byte(""),
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := decodeJWTSecret(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestNormalizePath(t *testing.T) {
	// Not parallel because it modifies global env var HOME
	// Save original HOME for restoration
	origHome := os.Getenv("HOME")
	defer os.Setenv("HOME", origHome)

	tests := []struct {
		name        string
		input       string
		setup       func()
		expected    interface{} // string or func() string
		expectError bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "already absolute path",
			input:    "/absolute/path/to/file.yaml",
			expected: "/absolute/path/to/file.yaml",
		},
		{
			name:     "tilde path with HOME set",
			input:    "~/config.yaml",
			setup:    func() { os.Setenv("HOME", "/home/testuser") },
			expected: "/home/testuser/config.yaml",
		},
		{
			name:  "relative path",
			input: "relative/path/file.yaml",
			expected: func() string {
				abs, _ := filepath.Abs("relative/path/file.yaml")
				return abs
			},
		},
		{
			name:     "tilde path without HOME set",
			input:    "~/config.yaml",
			setup:    func() { os.Unsetenv("HOME") },
			expected: "~/config.yaml", // Returns as-is when HOME not available
		},
		{
			name:  "tilde at root with HOME set",
			input: "~",
			setup: func() { os.Setenv("HOME", "/home/testuser") },
			expected: func() string {
				abs, _ := filepath.Abs("~")
				return abs
			},
		},
		{
			name:  "tilde at root without HOME set",
			input: "~",
			setup: func() { os.Unsetenv("HOME") },
			expected: func() string {
				abs, _ := filepath.Abs("~")
				return abs
			},
		},
		{
			name:  "path with null byte",
			input: string([]byte{0}), // null byte in path
			expected: func() string {
				abs, _ := filepath.Abs(string([]byte{0}))
				return abs
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			// Restore HOME before setup
			os.Setenv("HOME", origHome)
			if tt.setup != nil {
				tt.setup()
			}

			result, err := ExpandAndAbs(tt.input)

			if tt.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				if fn, ok := tt.expected.(func() string); ok {
					require.Equal(t, fn(), result)
				} else {
					require.Equal(t, tt.expected, result)
				}
			}
		})
	}
}

func TestApplyMessagingEnv(t *testing.T) {
	// Not parallel because it modifies global environment variables
	// Save original env vars for restoration
	origEnvVars := make(map[string]string)
	envVarsToCheck := []string{
		"HOTPLEX_MESSAGING_SLACK_ENABLED",
		"HOTPLEX_MESSAGING_SLACK_BOT_TOKEN",
		"HOTPLEX_MESSAGING_SLACK_APP_TOKEN",
		"HOTPLEX_MESSAGING_FEISHU_ENABLED",
		"HOTPLEX_MESSAGING_FEISHU_APP_ID",
		"HOTPLEX_MESSAGING_FEISHU_APP_SECRET",
		"HOTPLEX_MESSAGING_FEISHU_WORKER_TYPE",
		"HOTPLEX_MESSAGING_FEISHU_WORK_DIR",
		"HOTPLEX_MESSAGING_SLACK_WORKER_TYPE",
		"HOTPLEX_MESSAGING_SLACK_WORK_DIR",
		"HOTPLEX_MESSAGING_SLACK_DM_POLICY",
		"HOTPLEX_MESSAGING_SLACK_GROUP_POLICY",
		"HOTPLEX_MESSAGING_SLACK_REQUIRE_MENTION",
		"HOTPLEX_MESSAGING_SLACK_ALLOW_FROM",
		"HOTPLEX_MESSAGING_SLACK_ALLOW_DM_FROM",
		"HOTPLEX_MESSAGING_SLACK_ALLOW_GROUP_FROM",
		"HOTPLEX_MESSAGING_FEISHU_DM_POLICY",
		"HOTPLEX_MESSAGING_FEISHU_GROUP_POLICY",
		"HOTPLEX_MESSAGING_FEISHU_REQUIRE_MENTION",
		"HOTPLEX_MESSAGING_FEISHU_ALLOW_FROM",
		"HOTPLEX_MESSAGING_FEISHU_ALLOW_DM_FROM",
		"HOTPLEX_MESSAGING_FEISHU_ALLOW_GROUP_FROM",
	}

	// Save original values
	for _, envVar := range envVarsToCheck {
		if val, exists := os.LookupEnv(envVar); exists {
			origEnvVars[envVar] = val
		}
	}
	defer func() {
		// Restore original values
		for _, envVar := range envVarsToCheck {
			if val, exists := origEnvVars[envVar]; exists {
				os.Setenv(envVar, val)
			} else {
				os.Unsetenv(envVar)
			}
		}
	}()

	// Create a config with default values
	cfg := Default()

	// Test 1: No environment variables set - config should remain unchanged
	applyMessagingEnv(cfg)
	require.False(t, cfg.Messaging.Slack.Enabled) // Default is false
	require.Equal(t, "", cfg.Messaging.Slack.BotToken)
	require.Equal(t, "", cfg.Messaging.Slack.AppToken)
	require.False(t, cfg.Messaging.Feishu.Enabled) // Default is false
	require.Equal(t, "", cfg.Messaging.Feishu.AppID)
	require.Equal(t, "", cfg.Messaging.Feishu.AppSecret)
	require.Equal(t, "", cfg.Messaging.Feishu.WorkerType)
	require.Equal(t, "", cfg.Messaging.Feishu.WorkDir)
	require.Equal(t, "", cfg.Messaging.Slack.WorkerType)
	require.Equal(t, "", cfg.Messaging.Slack.WorkDir)
	require.Equal(t, "allowlist", cfg.Messaging.Slack.DMPolicy)    // Default is "allowlist"
	require.Equal(t, "allowlist", cfg.Messaging.Slack.GroupPolicy) // Default is "allowlist"
	require.True(t, cfg.Messaging.Slack.RequireMention)            // Default is true
	require.Nil(t, cfg.Messaging.Slack.AllowFrom)
	require.Nil(t, cfg.Messaging.Slack.AllowDMFrom)
	require.Nil(t, cfg.Messaging.Slack.AllowGroupFrom)

	// Test 2: Set environment variables - config should be updated
	os.Setenv("HOTPLEX_MESSAGING_SLACK_ENABLED", "true")
	os.Setenv("HOTPLEX_MESSAGING_SLACK_BOT_TOKEN", "slack-bot-token-123")
	os.Setenv("HOTPLEX_MESSAGING_SLACK_APP_TOKEN", "slack-app-token-456")
	os.Setenv("HOTPLEX_MESSAGING_FEISHU_ENABLED", "TRUE") // Test case-insensitive
	os.Setenv("HOTPLEX_MESSAGING_FEISHU_APP_ID", "feishu-app-id")
	os.Setenv("HOTPLEX_MESSAGING_FEISHU_APP_SECRET", "feishu-app-secret")
	os.Setenv("HOTPLEX_MESSAGING_FEISHU_WORKER_TYPE", "claude")
	os.Setenv("HOTPLEX_MESSAGING_FEISHU_WORK_DIR", "/tmp/feishu-work")
	os.Setenv("HOTPLEX_MESSAGING_SLACK_WORKER_TYPE", "opencode")
	os.Setenv("HOTPLEX_MESSAGING_SLACK_WORK_DIR", "/tmp/slack-work")
	os.Setenv("HOTPLEX_MESSAGING_SLACK_DM_POLICY", "allow")
	os.Setenv("HOTPLEX_MESSAGING_SLACK_GROUP_POLICY", "deny")
	os.Setenv("HOTPLEX_MESSAGING_SLACK_REQUIRE_MENTION", "True") // Test case-insensitive
	os.Setenv("HOTPLEX_MESSAGING_SLACK_ALLOW_FROM", "user1,user2,user3")
	os.Setenv("HOTPLEX_MESSAGING_SLACK_ALLOW_DM_FROM", "dmuser1,dmuser2")
	os.Setenv("HOTPLEX_MESSAGING_SLACK_ALLOW_GROUP_FROM", "group1,group2,group3")
	os.Setenv("HOTPLEX_MESSAGING_FEISHU_DM_POLICY", "allow")
	os.Setenv("HOTPLEX_MESSAGING_FEISHU_GROUP_POLICY", "deny")
	os.Setenv("HOTPLEX_MESSAGING_FEISHU_REQUIRE_MENTION", "FALSE")
	os.Setenv("HOTPLEX_MESSAGING_FEISHU_ALLOW_FROM", "feishu1,feishu2")
	os.Setenv("HOTPLEX_MESSAGING_FEISHU_ALLOW_DM_FROM", "feishudm1")
	os.Setenv("HOTPLEX_MESSAGING_FEISHU_ALLOW_GROUP_FROM", "feishugroup1,feishugroup2")

	// Create a fresh config to test
	cfg2 := Default()
	applyMessagingEnv(cfg2)

	// Verify Slack config
	require.True(t, cfg2.Messaging.Slack.Enabled)
	require.Equal(t, "slack-bot-token-123", cfg2.Messaging.Slack.BotToken)
	require.Equal(t, "slack-app-token-456", cfg2.Messaging.Slack.AppToken)
	require.Equal(t, "opencode", cfg2.Messaging.Slack.WorkerType)
	require.Equal(t, "/tmp/slack-work", cfg2.Messaging.Slack.WorkDir)
	require.Equal(t, "allow", cfg2.Messaging.Slack.DMPolicy)
	require.Equal(t, "deny", cfg2.Messaging.Slack.GroupPolicy)
	require.True(t, cfg2.Messaging.Slack.RequireMention)
	require.Equal(t, []string{"user1", "user2", "user3"}, cfg2.Messaging.Slack.AllowFrom)
	require.Equal(t, []string{"dmuser1", "dmuser2"}, cfg2.Messaging.Slack.AllowDMFrom)
	require.Equal(t, []string{"group1", "group2", "group3"}, cfg2.Messaging.Slack.AllowGroupFrom)

	// Verify Feishu config
	require.True(t, cfg2.Messaging.Feishu.Enabled)
	require.Equal(t, "feishu-app-id", cfg2.Messaging.Feishu.AppID)
	require.Equal(t, "feishu-app-secret", cfg2.Messaging.Feishu.AppSecret)
	require.Equal(t, "claude", cfg2.Messaging.Feishu.WorkerType)
	require.Equal(t, "/tmp/feishu-work", cfg2.Messaging.Feishu.WorkDir)
	require.Equal(t, "allow", cfg2.Messaging.Feishu.DMPolicy)
	require.Equal(t, "deny", cfg2.Messaging.Feishu.GroupPolicy)
	require.False(t, cfg2.Messaging.Feishu.RequireMention)
	require.Equal(t, []string{"feishu1", "feishu2"}, cfg2.Messaging.Feishu.AllowFrom)
	require.Equal(t, []string{"feishudm1"}, cfg2.Messaging.Feishu.AllowDMFrom)
	require.Equal(t, []string{"feishugroup1", "feishugroup2"}, cfg2.Messaging.Feishu.AllowGroupFrom)

	// Test 3: Empty string for boolean fields should not change defaults
	os.Unsetenv("HOTPLEX_MESSAGING_SLACK_ENABLED")
	os.Setenv("HOTPLEX_MESSAGING_SLACK_ENABLED", "")
	cfg3 := Default()
	cfg3.Messaging.Slack.Enabled = true // Set to true initially
	applyMessagingEnv(cfg3)
	require.True(t, cfg3.Messaging.Slack.Enabled) // Should remain true, not reset to false

	// Test 4: Invalid boolean value (not "true" or "false") should be treated as false
	os.Setenv("HOTPLEX_MESSAGING_SLACK_ENABLED", "yes")
	cfg4 := Default()
	applyMessagingEnv(cfg4)
	require.False(t, cfg4.Messaging.Slack.Enabled) // "yes" is not "true", so should be false

	// Test 5: Empty string for list fields should result in nil slice
	os.Setenv("HOTPLEX_MESSAGING_SLACK_ALLOW_FROM", "")
	cfg5 := Default()
	applyMessagingEnv(cfg5)
	require.Nil(t, cfg5.Messaging.Slack.AllowFrom)
}

func TestMustLoad_Success(t *testing.T) {
	t.Parallel()

	tempFile, err := os.CreateTemp("", "valid-config-*.yaml")
	require.NoError(t, err)
	defer os.Remove(tempFile.Name())

	configContent := `gateway:
  addr: ":8888"
  broadcast_queue_size: 256

admin:
  enabled: true
  addr: ":9999"

security:
  tls_enabled: false

session:
  retention_period: "168h"
  gc_scan_interval: "5m"

pool:
  max_size: 100
  max_idle_per_user: 5

db:
  path: "data/test.db"
  wal_mode: true`

	_, err = tempFile.Write([]byte(configContent))
	require.NoError(t, err)
	tempFile.Close()

	cfg := MustLoad(tempFile.Name(), LoadOptions{})
	require.NotNil(t, cfg)
	require.Equal(t, ":8888", cfg.Gateway.Addr)
	require.Equal(t, 256, cfg.Gateway.BroadcastQueueSize)
	require.True(t, cfg.Admin.Enabled)
	require.Equal(t, ":9999", cfg.Admin.Addr)
	require.False(t, cfg.Security.TLSEnabled)
	require.Equal(t, 168*time.Hour, cfg.Session.RetentionPeriod)
	require.Equal(t, 5*time.Minute, cfg.Session.GCScanInterval)
	require.Equal(t, 100, cfg.Pool.MaxSize)
	require.Equal(t, 5, cfg.Pool.MaxIdlePerUser)
	require.True(t, filepath.IsAbs(cfg.DB.Path))
	require.True(t, strings.HasSuffix(cfg.DB.Path, "/data/test.db"))
	require.True(t, cfg.DB.WALMode)
}

func TestMustLoad_WithEnvVars(t *testing.T) {
	t.Parallel()
	t.Skip("Environment variable expansion in YAML not currently supported by config loader")
}
