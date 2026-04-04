package opencodecli

import (
	"encoding/json"
	"fmt"
	"log/slog"
)

// EventType represents the type of a parsed worker event.
type EventType string

const (
	EventStepStart  EventType = "step_start"
	EventStepFinish EventType = "step_finish"
	EventText       EventType = "text"
	EventReasoning  EventType = "reasoning"
	EventToolUse    EventType = "tool_use"
	EventToolResult EventType = "tool_result"
	EventError      EventType = "error"
)

// WorkerEvent represents a parsed event ready for mapping to AEP.
type WorkerEvent struct {
	Type    EventType
	Payload any
}

// StepStartPayload carries session metadata from the first step.
type StepStartPayload struct {
	SessionID string // OpenCode internal session ID (ses_xxx)
	MessageID string
	Snapshot  string
}

// StepFinishPayload carries step completion stats.
type StepFinishPayload struct {
	Reason string
	Cost   float64
	Tokens TokenUsage
}

// TextPayload carries text content.
type TextPayload struct {
	Content   string
	MessageID string
}

// ReasoningPayload carries thinking content.
type ReasoningPayload struct {
	Content   string
	MessageID string
}

// ToolCallPayload carries a tool invocation.
type ToolCallPayload struct {
	ID    string
	Name  string
	Input map[string]any
}

// ResultPayload carries turn completion data.
type ResultPayload struct {
	Success bool
	Stats   map[string]any
	Error   string
}

// Parser parses OpenCode CLI NDJSON lines into WorkerEvents.
type Parser struct {
	log *slog.Logger
}

// NewParser creates a new Parser.
func NewParser(log *slog.Logger) *Parser {
	return &Parser{log: log}
}

// ParseLine parses a single NDJSON line into one or more WorkerEvents.
// Returns nil, nil for events that should be silently ignored.
func (p *Parser) ParseLine(line string) ([]*WorkerEvent, error) {
	var raw RawEvent
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return nil, fmt.Errorf("opencodecli: parse: %w", err)
	}

	switch raw.Type {
	case string(EventStepStart):
		return p.parseStepStart(&raw)
	case string(EventStepFinish):
		return p.parseStepFinish(&raw)
	case string(EventText):
		return p.parseText(&raw)
	case string(EventReasoning):
		return p.parseReasoning(&raw)
	case string(EventToolUse):
		return p.parseToolUse(&raw)
	case string(EventError):
		return p.parseError(&raw)
	default:
		p.log.Debug("opencodecli: ignoring event", "type", raw.Type)
		return nil, nil
	}
}

func (p *Parser) parseStepStart(raw *RawEvent) ([]*WorkerEvent, error) {
	var part StepStartPart
	if err := json.Unmarshal(raw.Part, &part); err != nil {
		return nil, fmt.Errorf("opencodecli: unmarshal step_start part: %w", err)
	}
	return []*WorkerEvent{{
		Type: EventStepStart,
		Payload: &StepStartPayload{
			SessionID: part.SessionID,
			MessageID: part.ID,
			Snapshot:  part.Snapshot,
		},
	}}, nil
}

func (p *Parser) parseStepFinish(raw *RawEvent) ([]*WorkerEvent, error) {
	var part StepFinishPart
	if err := json.Unmarshal(raw.Part, &part); err != nil {
		return nil, fmt.Errorf("opencodecli: unmarshal step_finish part: %w", err)
	}
	return []*WorkerEvent{{
		Type: EventStepFinish,
		Payload: &StepFinishPayload{
			Reason: part.Reason,
			Cost:   part.Cost,
			Tokens: part.Tokens,
		},
	}}, nil
}

func (p *Parser) parseText(raw *RawEvent) ([]*WorkerEvent, error) {
	var part TextPart
	if err := json.Unmarshal(raw.Part, &part); err != nil {
		return nil, fmt.Errorf("opencodecli: unmarshal text part: %w", err)
	}
	if part.Text == "" || part.Ignored {
		return nil, nil
	}
	return []*WorkerEvent{{
		Type: EventText,
		Payload: &TextPayload{
			Content:   part.Text,
			MessageID: part.ID,
		},
	}}, nil
}

func (p *Parser) parseReasoning(raw *RawEvent) ([]*WorkerEvent, error) {
	var part ReasoningPart
	if err := json.Unmarshal(raw.Part, &part); err != nil {
		return nil, fmt.Errorf("opencodecli: unmarshal reasoning part: %w", err)
	}
	if part.Text == "" {
		return nil, nil
	}
	return []*WorkerEvent{{
		Type: EventReasoning,
		Payload: &ReasoningPayload{
			Content:   part.Text,
			MessageID: part.ID,
		},
	}}, nil
}

func (p *Parser) parseToolUse(raw *RawEvent) ([]*WorkerEvent, error) {
	var part ToolPart
	if err := json.Unmarshal(raw.Part, &part); err != nil {
		return nil, fmt.Errorf("opencodecli: unmarshal tool_use part: %w", err)
	}
	// Emit a tool_call event when tool is invoked.
	return []*WorkerEvent{{
		Type: EventToolUse,
		Payload: &ToolCallPayload{
			ID:    part.CallID,
			Name:  part.Tool,
			Input: part.State.Input,
		},
	}}, nil
}

func (p *Parser) parseError(raw *RawEvent) ([]*WorkerEvent, error) {
	var errData SessionError
	if err := json.Unmarshal(raw.Error, &errData); err != nil {
		p.log.Warn("opencodecli: unmarshal error", "error", err)
		return []*WorkerEvent{{
			Type: EventError,
			Payload: &ResultPayload{
				Success: false,
				Error:   "unknown session error",
			},
		}}, nil
	}
	return []*WorkerEvent{{
		Type: EventError,
		Payload: &ResultPayload{
			Success: false,
			Error:   errData.Message,
		},
	}}, nil
}
