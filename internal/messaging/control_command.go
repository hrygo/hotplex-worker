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
