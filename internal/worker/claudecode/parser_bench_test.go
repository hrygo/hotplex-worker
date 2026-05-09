package claudecode

import (
	"encoding/json"
	"log/slog"
	"testing"
)

// Sample lines representing the most frequent message types in a typical turn.
// stream_event:text is by far the most common (~90% of lines in a streaming turn).
var benchLines = struct {
	streamText     string
	streamThinking string
	assistant      string
	controlReq     string
	result         string
}{
	streamText:     `{"type":"stream_event","event":{"type":"text","message":{"id":"msg_abc123","content":"Here is the implementation of the function that processes the input data and returns a structured result."}}}`,
	streamThinking: `{"type":"stream_event","event":{"type":"thinking","message":{"id":"msg_abc123","content":"Let me think about how to approach this problem..."}}}`,
	assistant:      `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"I'll help you with that."},{"type":"tool_use","id":"tu_123","name":"Bash","input":{"command":"ls -la"}}]}}`,
	controlReq:     `{"type":"control_request","request_id":"perm_123","response":{"subtype":"can_use_tool","tool_name":"Bash","input":{"command":"rm -rf /tmp/test"}}}`,
	result:         `{"type":"result","subtype":"success","duration_ms":5432,"duration_api_ms":3200,"num_turns":1,"total_cost_usd":0.0123,"result":"Task completed successfully","usage":{"input_tokens":1500,"output_tokens":800},"modelUsage":{"claude-sonnet-4-6":{"input_tokens":1500,"output_tokens":800}}}`,
}

// BenchmarkParseLine_StreamText measures the hot path: stream_event with text content.
func BenchmarkParseLine_StreamText(b *testing.B) {
	p := NewParser(slog.Default())
	line := benchLines.streamText
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = p.ParseLine(line)
	}
}

// BenchmarkParseLine_StreamThinking measures stream_event with thinking content.
func BenchmarkParseLine_StreamThinking(b *testing.B) {
	p := NewParser(slog.Default())
	line := benchLines.streamThinking
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = p.ParseLine(line)
	}
}

// BenchmarkParseLine_Assistant measures assistant message parsing (text + tool_use).
func BenchmarkParseLine_Assistant(b *testing.B) {
	p := NewParser(slog.Default())
	line := benchLines.assistant
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = p.ParseLine(line)
	}
}

// BenchmarkParseLine_ControlRequest measures control_request parsing.
func BenchmarkParseLine_ControlRequest(b *testing.B) {
	p := NewParser(slog.Default())
	line := benchLines.controlReq
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = p.ParseLine(line)
	}
}

// BenchmarkParseLine_Result measures result message parsing.
func BenchmarkParseLine_Result(b *testing.B) {
	p := NewParser(slog.Default())
	line := benchLines.result
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = p.ParseLine(line)
	}
}

// BenchmarkParseMessage_StreamText measures ParseMessage with pre-parsed SDKMessage.
// This shows the savings from avoiding redundant json.Unmarshal in readOutput.
func BenchmarkParseMessage_StreamText(b *testing.B) {
	p := NewParser(slog.Default())
	var msg SDKMessage
	_ = json.Unmarshal([]byte(benchLines.streamText), &msg)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = p.ParseMessage(&msg)
	}
}

// BenchmarkReadOutput_MixedSim simulates a realistic turn with mixed message types.
// Ratio: ~90% stream_text, ~5% stream_thinking, ~3% assistant, ~1% control, ~1% result.
func BenchmarkReadOutput_MixedSim(b *testing.B) {
	p := NewParser(slog.Default())
	lines := []string{
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamThinking,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.streamText,
		benchLines.assistant,
		benchLines.controlReq,
		benchLines.result,
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		for _, line := range lines {
			var msg SDKMessage
			if json.Unmarshal([]byte(line), &msg) != nil {
				continue
			}
			if msg.Type == "control_response" {
				continue
			}
			_, _ = p.ParseMessage(&msg)
		}
	}
}
