package messaging

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/hrygo/hotplex/pkg/events"
)

const TurnSummaryCooldown = 5 * time.Minute

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
		parts = append(parts, "⏱ "+FormatDuration(d.TurnDurationMs))
	}

	if d.ToolCallCount > 0 {
		toolStr := FormatToolNames(d.ToolNames, d.ToolCallCount)
		parts = append(parts, "🔧 "+toolStr)
	}

	return strings.Join(parts, " · ")
}

// FormatToolNames produces "N (Tool×M, ...)" sorted by count descending.
// Shows at most top 5 tools; remaining tools are summarized as "+N".
func FormatToolNames(names map[string]int, total int) string {
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

func FormatDuration(ms int64) string {
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
	return FormatDuration(int64(secs * 1000))
}

// TruncatePath shortens a file path for display, keeping the last n path components.
// Returns empty string if maxComponents <= 0. If the path is already short enough,
// it is returned unchanged. Uses "/" as separator for cross-platform display in
// messaging (Slack/Feishu). Preserves Windows drive letters (e.g. "C:").
// Replaces the user's home directory prefix with "~" before truncation so that
// paths under home are displayed relative to "~" instead of showing misleading
// truncated segments like "/.hotplex/workspace/hotplex".
func TruncatePath(p string, maxComponents int) string {
	if p == "" || maxComponents <= 0 {
		return ""
	}
	// Normalize to forward slashes for cross-platform display.
	// Do this before filepath.Clean so Windows backslashes are handled on Linux too.
	p = strings.ReplaceAll(p, "\\", "/")
	p = filepath.Clean(p)

	// Track home directory prefix for user-friendly display.
	prefix := ""

	// Replace home directory prefix with ~ so components are counted relative to home.
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		homeNorm := strings.ReplaceAll(home, "\\", "/")
		homeNorm = filepath.Clean(homeNorm)
		if p == homeNorm {
			return "~"
		}
		if strings.HasPrefix(p, homeNorm+"/") {
			p = p[len(homeNorm):] // e.g. "/.hotplex/workspace/hotplex"
			prefix = "~"
		}
	}

	// Detect and preserve Windows drive letter (e.g. "C:").
	if prefix == "" {
		if len(p) >= 2 && p[1] == ':' && (p[0] >= 'A' && p[0] <= 'Z' || p[0] >= 'a' && p[0] <= 'z') {
			prefix = p[:2]
			p = p[2:]
		}
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
		return prefix + "/" + strings.Join(parts, "/")
	}
	kept := parts[len(parts)-maxComponents:]
	return prefix + "/" + strings.Join(kept, "/")
}

// FormatTurnSummaryRich produces a multi-line turn summary with each metric on its own line.
// Used as the primary format for Feishu and rich fallback for Slack.
// Returns empty string if no meaningful data is available.
func FormatTurnSummaryRich(d TurnSummaryData) string {
	var lines []string

	// Line 1: Turn count
	if d.TurnCount > 0 {
		lines = append(lines, fmt.Sprintf("🔄 #%d", d.TurnCount))
	}

	// Line 2: Model
	if d.ModelName != "" {
		lines = append(lines, "🤖 "+d.ModelName)
	}

	// Line 2: Context window
	if d.ContextWindow > 0 && d.ContextFill > 0 {
		pct := int(d.ContextPct + 0.5)
		if pct > 100 {
			pct = 100
		}
		used := FormatTokenCount(int(d.ContextFill))
		max := FormatTokenCount(int(d.ContextWindow))
		lines = append(lines, fmt.Sprintf("🧠 %d%% · %s/%s", pct, used, max))
	}

	// Line 3: Git branch
	if d.GitBranch != "" {
		lines = append(lines, "🌿 "+d.GitBranch)
	}

	// Line 4: Work directory
	if d.WorkDir != "" {
		lines = append(lines, "📂 "+TruncatePath(d.WorkDir, 3))
	}

	// Line 5: Tools
	if d.ToolCallCount > 0 {
		lines = append(lines, "🔧 "+FormatToolNames(d.ToolNames, d.ToolCallCount))
	}

	// Line 6: Duration (merged Turn + Session)
	var durParts []string
	if d.TurnDurationMs > 0 {
		durParts = append(durParts, "Turn "+FormatDuration(d.TurnDurationMs))
	}
	if sessDur := FormatSessionDuration(d.SessionDuration); sessDur != "" {
		durParts = append(durParts, "Session "+sessDur)
	}
	if len(durParts) > 0 {
		lines = append(lines, "⏱️ "+strings.Join(durParts, " · "))
	}

	// Line 7: Tokens (turn + session total)
	if d.TurnInputTok > 0 || d.TurnOutputTok > 0 || d.TotalInputTok > 0 || d.TotalOutputTok > 0 {
		var tokParts []string
		if d.TurnInputTok > 0 || d.TurnOutputTok > 0 {
			var tp []string
			if d.TurnInputTok > 0 {
				tp = append(tp, fmt.Sprintf("%s in", FormatTokenCount(int(d.TurnInputTok))))
			}
			if d.TurnOutputTok > 0 {
				tp = append(tp, fmt.Sprintf("%s out", FormatTokenCount(int(d.TurnOutputTok))))
			}
			tokParts = append(tokParts, strings.Join(tp, " · "))
		}
		if d.TotalInputTok > 0 || d.TotalOutputTok > 0 {
			var tp []string
			if d.TotalInputTok > 0 {
				tp = append(tp, fmt.Sprintf("Σ %s in", FormatTokenCount(int(d.TotalInputTok))))
			}
			if d.TotalOutputTok > 0 {
				tp = append(tp, fmt.Sprintf("Σ %s out", FormatTokenCount(int(d.TotalOutputTok))))
			}
			tokParts = append(tokParts, strings.Join(tp, " · "))
		}
		lines = append(lines, "💎 "+strings.Join(tokParts, " | "))
	}

	return strings.Join(lines, "\n")
}
