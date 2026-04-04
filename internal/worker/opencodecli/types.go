// Package opencodecli implements the OpenCode CLI worker adapter.
package opencodecli

import "encoding/json"

// NDJSON event types emitted by `opencode run --format json`.
// Based on emit() in ~/opencode/packages/opencode/src/cli/cmd/run.ts.

type RawEvent struct {
	Type      string          `json:"type"`
	Timestamp int64           `json:"timestamp"`
	SessionID string          `json:"sessionID"`
	Part      json.RawMessage `json:"part,omitempty"`
	Error     json.RawMessage `json:"error,omitempty"`
}

// PartBase is embedded in every part object (from PartBase in message-v2.ts).
type PartBase struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionID"`
	MessageID string `json:"messageID"`
}

// StepStartPart is emitted when a new step begins.
type StepStartPart struct {
	PartBase
	Type     string `json:"type"` // "step-start"
	Snapshot string `json:"snapshot,omitempty"`
}

// StepFinishPart is emitted when a step completes.
type StepFinishPart struct {
	PartBase
	Type     string     `json:"type"` // "step-finish"
	Reason   string     `json:"reason"`
	Snapshot string     `json:"snapshot,omitempty"`
	Cost     float64    `json:"cost"`
	Tokens   TokenUsage `json:"tokens"`
}

// TokenUsage is the token usage breakdown from step_finish.
type TokenUsage struct {
	Total      float64 `json:"total,omitempty"`
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	Reasoning  float64 `json:"reasoning"`
	CacheRead  float64 `json:"cache?.read"`
	CacheWrite float64 `json:"cache?.write"`
}

// TextPart is emitted when text content is produced.
type TextPart struct {
	PartBase
	Type      string `json:"type"` // "text"
	Text      string `json:"text"`
	Synthetic bool   `json:"synthetic,omitempty"`
	Ignored   bool   `json:"ignored,omitempty"`
}

// ReasoningPart is emitted when thinking content is produced (only with --thinking).
type ReasoningPart struct {
	PartBase
	Type     string         `json:"type"` // "reasoning"
	Text     string         `json:"text"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// ToolPart is emitted when a tool is invoked.
// Based on MessageV2.ToolPart in message-v2.ts.
type ToolPart struct {
	PartBase
	Type   string    `json:"type"` // "tool"
	CallID string    `json:"callID"`
	Tool   string    `json:"tool"`
	State  ToolState `json:"state"`
}

// ToolState is the state of a tool invocation.
type ToolState struct {
	Status string         `json:"status"` // "pending", "running", "completed", "error"
	Input  map[string]any `json:"input,omitempty"`
	Title  string         `json:"title,omitempty"`
	Error  string         `json:"error,omitempty"`
	Output string         `json:"output,omitempty"`
}

// SessionError represents a session.error event.
type SessionError struct {
	Name    string `json:"name"`
	Message string `json:"message,omitempty"`
}
