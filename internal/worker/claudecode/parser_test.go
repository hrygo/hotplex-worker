package claudecode

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParser_ParseLine_StreamEvent(t *testing.T) {
	log := newTestLogger()
	parser := NewParser(log)

	tests := []struct {
		name     string
		line     string
		wantType EventType
		wantLen  int
	}{
		{
			name:     "thinking stream event",
			line:     `{"type":"stream_event","event":{"type":"thinking","message":{"id":"msg_123","content":"Let me analyze..."}}}`,
			wantType: EventStream,
			wantLen:  1,
		},
		{
			name:     "text stream event",
			line:     `{"type":"stream_event","event":{"type":"text","message":{"id":"msg_456","content":"Hello world"}}}`,
			wantType: EventStream,
			wantLen:  1,
		},
		{
			name:     "tool_use stream event",
			line:     `{"type":"stream_event","event":{"type":"tool_use","name":"read_file","input":{"path":"/app/main.go"},"message":{"id":"msg_789"}}}`,
			wantType: EventStream,
			wantLen:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, err := parser.ParseLine(tt.line)
			require.NoError(t, err)
			require.Len(t, events, tt.wantLen)
			require.Equal(t, tt.wantType, events[0].Type)
		})
	}
}

func TestParser_ParseLine_Assistant(t *testing.T) {
	log := newTestLogger()
	parser := NewParser(log)

	line := `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello"},{"type":"tool_use","id":"call_123","name":"read_file","input":{"path":"/app/main.go"}}]}}`

	events, err := parser.ParseLine(line)
	require.NoError(t, err)
	require.Len(t, events, 2)

	// First event: text
	require.Equal(t, EventAssistant, events[0].Type)
	textPayload, ok := events[0].Payload.(*StreamPayload)
	require.True(t, ok)
	require.Equal(t, "text", textPayload.Type)
	require.Equal(t, "Hello", textPayload.Content)

	// Second event: tool_use
	require.Equal(t, EventAssistant, events[1].Type)
	toolPayload, ok := events[1].Payload.(*ToolCallPayload)
	require.True(t, ok)
	require.Equal(t, "call_123", toolPayload.ID)
	require.Equal(t, "read_file", toolPayload.Name)
}

func TestParser_ParseLine_ToolProgress(t *testing.T) {
	log := newTestLogger()
	parser := NewParser(log)

	line := `{"type":"tool_progress","tool_use_id":"call_123","content":{"content":"file content...","error":""}}`

	events, err := parser.ParseLine(line)
	require.NoError(t, err)
	require.Len(t, events, 1)

	require.Equal(t, EventToolProgress, events[0].Type)
	payload, ok := events[0].Payload.(*ToolResultPayload)
	require.True(t, ok)
	require.Equal(t, "call_123", payload.ToolUseID)
	require.NotNil(t, payload.Output)
}

func TestParser_ParseLine_Result(t *testing.T) {
	log := newTestLogger()
	parser := NewParser(log)

	tests := []struct {
		name        string
		line        string
		wantSuccess bool
		wantMessage string
	}{
		{
			name:        "success result",
			line:        `{"type":"result","subtype":"success","result":"Task completed","duration_ms":5200,"num_turns":1,"total_cost_usd":0.05}`,
			wantSuccess: true,
			wantMessage: "Task completed",
		},
		{
			name:        "error result",
			line:        `{"type":"result","subtype":"error","is_error":true,"result":"Error occurred"}`,
			wantSuccess: false,
			wantMessage: "Error occurred",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			events, err := parser.ParseLine(tt.line)
			require.NoError(t, err)
			require.Len(t, events, 1)

			require.Equal(t, EventResult, events[0].Type)
			payload, ok := events[0].Payload.(*ResultPayload)
			require.True(t, ok)
			require.Equal(t, tt.wantSuccess, payload.Success)
			require.Equal(t, tt.wantMessage, payload.Message)
		})
	}
}

func TestParser_ParseLine_ControlRequest(t *testing.T) {
	log := newTestLogger()
	parser := NewParser(log)

	line := `{"type":"control_request","request_id":"req_123","response":{"subtype":"can_use_tool","tool_name":"read_file","input":{"path":"/app/main.go"}}}`

	events, err := parser.ParseLine(line)
	require.NoError(t, err)
	require.Len(t, events, 1)

	require.Equal(t, EventControl, events[0].Type)
	payload, ok := events[0].Payload.(*ControlRequestPayload)
	require.True(t, ok)
	require.Equal(t, "req_123", payload.RequestID)
	require.Equal(t, "read_file", payload.ToolName)
}

func TestParser_ParseLine_ControlRequestInterrupt(t *testing.T) {
	log := newTestLogger()
	parser := NewParser(log)

	line := `{"type":"control_request","request_id":"req_int","response":{"subtype":"interrupt"}}`

	events, err := parser.ParseLine(line)
	require.NoError(t, err)
	require.Len(t, events, 1)

	require.Equal(t, EventInterrupt, events[0].Type)
}

func TestParser_ParseLine_ControlRequestSetPermissionMode(t *testing.T) {
	log := newTestLogger()
	parser := NewParser(log)

	// set_permission_mode: ControlHandler sends auto-success; parser returns EventControl.
	line := `{"type":"control_request","request_id":"req_auto","response":{"subtype":"set_permission_mode","permission_mode":"auto-accept"}}`

	events, err := parser.ParseLine(line)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, EventControl, events[0].Type)
	cr, ok := events[0].Payload.(*ControlRequestPayload)
	require.True(t, ok)
	require.Equal(t, "req_auto", cr.RequestID)
	require.Equal(t, "set_permission_mode", cr.Subtype)
}

func TestParser_ParseLine_ControlRequestMCPStatus(t *testing.T) {
	log := newTestLogger()
	parser := NewParser(log)

	// mcp_status: ControlHandler sends auto-success; parser returns EventControl.
	line := `{"type":"control_request","request_id":"req_mcp","response":{"subtype":"mcp_status"}}`

	events, err := parser.ParseLine(line)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, EventControl, events[0].Type)
	cr, ok := events[0].Payload.(*ControlRequestPayload)
	require.True(t, ok)
	require.Equal(t, "req_mcp", cr.RequestID)
	require.Equal(t, "mcp_status", cr.Subtype)
}

func TestParser_ParseLine_UnknownType(t *testing.T) {
	log := newTestLogger()
	parser := NewParser(log)

	line := `{"type":"files_persisted","files":["/app/main.go"]}`

	events, err := parser.ParseLine(line)
	require.NoError(t, err)
	require.Nil(t, events) // Unknown types are ignored
}

func TestParser_ParseLine_InvalidJSON(t *testing.T) {
	log := newTestLogger()
	parser := NewParser(log)

	line := `{invalid json`

	events, err := parser.ParseLine(line)
	require.Error(t, err)
	require.Nil(t, events)
}
