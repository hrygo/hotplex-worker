package opencodecli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParser_ParseLine(t *testing.T) {
	parser := NewParser(newTestLogger())

	tests := []struct {
		name    string
		line    string
		wantLen int
		wantErr bool
	}{
		{
			name:    "step_start extracts session ID",
			line:    `{"type":"step_start","timestamp":1743772200000,"sessionID":"ses_abc123","part":{"id":"msg_001","sessionID":"ses_abc123","messageID":"msg_001","type":"step-start"}}`,
			wantLen: 1,
		},
		{
			name:    "step_finish with cost and tokens",
			line:    `{"type":"step_finish","timestamp":1743772205000,"sessionID":"ses_abc123","part":{"id":"msg_001","sessionID":"ses_abc123","messageID":"msg_001","type":"step-finish","reason":"stop","cost":0.012,"tokens":{"total":2048,"input":1200,"output":800,"reasoning":400,"cache":{"read":500,"write":100}}}}`,
			wantLen: 1,
		},
		{
			name:    "text event",
			line:    `{"type":"text","timestamp":1743772201000,"sessionID":"ses_abc123","part":{"id":"msg_002","sessionID":"ses_abc123","messageID":"msg_001","type":"text","text":"Hello world"}}`,
			wantLen: 1,
		},
		{
			name:    "text event ignores empty text",
			line:    `{"type":"text","timestamp":1743772201000,"sessionID":"ses_abc123","part":{"id":"msg_002","sessionID":"ses_abc123","messageID":"msg_001","type":"text","text":""}}`,
			wantLen: 0,
		},
		{
			name:    "reasoning event",
			line:    `{"type":"reasoning","timestamp":1743772201500,"sessionID":"ses_abc123","part":{"id":"msg_003","sessionID":"ses_abc123","messageID":"msg_001","type":"reasoning","text":"Let me think..."}}`,
			wantLen: 1,
		},
		{
			name:    "tool_use event",
			line:    `{"type":"tool_use","timestamp":1743772202000,"sessionID":"ses_abc123","part":{"id":"tool_001","sessionID":"ses_abc123","messageID":"msg_001","type":"tool","callID":"call_xyz","tool":"bash","state":{"status":"completed","input":{"command":"ls -la"},"output":"...","title":"Run bash command"}}}`,
			wantLen: 1,
		},
		{
			name:    "error event",
			line:    `{"type":"error","timestamp":1743772206000,"sessionID":"ses_abc123","error":{"name":"ContextOverflowError","message":"context length exceeded"}}`,
			wantLen: 1,
		},
		{
			name:    "unknown type is silently ignored",
			line:    `{"type":"unknown_event","data":"value"}`,
			wantLen: 0,
		},
		{
			name:    "invalid JSON",
			line:    `{not json`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, err := parser.ParseLine(tt.line)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Len(t, events, tt.wantLen)
		})
	}
}

func TestParser_ParseLine_StepStartPayload(t *testing.T) {
	parser := NewParser(newTestLogger())

	line := `{"type":"step_start","timestamp":1743772200000,"sessionID":"ses_abc123","part":{"id":"msg_001","sessionID":"ses_abc123","messageID":"msg_001","type":"step-start","snapshot":"abc123"}}`
	events, err := parser.ParseLine(line)
	require.NoError(t, err)
	require.Len(t, events, 1)

	payload, ok := events[0].Payload.(*StepStartPayload)
	require.True(t, ok, "expected *StepStartPayload, got %T", events[0].Payload)
	require.Equal(t, "ses_abc123", payload.SessionID)
	require.Equal(t, "msg_001", payload.MessageID)
	require.Equal(t, "abc123", payload.Snapshot)
}

func TestParser_ParseLine_TextPayload(t *testing.T) {
	parser := NewParser(newTestLogger())

	line := `{"type":"text","timestamp":1743772201000,"sessionID":"ses_abc123","part":{"id":"msg_002","sessionID":"ses_abc123","messageID":"msg_001","type":"text","text":"Hello world"}}`
	events, err := parser.ParseLine(line)
	require.NoError(t, err)
	require.Len(t, events, 1)

	payload, ok := events[0].Payload.(*TextPayload)
	require.True(t, ok)
	require.Equal(t, "Hello world", payload.Content)
	require.Equal(t, "msg_002", payload.MessageID)
}

func TestParser_ParseLine_ToolCallPayload(t *testing.T) {
	parser := NewParser(newTestLogger())

	line := `{"type":"tool_use","timestamp":1743772202000,"sessionID":"ses_abc123","part":{"id":"tool_001","sessionID":"ses_abc123","messageID":"msg_001","type":"tool","callID":"call_xyz","tool":"bash","state":{"status":"completed","input":{"command":"ls -la"}}}}`
	events, err := parser.ParseLine(line)
	require.NoError(t, err)
	require.Len(t, events, 1)

	payload, ok := events[0].Payload.(*ToolCallPayload)
	require.True(t, ok)
	require.Equal(t, "call_xyz", payload.ID)
	require.Equal(t, "bash", payload.Name)
	require.Equal(t, "ls -la", payload.Input["command"])
}

func TestParser_ParseLine_StepFinishPayload(t *testing.T) {
	parser := NewParser(newTestLogger())

	line := `{"type":"step_finish","timestamp":1743772205000,"sessionID":"ses_abc123","part":{"id":"msg_001","sessionID":"ses_abc123","messageID":"msg_001","type":"step-finish","reason":"stop","cost":0.025,"tokens":{"input":500,"output":1500,"reasoning":300,"cache":{"read":100,"write":50}}}}`
	events, err := parser.ParseLine(line)
	require.NoError(t, err)
	require.Len(t, events, 1)

	payload, ok := events[0].Payload.(*StepFinishPayload)
	require.True(t, ok)
	require.Equal(t, "stop", payload.Reason)
	require.InDelta(t, 0.025, payload.Cost, 0.001)
	require.Equal(t, float64(500), payload.Tokens.Input)
}

func TestParser_ParseLine_ErrorPayload(t *testing.T) {
	parser := NewParser(newTestLogger())

	line := `{"type":"error","timestamp":1743772206000,"sessionID":"ses_abc123","error":{"name":"ContextOverflowError","message":"context exceeded"}}`
	events, err := parser.ParseLine(line)
	require.NoError(t, err)
	require.Len(t, events, 1)

	payload, ok := events[0].Payload.(*ResultPayload)
	require.True(t, ok)
	require.False(t, payload.Success)
	require.Equal(t, "context exceeded", payload.Error)
}
