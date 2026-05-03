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

		require.Equal(t, int64(15234), acc.TotalInput)
		require.Equal(t, int64(15234), acc.ContextFill)
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
		require.Equal(t, int64(8400+2000+500), acc.ContextFill)
		require.Equal(t, int64(3634), acc.TotalOutput)
		require.InDelta(t, 0.0234, acc.TotalCostUSD, 0.0001)
	})

	t.Run("nil stats", func(t *testing.T) {
		acc := &sessionAccumulator{StartedAt: time.Now()}
		acc.mergePerTurnStats(events.DoneData{})
		require.Equal(t, int64(0), acc.TotalInput)
		require.Equal(t, int64(0), acc.TotalOutput)
	})

	t.Run("cache tokens not double-counted", func(t *testing.T) {
		acc := &sessionAccumulator{StartedAt: time.Now()}
		acc.mergePerTurnStats(events.DoneData{
			Stats: map[string]any{
				"usage": map[string]any{
					"input_tokens":                float64(50000),
					"cache_creation_input_tokens": float64(30000),
					"cache_read_input_tokens":     float64(10000),
					"output_tokens":               float64(5000),
				},
				"model_usage": map[string]any{
					"claude-sonnet-4-6": map[string]any{"contextWindow": float64(200000)},
				},
			},
		})
		require.Equal(t, int64(50000), acc.ContextFill, "ContextFill must equal input_tokens only")
		require.Equal(t, int64(50000), acc.TotalInput, "TotalInput must not add cache tokens")
		pct := acc.computeContextPct()
		require.Equal(t, 25.0, pct, "context % must be 50000/200000 = 25%%, not inflated by cache")
	})

	t.Run("context fill overwritten by latest turn", func(t *testing.T) {
		acc := &sessionAccumulator{StartedAt: time.Now()}

		// Turn 1
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
		require.Equal(t, int64(10000), acc.ContextFill)
		require.Equal(t, int64(10000), acc.TotalInput)

		// Turn 2: smaller input — ContextFill reflects latest, TotalInput accumulates
		acc.mergePerTurnStats(events.DoneData{
			Stats: map[string]any{
				"usage": map[string]any{
					"input_tokens":  float64(5000),
					"output_tokens": float64(1000),
				},
				"total_cost_usd": 0.03,
			},
		})

		require.Equal(t, int64(5000), acc.ContextFill) // latest turn only
		require.Equal(t, int64(15000), acc.TotalInput) // cumulative
		require.Equal(t, int64(3000), acc.TotalOutput)
		require.Equal(t, int64(200000), acc.ContextWindow)
		require.Equal(t, "Opus", acc.ModelName)
		require.InDelta(t, 0.08, acc.TotalCostUSD, 0.001)
	})
}

func TestSessionAccumulator_ComputeContextPct(t *testing.T) {
	tests := []struct {
		name          string
		contextFill   int64
		contextWindow int64
		wantPct       float64
	}{
		{"zero usage", 0, 200000, 0},
		{"50% usage", 100000, 200000, 50},
		{"with exact", 48000, 200000, 24},
		{"no window", 50000, 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			acc := &sessionAccumulator{
				ContextFill:   tt.contextFill,
				ContextWindow: tt.contextWindow,
			}
			got := acc.computeContextPct()
			require.Equal(t, tt.wantPct, got)
		})
	}
	t.Parallel()
}

func TestSessionAccumulator_MergeContextUsage(t *testing.T) {
	t.Parallel()

	t.Run("precise data overrides aggregated", func(t *testing.T) {
		acc := &sessionAccumulator{StartedAt: time.Now()}
		// First: simulated aggregated Done data (inflated)
		acc.ContextFill = 260000
		acc.ContextWindow = 200000

		// Then: precise control channel data overrides it
		acc.mergeContextUsage(&events.ContextUsageData{
			TotalTokens: 58000,
			MaxTokens:   200000,
			Percentage:  29,
		})

		require.Equal(t, int64(58000), acc.ContextFill)
		require.Equal(t, int64(200000), acc.ContextWindow)
		require.InDelta(t, 29.0, acc.computeContextPct(), 0.1)
	})

	t.Run("nil data ignored", func(t *testing.T) {
		acc := &sessionAccumulator{
			ContextFill:   100000,
			ContextWindow: 200000,
			StartedAt:     time.Now(),
		}
		acc.mergeContextUsage(nil)
		require.Equal(t, int64(100000), acc.ContextFill, "nil ContextUsageData must not change accumulator")
	})

	t.Run("zero max tokens ignored", func(t *testing.T) {
		acc := &sessionAccumulator{
			ContextFill:   100000,
			ContextWindow: 200000,
			StartedAt:     time.Now(),
		}
		acc.mergeContextUsage(&events.ContextUsageData{TotalTokens: 50000, MaxTokens: 0})
		require.Equal(t, int64(100000), acc.ContextFill, "zero MaxTokens must not change accumulator")
	})
}

func TestSessionAccumulator_Snapshot_Precise(t *testing.T) {
	t.Parallel()
	acc := &sessionAccumulator{
		ContextFill:   58000,
		ContextWindow: 200000,
		StartedAt:     time.Now(),
	}
	snap := acc.snapshot()
	require.Equal(t, int64(58000), snap["context_fill"])
	require.InDelta(t, 29.0, snap["context_pct"], 0.1)
}

func TestSessionAccumulator_Snapshot(t *testing.T) {
	acc := &sessionAccumulator{
		TurnCount:     3,
		ToolCallCount: 12,
		TotalInput:    48434,
		TotalOutput:   7821,
		ContextFill:   48434,
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
	require.Equal(t, int64(48434), snap["context_fill"])
	require.InDelta(t, 0.084, snap["total_cost_usd"], 0.001)
	require.Equal(t, "Sonnet", snap["model_name"])

	ctxPct, ok := snap["context_pct"].(float64)
	require.True(t, ok)
	require.InDelta(t, 24.2, ctxPct, 0.1)
}

func TestShortModelName(t *testing.T) {
	require.Equal(t, "Sonnet", shortModelName("claude-sonnet-4-6"))
	require.Equal(t, "Opus", shortModelName("claude-opus-4-6"))
	require.Equal(t, "Haiku", shortModelName("claude-haiku-4-5"))
	require.Equal(t, "gpt-4o", shortModelName("gpt-4o"))
}

// extractSessionStats is a test-only helper that extracts the _session map from a Done envelope.
func extractSessionStats(env *events.Envelope) map[string]any {
	dd, ok := asDoneData(env.Event.Data)
	if !ok {
		return nil
	}
	if dd.Stats == nil {
		return nil
	}
	ss, ok := dd.Stats["_session"]
	if !ok {
		return nil
	}
	m, ok := ss.(map[string]any)
	if !ok {
		return nil
	}
	return m
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

	t.Run("cloned envelope map string any", func(t *testing.T) {
		env := &events.Envelope{
			Event: events.Event{
				Type: events.Done,
				Data: map[string]any{
					"success": true,
					"stats": map[string]any{
						"_session": map[string]any{
							"turn_count": 3,
						},
					},
				},
			},
		}
		ss := extractSessionStats(env)
		require.NotNil(t, ss)
		require.Equal(t, float64(3), ss["turn_count"])
	})
}

func TestAsDoneData(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		data   any
		wantOK bool
		wantDD events.DoneData
	}{
		{
			name:   "typed struct",
			data:   events.DoneData{Success: true, Stats: map[string]any{"total_cost_usd": float64(0.042)}},
			wantOK: true,
			wantDD: events.DoneData{Success: true, Stats: map[string]any{"total_cost_usd": float64(0.042)}},
		},
		{
			name: "map from Clone",
			data: map[string]any{
				"success": true,
				"stats":   map[string]any{"total_cost_usd": float64(0.042)},
			},
			wantOK: true,
			wantDD: events.DoneData{Success: true, Stats: map[string]any{"total_cost_usd": float64(0.042)}},
		},
		{
			name:   "nil",
			data:   nil,
			wantOK: false,
		},
		{
			name:   "wrong type",
			data:   "not done data",
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dd, ok := asDoneData(tt.data)
			require.Equal(t, tt.wantOK, ok)
			if ok {
				require.Equal(t, tt.wantDD.Success, dd.Success)
			}
		})
	}
}

func TestAsToolCallData(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		data   any
		wantOK bool
		want   events.ToolCallData
	}{
		{
			name:   "typed struct",
			data:   events.ToolCallData{ID: "tc1", Name: "Read", Input: map[string]any{"path": "/tmp/x"}},
			wantOK: true,
			want:   events.ToolCallData{ID: "tc1", Name: "Read", Input: map[string]any{"path": "/tmp/x"}},
		},
		{
			name: "map from Clone",
			data: map[string]any{
				"id":    "tc2",
				"name":  "Bash",
				"input": map[string]any{"cmd": "ls"},
			},
			wantOK: true,
			want:   events.ToolCallData{ID: "tc2", Name: "Bash", Input: map[string]any{"cmd": "ls"}},
		},
		{
			name:   "nil",
			data:   nil,
			wantOK: false,
		},
		{
			name:   "wrong type",
			data:   42,
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tc, ok := asToolCallData(tt.data)
			require.Equal(t, tt.wantOK, ok)
			if ok {
				require.Equal(t, tt.want.ID, tc.ID)
				require.Equal(t, tt.want.Name, tc.Name)
			}
		})
	}
}
