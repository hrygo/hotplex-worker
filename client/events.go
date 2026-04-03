// Package client provides a Go client SDK for the HotPlex Worker Gateway.
package client

import "github.com/hotplex/hotplex-worker/pkg/events"

// AEP protocol version.
const Version = events.Version // "aep/v1"

// Event kind constants matching pkg/events/events.go.
const (
	KindError              = string(events.Error)
	KindState              = string(events.State)
	KindInput              = string(events.Input)
	KindDone               = string(events.Done)
	KindMessage            = string(events.Message)
	KindMessageStart       = string(events.MessageStart)
	KindMessageDelta       = string(events.MessageDelta)
	KindMessageEnd         = string(events.MessageEnd)
	KindToolCall           = string(events.ToolCall)
	KindToolResult         = string(events.ToolResult)
	KindReasoning          = string(events.Reasoning)
	KindStep               = string(events.Step)
	KindRaw                = string(events.Raw)
	KindPermissionRequest  = string(events.PermissionRequest)
	KindPermissionResponse = string(events.PermissionResponse)
	KindPing               = string(events.Ping)
	KindPong               = string(events.Pong)
	KindControl            = string(events.Control)
	KindInitAck            = "init_ack"
)

// SessionState mirrors pkg/events/events.go.
type SessionState = events.SessionState

// State constants.
const (
	StateCreated    = events.StateCreated
	StateRunning    = events.StateRunning
	StateIdle       = events.StateIdle
	StateTerminated = events.StateTerminated
	StateDeleted    = events.StateDeleted
)

// ErrorCode mirrors pkg/events/events.go.
type ErrorCode = events.ErrorCode

// ErrorCode values.
const (
	ErrCodeSessionBusy     = events.ErrCodeSessionBusy
	ErrCodeInternalError   = events.ErrCodeInternalError
	ErrCodeUnauthorized    = events.ErrCodeUnauthorized
	ErrCodeSessionNotFound = events.ErrCodeSessionNotFound
)

// DoneData is the payload of a done event.
type DoneData struct {
	Success bool   `json:"success"`
	Stats   *Stats `json:"stats,omitempty"`
}

// Stats holds session statistics.
type Stats struct {
	DurationMs      int64   `json:"duration_ms,omitempty"`
	ToolCalls       int     `json:"tool_calls,omitempty"`
	InputTokens     int     `json:"input_tokens,omitempty"`
	OutputTokens    int     `json:"output_tokens,omitempty"`
	TotalTokens     int     `json:"total_tokens,omitempty"`
	CacheReadTokens int     `json:"cache_read_tokens,omitempty"`
	CostUSD         float64 `json:"cost_usd,omitempty"`
	Model           string  `json:"model,omitempty"`
}

// ErrorData is the payload of an error event.
type ErrorData struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Details any       `json:"details,omitempty"`
}

// ToolCallData is the payload of a tool_call event.
type ToolCallData struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// ToolResultData is the payload of a tool_result event.
type ToolResultData struct {
	ID     string      `json:"id"`
	Output interface{} `json:"output,omitempty"`
	Error  string      `json:"error,omitempty"`
}

// PermissionRequestData is the payload of a permission_request event.
type PermissionRequestData struct {
	ID          string `json:"id"`
	ToolName    string `json:"tool_name"`
	Description string `json:"description,omitempty"`
}

// InitAckData is returned after the server acknowledges the init handshake.
type InitAckData struct {
	SessionID  string       `json:"session_id"`
	State      SessionState `json:"state"`
	ServerCaps ServerCaps   `json:"server_caps"`
	Error      string       `json:"error,omitempty"`
	Code       ErrorCode    `json:"code,omitempty"`
}

// ServerCaps describes server capabilities from init_ack.
type ServerCaps struct {
	ProtocolVersion string   `json:"protocol_version"`
	WorkerType      string   `json:"worker_type"`
	SupportsResume  bool     `json:"supports_resume"`
	SupportsDelta   bool     `json:"supports_delta"`
	SupportsTool    bool     `json:"supports_tool_call"`
	SupportsPing    bool     `json:"supports_ping"`
	MaxFrameSize    int      `json:"max_frame_size"`
	MaxTurns        int      `json:"max_turns,omitempty"`
	Modalities      []string `json:"modalities,omitempty"`
	Tools           []string `json:"tools,omitempty"`
}
