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

func TestParser_ParseLine_ControlRequest_WithRequestField(t *testing.T) {
	log := newTestLogger()
	parser := NewParser(log)

	tests := []struct {
		name    string
		line    string
		wantID  string
		wantSub string
		wantOk  bool
	}{
		{
			name:    "can_use_tool with request field (actual Claude Code format)",
			line:    `{"type":"control_request","request_id":"req_auq_1","request":{"subtype":"can_use_tool","tool_name":"AskUserQuestion","input":{"questions":[{"question":"PR strategy?","header":"PR","options":[{"label":"Single PR","description":"All phases in one PR"}],"multiSelect":false}]},"tool_use_id":"call_abc123"}}`,
			wantID:  "req_auq_1",
			wantSub: "can_use_tool",
			wantOk:  true,
		},
		{
			name:    "can_use_tool with response field (backward compat)",
			line:    `{"type":"control_request","request_id":"req_old","response":{"subtype":"can_use_tool","tool_name":"read_file","input":{"path":"/app/main.go"}}}`,
			wantID:  "req_old",
			wantSub: "can_use_tool",
			wantOk:  true,
		},
		{
			name:   "control_request with neither request nor response field",
			line:   `{"type":"control_request","request_id":"req_empty"}`,
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			events, err := parser.ParseLine(tt.line)
			if !tt.wantOk {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Len(t, events, 1)
			require.Equal(t, EventControl, events[0].Type)
			cr, ok := events[0].Payload.(*ControlRequestPayload)
			require.True(t, ok)
			require.Equal(t, tt.wantID, cr.RequestID)
			require.Equal(t, tt.wantSub, cr.Subtype)
		})
	}
}

func TestParser_ParseLine_SystemStatus(t *testing.T) {
	log := newTestLogger()
	parser := NewParser(log)

	line := `{"type":"system","subtype":"status","status":{"user_alive_interval":30,"max_web_socket_frame_size":1048576,"server":{"version":"1.2.3"}}}`

	events, err := parser.ParseLine(line)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, EventSystem, events[0].Type)
}

func TestParser_ParseLine_SystemNonStatus(t *testing.T) {
	log := newTestLogger()
	parser := NewParser(log)

	// Non-status system messages are ignored.
	line := `{"type":"system","subtype":"other","data":"something"}`

	events, err := parser.ParseLine(line)
	require.NoError(t, err)
	require.Nil(t, events)
}

func TestParser_ParseLine_SessionStateChanged(t *testing.T) {
	log := newTestLogger()
	parser := NewParser(log)

	line := `{"type":"session_state_changed","state":{"session_id":"sess_abc","is_resumed":true,"active_form":"Coding"}}`

	events, err := parser.ParseLine(line)
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, EventSessionState, events[0].Type)
}

func TestExtractTextFromContent_String(t *testing.T) {
	t.Parallel()
	result := extractTextFromContent([]byte(`"hello world"`))
	require.Equal(t, "hello world", result)
}

func TestExtractTextFromContent_Empty(t *testing.T) {
	t.Parallel()
	result := extractTextFromContent([]byte{})
	require.Empty(t, result)
}

func TestExtractTextFromContent_Array(t *testing.T) {
	t.Parallel()
	result := extractTextFromContent([]byte(`[{"type":"text","text":"part1"},{"type":"text","text":"part2"}]`))
	require.Equal(t, "part1part2", result)
}

func TestExtractTextFromContent_ArrayMixedTypes(t *testing.T) {
	t.Parallel()
	result := extractTextFromContent([]byte(`[{"type":"text","text":"only text"},{"type":"image","data":"skip"}]`))
	require.Equal(t, "only text", result)
}

func TestExtractTextFromContent_RawFallback(t *testing.T) {
	t.Parallel()
	result := extractTextFromContent([]byte(`not json at all`))
	require.Equal(t, "not json at all", result)
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

func TestParser_ParseLine_UserToolResult(t *testing.T) {
	log := newTestLogger()
	parser := NewParser(log)

	t.Run("single tool_result block", func(t *testing.T) {
		line := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_abc123","content":"file content here"}]}}`

		events, err := parser.ParseLine(line)
		require.NoError(t, err)
		require.Len(t, events, 1)

		require.Equal(t, EventToolProgress, events[0].Type)
		payload, ok := events[0].Payload.(*ToolResultPayload)
		require.True(t, ok)
		require.Equal(t, "toolu_abc123", payload.ToolUseID)
		require.Equal(t, "file content here", payload.Output)
		require.Empty(t, payload.Error)
	})

	t.Run("multiple tool_result blocks", func(t *testing.T) {
		line := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_111","content":"output 1"},{"type":"tool_result","tool_use_id":"toolu_222","content":"output 2"}]}}`

		events, err := parser.ParseLine(line)
		require.NoError(t, err)
		require.Len(t, events, 2)

		p1 := events[0].Payload.(*ToolResultPayload)
		require.Equal(t, "toolu_111", p1.ToolUseID)
		require.Equal(t, "output 1", p1.Output)

		p2 := events[1].Payload.(*ToolResultPayload)
		require.Equal(t, "toolu_222", p2.ToolUseID)
		require.Equal(t, "output 2", p2.Output)
	})

	t.Run("tool_result with content array", func(t *testing.T) {
		line := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_arr","content":[{"type":"text","text":"line 1"},{"type":"text","text":"line 2"}]}]}}`

		events, err := parser.ParseLine(line)
		require.NoError(t, err)
		require.Len(t, events, 1)

		payload := events[0].Payload.(*ToolResultPayload)
		require.Equal(t, "toolu_arr", payload.ToolUseID)
		require.Equal(t, "line 1\nline 2", payload.Output)
	})

	t.Run("tool_result with null content", func(t *testing.T) {
		line := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_null"}]}}`

		events, err := parser.ParseLine(line)
		require.NoError(t, err)
		require.Len(t, events, 1)

		payload := events[0].Payload.(*ToolResultPayload)
		require.Equal(t, "toolu_null", payload.ToolUseID)
		require.Nil(t, payload.Output)
	})

	t.Run("user message with no message body", func(t *testing.T) {
		line := `{"type":"user"}`

		events, err := parser.ParseLine(line)
		require.NoError(t, err)
		require.Nil(t, events)
	})

	t.Run("user message with plain text only", func(t *testing.T) {
		line := `{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hello"}]}}`

		events, err := parser.ParseLine(line)
		require.NoError(t, err)
		require.Nil(t, events)
	})

	t.Run("tool_result with is_error", func(t *testing.T) {
		line := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_err","is_error":true,"content":"permission denied"}]}}`

		events, err := parser.ParseLine(line)
		require.NoError(t, err)
		require.Len(t, events, 1)

		payload := events[0].Payload.(*ToolResultPayload)
		require.Equal(t, "toolu_err", payload.ToolUseID)
		require.Equal(t, "permission denied", payload.Error)
	})
}
