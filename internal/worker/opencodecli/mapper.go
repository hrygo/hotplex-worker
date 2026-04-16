package opencodecli

import (
	"fmt"
	"log/slog"
	"sync"

	"github.com/hotplex/hotplex-worker/pkg/aep"
	"github.com/hotplex/hotplex-worker/pkg/events"
)

// Mapper converts WorkerEvents to AEP envelopes.
type Mapper struct {
	log       *slog.Logger
	sessionID string
	seqGen    func() int64
	mu        sync.RWMutex
}

// NewMapper creates a new Mapper.
func NewMapper(log *slog.Logger, sessionID string, seqGen func() int64) *Mapper {
	return &Mapper{
		log:       log,
		sessionID: sessionID,
		seqGen:    seqGen,
	}
}

// UpdateSessionID updates the session ID used for outgoing envelopes.
func (m *Mapper) UpdateSessionID(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessionID = id
}

// SessionID returns the current session ID.
func (m *Mapper) SessionID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessionID
}

// Map converts a WorkerEvent to zero or more AEP envelopes.
// Returns nil slice for events that should not be sent to the client.
func (m *Mapper) Map(evt *WorkerEvent) ([]*events.Envelope, error) {
	switch evt.Type {
	case EventText:
		payload, ok := evt.Payload.(*TextPayload)
		if !ok {
			return nil, fmt.Errorf("opencodecli: text payload is not *TextPayload: %T", evt.Payload)
		}
		return []*events.Envelope{m.mapText(payload)}, nil

	case EventReasoning:
		payload, ok := evt.Payload.(*ReasoningPayload)
		if !ok {
			return nil, fmt.Errorf("opencodecli: reasoning payload is not *ReasoningPayload: %T", evt.Payload)
		}
		return []*events.Envelope{m.mapReasoning(payload)}, nil

	case EventToolUse:
		payload, ok := evt.Payload.(*ToolCallPayload)
		if !ok {
			return nil, fmt.Errorf("opencodecli: tool_use payload is not *ToolCallPayload: %T", evt.Payload)
		}
		return []*events.Envelope{m.mapToolCall(payload)}, nil

	default:
		return nil, nil
	}
}

func (m *Mapper) seq() int64 {
	if m.seqGen != nil {
		return m.seqGen()
	}
	return 0 // fallback: no seq gen registered
}

func (m *Mapper) newID() string {
	return aep.NewID()
}

func (m *Mapper) mapText(p *TextPayload) *events.Envelope {
	return events.NewEnvelope(
		m.newID(),
		m.SessionID(),
		m.seq(),
		events.MessageDelta,
		events.MessageDeltaData{
			MessageID: p.MessageID,
			Content:   p.Content,
		},
	)
}

func (m *Mapper) mapReasoning(p *ReasoningPayload) *events.Envelope {
	return events.NewEnvelope(
		m.newID(),
		m.SessionID(),
		m.seq(),
		events.Reasoning,
		events.ReasoningData{
			ID:      p.MessageID,
			Content: p.Content,
		},
	)
}

func (m *Mapper) mapToolCall(p *ToolCallPayload) *events.Envelope {
	return events.NewEnvelope(
		m.newID(),
		m.SessionID(),
		m.seq(),
		events.ToolCall,
		events.ToolCallData{
			ID:    p.ID,
			Name:  p.Name,
			Input: p.Input,
		},
	)
}
