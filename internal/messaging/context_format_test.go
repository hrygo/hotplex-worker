package messaging

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

func TestSeverityLevel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		pct  int
		want ContextSeverity
	}{
		{0, SeverityComfortable},
		{25, SeverityComfortable},
		{49, SeverityComfortable},
		{50, SeverityModerate},
		{75, SeverityModerate},
		{76, SeverityHigh},
		{90, SeverityHigh},
		{91, SeverityCritical},
		{100, SeverityCritical},
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, SeverityLevel(tt.pct))
		})
	}
}

func TestSeverityIcon(t *testing.T) {
	t.Parallel()
	tests := []struct {
		severity ContextSeverity
		want     string
	}{
		{SeverityComfortable, "🟢"},
		{SeverityModerate, "🟡"},
		{SeverityHigh, "🟠"},
		{SeverityCritical, "🔴"},
		{ContextSeverity("unknown"), "⚪"},
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, SeverityIcon(tt.severity))
		})
	}
}

func TestSeverityLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		severity ContextSeverity
		want     string
	}{
		{SeverityComfortable, "Comfortable"},
		{SeverityModerate, "Moderate"},
		{SeverityHigh, "High"},
		{SeverityCritical, "Critical"},
		{ContextSeverity("unknown"), ""},
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, SeverityLabel(tt.severity))
		})
	}
}

func TestFormatTokenCount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		tokens int
		want   string
	}{
		{0, "0"},
		{500, "500"},
		{999, "999"},
		{1000, "1K"},
		{1500, "~1.5K"},
		{9999, "~10.0K"},
		{10000, "10K"},
		{76284, "~76.3K"},
		{200000, "200K"},
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, FormatTokenCount(tt.tokens))
		})
	}
}

func TestFormatTokenDisplay(t *testing.T) {
	t.Parallel()
	require.Equal(t, "~76.3K / 200K", FormatTokenDisplay(76284, 200000))
	require.Equal(t, "500 / 1K", FormatTokenDisplay(500, 1000))
}

func TestBuildProgressBar(t *testing.T) {
	t.Parallel()
	tests := []struct {
		pct   int
		width int
		want  string
	}{
		{0, 10, "[░░░░░░░░░░]"},
		{38, 10, "[███░░░░░░░]"},
		{50, 10, "[█████░░░░░]"},
		{75, 10, "[███████░░░]"},
		{100, 10, "[██████████]"},
		{67, 8, "[█████░░░]"},
	}
	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, BuildProgressBar(tt.pct, tt.width))
		})
	}
}

func TestBuildProgressBarClamping(t *testing.T) {
	t.Parallel()
	require.Equal(t, "[░░░░░░░░░░]", BuildProgressBar(-5, 10))
	require.Equal(t, "[██████████]", BuildProgressBar(150, 10))
	require.Equal(t, "[█████░░░░░]", BuildProgressBar(50, 0))
}

func TestFormatTopCategories(t *testing.T) {
	t.Parallel()
	cats := []events.ContextCategory{
		{Name: "System Prompt", Tokens: 603},
		{Name: "Skills", Tokens: 9073},
		{Name: "Messages", Tokens: 55228},
		{Name: "Tools", Tokens: 12000},
	}

	t.Run("top 3 with skills filter", func(t *testing.T) {
		t.Parallel()
		result := FormatTopCategories(cats, 3, true)
		require.Len(t, result, 3)
		require.Equal(t, "Messages", result[0].Name)
		require.Equal(t, 55228, result[0].Tokens)
		require.Equal(t, "Tools", result[1].Name)
		require.Equal(t, "System Prompt", result[2].Name)
	})

	t.Run("all without skills filter", func(t *testing.T) {
		t.Parallel()
		result := FormatTopCategories(cats, 0, false)
		require.Len(t, result, 4)
		require.Equal(t, "Messages", result[0].Name)
	})

	t.Run("empty", func(t *testing.T) {
		t.Parallel()
		result := FormatTopCategories(nil, 3, false)
		require.Nil(t, result)
	})

	t.Run("fewer than limit", func(t *testing.T) {
		t.Parallel()
		result := FormatTopCategories(cats[:2], 5, false)
		require.Len(t, result, 2)
	})
}

func TestFormatActionTip(t *testing.T) {
	t.Parallel()
	require.Empty(t, FormatActionTip(SeverityComfortable))
	require.Empty(t, FormatActionTip(SeverityModerate))
	require.NotEmpty(t, FormatActionTip(SeverityHigh))
	require.Contains(t, FormatActionTip(SeverityHigh), "/compact")
	require.NotEmpty(t, FormatActionTip(SeverityCritical))
	require.Contains(t, FormatActionTip(SeverityCritical), "/compact")
	require.Contains(t, FormatActionTip(SeverityCritical), "/reset")
}

func TestBuildExtrasLine(t *testing.T) {
	t.Parallel()
	t.Run("all fields", func(t *testing.T) {
		d := events.ContextUsageData{
			Skills:      events.ContextSkillInfo{Total: 8},
			MemoryFiles: 3,
			MCPTools:    5,
			Agents:      2,
		}
		require.Equal(t, "8 skills · 3 memory files · 5 MCP tools · 2 agents", BuildExtrasLine(d))
	})
	t.Run("partial fields", func(t *testing.T) {
		d := events.ContextUsageData{
			MemoryFiles: 3,
			MCPTools:    5,
		}
		require.Equal(t, "3 memory files · 5 MCP tools", BuildExtrasLine(d))
	})
	t.Run("empty", func(t *testing.T) {
		require.Empty(t, BuildExtrasLine(events.ContextUsageData{}))
	})
}

func TestBuildContextDisplay(t *testing.T) {
	t.Parallel()
	d := events.ContextUsageData{
		TotalTokens: 76284,
		MaxTokens:   200000,
		Percentage:  38,
		Model:       "claude-sonnet-4",
		Categories: []events.ContextCategory{
			{Name: "System Prompt", Tokens: 603},
			{Name: "Messages", Tokens: 55228},
		},
		MemoryFiles: 3,
		MCPTools:    124,
		Agents:      157,
		Skills:      events.ContextSkillInfo{Total: 147, Included: 147, Tokens: 9073},
	}

	info := BuildContextDisplay(d)
	require.Equal(t, SeverityComfortable, info.Severity)
	require.Equal(t, "🟢", info.Icon)
	require.Equal(t, "Comfortable", info.Label)
	require.Equal(t, "~76.3K / 200K", info.TokenDisplay)
	require.Equal(t, "[███░░░░░░░]", info.ProgressBar)
	require.Equal(t, 38, info.Percentage)
	require.Equal(t, "claude-sonnet-4", info.Model)
	require.Contains(t, info.ExtrasLine, "147 skills")
	require.Contains(t, info.ExtrasLine, "3 memory files")
	require.Empty(t, info.ActionTip)
}

func TestFormatCanonicalText(t *testing.T) {
	t.Parallel()
	d := events.ContextUsageData{
		TotalTokens: 76284,
		MaxTokens:   200000,
		Percentage:  38,
		Model:       "claude-sonnet-4",
		Categories: []events.ContextCategory{
			{Name: "System Prompt", Tokens: 603},
			{Name: "Messages", Tokens: 55228},
		},
		MemoryFiles: 3,
		MCPTools:    5,
		Skills:      events.ContextSkillInfo{Total: 8},
	}

	result := FormatCanonicalText(d)
	require.Contains(t, result, "🟢 Context: 38% — Comfortable")
	require.Contains(t, result, "[███░░░░░░░] ~76.3K / 200K")
	require.Contains(t, result, "Model: claude-sonnet-4")
	require.Contains(t, result, "Top: Messages: ~55.2K, System Prompt: 603")
	require.Contains(t, result, "⚡ 8 skills · 3 memory files · 5 MCP tools")
}

func TestFormatCanonicalTextCritical(t *testing.T) {
	t.Parallel()
	d := events.ContextUsageData{
		TotalTokens: 185000,
		MaxTokens:   200000,
		Percentage:  93,
		Model:       "claude-opus-4",
	}

	result := FormatCanonicalText(d)
	require.Contains(t, result, "🔴 Context: 93% — Critical")
	require.Contains(t, result, "/compact")
	require.Contains(t, result, "/reset")
}

func TestFormatCanonicalTextMinimal(t *testing.T) {
	t.Parallel()
	d := events.ContextUsageData{
		TotalTokens: 500,
		MaxTokens:   200000,
		Percentage:  0,
	}

	result := FormatCanonicalText(d)
	require.Contains(t, result, "🟢 Context: 0% — Comfortable")
	require.Contains(t, result, "[░░░░░░░░░░] 500 / 200K")
	require.NotContains(t, result, "Model:")
	require.NotContains(t, result, "Top:")
	require.NotContains(t, result, "⚡")
}
