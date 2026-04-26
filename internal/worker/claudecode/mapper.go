package claudecode

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"

	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// Mapper converts WorkerEvents to AEP envelopes.
type Mapper struct {
	log       *slog.Logger
	sessionID string
	seqGen    func() int64 // Sequence generator (provided by Hub)
}

// NewMapper creates a new Mapper instance.
func NewMapper(log *slog.Logger, sessionID string, seqGen func() int64) *Mapper {
	return &Mapper{
		log:       log,
		sessionID: sessionID,
		seqGen:    seqGen,
	}
}

// statusToSessionState maps Claude Code status strings to AEP session states.
// Returns ok=false for unknown status values.
func statusToSessionState(s string) (events.SessionState, bool) {
	switch s {
	case "idle":
		return events.StateIdle, true
	case "processing":
		return events.StateRunning, true
	default:
		return "", false
	}
}

// Map converts a WorkerEvent to one or more AEP envelopes.
// Returns nil slice for internal events that should not be sent to client.
func (m *Mapper) Map(evt *WorkerEvent) ([]*events.Envelope, error) {
	switch evt.Type {
	case EventStream:
		payload, ok := evt.Payload.(*StreamPayload)
		if !ok {
			return nil, fmt.Errorf("mapper: stream payload is not *StreamPayload: %T", evt.Payload)
		}
		env, err := m.mapStream(payload)
		if err != nil {
			return nil, err
		}
		return []*events.Envelope{env}, nil
	case EventAssistant:
		switch p := evt.Payload.(type) {
		case *StreamPayload:
			env, err := m.mapStream(p)
			if err != nil {
				return nil, err
			}
			return []*events.Envelope{env}, nil
		case *ToolCallPayload:
			env, err := m.mapToolCall(p)
			if err != nil {
				return nil, err
			}
			return []*events.Envelope{env}, nil
		default:
			return nil, fmt.Errorf("mapper: unknown assistant payload type: %T", p)
		}
	case EventToolProgress:
		payload, ok := evt.Payload.(*ToolResultPayload)
		if !ok {
			return nil, fmt.Errorf("mapper: tool_progress payload is not *ToolResultPayload: %T", evt.Payload)
		}
		env, err := m.mapToolProgress(payload)
		if err != nil {
			return nil, err
		}
		return []*events.Envelope{env}, nil
	case EventResult:
		payload, ok := evt.Payload.(*ResultPayload)
		if !ok {
			return nil, fmt.Errorf("mapper: result payload is not *ResultPayload: %T", evt.Payload)
		}
		return m.mapResult(payload)
	case EventSystem:
		raw, ok := evt.Payload.(json.RawMessage)
		if !ok {
			return nil, fmt.Errorf("mapper: system payload is not json.RawMessage: %T", evt.Payload)
		}
		return m.mapRawStringPayload(raw, m.mapSystem)
	case EventSessionState:
		raw, ok := evt.Payload.(json.RawMessage)
		if !ok {
			return nil, fmt.Errorf("mapper: session_state payload is not json.RawMessage: %T", evt.Payload)
		}
		return m.mapRawStringPayload(raw, m.mapSessionState)
	case EventToolResult:
		// Signal-only: tool completed. Emit lightweight tool_result for activity feedback.
		return []*events.Envelope{events.NewEnvelope(
			aep.NewID(),
			m.sessionID,
			m.seqGen(),
			events.ToolResult,
			events.ToolResultData{},
		)}, nil
	default:
		return nil, fmt.Errorf("mapper: unknown event type: %v", evt.Type)
	}
}

// mapRawStringPayload unmarshal a json.RawMessage payload to string and delegates
// to the given map function. Returns nil for non-string payloads (JSON objects).
func (m *Mapper) mapRawStringPayload(raw json.RawMessage, mapFn func(string) (*events.Envelope, error)) ([]*events.Envelope, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, nil
	}
	env, err := mapFn(s)
	if err != nil {
		return nil, err
	}
	if env == nil {
		return nil, nil
	}
	return []*events.Envelope{env}, nil
}

// mapStream converts a stream_event to an AEP envelope.
// thinking → events.Reasoning; all other types → events.MessageDelta.
func (m *Mapper) mapStream(p *StreamPayload) (*events.Envelope, error) { //nolint:unparam // consistent mapper API
	if p.Type == "thinking" {
		return events.NewEnvelope(
			aep.NewID(),
			m.sessionID,
			m.seqGen(),
			events.Reasoning,
			events.ReasoningData{
				ID:      p.MessageID,
				Content: p.Content,
			},
		), nil
	}

	return events.NewEnvelope(
		aep.NewID(),
		m.sessionID,
		m.seqGen(),
		events.MessageDelta,
		events.MessageDeltaData{
			MessageID: p.MessageID,
			Content:   p.Content,
		},
	), nil
}

// mapToolCall converts tool_use to tool_call event.
func (m *Mapper) mapToolCall(p *ToolCallPayload) (*events.Envelope, error) { //nolint:unparam // consistent mapper API
	return events.NewEnvelope(
		aep.NewID(),
		m.sessionID,
		m.seqGen(),
		events.ToolCall,
		events.ToolCallData{
			ID:    p.ID,
			Name:  p.Name,
			Input: p.Input,
		},
	), nil
}

// mapToolProgress converts tool_progress to tool_result.
func (m *Mapper) mapToolProgress(p *ToolResultPayload) (*events.Envelope, error) { //nolint:unparam // consistent mapper API
	return events.NewEnvelope(
		aep.NewID(),
		m.sessionID,
		m.seqGen(),
		events.ToolResult,
		events.ToolResultData{
			ID:     p.ToolUseID,
			Output: p.Output,
			Error:  p.Error,
		},
	), nil
}

// mapResult converts result to done (+ optional error).
// Merges Usage and ModelUsage into DoneData.Stats so downstream consumers
// (Bridge accumulator, platform adapters) can extract token/cost/context data.
func (m *Mapper) mapResult(p *ResultPayload) ([]*events.Envelope, error) {
	stats := make(map[string]any, len(p.Stats)+2)
	maps.Copy(stats, p.Stats)
	if p.Usage != nil {
		stats["usage"] = p.Usage
	}
	if p.ModelUsage != nil {
		stats["model_usage"] = p.ModelUsage
	}

	if !p.Success {
		return []*events.Envelope{
			{
				ID:        aep.NewID(),
				SessionID: m.sessionID,
				Seq:       m.seqGen(),
				Event: events.Event{
					Type: events.Error,
					Data: events.ErrorData{
						Code:    events.ErrCodeInternalError,
						Message: p.Message,
					},
				},
			},
			{
				ID:        aep.NewID(),
				SessionID: m.sessionID,
				Seq:       m.seqGen(),
				Event: events.Event{
					Type: events.Done,
					Data: events.DoneData{
						Success: false,
						Stats:   stats,
					},
				},
			},
		}, nil
	}

	return []*events.Envelope{{
		ID:        aep.NewID(),
		SessionID: m.sessionID,
		Seq:       m.seqGen(),
		Event: events.Event{
			Type: events.Done,
			Data: events.DoneData{
				Success: true,
				Stats:   stats,
			},
		},
	}}, nil
}

// mapSystem converts system status to state event.
func (m *Mapper) mapSystem(status string) (*events.Envelope, error) { //nolint:unparam // consistent mapper API
	state, ok := statusToSessionState(status)
	if !ok {
		return nil, nil
	}

	return events.NewEnvelope(
		aep.NewID(),
		m.sessionID,
		m.seqGen(),
		events.State,
		events.StateData{
			State: state,
		},
	), nil
}

// mapSessionState converts session_state_changed to state event.
func (m *Mapper) mapSessionState(stateStr string) (*events.Envelope, error) { //nolint:unparam // consistent mapper API
	state, ok := statusToSessionState(stateStr)
	if !ok {
		return nil, nil
	}

	return events.NewEnvelope(
		aep.NewID(),
		m.sessionID,
		m.seqGen(),
		events.State,
		events.StateData{
			State: state,
		},
	), nil
}

func mapContextUsageResponse(raw map[string]any) *events.ContextUsageData {
	return events.MapContextUsageResponse(raw)
}

func mapMCPStatusResponse(raw map[string]any) *events.MCPStatusData {
	return events.MapMCPStatusResponse(raw)
}
