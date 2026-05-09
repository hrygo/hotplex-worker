// Package toolfmt provides shared tool call/result formatting for messaging adapters.
package toolfmt

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// FormatCall produces a human-readable status for a tool_call event.
// Merges the best formatting logic from both Feishu and Slack adapters.
func FormatCall(name string, input map[string]any) string {
	if fn, ok := callFormatters[name]; ok && input != nil {
		return fn(input)
	}
	return formatFallbackCall(name, input)
}

// FormatResult produces a short result summary for a tool_result event.
// name may be empty if the tool name is unknown.
// Returns empty string if no meaningful summary can be produced.
func FormatResult(name string, output any, errMsg string) string {
	if errMsg != "" {
		return "✗ " + TruncateRunes(errMsg, 25)
	}
	if output == nil {
		return ""
	}
	s, ok := output.(string)
	if !ok || s == "" {
		return ""
	}
	lines := strings.Count(s, "\n") + 1
	switch name {
	case "Grep":
		if lines > 0 {
			return fmt.Sprintf("%d matches", lines)
		}
	case "Read":
		if lines > 1 {
			return fmt.Sprintf("%d lines", lines)
		}
	case "Glob":
		if lines > 0 {
			return fmt.Sprintf("%d files", lines)
		}
	}
	return ""
}

// ShortenPath returns the last path component of s.
func ShortenPath(s string) string {
	if s == "" {
		return ""
	}
	return filepath.Base(s)
}

// TruncateRunes truncates s to max runes, appending "…" if truncated.
func TruncateRunes(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	if max > 1 {
		return string(runes[:max-1]) + "…"
	}
	return string(runes[:max])
}

// --- Tool call formatters ---

type callFormatter func(input map[string]any) string

var callFormatters = map[string]callFormatter{
	"TodoWrite":    formatTodoWrite,
	"Read":         formatFile("📖 Reading", "file_path"),
	"Edit":         formatFile("✏️ Editing", "file_path"),
	"Write":        formatFile("📝 Writing", "file_path"),
	"NotebookEdit": formatFile("📓 Editing", "notebook_path"),
	"Bash":         formatBash,
	"Grep":         formatGrep,
	"Glob":         formatGlob,
	"Agent":        formatAgent,
	"WebSearch":    formatSimple("🌐 Searching", "query"),
	"WebFetch":     formatSimple("🌐 Fetching", "url"),
	"LSP":          formatLSP,
}

func formatFile(prefix, key string) callFormatter {
	return func(input map[string]any) string {
		path, _ := input[key].(string)
		if path == "" {
			return prefix + "..."
		}
		return prefix + " " + ShortenPath(path)
	}
}

func formatSimple(prefix, key string) callFormatter {
	return func(input map[string]any) string {
		val, _ := input[key].(string)
		if val == "" {
			return prefix + "..."
		}
		return prefix + " " + val
	}
}

func formatBash(input map[string]any) string {
	cmd, _ := input["command"].(string)
	if cmd == "" {
		return "⏳ Running command..."
	}
	return "⏳ " + cmd
}

func formatGrep(input map[string]any) string {
	pattern, _ := input["pattern"].(string)
	if pattern == "" {
		return "🔍 Searching..."
	}
	path, _ := input["path"].(string)
	if path != "" {
		return "🔍 \"" + pattern + "\" in " + ShortenPath(path)
	}
	return "🔍 \"" + pattern + "\""
}

func formatGlob(input map[string]any) string {
	pattern, _ := input["pattern"].(string)
	if pattern == "" {
		return "📂 Finding files..."
	}
	return "📂 " + pattern
}

func formatAgent(input map[string]any) string {
	desc, _ := input["description"].(string)
	if desc != "" {
		return "🤖 " + desc
	}
	subagent, _ := input["subagent_type"].(string)
	if subagent != "" {
		return "🤖 " + subagent
	}
	return "🤖 Spawning agent..."
}

func formatLSP(input map[string]any) string {
	op, _ := input["operation"].(string)
	filePath, _ := input["filePath"].(string)
	label := lspOpLabel(op)
	if filePath != "" {
		return label + " → " + ShortenPath(filePath)
	}
	return label
}

func lspOpLabel(op string) string {
	switch op {
	case "hover":
		return "🔎 Hover"
	case "goToDefinition":
		return "🔎 Go to def"
	case "findReferences":
		return "🔎 Find refs"
	case "documentSymbol":
		return "🔎 Symbols"
	case "workspaceSymbol":
		return "🔎 Workspace search"
	case "goToImplementation":
		return "🔎 Go to impl"
	case "prepareCallHierarchy":
		return "🔎 Call hierarchy"
	case "incomingCalls":
		return "🔎 Incoming calls"
	case "outgoingCalls":
		return "🔎 Outgoing calls"
	default:
		return "🔎 LSP"
	}
}

func formatTodoWrite(input map[string]any) string {
	raw, ok := input["todos"]
	if !ok {
		return "📋 Updating tasks..."
	}

	type todoItem struct {
		content    string
		activeForm string
		status     string
	}

	var todos []todoItem
	if v, ok := raw.([]any); ok {
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			t := todoItem{}
			if c, ok := m["content"].(string); ok {
				t.content = c
			}
			if a, ok := m["activeForm"].(string); ok {
				t.activeForm = a
			}
			if s, ok := m["status"].(string); ok {
				t.status = s
			}
			todos = append(todos, t)
		}
	}

	if len(todos) == 0 {
		return "📋 Updating tasks..."
	}

	var inProgress []string
	var completed, pending int
	for _, t := range todos {
		switch t.status {
		case "completed":
			completed++
		case "in_progress":
			label := t.activeForm
			if label == "" {
				label = t.content
			}
			if label != "" {
				inProgress = append(inProgress, label)
			}
		default:
			pending++
		}
	}

	if len(inProgress) > 0 {
		return "📋 " + inProgress[0]
	}

	return fmt.Sprintf("📋 %d tasks (%d done · %d pending)", len(todos), completed, pending)
}

func formatFallbackCall(name string, input map[string]any) string {
	if len(input) == 0 {
		return name
	}
	keys := make([]string, 0, len(input))
	for k := range input {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		val := fmt.Sprintf("%v", input[k])
		parts = append(parts, k+"="+TruncateRunes(ShortenPath(val), 30))
	}
	return TruncateRunes(name+"("+strings.Join(parts, ", ")+")", 60)
}
