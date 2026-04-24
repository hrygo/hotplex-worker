package gateway

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

func TestSessionAccumulator_MergePerTurnStats(t *testing.T) {
	t.Run("claude code format", func(t *testing.T) {
		acc := &sessionAccumulator{StartedAt: time.Now()}
		acc.mergePerTurnStats(events.DoneData{
			Stats: map[string]any{
				"usage": map[string]any{
					"input_tokens":                float64(15234),
					"cache_creation_input_tokens": float64(8200),
					"cache_read_input_tokens":     float64(0),
					"output_tokens":               float64(3821),
				},
				"model_usage": map[string]any{
					"claude-sonnet-4-6": map[string]any{
						"contextWindow": float64(200000),
						"costUSD":       float64(0.042),
					},
				},
				"total_cost_usd": 0.042,
			},
		})

		require.Equal(t, int64(15234+8200+0), acc.TotalInput)
		require.Equal(t, int64(3821), acc.TotalOutput)
		require.Equal(t, int64(200000), acc.ContextWindow)
		require.Equal(t, "Sonnet", acc.ModelName)
		require.InDelta(t, 0.042, acc.TotalCostUSD, 0.001)
	})

	t.Run("opencode format", func(t *testing.T) {
		acc := &sessionAccumulator{StartedAt: time.Now()}
		acc.mergePerTurnStats(events.DoneData{
			Stats: map[string]any{
				"tokens": map[string]any{
					"input":       float64(8400),
					"output":      float64(3634),
					"cache_read":  float64(2000),
					"cache_write": float64(500),
				},
				"cost": 0.0234,
			},
		})

		require.Equal(t, int64(8400+2000+500), acc.TotalInput)
		require.Equal(t, int64(3634), acc.TotalOutput)
		require.InDelta(t, 0.0234, acc.TotalCostUSD, 0.0001)
	})

	t.Run("nil stats", func(t *testing.T) {
		acc := &sessionAccumulator{StartedAt: time.Now()}
		acc.mergePerTurnStats(events.DoneData{})
		require.Equal(t, int64(0), acc.TotalInput)
		require.Equal(t, int64(0), acc.TotalOutput)
	})

	t.Run("accumulates across multiple turns", func(t *testing.T) {
		acc := &sessionAccumulator{StartedAt: time.Now()}

		// Turn 1: Claude Code
		acc.mergePerTurnStats(events.DoneData{
			Stats: map[string]any{
				"usage": map[string]any{
					"input_tokens":  float64(10000),
					"output_tokens": float64(2000),
				},
				"model_usage": map[string]any{
					"claude-opus-4-6": map[string]any{
						"contextWindow": float64(200000),
					},
				},
				"total_cost_usd": 0.05,
			},
		})

		// Turn 2: Claude Code
		acc.mergePerTurnStats(events.DoneData{
			Stats: map[string]any{
				"usage": map[string]any{
					"input_tokens":  float64(5000),
					"output_tokens": float64(1000),
				},
				"total_cost_usd": 0.03,
			},
		})

		require.Equal(t, int64(15000), acc.TotalInput)
		require.Equal(t, int64(3000), acc.TotalOutput)
		require.Equal(t, int64(200000), acc.ContextWindow)
		require.Equal(t, "Opus", acc.ModelName)
		require.InDelta(t, 0.08, acc.TotalCostUSD, 0.001)
	})
}

func TestSessionAccumulator_ComputeContextPct(t *testing.T) {
	tests := []struct {
		name          string
		totalInput    int64
		contextWindow int64
		wantPct       float64
	}{
		{"zero usage", 0, 200000, 0},
		{"50% usage", 100000, 200000, 50},
		{"with exact", 48000, 200000, 24},
		{"over 100 clamped", 300000, 200000, 100}, // 150% unclamped, clamped to 100
		{"no window", 50000, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acc := &sessionAccumulator{
				TotalInput:    tt.totalInput,
				ContextWindow: tt.contextWindow,
			}
			got := acc.computeContextPct()
			require.Equal(t, tt.wantPct, got)
		})
	}

	t.Run("over 100% clamped", func(t *testing.T) {
		acc := &sessionAccumulator{
			TotalInput:    250000,
			ContextWindow: 200000,
		}
		require.Equal(t, float64(100), acc.computeContextPct())
	})
}

func TestSessionAccumulator_Snapshot(t *testing.T) {
	acc := &sessionAccumulator{
		TurnCount:     3,
		ToolCallCount: 12,
		TotalInput:    48434,
		TotalOutput:   7821,
		ContextWindow: 200000,
		TotalCostUSD:  0.084,
		ModelName:     "Sonnet",
		StartedAt:     time.Now().Add(-222 * time.Second),
	}

	snap := acc.snapshot()
	require.Equal(t, 3, snap["turn_count"])
	require.Equal(t, 12, snap["tool_call_count"])
	require.Equal(t, int64(48434), snap["total_input_tok"])
	require.Equal(t, int64(7821), snap["total_output_tok"])
	require.Equal(t, int64(200000), snap["context_window"])
	require.InDelta(t, 0.084, snap["total_cost_usd"], 0.001)
	require.Equal(t, "Sonnet", snap["model_name"])

	ctxPct, ok := snap["context_pct"].(float64)
	require.True(t, ok)
	require.InDelta(t, 24.2, ctxPct, 0.1)
}

func TestToInt64(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  int64
	}{
		{"float64", float64(12345), 12345},
		{"int", 12345, 12345},
		{"int64", int64(12345), 12345},
		{"nil", nil, 0},
		{"string", "abc", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, toInt64(tt.input))
		})
	}
}

func TestToFloat64(t *testing.T) {
	require.Equal(t, float64(42), toFloat64(float64(42)))
	require.Equal(t, float64(42), toFloat64(42))
	require.Equal(t, float64(42), toFloat64(int64(42)))
	require.Equal(t, float64(0), toFloat64("x"))
}

func TestShortModelName(t *testing.T) {
	require.Equal(t, "Sonnet", shortModelName("claude-sonnet-4-6"))
	require.Equal(t, "Opus", shortModelName("claude-opus-4-6"))
	require.Equal(t, "Haiku", shortModelName("claude-haiku-4-5"))
	require.Equal(t, "gpt-4o", shortModelName("gpt-4o"))
}

func TestFormatTokenCount(t *testing.T) {
	require.Equal(t, "500", formatTokenCount(500))
	require.Equal(t, "1.5K", formatTokenCount(1500))
	require.Equal(t, "45.2K", formatTokenCount(45200))
	require.Equal(t, "1.2M", formatTokenCount(1200000))
}

func TestExtractSessionStats(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		env := &events.Envelope{
			Event: events.Event{
				Type: events.Done,
				Data: events.DoneData{
					Stats: map[string]any{
						"_session": map[string]any{
							"turn_count": 5,
						},
					},
				},
			},
		}
		ss := extractSessionStats(env)
		require.NotNil(t, ss)
		require.Equal(t, 5, ss["turn_count"])
	})

	t.Run("missing _session key", func(t *testing.T) {
		env := &events.Envelope{
			Event: events.Event{
				Type: events.Done,
				Data: events.DoneData{Stats: map[string]any{}},
			},
		}
		require.Nil(t, extractSessionStats(env))
	})

	t.Run("not DoneData", func(t *testing.T) {
		env := &events.Envelope{
			Event: events.Event{
				Type: events.Message,
				Data: events.MessageData{},
			},
		}
		require.Nil(t, extractSessionStats(env))
	})
}
