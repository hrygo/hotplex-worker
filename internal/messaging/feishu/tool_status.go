package feishu

import (
	"strings"
	"unicode"

	"github.com/hrygo/hotplex/internal/messaging/toolfmt"
	"github.com/hrygo/hotplex/pkg/events"
)

// toolActivityMaxCols is the maximum visual column width per tool activity line.
// CJK/emoji glyphs occupy 2 columns; ASCII occupies 1. Lines exceeding this are
// truncated with "…" to prevent card layout wrapping.
const toolActivityMaxCols = 50

// toolEntry tracks a single tool call for the activity strip.
type toolEntry struct {
	id     string // matches ToolCallData.ID for result correlation
	name   string // tool name for result formatting
	text   string // formatted status from toolfmt.FormatCall
	done   bool   // set true when tool_result arrives
	result string // formatted result summary from toolfmt.FormatResult
}

// renderToolActivity renders up to 2 tool entries as newline-separated markdown.
// Done entries with result info append " · <result>" after the call text.
// Each line is truncated to toolActivityMaxCols visual columns.
func renderToolActivity(entries []toolEntry) string {
	if len(entries) == 0 {
		return ""
	}
	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		line := e.text
		if e.done && e.result != "" {
			line += " · " + e.result
		}
		parts = append(parts, truncateVisual(line, toolActivityMaxCols))
	}
	return strings.Join(parts, "\n")
}

// extractToolCallData extracts (id, name, input) from a ToolCall envelope.
func extractToolCallData(env *events.Envelope) (id, name string, input map[string]any) {
	data, ok := events.DecodeAs[events.ToolCallData](env.Event.Data)
	if !ok {
		return "", "", nil
	}
	return data.ID, data.Name, data.Input
}

// extractToolResultData extracts (id, output, errMsg) from a ToolResult envelope.
func extractToolResultData(env *events.Envelope) (id string, output any, errMsg string) {
	data, ok := events.DecodeAs[events.ToolResultData](env.Event.Data)
	if !ok {
		return "", nil, ""
	}
	return data.ID, data.Output, data.Error
}

// formatToolCall formats a tool call using the shared toolfmt package.
func formatToolCall(name string, input map[string]any) string {
	return toolfmt.FormatCall(name, input)
}

// formatToolResult formats a tool result using the shared toolfmt package.
func formatToolResult(name string, output any, errMsg string) string {
	return toolfmt.FormatResult(name, output, errMsg)
}

// runeWidth returns the visual column width of a single rune.
// CJK ideographs, most emoji, and full-width forms occupy 2 columns.
func runeWidth(r rune) int {
	// Fast path: Latin-1 (covers ASCII + Western European) is always single-width.
	if r < 256 {
		return 1
	}
	// Range checks first (cheap integer compare), table lookups last.
	switch {
	case r >= 0x1F300 && r <= 0x1FAFF: // emoji
		return 2
	case r >= 0xFF01 && r <= 0xFF60: // fullwidth forms
		return 2
	case r >= 0x2000 && r <= 0x206F: // general punctuation
		return 2
	case unicode.Is(unicode.Han, r):
		return 2
	case unicode.Is(unicode.Hangul, r):
		return 2
	case unicode.Is(unicode.Hiragana, r):
		return 2
	case unicode.Is(unicode.Katakana, r):
		return 2
	default:
		return 1
	}
}

// truncateVisual truncates s to maxCols visual columns, appending "…" if truncated.
func truncateVisual(s string, maxCols int) string {
	if maxCols <= 0 {
		return ""
	}
	// Fast path: if byte length fits, visual width <= byte length (each rune ≥ 1 byte ≥ 1 col).
	if len(s) <= maxCols {
		return s
	}
	cols := 0
	for i, r := range s {
		cols += runeWidth(r)
		if cols > maxCols {
			return s[:i] + "…"
		}
	}
	return s
}
