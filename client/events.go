// Package client provides a Go client SDK for the HotPlex Worker Gateway.
package client

import "github.com/hotplex/hotplex-worker/pkg/events"

// AEP protocol version.
const Version = events.Version // "aep/v1"

// Event kind constants matching pkg/events/events.go.
const (
	EventInit                = string(events.Init)
	EventError               = string(events.Error)
	EventState               = string(events.State)
	EventInput               = string(events.Input)
	EventDone                = string(events.Done)
	EventMessage             = string(events.Message)
	EventMessageStart        = string(events.MessageStart)
	EventMessageDelta        = string(events.MessageDelta)
	EventMessageEnd          = string(events.MessageEnd)
	EventToolCall            = string(events.ToolCall)
	EventToolResult          = string(events.ToolResult)
	EventReasoning           = string(events.Reasoning)
	EventStep                = string(events.Step)
	EventRaw                 = string(events.Raw)
	EventPermissionRequest   = string(events.PermissionRequest)
	EventPermissionResponse  = string(events.PermissionResponse)
	EventQuestionRequest     = string(events.QuestionRequest)
	EventQuestionResponse    = string(events.QuestionResponse)
	EventElicitationRequest  = string(events.ElicitationRequest)
	EventElicitationResponse = string(events.ElicitationResponse)
	EventPing                = string(events.Ping)
	EventPong                = string(events.Pong)
	EventControl             = string(events.Control)
	EventInitAck             = "init_ack"
)

// ControlAction constants for client-initiated control.
const (
	ControlActionTerminate = string(events.ControlActionTerminate)
	ControlActionDelete    = string(events.ControlActionDelete)
	ControlActionReset     = string(events.ControlActionReset)
	ControlActionGC        = string(events.ControlActionGC)
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
	Success bool           `json:"success"`
	Stats   map[string]any `json:"stats,omitempty"`
	Dropped bool           `json:"dropped,omitempty"`
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
	ID          string   `json:"id"`
	ToolName    string   `json:"tool_name"`
	Description string   `json:"description,omitempty"`
	Args        []string `json:"args,omitempty"`
}

// QuestionOption represents a single selectable option in a question.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Preview     string `json:"preview,omitempty"`
}

// Question represents a single question with options.
type Question struct {
	Question    string           `json:"question"`
	Header      string           `json:"header"`
	Options     []QuestionOption `json:"options"`
	MultiSelect bool             `json:"multi_select"`
}

// QuestionRequestData is the payload of a question_request event.
type QuestionRequestData struct {
	ID        string     `json:"id"`
	ToolName  string     `json:"tool_name,omitempty"`
	Questions []Question `json:"questions"`
}

// QuestionResponseData is the payload of a question_response event.
type QuestionResponseData struct {
	ID      string            `json:"id"`
	Answers map[string]string `json:"answers"`
}

// ElicitationRequestData is the payload of an elicitation_request event.
type ElicitationRequestData struct {
	ID              string         `json:"id"`
	MCPServerName   string         `json:"mcp_server_name"`
	Message         string         `json:"message"`
	Mode            string         `json:"mode,omitempty"`
	URL             string         `json:"url,omitempty"`
	ElicitationID   string         `json:"elicitation_id,omitempty"`
	RequestedSchema map[string]any `json:"requested_schema,omitempty"`
}

// ElicitationResponseData is the payload of an elicitation_response event.
type ElicitationResponseData struct {
	ID      string         `json:"id"`
	Action  string         `json:"action"` // "accept" | "decline" | "cancel"
	Content map[string]any `json:"content,omitempty"`
}

// Priority is the message delivery priority.
type Priority = events.Priority

const (
	PriorityControl = events.PriorityControl
	PriorityData    = events.PriorityData
)

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
