package events

import "time"

// Version is the current AEP protocol version.
const Version = "aep/v1"

// Kind is the event type discriminator, matching the CloudEvents "type" field.
type Kind string

// AEP v1 defined event kinds.
const (
	Error        Kind = "error"
	State        Kind = "state"
	Input        Kind = "input"
	Done         Kind = "done"
	MessageStart Kind = "message.start"
	MessageDelta Kind = "message.delta"
	MessageEnd   Kind = "message.end"
	ToolCall     Kind = "tool_call"
	ToolResult   Kind = "tool_result"
	Raw          Kind = "raw"
	Ping         Kind = "ping"
	Pong         Kind = "pong"
	Control      Kind = "control"
)

// Priority levels for message delivery.
type Priority string

const (
	PriorityControl Priority = "control" // control messages bypass backpressure
	PriorityData    Priority = "data"    // default priority, subject to backpressure
)

// ErrorCode defines standardized error codes.
type ErrorCode string

const (
	ErrCodeWorkerStartFailed ErrorCode = "WORKER_START_FAILED"
	ErrCodeWorkerCrash       ErrorCode = "WORKER_CRASH"
	ErrCodeWorkerTimeout     ErrorCode = "WORKER_TIMEOUT"
	ErrCodeWorkerSIGKILL     ErrorCode = "PROCESS_SIGKILL"
	ErrCodeInvalidMessage    ErrorCode = "INVALID_MESSAGE"
	ErrCodeSessionNotFound   ErrorCode = "SESSION_NOT_FOUND"
	ErrCodeSessionBusy       ErrorCode = "SESSION_BUSY"
	ErrCodeUnauthorized      ErrorCode = "UNAUTHORIZED"
	ErrCodeInternalError     ErrorCode = "INTERNAL_ERROR"
	ErrCodeProtocolViolation ErrorCode = "PROTOCOL_VIOLATION"
	ErrCodeRateLimited       ErrorCode = "RATE_LIMITED"
)

// Envelope is the unified AEP v1 message envelope, shared by both client→gateway and gateway→client.
type Envelope struct {
	Version   string   `json:"version"`
	ID        string   `json:"id"`
	Seq       int64    `json:"seq"`
	Priority  Priority `json:"priority,omitempty"`
	SessionID string   `json:"session_id"`
	Timestamp int64    `json:"timestamp"`
	Event     Event    `json:"event"`
}

// Event wraps a kind and its data payload.
type Event struct {
	Type Kind        `json:"type"`
	Data interface{} `json:"data"`
}

// ErrorData is the payload for Error events.
type ErrorData struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

// StateData is the payload for State events.
type StateData struct {
	State   SessionState `json:"state"`
	Message string       `json:"message,omitempty"`
}

// InputData is the payload for Input events (client → gateway).
type InputData struct {
	Content  string         `json:"content"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// MessageStartData is the payload for MessageStart events.
type MessageStartData struct {
	ID          string         `json:"id"`
	Role        string         `json:"role"`
	ContentType string         `json:"content_type"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

// MessageDeltaData is the payload for MessageDelta events.
type MessageDeltaData struct {
	MessageID string `json:"message_id"`
	Content   string `json:"content"`
}

// MessageEndData is the payload for MessageEnd events.
type MessageEndData struct {
	MessageID string `json:"message_id"`
}

// ToolCallData is the payload for ToolCall events.
type ToolCallData struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// ToolResultData is the payload for ToolResult events.
type ToolResultData struct {
	ID     string `json:"id"`
	Output any    `json:"output"`
	Error  string `json:"error,omitempty"`
}

// RawData is the payload for Raw events (passthrough for agent-specific messages).
type RawData struct {
	Kind string `json:"kind"`
	Raw  any    `json:"raw"`
}

// DoneData is the payload for Done events.
type DoneData struct {
	Success bool           `json:"success"`
	Stats   map[string]any `json:"stats,omitempty"`
	// Dropped is true if the UI Reconciliation triggered due to silent backpressure drops
	Dropped bool `json:"dropped,omitempty"`
}

// ControlAction identifies the type of server-originated control instruction.
type ControlAction string

const (
	ControlActionReconnect      ControlAction = "reconnect"
	ControlActionSessionInvalid ControlAction = "session_invalid"
	ControlActionThrottle       ControlAction = "throttle"
	ControlActionTerminate      ControlAction = "terminate"
	ControlActionDelete         ControlAction = "delete"
)

// ControlData is the payload for Control events.
type ControlData struct {
	Action      ControlAction  `json:"action"`
	Reason      string         `json:"reason,omitempty"`
	DelayMs     int            `json:"delay_ms,omitempty"`
	Recoverable bool           `json:"recoverable,omitempty"`
	Suggestion  map[string]any `json:"suggestion,omitempty"`
	Details     map[string]any `json:"details,omitempty"`
}

// SessionState represents the state of a session.
type SessionState string

const (
	StateCreated    SessionState = "created"
	StateRunning    SessionState = "running"
	StateIdle       SessionState = "idle"
	StateTerminated SessionState = "terminated"
	StateDeleted    SessionState = "deleted"
)

// IsTerminal returns true if the session is in a terminal state.
func (s SessionState) IsTerminal() bool {
	return s == StateDeleted
}

// IsActive returns true if the session is in an active state.
func (s SessionState) IsActive() bool {
	return s == StateRunning || s == StateIdle || s == StateCreated
}

// ValidTransitions maps from a state to the set of valid next states.
var ValidTransitions = map[SessionState]map[SessionState]bool{
	StateCreated: {
		StateRunning:    true,
		StateTerminated: true,
	},
	StateRunning: {
		StateIdle:       true,
		StateTerminated: true,
		StateDeleted:    true,
	},
	StateIdle: {
		StateRunning:    true,
		StateTerminated: true,
		StateDeleted:    true,
	},
	StateTerminated: {
		StateRunning: true, // resume
		StateDeleted: true,
	},
	StateDeleted: {}, // terminal
}

// IsValidTransition returns true if transitioning from from → to is allowed.
func IsValidTransition(from, to SessionState) bool {
	if m, ok := ValidTransitions[from]; ok {
		return m[to]
	}
	return false
}

// NewEnvelope creates a new Envelope with timestamp and version set.
func NewEnvelope(id, sessionID string, seq int64, kind Kind, data interface{}) *Envelope {
	return &Envelope{
		Version:   Version,
		ID:        id,
		Seq:       seq,
		SessionID: sessionID,
		Timestamp: time.Now().UnixMilli(),
		Event: Event{
			Type: kind,
			Data: data,
		},
	}
}
