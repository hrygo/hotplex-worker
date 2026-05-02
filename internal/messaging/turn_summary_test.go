package messaging

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

func TestExtractTurnSummary_Full(t *testing.T) {
	t.Parallel()
	env := &events.Envelope{
		Event: events.Event{
			Type: events.Done,
			Data: events.DoneData{
				Stats: map[string]any{
					"_session": map[string]any{
						"context_pct":     24.2,
						"context_window":  float64(200000),
						"total_input_tok": float64(48434),
						"model_name":      "Sonnet",
						"tool_call_count": float64(12),
						"tool_names": map[string]any{
							"Read": float64(5),
							"Bash": float64(3),
							"Edit": float64(2),
							"Grep": float64(2),
						},
						"turn_duration_ms": float64(42000),
						"turn_count":       float64(3),
						"turn_input_tok":   float64(12000),
						"turn_output_tok":  float64(2000),
						"turn_cost_usd":    0.04,
						"total_cost_usd":   0.12,
					},
				},
			},
		},
	}
	d := ExtractTurnSummary(env)
	require.InDelta(t, 24.2, d.ContextPct, 0.01)
	require.Equal(t, int64(200000), d.ContextWindow)
	require.Equal(t, int64(48434), d.TotalInputTok)
	require.Equal(t, "Sonnet", d.ModelName)
	require.Equal(t, 12, d.ToolCallCount)
	require.Equal(t, int64(42000), d.TurnDurationMs)
	require.Equal(t, 3, d.TurnCount)
	require.Equal(t, int64(12000), d.TurnInputTok)
	require.Equal(t, int64(2000), d.TurnOutputTok)
	require.InDelta(t, 0.04, d.TurnCostUSD, 0.001)
	require.InDelta(t, 0.12, d.TotalCostUSD, 0.001)
	require.Equal(t, map[string]int{"Read": 5, "Bash": 3, "Edit": 2, "Grep": 2}, d.ToolNames)
}

func TestExtractTurnSummary_NilSession(t *testing.T) {
	t.Parallel()
	env := &events.Envelope{
		Event: events.Event{
			Type: events.Done,
			Data: events.DoneData{
				Stats: map[string]any{},
			},
		},
	}
	d := ExtractTurnSummary(env)
	require.Equal(t, TurnSummaryData{}, d)
}

func TestExtractTurnSummary_NilStats(t *testing.T) {
	t.Parallel()
	env := &events.Envelope{
		Event: events.Event{
			Type: events.Done,
			Data: events.DoneData{},
		},
	}
	d := ExtractTurnSummary(env)
	require.Equal(t, TurnSummaryData{}, d)
}

func TestFormatTurnSummary_Full(t *testing.T) {
	t.Parallel()
	d := TurnSummaryData{
		ContextPct:     24.2,
		ContextWindow:  200000,
		TotalInputTok:  48434,
		ModelName:      "Sonnet",
		ToolCallCount:  12,
		TurnDurationMs: 42000,
		TurnCostUSD:    0.04,
	}
	got := FormatTurnSummary(d)
	require.Equal(t, "🟢 Context 24% (~48.4K/200K) | Sonnet | \U0001f6e0 12 tools | ⏱ 42s | $0.04", got)
}

func TestFormatTurnSummary_NoContext(t *testing.T) {
	t.Parallel()
	d := TurnSummaryData{
		ModelName:      "Sonnet",
		ToolCallCount:  5,
		TurnDurationMs: 12000,
	}
	got := FormatTurnSummary(d)
	require.Equal(t, "Sonnet | \U0001f6e0 5 tools | ⏱ 12s", got)
}

func TestFormatTurnSummary_NoModel(t *testing.T) {
	t.Parallel()
	d := TurnSummaryData{
		ContextPct:     50.0,
		ContextWindow:  200000,
		TotalInputTok:  100000,
		ToolCallCount:  3,
		TurnDurationMs: 8000,
	}
	got := FormatTurnSummary(d)
	require.Contains(t, got, "🟡 Context 50% (100K/200K)")
	require.NotContains(t, got, "Sonnet")
}

func TestFormatTurnSummary_NoTools(t *testing.T) {
	t.Parallel()
	d := TurnSummaryData{
		ContextPct:     24.0,
		ContextWindow:  200000,
		TotalInputTok:  48000,
		ModelName:      "Sonnet",
		TurnDurationMs: 5000,
	}
	got := FormatTurnSummary(d)
	require.NotContains(t, got, "tools")
	require.Contains(t, got, "🟢 Context 24% (48K/200K) | Sonnet | ⏱ 5s")
}

func TestFormatTurnSummary_Minimal(t *testing.T) {
	t.Parallel()
	d := TurnSummaryData{
		TurnDurationMs: 3000,
	}
	got := FormatTurnSummary(d)
	require.Equal(t, "⏱ 3s", got)
}

func TestFormatTurnSummary_Empty(t *testing.T) {
	t.Parallel()
	got := FormatTurnSummary(TurnSummaryData{})
	require.Equal(t, "", got)
}

func TestFormatTurnSummary_DurationFormats(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ms   int64
		want string
	}{
		{"milliseconds", 420, "420ms"},
		{"seconds", 42000, "42s"},
		{"minutes and seconds", 222000, "3m42s"},
		{"hours and minutes", 4980000, "1h23m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := formatDuration(tt.ms)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestFormatTurnSummary_CostThreshold(t *testing.T) {
	t.Parallel()
	t.Run("below threshold", func(t *testing.T) {
		t.Parallel()
		d := TurnSummaryData{
			TurnDurationMs: 1000,
			TurnCostUSD:    0.009,
		}
		got := FormatTurnSummary(d)
		require.NotContains(t, got, "$")
	})

	t.Run("at threshold", func(t *testing.T) {
		t.Parallel()
		d := TurnSummaryData{
			TurnDurationMs: 1000,
			TurnCostUSD:    0.01,
		}
		got := FormatTurnSummary(d)
		require.Contains(t, got, "$0.01")
	})
}
