package claudecode

import "encoding/json"

// SDKMessage represents a raw Claude Code SDK output message.
// Based on Claude Code source: src/entrypoints/sdk/coreSchemas.ts
type SDKMessage struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype,omitempty"`
	Event     json.RawMessage `json:"event,omitempty"`   // For stream_event
	Message   json.RawMessage `json:"message,omitempty"` // For assistant/user
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`    // For tool_progress
	RequestID string          `json:"request_id,omitempty"` // For control_request
	Response  json.RawMessage `json:"response,omitempty"`   // For control_request

	// Result fields (type="result")
	DurationMs        int64           `json:"duration_ms,omitempty"`
	DurationAPIMs     int64           `json:"duration_api_ms,omitempty"`
	IsError           bool            `json:"is_error,omitempty"`
	NumTurns          int             `json:"num_turns,omitempty"`
	Result            string          `json:"result,omitempty"`
	TotalCostUSD      float64         `json:"total_cost_usd,omitempty"`
	Usage             json.RawMessage `json:"usage,omitempty"`
	ModelUsage        json.RawMessage `json:"modelUsage,omitempty"`
	PermissionDenials json.RawMessage `json:"permission_denials,omitempty"`
	UUID              string          `json:"uuid,omitempty"`
	SessionID         string          `json:"session_id,omitempty"`

	// System fields (type="system")
	Status string `json:"status,omitempty"` // For system.status

	// Session state fields (type="session_state_changed")
	State string `json:"state,omitempty"`
}

// StreamEvent represents a streaming event from Claude Code.
type StreamEvent struct {
	Type    string          `json:"type"` // thinking, text, tool_use, code, image
	Message StreamMessage   `json:"message,omitempty"`
	Name    string          `json:"name,omitempty"`  // For tool_use
	Input   json.RawMessage `json:"input,omitempty"` // For tool_use
}

// StreamMessage represents the message content in a stream event.
type StreamMessage struct {
	ID      string          `json:"id,omitempty"`
	Content json.RawMessage `json:"content,omitempty"`
	Role    string          `json:"role,omitempty"`
}

// AssistantMessage represents a complete assistant message.
type AssistantMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // Array of content blocks
}

// ContentBlock represents a content block in a message.
type ContentBlock struct {
	Type    string          `json:"type"` // text, tool_use, image, thinking
	Text    string          `json:"text,omitempty"`
	Thinking string          `json:"thinking,omitempty"` // For thinking blocks
	ID      string          `json:"id,omitempty"`
	Name    string          `json:"name,omitempty"`
	Input   json.RawMessage `json:"input,omitempty"`
}

// ControlRequestPayload represents a control request from Claude Code.
type ControlRequestPayload struct {
	// RequestID is always populated from the outer SDKMessage.RequestID field,
	// NOT from the inner JSON body (which has no request_id field).
	RequestID string          `json:"request_id,omitempty"`
	Subtype   string          `json:"subtype"`
	ToolName  string          `json:"tool_name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
}

// SystemEventData represents system event data.
type SystemEventData struct {
	Init   *SystemInitData `json:"init,omitempty"`
	Status string          `json:"status,omitempty"`
}

// SystemInitData represents init system event data.
type SystemInitData struct {
	Version string `json:"version,omitempty"`
}
