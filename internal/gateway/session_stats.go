package gateway

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/hrygo/hotplex/pkg/events"
)

// sessionAccumulator tracks session-level statistics across turns.
// One instance per session, stored in Bridge.accum.
type sessionAccumulator struct {
	TurnCount     int
	ToolCallCount int
	TotalCostUSD  float64
	TotalInput    int64 // input_tokens + cache_creation + cache_read
	TotalOutput   int64
	ContextWindow int64  // from modelUsage.contextWindow (0 = unknown)
	ModelName     string // first model seen
	StartedAt     time.Time

	// Per-turn tracking (reset after each done).
	ToolNames     map[string]int // tool name -> call count this turn
	PerTurnInput  int64
	PerTurnOutput int64
	PerTurnCost   float64

	// Cumulative totals at the end of the previous turn (for delta computation).
	PrevTotalIn   int64
	PrevTotalOut  int64
	PrevTotalCost float64
}

// mergePerTurnStats extracts standard fields from different Worker formats
// and accumulates them into the session totals.
func (a *sessionAccumulator) mergePerTurnStats(data events.DoneData) {
	if data.Stats == nil {
		return
	}

	// Claude Code format: usage.input_tokens + cache tokens
	if usage, ok := data.Stats["usage"].(map[string]any); ok {
		a.TotalInput += toInt64(usage["input_tokens"]) +
			toInt64(usage["cache_creation_input_tokens"]) +
			toInt64(usage["cache_read_input_tokens"])
		a.TotalOutput += toInt64(usage["output_tokens"])
	}

	// Claude Code modelUsage: extract model name + contextWindow
	if modelUsage, ok := data.Stats["model_usage"].(map[string]any); ok {
		for modelName, v := range modelUsage {
			mu, ok := v.(map[string]any)
			if !ok {
				continue
			}
			if a.ModelName == "" {
				a.ModelName = shortModelName(modelName)
			}
			if cw := toInt64(mu["contextWindow"]); cw > 0 {
				a.ContextWindow = cw
			}
		}
	}

	// OpenCode format: tokens.input + cache tokens
	if tokens, ok := data.Stats["tokens"].(map[string]any); ok {
		a.TotalInput += toInt64(tokens["input"]) +
			toInt64(tokens["cache_read"]) +
			toInt64(tokens["cache_write"])
		a.TotalOutput += toInt64(tokens["output"])
	}

	// Cost: Claude Code uses "total_cost_usd", OpenCode uses "cost"
	a.TotalCostUSD += toFloat64(data.Stats["total_cost_usd"])
	a.TotalCostUSD += toFloat64(data.Stats["cost"])
}

// Workers report cumulative token totals, not per-turn values. computePerTurnDeltas
// derives the delta for the current turn by subtracting the previous baseline.
func (a *sessionAccumulator) computePerTurnDeltas() {
	a.PerTurnInput = a.TotalInput - a.PrevTotalIn
	a.PerTurnOutput = a.TotalOutput - a.PrevTotalOut
	a.PerTurnCost = a.TotalCostUSD - a.PrevTotalCost
	if a.PerTurnInput < 0 {
		a.PerTurnInput = 0
	}
	if a.PerTurnOutput < 0 {
		a.PerTurnOutput = 0
	}
	if a.PerTurnCost < 0 {
		a.PerTurnCost = 0
	}
}

// resetPerTurn must be called after computePerTurnDeltas and the record is written.
func (a *sessionAccumulator) resetPerTurn() {
	a.PrevTotalIn = a.TotalInput
	a.PrevTotalOut = a.TotalOutput
	a.PrevTotalCost = a.TotalCostUSD
	a.ToolNames = nil
	a.ToolCallCount = 0
	a.PerTurnInput = 0
	a.PerTurnOutput = 0
	a.PerTurnCost = 0
}

// computeContextPct calculates context window usage percentage.
// Aligns with Claude Code formula: (input + cache_creation + cache_read) / contextWindow * 100.
func (a *sessionAccumulator) computeContextPct() float64 {
	if a.ContextWindow <= 0 {
		return 0
	}
	pct := float64(a.TotalInput) / float64(a.ContextWindow) * 100
	if pct > 100 {
		pct = 100
	}
	if pct < 0 {
		pct = 0
	}
	return pct
}

// snapshot returns the current accumulator state as a map for injection into DoneData.Stats["_session"].
func (a *sessionAccumulator) snapshot() map[string]any {
	ctxPct := a.computeContextPct()
	return map[string]any{
		"turn_count":       a.TurnCount,
		"tool_call_count":  a.ToolCallCount,
		"duration":         time.Since(a.StartedAt).Round(time.Second).String(),
		"duration_seconds": time.Since(a.StartedAt).Seconds(),
		"total_input_tok":  a.TotalInput,
		"total_output_tok": a.TotalOutput,
		"context_window":   a.ContextWindow,
		"context_pct":      ctxPct,
		"total_cost_usd":   a.TotalCostUSD,
		"model_name":       a.ModelName,
	}
}

// extractSessionStats extracts the _session map from a Done envelope.
func extractSessionStats(env *events.Envelope) map[string]any {
	dd, ok := env.Event.Data.(events.DoneData)
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

// toInt64 converts any numeric value to int64.
func toInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	case json.Number:
		i, _ := n.Int64()
		return i
	default:
		return 0
	}
}

// toFloat64 converts any numeric value to float64.
func toFloat64(v any) float64 {
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

// shortModelName returns a human-readable model name.
func shortModelName(full string) string {
	switch {
	case strings.Contains(full, "opus"):
		return "Opus"
	case strings.Contains(full, "sonnet"):
		return "Sonnet"
	case strings.Contains(full, "haiku"):
		return "Haiku"
	default:
		return full
	}
}

// formatTokenCount formats a token count with K/M suffixes.
func formatTokenCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
