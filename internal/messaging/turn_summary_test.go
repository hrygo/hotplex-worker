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
						"context_pct":       24.2,
						"context_fill":      float64(48434),
						"context_window":    float64(200000),
						"total_input_tok":   float64(48434),
						"total_output_tok":  float64(2000),
						"model_name":        "Sonnet",
						"tool_call_count":   float64(12),
						"tool_names":        map[string]any{"Read": float64(5), "Edit": float64(3), "Bash": float64(2), "Grep": float64(2)},
						"turn_duration_ms":  float64(42000),
						"turn_count":        float64(3),
						"turn_input_tok":    float64(12000),
						"turn_output_tok":   float64(2000),
						"duration_seconds":  float64(750),
					},
				},
			},
		},
	}
	d := ExtractTurnSummary(env)
	require.InDelta(t, 24.2, d.ContextPct, 0.01)
	require.Equal(t, int64(200000), d.ContextWindow)
	require.Equal(t, int64(48434), d.ContextFill)
	require.Equal(t, int64(48434), d.TotalInputTok)
	require.Equal(t, "Sonnet", d.ModelName)
	require.Equal(t, 12, d.ToolCallCount)
	require.Equal(t, map[string]int{"Read": 5, "Edit": 3, "Bash": 2, "Grep": 2}, d.ToolNames)
	require.Equal(t, int64(42000), d.TurnDurationMs)
	require.Equal(t, 3, d.TurnCount)
	require.Equal(t, int64(12000), d.TurnInputTok)
	require.Equal(t, int64(2000), d.TurnOutputTok)
	require.InDelta(t, 750.0, d.SessionDuration, 0.01)
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
		ContextFill:    48434,
		ContextWindow:  200000,
		ModelName:      "Sonnet",
		ToolCallCount:  12,
		ToolNames:      map[string]int{"Read": 5, "Edit": 3, "Bash": 2, "Grep": 2},
		TurnDurationMs: 42000,
	}
	got := FormatTurnSummary(d)
	require.Equal(t, "Sonnet · 24% · ⏱ 42s · 🔧 12 (Read×5, Edit×3, Bash×2, Grep×2)", got)
}

func TestFormatTurnSummary_ToolNamesEmpty(t *testing.T) {
	t.Parallel()
	d := TurnSummaryData{
		ModelName:      "Sonnet",
		ToolCallCount:  5,
		TurnDurationMs: 12000,
	}
	got := FormatTurnSummary(d)
	require.Equal(t, "Sonnet · ⏱ 12s · 🔧 5", got)
}

func TestFormatTurnSummary_NoModel(t *testing.T) {
	t.Parallel()
	d := TurnSummaryData{
		ContextPct:     50.0,
		ContextFill:    100000,
		ContextWindow:  200000,
		ToolCallCount:  3,
		TurnDurationMs: 8000,
	}
	got := FormatTurnSummary(d)
	require.Contains(t, got, "50%")
	require.NotContains(t, got, "Sonnet")
}

func TestFormatTurnSummary_NoTools(t *testing.T) {
	t.Parallel()
	d := TurnSummaryData{
		ContextPct:     24.0,
		ContextFill:    48000,
		ContextWindow:  200000,
		ModelName:      "Sonnet",
		TurnDurationMs: 5000,
	}
	got := FormatTurnSummary(d)
	require.NotContains(t, got, "🔧")
	require.Contains(t, got, "Sonnet · 24% · ⏱ 5s")
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

func TestExtractTurnSummary_CloneRoundTrip(t *testing.T) {
	t.Parallel()
	original := &events.Envelope{
		Event: events.Event{
			Type: events.Done,
			Data: events.DoneData{
				Stats: map[string]any{
					"_session": map[string]any{
						"context_pct":       50.0,
						"context_window":    float64(200000),
						"total_input_tok":   float64(100000),
						"total_output_tok":  float64(5000),
						"model_name":        "Opus",
						"tool_call_count":   float64(5),
						"tool_names":        map[string]any{"Bash": float64(3), "Read": float64(2)},
						"turn_duration_ms":  float64(8000),
						"duration_seconds":  float64(300),
					},
				},
			},
		},
	}
	cloned := events.Clone(original)
	d := ExtractTurnSummary(cloned)
	require.InDelta(t, 50.0, d.ContextPct, 0.01)
	require.Equal(t, int64(200000), d.ContextWindow)
	require.Equal(t, "Opus", d.ModelName)
	require.Equal(t, 5, d.ToolCallCount)
	require.Equal(t, map[string]int{"Bash": 3, "Read": 2}, d.ToolNames)
	require.Equal(t, int64(8000), d.TurnDurationMs)
	require.InDelta(t, 300.0, d.SessionDuration, 0.01)
}

func TestFormatSessionDuration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		secs float64
		want string
	}{
		{"zero", 0, ""},
		{"seconds", 5.3, "5s"},
		{"minutes", 150, "2m30s"},
		{"hours", 7500, "2h5m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := FormatSessionDuration(tt.secs)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestFormatToolNames_Sorted(t *testing.T) {
	t.Parallel()
	names := map[string]int{"Edit": 1, "Read": 5, "Bash": 3}
	got := formatToolNames(names, 9)
	require.Equal(t, "9 (Read×5, Bash×3, Edit×1)", got)
}

func TestTruncatePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		path          string
		maxComponents int
		want          string
	}{
		{"empty", "", 3, ""},
		{"short", "/home/user", 3, "/home/user"},
		{"exact", "/a/b/c", 3, "/a/b/c"},
		{"long", "/home/user/workspace/project", 3, "/user/workspace/project"},
		{"one component", "/home/user/workspace/project", 1, "/project"},
		{"zero max", "/home/user", 0, ""},
		{"windows absolute", "C:\\Users\\admin\\workspace\\project", 2, "C:/workspace/project"},
		{"windows short", "D:\\dev\\app", 5, "D:/dev/app"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := TruncatePath(tt.path, tt.maxComponents)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestFormatTurnSummaryRich_Full(t *testing.T) {
	t.Parallel()
	d := TurnSummaryData{
		TurnCount:       3,
		ModelName:       "Sonnet",
		ContextPct:      24.2,
		ContextFill:     48434,
		ContextWindow:   200000,
		TurnDurationMs:  42000,
		ToolCallCount:   12,
		ToolNames:       map[string]int{"Read": 5, "Edit": 3, "Bash": 2, "Grep": 2},
		TurnInputTok:    12000,
		TurnOutputTok:   2000,
		WorkDir:         "/home/user/workspace/hotplex",
		GitBranch:       "feat/117-turn-summary",
		SessionDuration: 750,
	}
	got := FormatTurnSummaryRich(d)
	require.Contains(t, got, "🔄 #3")
	require.Contains(t, got, "🤖 Sonnet")
	require.Contains(t, got, "🧠 24%")
	require.Contains(t, got, "⏱ 42s")
	require.Contains(t, got, "🔧 12 (")
	require.Contains(t, got, "💎")
	require.Contains(t, got, "📂")
	require.Contains(t, got, "🌿 feat/117-turn-summary")
	require.Contains(t, got, "⏳ 12m30s")
}

func TestFormatTurnSummaryRich_Minimal(t *testing.T) {
	t.Parallel()
	got := FormatTurnSummaryRich(TurnSummaryData{TurnDurationMs: 3000})
	require.Contains(t, got, "⏱ 3s")
}

func TestFormatTurnSummaryRich_Empty(t *testing.T) {
	t.Parallel()
	got := FormatTurnSummaryRich(TurnSummaryData{})
	require.Equal(t, "", got)
}
