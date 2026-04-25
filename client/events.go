// Package client provides a Go client SDK for the HotPlex Worker Gateway.
package client

import "github.com/hrygo/hotplex/pkg/events"

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

// Event data types re-exported from pkg/events for type-safe access.
type (
	MessageStartData        = events.MessageStartData
	MessageDeltaData        = events.MessageDeltaData
	MessageEndData          = events.MessageEndData
	StateData               = events.StateData
	ReasoningData           = events.ReasoningData
	StepData                = events.StepData
	DoneData                = events.DoneData
	ToolCallData            = events.ToolCallData
	ToolResultData          = events.ToolResultData
	PermissionRequestData   = events.PermissionRequestData
	PermissionResponseData  = events.PermissionResponseData
	QuestionOption          = events.QuestionOption
	Question                = events.Question
	QuestionRequestData     = events.QuestionRequestData
	QuestionResponseData    = events.QuestionResponseData
	ElicitationRequestData  = events.ElicitationRequestData
	ElicitationResponseData = events.ElicitationResponseData
)

// ErrorData is the payload of an error event.
// Extends events.ErrorData with a Details field for structured error info.
type ErrorData struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Details any       `json:"details,omitempty"`
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
