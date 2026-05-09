package feishu

import (
	"strings"

	"github.com/hrygo/hotplex/internal/messaging/toolfmt"
	"github.com/hrygo/hotplex/pkg/events"
)

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
		parts = append(parts, line)
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
