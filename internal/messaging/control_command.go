package messaging

import (
	"strings"

	"github.com/hotplex/hotplex-worker/pkg/events"
)

// ControlCommandResult holds the parsed control action and a human-readable label.
type ControlCommandResult struct {
	Action events.ControlAction
	Label  string // e.g. "gc" or "reset"
}

// slashCommandMap maps slash-form strings to control actions.
var slashCommandMap = map[string]ControlCommandResult{
	// GC: reclaim process and resources, session preserved for resume.
	"/gc":   {events.ControlActionGC, "gc"},
	"/park": {events.ControlActionGC, "gc"},
	// Reset: reuse session ID, everything else starts from scratch.
	"/reset":   {events.ControlActionReset, "reset"},
	"/restart": {events.ControlActionReset, "reset"},
	"/new":     {events.ControlActionReset, "reset"},
}

// naturalLanguageMap maps normalized natural language triggers to control actions.
// All keys require $ prefix to avoid accidental matches in normal conversation.
var naturalLanguageMap = map[string]ControlCommandResult{
	// GC: sleep, suspend — worker stopped but session alive for resume.
	"$gc": {events.ControlActionGC, "gc"},
	"$休眠": {events.ControlActionGC, "gc"},
	"$挂起": {events.ControlActionGC, "gc"},
	// Reset: start over — same session ID, fresh context.
	"$重置":    {events.ControlActionReset, "reset"},
	"$reset": {events.ControlActionReset, "reset"},
}

// ParseControlCommand checks whether text is a control command.
// Returns nil if the text is not a control command.
// Matching: exact match after trim + lowercase + strip trailing punctuation.
func ParseControlCommand(text string) *ControlCommandResult {
	t := strings.TrimSpace(strings.ToLower(text))
	t = trimTrailingPunct(t)

	if result, ok := slashCommandMap[t]; ok {
		return &result
	}
	if result, ok := naturalLanguageMap[t]; ok {
		return &result
	}
	return nil
}

func parseWorkerSlashCommands(text string) (base, args string) {
	parts := strings.SplitN(text, " ", 2)
	base = parts[0]
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}
	return base, args
}

// workerSlashCommandsWithArgs lists slash commands that accept arguments.
var workerSlashCommandsWithArgs = map[string]bool{
	"/model":  true,
	"/perm":   true,
	"/effort": true,
}

// workerSlashMap maps slash-form strings to worker stdio commands.
var workerSlashMap = map[string]events.WorkerStdioCommand{
	"/context": events.StdioContextUsage,
	"/mcp":     events.StdioMCPStatus,
	"/model":   events.StdioSetModel,
	"/perm":    events.StdioSetPermMode,
	"/compact": events.StdioCompact,
	"/clear":   events.StdioClear,
	"/effort":  events.StdioEffort,
	"/rewind":  events.StdioRewind,
	"/commit":  events.StdioCommit,
}

// workerNLMap maps natural language triggers to worker stdio commands.
// All keys require $ prefix to avoid accidental matches in normal conversation.
var workerNLMap = map[string]events.WorkerStdioCommand{
	"$context": events.StdioContextUsage,
	"$上下文":     events.StdioContextUsage,
	"$mcp":     events.StdioMCPStatus,
	"$model":   events.StdioSetModel,
	"$切换模型":    events.StdioSetModel,
	"$perm":    events.StdioSetPermMode,
	"$权限模式":    events.StdioSetPermMode,
	"$compact": events.StdioCompact,
	"$压缩":      events.StdioCompact,
	"$clear":   events.StdioClear,
	"$清空":      events.StdioClear,
	"$effort":  events.StdioEffort,
	"$rewind":  events.StdioRewind,
	"$回退":      events.StdioRewind,
	"$commit":  events.StdioCommit,
	"$提交":      events.StdioCommit,
}

// WorkerCommandResult holds the parsed worker stdio command and its arguments.
type WorkerCommandResult struct {
	Command events.WorkerStdioCommand
	Label   string
	Args    string
	Extra   map[string]any
}

// ParseWorkerCommand checks whether text is a worker stdio command.
// Returns nil if the text is not a worker command.
//
// Supported formats:
//   - Slash commands: /context, /mcp, /model sonnet-4, /perm bypassPermissions, /effort high
//   - Natural language: $上下文, $MCP状态, $切换模型, etc. (require $ prefix)
func ParseWorkerCommand(text string) *WorkerCommandResult {
	t := strings.TrimSpace(strings.ToLower(text))
	t = trimTrailingPunct(t)

	base, args := parseWorkerSlashCommands(t)
	if cmd, ok := workerSlashMap[base]; ok {
		label := base
		if !workerSlashCommandsWithArgs[base] {
			args = ""
		}
		return &WorkerCommandResult{
			Command: cmd,
			Label:   label,
			Args:    args,
		}
	}

	if cmd, ok := workerNLMap[t]; ok {
		return &WorkerCommandResult{
			Command: cmd,
			Label:   t,
		}
	}

	return nil
}

// trimTrailingPunct strips trailing punctuation (same character set as slack/abort.go).
func trimTrailingPunct(s string) string {
	return strings.TrimRightFunc(s, func(r rune) bool {
		switch r {
		case '.', '!', '?', ',', ';', ':', '"', '\'', ')', ']',
			'…', '，', '。', '；', '：', '！', '？', '、':
			return true
		}
		return false
	})
}
