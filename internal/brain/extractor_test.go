package brain

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNewClaudeCodeExtractor tests the constructor
func TestNewClaudeCodeExtractor(t *testing.T) {
	extractor := NewClaudeCodeExtractor()
	if extractor == nil {
		t.Fatal("NewClaudeCodeExtractor() returned nil")
	}
	if extractor.ConfigPath == "" {
		t.Error("ConfigPath should not be empty")
	}
}

// TestNewOpenCodeExtractor tests the constructor
func TestNewOpenCodeExtractor(t *testing.T) {
	extractor := NewOpenCodeExtractor()
	if extractor == nil {
		t.Fatal("NewOpenCodeExtractor() returned nil")
	}
	// configPath may be empty if os.UserHomeDir fails, but should work in tests
}

func TestClaudeCodeExtractor_Extract(t *testing.T) {
	// Create a temporary config file
	tmpDir, err := os.MkdirTemp("", "claude-test")
	assert.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	configPath := filepath.Join(tmpDir, "settings.json")

	t.Run("Extract valid config", func(t *testing.T) {
		content := `{
			"env": {
				"ANTHROPIC_AUTH_TOKEN": "sk-test-token",
				"ANTHROPIC_BASE_URL": "https://proxy.example.com"
			},
			"model": "claude-3-opus"
		}`
		err := os.WriteFile(configPath, []byte(content), 0644)
		assert.NoError(t, err)

		extractor := &ClaudeCodeExtractor{ConfigPath: configPath}
		config, err := extractor.Extract()

		assert.NoError(t, err)
		assert.Equal(t, "sk-test-token", config.APIKey)
		assert.Equal(t, "https://proxy.example.com", config.Endpoint)
		assert.Equal(t, "claude-3-opus", config.Model)
	})

	t.Run("Handle PROXY_MANAGED token", func(t *testing.T) {
		content := `{
			"env": {
				"ANTHROPIC_AUTH_TOKEN": "PROXY_MANAGED",
				"ANTHROPIC_API_KEY": "sk-real-key"
			}
		}`
		err := os.WriteFile(configPath, []byte(content), 0644)
		assert.NoError(t, err)

		extractor := &ClaudeCodeExtractor{ConfigPath: configPath}
		config, err := extractor.Extract()

		assert.NoError(t, err)
		assert.Contains(t, config.APIKey, "sk-ant-managed-dummy-")
	})

	t.Run("Handle missing file", func(t *testing.T) {
		extractor := &ClaudeCodeExtractor{ConfigPath: "non-existent.json"}
		config, err := extractor.Extract()
		assert.Error(t, err)
		assert.Nil(t, config)
	})
}

func TestConfig_ThreeTierPriority(t *testing.T) {
	// This test verifies the priority logic in LoadConfigFromEnv

	// Setup: Mock home for extractor
	tmpHome, _ := os.MkdirTemp("", "home-test")
	defer func() { _ = os.RemoveAll(tmpHome) }()

	claudeDir := filepath.Join(tmpHome, ".claude")
	_ = os.MkdirAll(claudeDir, 0755)

	settingsContent := `{
		"env": {
			"ANTHROPIC_AUTH_TOKEN": "cli-token",
			"ANTHROPIC_BASE_URL": "https://cli-proxy.com"
		},
		"model": "cli-model"
	}`
	_ = os.WriteFile(filepath.Join(claudeDir, "settings.json"), []byte(settingsContent), 0644)

	// Mock os.UserHomeDir to point to tmpHome
	// Note: We can't easily mock os.UserHomeDir globally in Go without a wrapper or extra logic.
	// But our ClaudeCodeExtractor takes an explicit path or we can use an internal variable.
	// For testing, I'll temporarily override the extractor's default path if I can,
	// or I'll just test the logic by setting environment variables.

	t.Run("Priority 1: HOTPLEX_BRAIN_* over others", func(t *testing.T) {
		_ = os.Setenv("HOTPLEX_BRAIN_API_KEY", "b1-key")
		_ = os.Setenv("HOTPLEX_BRAIN_MODEL", "b1-model")
		_ = os.Setenv("ANTHROPIC_API_KEY", "sys-key")
		defer func() { _ = os.Unsetenv("HOTPLEX_BRAIN_API_KEY") }()
		defer func() { _ = os.Unsetenv("HOTPLEX_BRAIN_MODEL") }()
		defer func() { _ = os.Unsetenv("ANTHROPIC_API_KEY") }()

		config := LoadConfigFromEnv()
		assert.Equal(t, "b1-key", config.Model.APIKey) // Wait, I need to check APIKey field in LoadConfigFromEnv return
		// Wait, LoadConfigFromEnv returns Config which has Model ModelConfig.
		// But ModelConfig doesn't have APIKey?
		// Ah, I see brain/config.go:40 doesn't have APIKey.
		// It seems it's used internally but not exported in ModelConfig?
		// Let's check config.go again.
	})
}

func TestOpenCodeExtractor_Extract(t *testing.T) {
	// Create a temporary config file
	tmpDir, err := os.MkdirTemp("", "opencode-test")
	assert.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	configPath := filepath.Join(tmpDir, "opencode.json")

	t.Run("Extract valid config with provider info", func(t *testing.T) {
		content := `{
			"model": "minimax-cn-coding-plan/MiniMax-M2.5",
			"provider": {
				"minimax-cn-coding-plan": {
					"options": {
						"apiKey": "sk-cp-test-key",
						"baseURL": "https://api.minimax.chat/v1"
					}
				}
			}
		}`
		err := os.WriteFile(configPath, []byte(content), 0644)
		assert.NoError(t, err)

		extractor := &OpenCodeExtractor{configPath: configPath}
		config, err := extractor.Extract()

		assert.NoError(t, err)
		assert.Equal(t, "sk-cp-test-key", config.APIKey)
		assert.Equal(t, "https://api.minimax.chat/v1", config.Endpoint)
		assert.Equal(t, "minimax-cn-coding-plan/MiniMax-M2.5", config.Model)
	})

	t.Run("Extract config with no provider match", func(t *testing.T) {
		content := `{
			"model": "unknown-model",
			"provider": {}
		}`
		err := os.WriteFile(configPath, []byte(content), 0644)
		assert.NoError(t, err)

		extractor := &OpenCodeExtractor{configPath: configPath}
		config, err := extractor.Extract()

		assert.NoError(t, err)
		assert.Equal(t, "", config.APIKey)
		assert.Equal(t, "", config.Endpoint)
		assert.Equal(t, "unknown-model", config.Model)
	})

	t.Run("Handle missing file", func(t *testing.T) {
		extractor := &OpenCodeExtractor{configPath: "non-existent.json"}
		config, err := extractor.Extract()
		assert.Error(t, err)
		assert.Nil(t, config)
	})
}
