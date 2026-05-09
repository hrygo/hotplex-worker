package feishu

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/hrygo/hotplex/pkg/events"
)

const toolStatusMaxRunes = 30

// toolEntry tracks a single tool call for the activity strip.
type toolEntry struct {
	id   string // matches ToolCallData.ID for result correlation
	text string // formatted short status, e.g. "📖 Reading(adapter.go)"
	done bool   // set true when tool_result arrives
}

// toolFmt defines how a tool name maps to a human-readable status.
type toolFmt struct {
	emoji string // e.g. "📖"
	verb  string // e.g. "Reading"; empty means use just emoji + value
	key   string // input field to extract, e.g. "file_path"
}

var toolFmtMap = map[string]toolFmt{
	"Read":         {"📖", "Reading", "file_path"},
	"Edit":         {"✏️", "Editing", "file_path"},
	"Write":        {"📝", "Writing", "file_path"},
	"NotebookEdit": {"📓", "Editing", "notebook_path"},
	"Bash":         {"⏳", "", "command"},
	"Grep":         {"🔍", "", "pattern"},
	"Glob":         {"📂", "", "pattern"},
	"WebSearch":    {"🌐", "Searching", "query"},
	"WebFetch":     {"🌐", "Fetching", "url"},
	"Agent":        {"🤖", "", "prompt"},
	"TodoWrite":    {"📋", "", "todos"},
	"LSP":          {"🔎", "", "text"},
}

// formatToolCall produces a short status string for a tool_call event.
func formatToolCall(name string, input map[string]any) string {
	if f, ok := toolFmtMap[name]; ok {
		val := extractInputField(input, f.key)
		val = truncateRunes(shortenPath(val), toolStatusMaxRunes)
		if f.verb != "" {
			if val != "" {
				return f.emoji + " " + f.verb + " " + val
			}
			return f.emoji + " " + f.verb
		}
		if val != "" {
			return f.emoji + " " + val
		}
		return f.emoji + " " + name
	}
	return formatFallbackTool(name, input)
}

func formatFallbackTool(name string, input map[string]any) string {
	keys := sortedKeys(input)
	if len(keys) == 0 {
		return name
	}
	val := fmt.Sprintf("%v", input[keys[0]])
	val = truncateRunes(shortenPath(val), toolStatusMaxRunes/2)
	return truncateRunes(name+"("+val+")", toolStatusMaxRunes)
}

func extractInputField(input map[string]any, key string) string {
	if input == nil {
		return ""
	}
	v, ok := input[key]
	if !ok {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

func shortenPath(s string) string {
	parts := strings.Split(s, "/")
	if len(parts) <= 1 {
		return s
	}
	return parts[len(parts)-1]
}

func truncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	if max > 1 {
		return string(runes[:max-1]) + "…"
	}
	return string(runes[:max])
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// renderToolActivity renders up to 2 tool entries as a single-line markdown string.
// Returns empty string if no entries.
// Format: "✅ Reading(main.go) · 🔧 make test"
func renderToolActivity(entries []toolEntry) string {
	if len(entries) == 0 {
		return ""
	}
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		prefix := "🔧 "
		if e.done {
			prefix = "✅ "
		}
		parts = append(parts, prefix+e.text)
	}
	return strings.Join(parts, " · ")
}

// extractToolCallStatus extracts a formatted status from a ToolCall envelope.
func extractToolCallStatus(env *events.Envelope) (id, text string) {
	data, ok := events.DecodeAs[events.ToolCallData](env.Event.Data)
	if !ok {
		return "", ""
	}
	return data.ID, formatToolCall(data.Name, data.Input)
}

// extractToolResultID extracts the tool call ID from a ToolResult envelope.
func extractToolResultID(env *events.Envelope) string {
	data, ok := events.DecodeAs[events.ToolResultData](env.Event.Data)
	if !ok {
		return ""
	}
	return data.ID
}
