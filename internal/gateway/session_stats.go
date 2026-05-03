package gateway

import (
	"encoding/json"
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
	TotalInput    int64 // cumulative input_tokens + cache_creation + cache_read
	TotalOutput   int64
	ContextWindow int64  // from modelUsage.contextWindow (0 = unknown)
	ContextFill   int64  // latest turn's input + cache_creation + cache_read (not cumulative)
	ModelName     string // first model seen
	StartedAt     time.Time
	WorkDir       string // session working directory
	GitBranch     string // current git branch (captured once at start)

	// Per-turn tracking (reset after each done).
	ToolNames      map[string]int // tool name -> call count this turn
	PerTurnInput   int64
	PerTurnOutput  int64
	PerTurnCost    float64
	TurnDurationMs int64 // current turn duration in milliseconds

	// Cumulative totals at the end of the previous turn (for delta computation).
	PrevTotalIn   int64
	PrevTotalOut  int64
	PrevTotalCost float64
}

// mergePerTurnStats handles both Claude Code and OpenCode worker stat formats.
func (a *sessionAccumulator) mergePerTurnStats(data events.DoneData) {
	if data.Stats == nil {
		return
	}

	// Claude Code format: usage.input_tokens + cache tokens
	if usage, ok := data.Stats["usage"].(map[string]any); ok {
		input := events.ToInt64(usage["input_tokens"]) +
			events.ToInt64(usage["cache_creation_input_tokens"]) +
			events.ToInt64(usage["cache_read_input_tokens"])
		a.TotalInput += input
		a.ContextFill = input
		a.TotalOutput += events.ToInt64(usage["output_tokens"])
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
			if cw := events.ToInt64(mu["contextWindow"]); cw > 0 {
				a.ContextWindow = cw
			}
		}
	}

	// OpenCode format: tokens.input + cache tokens
	if tokens, ok := data.Stats["tokens"].(map[string]any); ok {
		input := events.ToInt64(tokens["input"]) +
			events.ToInt64(tokens["cache_read"]) +
			events.ToInt64(tokens["cache_write"])
		a.TotalInput += input
		a.ContextFill = input
		a.TotalOutput += events.ToInt64(tokens["output"])
	}

	// Cost: Claude Code uses "total_cost_usd", OpenCode uses "cost"
	a.TotalCostUSD += events.ToFloat64(data.Stats["total_cost_usd"])
	a.TotalCostUSD += events.ToFloat64(data.Stats["cost"])
}

// Workers report cumulative totals, so deltas are derived by subtracting
// the previous baseline.
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
	a.TurnDurationMs = 0
}

// mergeContextUsage updates ContextFill and ContextWindow from precise worker control data.
// Called after get_context_usage returns; overrides the aggregated Done event values.
func (a *sessionAccumulator) mergeContextUsage(cu *events.ContextUsageData) {
	if cu == nil || cu.MaxTokens <= 0 {
		return
	}
	a.ContextFill = int64(cu.TotalTokens)
	a.ContextWindow = int64(cu.MaxTokens)
}

// computeContextPct returns context window usage percentage (0-100).
// Data comes from get_context_usage control channel (precise) or Done event usage (fallback).
func (a *sessionAccumulator) computeContextPct() float64 {
	if a.ContextWindow <= 0 || a.ContextFill <= 0 {
		return 0
	}
	return float64(a.ContextFill) / float64(a.ContextWindow) * 100
}

// snapshot returns the current accumulator state as a map for injection into DoneData.Stats["_session"].
func (a *sessionAccumulator) snapshot() map[string]any {
	ctxPct := a.computeContextPct()
	elapsed := time.Since(a.StartedAt)
	var toolNames map[string]int
	if len(a.ToolNames) > 0 {
		toolNames = make(map[string]int, len(a.ToolNames))
		for k, v := range a.ToolNames {
			toolNames[k] = v
		}
	}
	return map[string]any{
		"turn_count":       a.TurnCount,
		"tool_call_count":  a.ToolCallCount,
		"duration":         elapsed.Round(time.Second).String(),
		"duration_seconds": elapsed.Seconds(),
		"total_input_tok":  a.TotalInput,
		"total_output_tok": a.TotalOutput,
		"context_fill":     a.ContextFill,
		"context_window":   a.ContextWindow,
		"context_pct":      ctxPct,
		"total_cost_usd":   a.TotalCostUSD,
		"model_name":       a.ModelName,
		"turn_duration_ms": a.TurnDurationMs,
		"turn_input_tok":   a.PerTurnInput,
		"turn_output_tok":  a.PerTurnOutput,
		"turn_cost_usd":    a.PerTurnCost,
		"tool_names":       toolNames,
		"work_dir":         a.WorkDir,
		"git_branch":       a.GitBranch,
	}
}

// asDoneData extracts DoneData from Event.Data, handling both the original typed
// struct and the map[string]any produced by events.Clone JSON round-tripping.
func asDoneData(data any) (events.DoneData, bool) {
	switch v := data.(type) {
	case events.DoneData:
		return v, true
	case map[string]any:
		var dd events.DoneData
		raw, _ := json.Marshal(v)
		_ = json.Unmarshal(raw, &dd)
		return dd, true
	default:
		return events.DoneData{}, false
	}
}

// asToolCallData extracts ToolCallData from Event.Data, handling both the original
// typed struct and the map[string]any produced by events.Clone JSON round-tripping.
func asToolCallData(data any) (events.ToolCallData, bool) {
	switch v := data.(type) {
	case events.ToolCallData:
		return v, true
	case map[string]any:
		var tc events.ToolCallData
		raw, _ := json.Marshal(v)
		_ = json.Unmarshal(raw, &tc)
		return tc, true
	default:
		return events.ToolCallData{}, false
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
