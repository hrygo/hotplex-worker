package main

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
)

func TestBuildMCPConfigJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		servers    map[string]*config.MCPServerConfig
		wantEmpty  bool
		wantSubset string
	}{
		{
			name:      "nil servers returns empty",
			servers:   nil,
			wantEmpty: true,
		},
		{
			name:      "empty servers returns empty",
			servers:   map[string]*config.MCPServerConfig{},
			wantEmpty: true,
		},
		{
			name: "single command server",
			servers: map[string]*config.MCPServerConfig{
				"test": {Command: "echo"},
			},
			wantSubset: `"test"`,
		},
		{
			name: "single URL server",
			servers: map[string]*config.MCPServerConfig{
				"remote": {URL: "http://localhost:8080"},
			},
			wantSubset: `"remote"`,
		},
		{
			name: "multiple servers",
			servers: map[string]*config.MCPServerConfig{
				"local":  {Command: "my-mcp", Args: []string{"--port", "3000"}},
				"remote": {URL: "http://example.com/mcp"},
			},
			wantSubset: `"local"`,
		},
		{
			name: "invalid server skipped",
			servers: map[string]*config.MCPServerConfig{
				"bad":  {},
				"good": {Command: "echo"},
				"ugly": {Command: "test", URL: "http://dual"},
			},
			wantSubset: `"good"`,
		},
		{
			name: "all invalid servers returns empty",
			servers: map[string]*config.MCPServerConfig{
				"empty": {},
				"both":  {Command: "test", URL: "http://dual"},
			},
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := &config.Config{
				Worker: config.WorkerConfig{
					ClaudeCode: config.ClaudeCodeConfig{
						MCPServers: tt.servers,
					},
				},
			}

			result := buildMCPConfigJSON(cfg)
			if tt.wantEmpty {
				assert.Empty(t, result)
				return
			}

			require.NotEmpty(t, result)
			assert.Contains(t, result, tt.wantSubset)
			assert.Contains(t, result, `"mcpServers"`)

			// Verify the output is valid JSON.
			var parsed map[string]any
			require.NoError(t, json.Unmarshal([]byte(result), &parsed))
		})
	}
}
