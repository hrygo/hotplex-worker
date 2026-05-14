package toolfmt

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFormatCall(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		tool  string
		input map[string]any
		want  string
	}{
		// File tools
		{"Read with path", "Read", map[string]any{"file_path": "/src/adapter.go"}, "📖 Reading adapter.go"},
		{"Read no path", "Read", map[string]any{}, "📖 Reading..."},
		{"Edit with path", "Edit", map[string]any{"file_path": "main.go"}, "✏️ Editing main.go"},
		{"Write with path", "Write", map[string]any{"file_path": "/tmp/new.go"}, "📝 Writing new.go"},
		{"NotebookEdit", "NotebookEdit", map[string]any{"notebook_path": "nb.ipynb"}, "📓 Editing nb.ipynb"},

		// Bash
		{"Bash with command", "Bash", map[string]any{"command": "make test"}, "⏳ make test"},
		{"Bash no command", "Bash", map[string]any{}, "⏳ Running command..."},
		{"Bash multiline", "Bash", map[string]any{"command": "jq -r '\nBuild KR→O mapping\ndef kr_to_o:\n[.key | .]"}, "⏳ jq -r '"},

		// Grep
		{"Grep with path", "Grep", map[string]any{"pattern": "func main", "path": "src/"}, `🔍 "func main" in src`},
		{"Grep pattern only", "Grep", map[string]any{"pattern": "hello"}, `🔍 "hello"`},
		{"Grep no pattern", "Grep", map[string]any{}, "🔍 Searching..."},

		// Glob
		{"Glob with pattern", "Glob", map[string]any{"pattern": "**/*.go"}, "📂 **/*.go"},
		{"Glob no pattern", "Glob", map[string]any{}, "📂 Finding files..."},

		// Agent
		{"Agent with description", "Agent", map[string]any{"description": "code-review"}, "🤖 code-review"},
		{"Agent with subagent_type", "Agent", map[string]any{"subagent_type": "Explore"}, "🤖 Explore"},
		{"Agent no info", "Agent", map[string]any{}, "🤖 Spawning agent..."},

		// WebSearch / WebFetch
		{"WebSearch with query", "WebSearch", map[string]any{"query": "golang generics"}, "🌐 Searching golang generics"},
		{"WebFetch with url", "WebFetch", map[string]any{"url": "https://example.com"}, "🌐 Fetching https://example.com"},
		{"WebSearch no query", "WebSearch", map[string]any{}, "🌐 Searching..."},

		// LSP
		{"LSP hover", "LSP", map[string]any{"operation": "hover", "filePath": "/src/main.go"}, "🔎 Hover → main.go"},
		{"LSP goToDefinition", "LSP", map[string]any{"operation": "goToDefinition", "filePath": "/src/main.go"}, "🔎 Go to def → main.go"},
		{"LSP no file", "LSP", map[string]any{"operation": "hover"}, "🔎 Hover"},
		{"LSP unknown op", "LSP", map[string]any{"operation": "unknown"}, "🔎 LSP"},

		// TodoWrite
		{"TodoWrite in_progress", "TodoWrite", map[string]any{
			"todos": []any{
				map[string]any{"content": "Fix bug", "activeForm": "Fixing bug", "status": "in_progress"},
				map[string]any{"content": "Add tests", "status": "pending"},
			},
		}, "📋 Fixing bug"},
		{"TodoWrite all completed", "TodoWrite", map[string]any{
			"todos": []any{
				map[string]any{"content": "Task 1", "status": "completed"},
				map[string]any{"content": "Task 2", "status": "completed"},
				map[string]any{"content": "Task 3", "status": "pending"},
			},
		}, "📋 3 tasks (2 done · 1 pending)"},
		{"TodoWrite no todos", "TodoWrite", map[string]any{}, "📋 Updating tasks..."},

		// Fallback
		{"Unknown tool with input", "CustomTool", map[string]any{"key1": "val1"}, "CustomTool(key1=val1)"},
		{"Unknown tool no input", "CustomTool", map[string]any{}, "CustomTool"},
		{"nil input registered", "Read", nil, "Read"},
		{"nil input unregistered", "CustomTool", nil, "CustomTool"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := FormatCall(tt.tool, tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestFormatResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		tool   string
		output any
		errMsg string
		want   string
	}{
		{"error message", "Read", nil, "file not found", "✗ file not found"},
		{"error truncated", "Read", nil, strings.Repeat("x", 50), "✗ " + strings.Repeat("x", 24) + "…"},
		{"nil output", "Read", nil, "", ""},
		{"empty string output", "Read", "", "", ""},
		{"non-string output", "Read", 42, "", ""},
		{"Grep with matches", "Grep", "line1\nline2\nline3", "", "3 matches"},
		{"Grep single line", "Grep", "only line", "", "1 matches"},
		{"Read multi line", "Read", "line1\nline2", "", "2 lines"},
		{"Read single line", "Read", "only", "", ""},
		{"Glob with files", "Glob", "a.go\nb.go\nc.go", "", "3 files"},
		{"Glob single", "Glob", "a.go", "", "1 files"},
		{"Bash success", "Bash", "ok", "", ""},
		{"unknown tool", "Other", "output", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := FormatResult(tt.tool, tt.output, tt.errMsg)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestShortenPath(t *testing.T) {
	t.Parallel()
	require.Equal(t, "main.go", ShortenPath("/src/main.go"))
	require.Equal(t, "main.go", ShortenPath("main.go"))
	require.Equal(t, "", ShortenPath(""))
}

func TestTruncateRunes(t *testing.T) {
	t.Parallel()
	require.Equal(t, "hello", TruncateRunes("hello", 10))
	require.Equal(t, "hel…", TruncateRunes("hello", 4))
	require.Equal(t, "h", TruncateRunes("hello", 1))
	require.Equal(t, "你好…", TruncateRunes("你好世界", 3))
}
