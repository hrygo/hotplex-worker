package messaging

import (
	"fmt"
	"strings"

	"github.com/hrygo/hotplex/pkg/events"
)

// TurnSummaryData holds per-turn summary fields extracted from a Done envelope.
type TurnSummaryData struct {
	ContextPct     float64
	ContextWindow  int64
	TotalInputTok  int64
	ModelName      string
	ToolCallCount  int
	ToolNames      map[string]int
	TurnDurationMs int64
	TurnCount      int
	TurnInputTok   int64
	TurnOutputTok  int64
	TurnCostUSD    float64
	TotalCostUSD   float64
}

// ExtractTurnSummary extracts TurnSummaryData from a Done envelope.
// Handles events.Clone JSON round-trip where Event.Data becomes map[string]any.
func ExtractTurnSummary(env *events.Envelope) TurnSummaryData {
	var dataMap map[string]any
	switch v := env.Event.Data.(type) {
	case events.DoneData:
		dataMap = v.Stats
	case map[string]any:
		if s, ok := v["stats"].(map[string]any); ok {
			dataMap = s
		}
	}

	if dataMap == nil {
		return TurnSummaryData{}
	}

	ss, ok := dataMap["_session"]
	if !ok {
		return TurnSummaryData{}
	}
	m, ok := ss.(map[string]any)
	if !ok {
		return TurnSummaryData{}
	}

	return TurnSummaryData{
		ContextPct:     toFloat64Msg(m["context_pct"]),
		ContextWindow:  toInt64Msg(m["context_window"]),
		TotalInputTok:  toInt64Msg(m["total_input_tok"]),
		ModelName:      toString(m["model_name"]),
		ToolCallCount:  int(toInt64Msg(m["tool_call_count"])),
		ToolNames:      toToolNames(m["tool_names"]),
		TurnDurationMs: toInt64Msg(m["turn_duration_ms"]),
		TurnCount:      int(toInt64Msg(m["turn_count"])),
		TurnInputTok:   toInt64Msg(m["turn_input_tok"]),
		TurnOutputTok:  toInt64Msg(m["turn_output_tok"]),
		TurnCostUSD:    toFloat64Msg(m["turn_cost_usd"]),
		TotalCostUSD:   toFloat64Msg(m["total_cost_usd"]),
	}
}

// FormatTurnSummary produces a single-line turn summary for messaging platforms.
// Returns empty string if no meaningful data is available.
func FormatTurnSummary(d TurnSummaryData) string {
	var parts []string

	// Context segment
	if d.ContextWindow > 0 && d.TotalInputTok > 0 {
		pct := int(d.ContextPct + 0.5)
		if pct > 100 {
			pct = 100
		}
		severity := SeverityLevel(pct)
		icon := SeverityIcon(severity)
		used := FormatTokenCount(int(d.TotalInputTok))
		max := FormatTokenCount(int(d.ContextWindow))
		parts = append(parts, fmt.Sprintf("%s Context %d%% (%s/%s)", icon, pct, used, max))
	} else if d.TotalInputTok > 0 {
		used := FormatTokenCount(int(d.TotalInputTok))
		parts = append(parts, fmt.Sprintf("%s Context %s tokens", SeverityIcon(SeverityComfortable), used))
	}

	// Model segment
	if d.ModelName != "" {
		parts = append(parts, d.ModelName)
	}

	// Tools segment
	if d.ToolCallCount > 0 {
		parts = append(parts, fmt.Sprintf("\U0001f6e0 %d tools", d.ToolCallCount))
	}

	// Duration segment
	if d.TurnDurationMs > 0 {
		parts = append(parts, "⏱ "+formatDuration(d.TurnDurationMs))
	}

	// Cost segment (skip if < $0.01)
	if d.TurnCostUSD >= 0.01 {
		parts = append(parts, formatCost(d.TurnCostUSD))
	}

	return strings.Join(parts, " | ")
}

func formatDuration(ms int64) string {
	switch {
	case ms < 1000:
		return fmt.Sprintf("%dms", ms)
	case ms < 60_000:
		return fmt.Sprintf("%ds", ms/1000)
	case ms < 3_600_000:
		return fmt.Sprintf("%dm%ds", ms/60_000, (ms%60_000)/1000)
	default:
		return fmt.Sprintf("%dh%dm", ms/3_600_000, (ms%3_600_000)/60_000)
	}
}

func formatCost(usd float64) string {
	if usd < 1 {
		return fmt.Sprintf("$%.2f", usd)
	}
	return fmt.Sprintf("$%.2f", usd)
}

func toInt64Msg(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	default:
		return 0
	}
}

func toFloat64Msg(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

func toString(v any) string {
	s, _ := v.(string)
	return s
}

func toToolNames(v any) map[string]int {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	result := make(map[string]int, len(m))
	for k, val := range m {
		result[k] = int(toInt64Msg(val))
	}
	return result
}
