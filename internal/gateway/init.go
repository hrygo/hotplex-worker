// Package gateway implements the WebSocket gateway that speaks AEP v1 to clients.
package gateway

import (
	"time"

	"hotplex-worker/internal/aep"
	"hotplex-worker/internal/worker"
	"hotplex-worker/pkg/events"
)

// AEP v1 init message kinds (both directions).
const (
	Init    = "init"
	InitAck = "init_ack"
)

// InitData is the payload of a client → gateway init message.
type InitData struct {
	Version    string            `json:"version"`
	WorkerType worker.WorkerType `json:"worker_type"`
	SessionID  string            `json:"session_id,omitempty"`
	Config     InitConfig        `json:"config,omitempty"`
	ClientCaps ClientCaps        `json:"client_caps,omitempty"`
}

// InitConfig carries per-session configuration.
type InitConfig struct {
	Model           string         `json:"model,omitempty"`
	SystemPrompt    string         `json:"system_prompt,omitempty"`
	AllowedTools    []string       `json:"allowed_tools,omitempty"`
	DisallowedTools []string       `json:"disallowed_tools,omitempty"`
	MaxTurns        int            `json:"max_turns,omitempty"`
	WorkDir         string         `json:"work_dir,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

// ClientCaps declares what event kinds the client supports receiving.
type ClientCaps struct {
	SupportsDelta    bool     `json:"supports_delta"`
	SupportsToolCall bool     `json:"supports_tool_call"`
	SupportedKinds   []string `json:"supported_kinds,omitempty"`
}

// InitAckData is the payload of a gateway → client init_ack message.
type InitAckData struct {
	SessionID  string              `json:"session_id"`
	State      events.SessionState `json:"state"`
	ServerCaps ServerCaps          `json:"server_caps"`
	Error      string              `json:"error,omitempty"`
}

// ServerCaps declares what the gateway / worker supports.
type ServerCaps struct {
	ProtocolVersion  string            `json:"protocol_version"`
	WorkerType       worker.WorkerType `json:"worker_type"`
	SupportsResume   bool              `json:"supports_resume"`
	SupportsDelta    bool              `json:"supports_delta"`
	SupportsToolCall bool              `json:"supports_tool_call"`
	SupportsPing     bool              `json:"supports_ping"`
	MaxFrameSize     int64             `json:"max_frame_size"`
	Tools            []string          `json:"tools,omitempty"`
}

// InitError holds the result of a failed handshake.
type InitError struct {
	Code    events.ErrorCode
	Message string
}

func (e *InitError) Error() string {
	return e.Message
}

// ErrInitVersionMismatch is returned when the client protocol version is incompatible.
var ErrInitVersionMismatch = &InitError{Code: events.ErrCodeProtocolViolation, Message: "version mismatch"}

// ErrInitCapacityExceeded is returned when the server cannot accept new sessions.
var ErrInitCapacityExceeded = &InitError{Code: events.ErrCodeRateLimited, Message: "capacity exceeded"}

// ErrInitSessionNotFound is returned when a resume references a non-existent session.
var ErrInitSessionNotFound = &InitError{Code: events.ErrCodeSessionNotFound, Message: "session not found"}

// ErrInitSessionDeleted is returned when a resume references a deleted session.
var ErrInitSessionDeleted = &InitError{Code: events.ErrCodeSessionNotFound, Message: "session was deleted"}

// BuildInitAck builds an init_ack envelope from handshake result.
func BuildInitAck(sessionID string, state events.SessionState, wt worker.WorkerType) *events.Envelope {
	return events.NewEnvelope(
		aep.NewID(),
		sessionID,
		0, // seq assigned by gateway
		InitAck,
		InitAckData{
			SessionID:  sessionID,
			State:      state,
			ServerCaps: DefaultServerCaps(wt),
		},
	)
}

// BuildInitAckError builds an init_ack error envelope.
func BuildInitAckError(sessionID string, initErr *InitError) *events.Envelope {
	return events.NewEnvelope(
		aep.NewID(),
		sessionID,
		0,
		InitAck,
		InitAckData{
			SessionID: sessionID,
			State:     events.StateDeleted,
			Error:     initErr.Message,
		},
	)
}

// ValidateInit checks init message validity.
func ValidateInit(env *events.Envelope) (InitData, *InitError) {
	data, ok := env.Event.Data.(map[string]any)
	if !ok {
		return InitData{}, &InitError{Code: events.ErrCodeInvalidMessage, Message: "init: invalid data"}
	}

	// Version check.
	version, _ := data["version"].(string)
	if version == "" {
		return InitData{}, &InitError{Code: events.ErrCodeInvalidMessage, Message: "init: version required"}
	}
	if version != events.Version {
		return InitData{}, &InitError{Code: events.ErrCodeProtocolViolation,
			Message: "init: unsupported version " + version}
	}

	// Worker type check.
	wt, _ := data["worker_type"].(string)
	if wt == "" {
		return InitData{}, &InitError{Code: events.ErrCodeInvalidMessage, Message: "init: worker_type required"}
	}

	sessionID, _ := data["session_id"].(string)

	return InitData{
		Version:    version,
		WorkerType: worker.WorkerType(wt),
		SessionID:  sessionID,
	}, nil
}

// Seq returns the next sequence number for a session, assigning it to env.
func AssignSeq(seqGen *SeqGen, env *events.Envelope) {
	env.Seq = seqGen.Next(env.SessionID)
}

// DefaultServerCaps returns a ServerCaps with default values.
func DefaultServerCaps(wt worker.WorkerType) ServerCaps {
	return ServerCaps{
		ProtocolVersion:  events.Version,
		WorkerType:       wt,
		SupportsResume:   true,
		SupportsDelta:    true,
		SupportsToolCall: true,
		SupportsPing:     true,
		MaxFrameSize:     32 * 1024,
		Tools:            nil,
	}
}

// SessionStateForWorker returns the appropriate initial state for a worker type.
func SessionStateForWorker(wt worker.WorkerType) events.SessionState {
	return events.StateCreated
}

// BackoffDuration computes a simple exponential backoff for throttled clients.
func BackoffDuration(attempt int) time.Duration {
	const base = 1 * time.Second
	const max = 60 * time.Second
	d := base * (1 << uint(attempt))
	if d > max {
		return max
	}
	return d
}

// IsSessionRecoverable returns true if a session in state can be resumed.
func IsSessionRecoverable(state events.SessionState) bool {
	return state == events.StateCreated ||
		state == events.StateRunning ||
		state == events.StateIdle ||
		state == events.StateTerminated
}
