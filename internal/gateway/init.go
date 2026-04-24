// Package gateway implements the WebSocket gateway that speaks AEP v1 to clients.
package gateway

import (
	"time"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// AEP v1 init message kinds (both directions).
const (
	InitAck = "init_ack"
)

// InitData is the payload of a client → gateway init message.
type InitData struct {
	Version    string            `json:"version"`
	WorkerType worker.WorkerType `json:"worker_type"`
	SessionID  string            `json:"session_id,omitempty"`
	Auth       InitAuth          `json:"auth,omitempty"`
	Config     InitConfig        `json:"config,omitempty"`
	ClientCaps ClientCaps        `json:"client_caps,omitempty"`
}

// InitAuth carries authentication data embedded in the init envelope.
type InitAuth struct {
	Token string `json:"token,omitempty"`
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
	Code       events.ErrorCode    `json:"code,omitempty"`
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
	MaxTurns         int               `json:"max_turns,omitempty"`
	Modalities       []string          `json:"modalities,omitempty"`
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

// Init error sentinels.
var (
	ErrInitVersionMismatch  = &InitError{Code: events.ErrCodeVersionMismatch, Message: "version mismatch"}
	ErrInitCapacityExceeded = &InitError{Code: events.ErrCodeRateLimited, Message: "capacity exceeded"}
	ErrInitSessionNotFound  = &InitError{Code: events.ErrCodeSessionNotFound, Message: "session not found"}
	ErrInitSessionDeleted   = &InitError{Code: events.ErrCodeSessionNotFound, Message: "session was deleted"}
)

// BuildInitAck builds an init_ack envelope from handshake result.
func BuildInitAck(sessionID string, state events.SessionState, wt worker.WorkerType) *events.Envelope {
	return events.NewEnvelope(
		aep.NewID(),
		sessionID,
		0,
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
			Code:      initErr.Code,
		},
	)
}

// ValidateInit checks init message validity.
func ValidateInit(env *events.Envelope) (InitData, *InitError) {
	data, ok := env.Event.Data.(map[string]any)
	if !ok {
		return InitData{}, &InitError{Code: events.ErrCodeInvalidMessage, Message: "init: invalid data"}
	}

	version, _ := data["version"].(string)
	if version == "" {
		return InitData{}, &InitError{Code: events.ErrCodeInvalidMessage, Message: "init: version required"}
	}
	if version != events.Version {
		return InitData{}, &InitError{Code: events.ErrCodeVersionMismatch,
			Message: "init: unsupported version " + version}
	}

	wt, _ := data["worker_type"].(string)
	if wt == "" {
		return InitData{}, &InitError{Code: events.ErrCodeInvalidMessage, Message: "init: worker_type required"}
	}

	sessionID, _ := data["session_id"].(string)

	var auth InitAuth
	if authData, ok := data["auth"].(map[string]any); ok {
		if token, ok := authData["token"].(string); ok {
			auth.Token = token
		}
	}

	cfg := InitConfig{}
	if cfgData, ok := data["config"].(map[string]any); ok {
		if model, ok := cfgData["model"].(string); ok {
			cfg.Model = model
		}
		if sysPrompt, ok := cfgData["system_prompt"].(string); ok {
			cfg.SystemPrompt = sysPrompt
		}
		if allowedTools, ok := cfgData["allowed_tools"].([]any); ok {
			for _, t := range allowedTools {
				if s, ok := t.(string); ok {
					cfg.AllowedTools = append(cfg.AllowedTools, s)
				}
			}
		}
		if disallowedTools, ok := cfgData["disallowed_tools"].([]any); ok {
			for _, t := range disallowedTools {
				if s, ok := t.(string); ok {
					cfg.DisallowedTools = append(cfg.DisallowedTools, s)
				}
			}
		}
		if maxTurns, ok := cfgData["max_turns"].(float64); ok {
			cfg.MaxTurns = int(maxTurns)
		}
		if workDir, ok := cfgData["work_dir"].(string); ok {
			cfg.WorkDir = workDir
		}
	}

	return InitData{
		Version:    version,
		WorkerType: worker.WorkerType(wt),
		SessionID:  sessionID,
		Auth:       auth,
		Config:     cfg,
	}, nil
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
		MaxTurns:         0,
		Modalities:       []string{"text", "code"},
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
