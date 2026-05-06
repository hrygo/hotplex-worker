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

// TurnSummaryField is a labeled key-value pair for turn summary display.
type TurnSummaryField struct {
	Label string
	Value string
}

// Fields returns the ordered list of non-empty fields for turn summary display.
// Used by all rendering paths (compact text, rich text, table) to ensure consistency.
func (d TurnSummaryData) Fields() []TurnSummaryField {
	var fields []TurnSummaryField

	if d.TurnCount > 0 {
		fields = append(fields, TurnSummaryField{"🔄 Turn", fmt.Sprintf("#%d", d.TurnCount)})
	}
	if d.ModelName != "" {
		fields = append(fields, TurnSummaryField{"🤖 Model", d.ModelName})
	}
	if d.ContextWindow > 0 && d.ContextFill > 0 {
		used := FormatTokenCount(int(d.ContextFill))
		max := FormatTokenCount(int(d.ContextWindow))
		fields = append(fields, TurnSummaryField{"🧠 Context", fmt.Sprintf("%d%% · %s/%s", clampContextPct(d.ContextPct), used, max)})
	}
	if d.ToolCallCount > 0 {
		fields = append(fields, TurnSummaryField{"🔧 Tools", FormatToolNames(d.ToolNames, d.ToolCallCount)})
	}
	if d.WorkDir != "" {
		fields = append(fields, TurnSummaryField{"📂 Dir", TruncatePath(d.WorkDir, 3)})
	}
	if d.GitBranch != "" {
		fields = append(fields, TurnSummaryField{"🌿 Branch", d.GitBranch})
	}
	if durStr := FormatDurationParts(d); durStr != "" {
		fields = append(fields, TurnSummaryField{"⏱ Timer", durStr})
	}
	if tokStr := FormatTokenUsage(d); tokStr != "" {
		fields = append(fields, TurnSummaryField{"💎 Tokens", tokStr})
	}

	return fields
}

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

// clampContextPct rounds contextPct to the nearest integer and caps at 100.
func clampContextPct(contextPct float64) int {
	pct := int(contextPct + 0.5)
	if pct > 100 {
		return 100
	}
	return pct
}

// FormatTurnSummary produces a compact single-line turn summary.
// Format: "ModelName · N% · 🔧 N (Tool×M, ...) · ⏱ Timer Xs"
func FormatTurnSummary(d TurnSummaryData) string {
	var parts []string

	if d.ModelName != "" {
		parts = append(parts, d.ModelName)
	}

	if d.ContextWindow > 0 && d.ContextFill > 0 {
		parts = append(parts, fmt.Sprintf("%d%%", clampContextPct(d.ContextPct)))
	}

	if d.ToolCallCount > 0 {
		toolStr := FormatToolNames(d.ToolNames, d.ToolCallCount)
		parts = append(parts, "🔧 "+toolStr)
	}

	if d.TurnDurationMs > 0 {
		parts = append(parts, "⏱ Timer "+FormatDuration(d.TurnDurationMs))
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
		if len(p) >= 2 && p[1] == ':' && ((p[0] >= 'A' && p[0] <= 'Z') || (p[0] >= 'a' && p[0] <= 'z')) {
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
	fields := d.Fields()
	if len(fields) == 0 {
		return ""
	}
	lines := make([]string, len(fields))
	for i, f := range fields {
		lines[i] = f.Label + " " + f.Value
	}
	return strings.Join(lines, "\n")
}

// FormatDurationParts returns a formatted duration string combining turn and session durations.
// Returns empty string if no duration data is available.
func FormatDurationParts(d TurnSummaryData) string {
	var parts []string
	if d.TurnDurationMs > 0 {
		parts = append(parts, FormatDuration(d.TurnDurationMs))
	}
	if d.SessionDuration > 0 {
		parts = append(parts, "Σ "+FormatSessionDuration(d.SessionDuration))
	}
	return strings.Join(parts, " · ")
}

// FormatTokenUsage returns a formatted token usage string with turn and total breakdowns.
// Returns empty string if no token data is available.
func FormatTokenUsage(d TurnSummaryData) string {
	if d.TurnInputTok <= 0 && d.TurnOutputTok <= 0 && d.TotalInputTok <= 0 && d.TotalOutputTok <= 0 {
		return ""
	}
	var tokParts []string
	if s := tokenPair(d.TurnInputTok, d.TurnOutputTok, ""); s != "" {
		tokParts = append(tokParts, s)
	}
	if s := tokenPair(d.TotalInputTok, d.TotalOutputTok, "Σ"); s != "" {
		tokParts = append(tokParts, s)
	}
	return strings.Join(tokParts, " · ")
}

// tokenPair builds "X↓ · Y↑" (with optional prefix on each value) for a single input/output pair.
func tokenPair(inputTok, outputTok int64, prefix string) string {
	var parts []string
	if inputTok > 0 {
		parts = append(parts, prefix+FormatTokenCount(int(inputTok))+"↓")
	}
	if outputTok > 0 {
		parts = append(parts, prefix+FormatTokenCount(int(outputTok))+"↑")
	}
	return strings.Join(parts, " · ")
}
