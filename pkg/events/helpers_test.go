package events

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIntFloat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    any
		expected int
	}{
		{
			name:     "float64 value",
			input:    float64(42.5),
			expected: 42,
		},
		{
			name:     "float64 integer",
			input:    float64(100),
			expected: 100,
		},
		{
			name:     "float64 zero",
			input:    float64(0),
			expected: 0,
		},
		{
			name:     "float64 negative",
			input:    float64(-15.3),
			expected: -15,
		},
		{
			name:     "non-float int",
			input:    123,
			expected: 0,
		},
		{
			name:     "non-float string",
			input:    "42.5",
			expected: 0,
		},
		{
			name:     "non-float bool",
			input:    true,
			expected: 0,
		},
		{
			name:     "nil input",
			input:    nil,
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := IntFloat(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestStrVal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    any
		expected string
	}{
		{
			name:     "string value",
			input:    "hello world",
			expected: "hello world",
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "non-string int",
			input:    42,
			expected: "",
		},
		{
			name:     "non-string float",
			input:    3.14,
			expected: "",
		},
		{
			name:     "non-string bool",
			input:    true,
			expected: "",
		},
		{
			name:     "nil input",
			input:    nil,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := StrVal(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestSliceVal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    any
		expected []any
	}{
		{
			name:     "slice of any",
			input:    []any{"a", 1, true},
			expected: []any{"a", 1, true},
		},
		{
			name:     "empty slice",
			input:    []any{},
			expected: []any{},
		},
		{
			name:     "nil slice",
			input:    []any(nil),
			expected: nil,
		},
		{
			name:     "non-slice string",
			input:    "not a slice",
			expected: nil,
		},
		{
			name:     "non-slice int",
			input:    42,
			expected: nil,
		},
		{
			name:     "nil input",
			input:    nil,
			expected: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := SliceVal(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestMapContextUsageResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    map[string]any
		expected *ContextUsageData
	}{
		{
			name:  "nil input returns empty struct",
			input: nil,
			expected: &ContextUsageData{
				Categories: nil,
			},
		},
		{
			name:  "empty map returns empty struct",
			input: map[string]any{},
			expected: &ContextUsageData{
				Categories: nil,
			},
		},
		{
			name: "full map with all fields",
			input: map[string]any{
				"totalTokens": float64(1500),
				"maxTokens":   float64(2000),
				"percentage":  float64(75),
				"model":       "claude-3.5-sonnet",
				"memoryFiles": float64(5),
				"mcpTools":    float64(3),
				"agents":      float64(2),
				"categories": []any{
					map[string]any{
						"name":   "system",
						"tokens": float64(200),
					},
					map[string]any{
						"name":   "user",
						"tokens": float64(800),
					},
					map[string]any{
						"name":   "assistant",
						"tokens": float64(500),
					},
				},
				"skills": map[string]any{
					"totalSkills":    float64(10),
					"includedSkills": float64(7),
					"tokens":         float64(350),
				},
			},
			expected: &ContextUsageData{
				TotalTokens: 1500,
				MaxTokens:   2000,
				Percentage:  75,
				Model:       "claude-3.5-sonnet",
				MemoryFiles: 5,
				MCPTools:    3,
				Agents:      2,
				Categories: []ContextCategory{
					{Name: "system", Tokens: 200},
					{Name: "user", Tokens: 800},
					{Name: "assistant", Tokens: 500},
				},
				Skills: ContextSkillInfo{
					Total:    10,
					Included: 7,
					Tokens:   350,
				},
			},
		},
		{
			name: "partial map with missing fields",
			input: map[string]any{
				"totalTokens": float64(500),
				"maxTokens":   float64(1000),
				"model":       "test-model",
			},
			expected: &ContextUsageData{
				TotalTokens: 500,
				MaxTokens:   1000,
				Percentage:  0,
				Model:       "test-model",
				MemoryFiles: 0,
				MCPTools:    0,
				Agents:      0,
				Categories:  nil,
			},
		},
		{
			name: "map with invalid categories array element",
			input: map[string]any{
				"categories": []any{
					"not a map",
					map[string]any{
						"name":   "valid",
						"tokens": float64(100),
					},
				},
			},
			expected: &ContextUsageData{
				Categories: []ContextCategory{
					{Name: "", Tokens: 0}, // Empty category for invalid element
					{Name: "valid", Tokens: 100},
				},
			},
		},
		{
			name: "map with categories missing name or tokens",
			input: map[string]any{
				"categories": []any{
					map[string]any{
						"name": "missing-tokens",
					},
					map[string]any{
						"tokens": float64(200),
					},
					map[string]any{
						"name":   "complete",
						"tokens": float64(300),
					},
				},
			},
			expected: &ContextUsageData{
				Categories: []ContextCategory{
					{Name: "missing-tokens", Tokens: 0},
					{Name: "", Tokens: 200},
					{Name: "complete", Tokens: 300},
				},
			},
		},
		{
			name: "map with invalid skills field type",
			input: map[string]any{
				"skills": "not a map",
			},
			expected: &ContextUsageData{
				Categories: nil,
			},
		},
		{
			name: "map with partial skills map",
			input: map[string]any{
				"skills": map[string]any{
					"totalSkills": float64(5),
				},
			},
			expected: &ContextUsageData{
				Categories: nil,
				Skills: ContextSkillInfo{
					Total:    5,
					Included: 0,
					Tokens:   0,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := MapContextUsageResponse(tt.input)

			// Compare fields individually since nil vs empty slice comparison fails with require.Equal
			require.Equal(t, tt.expected.TotalTokens, result.TotalTokens)
			require.Equal(t, tt.expected.MaxTokens, result.MaxTokens)
			require.Equal(t, tt.expected.Percentage, result.Percentage)
			require.Equal(t, tt.expected.Model, result.Model)
			require.Equal(t, tt.expected.MemoryFiles, result.MemoryFiles)
			require.Equal(t, tt.expected.MCPTools, result.MCPTools)
			require.Equal(t, tt.expected.Agents, result.Agents)
			require.Equal(t, tt.expected.Skills, result.Skills)

			// For Categories, check length and elements (handles nil vs empty)
			if len(tt.expected.Categories) == 0 {
				require.Nil(t, result.Categories)
			} else {
				require.Equal(t, tt.expected.Categories, result.Categories)
			}
		})
	}
}

func TestMapMCPStatusResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    map[string]any
		expected *MCPStatusData
	}{
		{
			name:  "nil input returns empty struct",
			input: nil,
			expected: &MCPStatusData{
				Servers: nil,
			},
		},
		{
			name:  "empty map returns empty struct",
			input: map[string]any{},
			expected: &MCPStatusData{
				Servers: nil,
			},
		},
		{
			name: "map with servers array",
			input: map[string]any{
				"servers": []any{
					map[string]any{
						"name":   "filesystem",
						"status": "connected",
					},
					map[string]any{
						"name":   "github",
						"status": "disconnected",
					},
					map[string]any{
						"name":   "sqlite",
						"status": "connecting",
					},
				},
			},
			expected: &MCPStatusData{
				Servers: []MCPServerInfo{
					{Name: "filesystem", Status: "connected"},
					{Name: "github", Status: "disconnected"},
					{Name: "sqlite", Status: "connecting"},
				},
			},
		},
		{
			name: "map with empty servers array",
			input: map[string]any{
				"servers": []any{},
			},
			expected: &MCPStatusData{
				Servers: []MCPServerInfo{},
			},
		},
		{
			name: "map with servers missing fields",
			input: map[string]any{
				"servers": []any{
					map[string]any{
						"name": "missing-status",
					},
					map[string]any{
						"status": "missing-name",
					},
					map[string]any{
						"name":   "complete",
						"status": "connected",
					},
				},
			},
			expected: &MCPStatusData{
				Servers: []MCPServerInfo{
					{Name: "missing-status", Status: ""},
					{Name: "", Status: "missing-name"},
					{Name: "complete", Status: "connected"},
				},
			},
		},
		{
			name: "map with invalid servers field type",
			input: map[string]any{
				"servers": "not an array",
			},
			expected: &MCPStatusData{
				Servers: nil,
			},
		},
		{
			name: "map with invalid server element type",
			input: map[string]any{
				"servers": []any{
					"not a map",
					map[string]any{
						"name":   "valid",
						"status": "ok",
					},
				},
			},
			expected: &MCPStatusData{
				Servers: []MCPServerInfo{
					{Name: "valid", Status: "ok"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := MapMCPStatusResponse(tt.input)

			if len(tt.expected.Servers) == 0 {
				require.Nil(t, result.Servers)
			} else {
				require.Equal(t, tt.expected.Servers, result.Servers)
			}
		})
	}
}
