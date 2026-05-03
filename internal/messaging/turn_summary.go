package messaging

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hrygo/hotplex/pkg/events"
)

// TurnSummaryData holds per-turn summary fields extracted from a Done envelope.
type TurnSummaryData struct {
	ContextPct      float64
	ContextFill     int64
	ContextWindow   int64
	TotalInputTok   int64
	TotalOutputTok  int64
	ModelName       string
	ToolCallCount   int
	ToolNames       map[string]int
	TurnDurationMs  int64
	TurnCount       int
	TurnInputTok    int64
	TurnOutputTok   int64
	SessionDuration float64 // seconds since session start
	WorkDir         string
	GitBranch       string
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

	var toolNames map[string]int
	if tn, ok := m["tool_names"].(map[string]any); ok {
		toolNames = make(map[string]int, len(tn))
		for k, v := range tn {
			if n, ok := v.(float64); ok {
				toolNames[k] = int(n)
			}
		}
	}

	return TurnSummaryData{
		ContextPct:      events.ToFloat64(m["context_pct"]),
		ContextFill:     events.ToInt64(m["context_fill"]),
		ContextWindow:   events.ToInt64(m["context_window"]),
		TotalInputTok:   events.ToInt64(m["total_input_tok"]),
		TotalOutputTok:  events.ToInt64(m["total_output_tok"]),
		ModelName:       events.StrVal(m["model_name"]),
		ToolCallCount:   int(events.ToInt64(m["tool_call_count"])),
		ToolNames:       toolNames,
		TurnDurationMs:  events.ToInt64(m["turn_duration_ms"]),
		TurnCount:       int(events.ToInt64(m["turn_count"])),
		TurnInputTok:    events.ToInt64(m["turn_input_tok"]),
		TurnOutputTok:   events.ToInt64(m["turn_output_tok"]),
		SessionDuration: events.ToFloat64(m["duration_seconds"]),
		WorkDir:         events.StrVal(m["work_dir"]),
		GitBranch:       events.StrVal(m["git_branch"]),
	}
}

// FormatTurnSummary produces a compact single-line turn summary.
// Format: "ModelName · N% · ⏱ Xs · 🔧 N (Tool×M, ...)"
// Returns empty string if no meaningful data is available.
func FormatTurnSummary(d TurnSummaryData) string {
	var parts []string

	if d.ModelName != "" {
		parts = append(parts, d.ModelName)
	}

	if d.ContextWindow > 0 && d.ContextFill > 0 {
		pct := int(d.ContextPct + 0.5)
		if pct > 100 {
			pct = 100
		}
		parts = append(parts, fmt.Sprintf("%d%%", pct))
	}

	if d.TurnDurationMs > 0 {
		parts = append(parts, "⏱ "+formatDuration(d.TurnDurationMs))
	}

	if d.ToolCallCount > 0 {
		toolStr := formatToolNames(d.ToolNames, d.ToolCallCount)
		parts = append(parts, "🔧 "+toolStr)
	}

	return strings.Join(parts, " · ")
}

// formatToolNames produces "N (Tool×M, ...)" sorted by count descending.
// Shows at most top 5 tools; remaining tools are summarized as "+N".
func formatToolNames(names map[string]int, total int) string {
	if len(names) == 0 {
		return fmt.Sprintf("%d", total)
	}
	type kv struct {
		name  string
		count int
	}
	sorted := make([]kv, 0, len(names))
	for k, v := range names {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].count != sorted[j].count {
			return sorted[i].count > sorted[j].count
		}
		return sorted[i].name < sorted[j].name
	})
	const topN = 5
	shown := sorted
	remaining := len(sorted) - topN
	if remaining > 0 {
		shown = sorted[:topN]
	}
	parts := make([]string, len(shown))
	for i, s := range shown {
		parts[i] = fmt.Sprintf("%s×%d", s.name, s.count)
	}
	result := fmt.Sprintf("%d (%s)", total, strings.Join(parts, ", "))
	if remaining > 0 {
		result += fmt.Sprintf(" +%d", remaining)
	}
	return result
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

// FormatSessionDuration formats session elapsed seconds to human-readable string.
func FormatSessionDuration(secs float64) string {
	if secs <= 0 {
		return ""
	}
	return formatDuration(int64(secs * 1000))
}

// TruncatePath shortens a file path for display, keeping the last n path components.
// Returns empty string if maxComponents <= 0. If the path is already short enough,
// it is returned unchanged. Uses "/" as separator for cross-platform display in
// messaging (Slack/Feishu). Preserves Windows drive letters (e.g. "C:").
func TruncatePath(p string, maxComponents int) string {
	if p == "" || maxComponents <= 0 {
		return ""
	}
	// Normalize to forward slashes for cross-platform display.
	// Do this before filepath.Clean so Windows backslashes are handled on Linux too.
	p = strings.ReplaceAll(p, "\\", "/")
	p = filepath.Clean(p)

	// Detect and preserve Windows drive letter (e.g. "C:").
	drive := ""
	if len(p) >= 2 && p[1] == ':' && (p[0] >= 'A' && p[0] <= 'Z' || p[0] >= 'a' && p[0] <= 'z') {
		drive = p[:2]
		p = p[2:]
	}

	// Split and filter empty parts.
	raw := strings.Split(strings.Trim(p, "/"), "/")
	parts := make([]string, 0, len(raw))
	for _, s := range raw {
		if s != "" {
			parts = append(parts, s)
		}
	}
	if len(parts) <= maxComponents {
		return drive + "/" + strings.Join(parts, "/")
	}
	kept := parts[len(parts)-maxComponents:]
	return drive + "/" + strings.Join(kept, "/")
}

// FormatTurnSummaryRich produces a multi-line turn summary with emoji-prefixed fields.
// Used as rich fallback when TableBlock is rejected.
// Returns empty string if no meaningful data is available.
func FormatTurnSummaryRich(d TurnSummaryData) string {
	var lines []string

	// Line 1: Core metrics
	var core []string
	if d.TurnCount > 0 {
		core = append(core, fmt.Sprintf("🔄 #%d", d.TurnCount))
	}
	if d.ModelName != "" {
		core = append(core, "🤖 "+d.ModelName)
	}
	if d.ContextWindow > 0 && d.ContextFill > 0 {
		pct := int(d.ContextPct + 0.5)
		if pct > 100 {
			pct = 100
		}
		core = append(core, fmt.Sprintf("🧠 %d%%", pct))
	}
	if d.TurnDurationMs > 0 {
		core = append(core, "⏱ "+formatDuration(d.TurnDurationMs))
	}
	if d.ToolCallCount > 0 {
		core = append(core, "🔧 "+formatToolNames(d.ToolNames, d.ToolCallCount))
	}
	if len(core) > 0 {
		lines = append(lines, strings.Join(core, " · "))
	}

	// Line 2: Token details
	if d.TurnInputTok > 0 || d.TurnOutputTok > 0 {
		var tokParts []string
		if d.TurnInputTok > 0 {
			tokParts = append(tokParts, fmt.Sprintf("in %s", FormatTokenCount(int(d.TurnInputTok))))
		}
		if d.TurnOutputTok > 0 {
			tokParts = append(tokParts, fmt.Sprintf("out %s", FormatTokenCount(int(d.TurnOutputTok))))
		}
		lines = append(lines, "💎 "+strings.Join(tokParts, " · "))
	}

	// Line 3: Environment
	var envParts []string
	if d.WorkDir != "" {
		envParts = append(envParts, "📂 "+TruncatePath(d.WorkDir, 3))
	}
	if d.GitBranch != "" {
		envParts = append(envParts, "🌿 "+d.GitBranch)
	}
	if sessDur := FormatSessionDuration(d.SessionDuration); sessDur != "" {
		envParts = append(envParts, "⏳ "+sessDur)
	}
	if len(envParts) > 0 {
		lines = append(lines, strings.Join(envParts, " · "))
	}

	return strings.Join(lines, "\n")
}
