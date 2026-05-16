package opencodeserver

import (
	"encoding/json"
	"strings"

	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// OCS event type constants — used by Converter and singleton dispatch filter.
const (
	ocsStepStarted      = "session.next.step.started"
	ocsStepEnded        = "session.next.step.ended"
	ocsStepFailed       = "session.next.step.failed"
	ocsPartDelta        = "message.part.delta" // OCS 1.15+: unified text/reasoning delta
	ocsToolCalled       = "session.next.tool.called"
	ocsToolSuccess      = "session.next.tool.success"
	ocsToolFailed       = "session.next.tool.failed"
	ocsSessionStatus    = "session.status"
	ocsSessionIdle      = "session.idle"
	ocsSessionError     = "session.error"
	ocsReasoningStarted = "session.next.reasoning.started"
	ocsReasoningEnded   = "session.next.reasoning.ended"
	ocsPermAsked        = "permission.asked"
	ocsQuestionAsked    = "question.asked"
)

// Converter maps OCS BusEvents to AEP envelopes.
// It handles both V2 events (session.next.* prefix) and legacy V1 events
// (session.status, session.error, permission.asked, question.asked).
//
// Thread safety: Convert and Reset are NOT safe for concurrent use. They must
// only be called from the readGlobalSSE goroutine (which also calls
// dispatchToAllSubscribers). If future callers need concurrent access, add a
// mutex to Converter.
type Converter struct {
	states map[string]*turnState // sessionID → state
}

// turnState tracks per-session accumulation within a single turn.
// Reset when session.status(idle) fires or session.error occurs.
type turnState struct {
	cost            float64
	tokens          tokenAccum
	reasoningActive bool // true between reasoning.started and reasoning.ended
}

type tokenAccum struct {
	input, output, reasoning, cacheRead, cacheWrite int64
}

// NewConverter creates a Converter ready to use.
func NewConverter() *Converter {
	return &Converter{
		states: make(map[string]*turnState),
	}
}

// Convert maps an OCS BusEvent to zero or more AEP envelopes.
// eventType is payload.type, props is payload.properties (raw JSON).
func (c *Converter) Convert(sessionID, eventType string, props json.RawMessage) []*events.Envelope {
	switch {
	case eventType == ocsPartDelta:
		return c.handlePartDelta(sessionID, props)
	case strings.HasPrefix(eventType, "session.next."):
		return c.convertV2(sessionID, eventType, props)
	default:
		return c.convertV1(sessionID, eventType, props)
	}
}

// --- V2 event handlers ---

func (c *Converter) convertV2(sessionID, eventType string, props json.RawMessage) []*events.Envelope {
	switch eventType {
	case ocsStepStarted:
		return c.handleStepStarted(sessionID, props)
	case ocsStepEnded:
		return c.handleStepEnded(sessionID, props)
	case ocsStepFailed:
		return c.handleStepFailed(sessionID, props)
	case ocsToolCalled:
		return c.handleToolCalled(sessionID, props)
	case ocsToolSuccess:
		return c.handleToolOutcome(sessionID, props, false)
	case ocsToolFailed:
		return c.handleToolOutcome(sessionID, props, true)
	case ocsReasoningStarted:
		st := c.getOrCreateState(sessionID)
		st.reasoningActive = true
		return nil
	case ocsReasoningEnded:
		// Direct lookup: orphan ended without started is a no-op.
		if st, ok := c.states[sessionID]; ok {
			st.reasoningActive = false
		}
		return nil
	default:
		return nil
	}
}

func (c *Converter) handleStepStarted(_ string, _ json.RawMessage) []*events.Envelope {
	return nil
}

func (c *Converter) handleStepEnded(sessionID string, props json.RawMessage) []*events.Envelope {
	var evt struct {
		Cost   float64 `json:"cost"`
		Tokens struct {
			Input     float64 `json:"input"`
			Output    float64 `json:"output"`
			Reasoning float64 `json:"reasoning"`
			Cache     struct {
				Read  float64 `json:"read"`
				Write float64 `json:"write"`
			} `json:"cache"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(props, &evt); err != nil {
		return nil
	}

	st := c.getOrCreateState(sessionID)
	st.cost += evt.Cost
	st.tokens.input += int64(evt.Tokens.Input)
	st.tokens.output += int64(evt.Tokens.Output)
	st.tokens.reasoning += int64(evt.Tokens.Reasoning)
	st.tokens.cacheRead += int64(evt.Tokens.Cache.Read)
	st.tokens.cacheWrite += int64(evt.Tokens.Cache.Write)
	return nil
}

func (c *Converter) handleStepFailed(sessionID string, props json.RawMessage) []*events.Envelope {
	var evt struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(props, &evt)

	msg := "step failed"
	if evt.Error.Message != "" {
		msg = evt.Error.Message
	}
	return []*events.Envelope{
		events.NewEnvelope(aep.NewID(), sessionID, 0, events.Error, events.ErrorData{Message: msg}),
	}
}

// handlePartDelta handles message.part.delta from OCS 1.15+.
// Routes by field: "reasoning" → Reasoning, "text" → Reasoning (if in reasoning
// phase) or MessageDelta. The reasoning phase is bracketed by
// session.next.reasoning.started/ended events stored in turnState.
func (c *Converter) handlePartDelta(sessionID string, props json.RawMessage) []*events.Envelope {
	var evt struct {
		PartID string `json:"partID"`
		Field  string `json:"field"`
		Delta  string `json:"delta"`
	}
	if err := json.Unmarshal(props, &evt); err != nil {
		return nil
	}
	if evt.Delta == "" {
		return nil
	}

	switch evt.Field {
	case "reasoning":
		return []*events.Envelope{
			events.NewEnvelope(aep.NewID(), sessionID, 0, events.Reasoning, events.ReasoningData{
				ID:      evt.PartID,
				Content: evt.Delta,
			}),
		}
	default: // "text" or unspecified
		if st, ok := c.states[sessionID]; ok && st.reasoningActive {
			return []*events.Envelope{
				events.NewEnvelope(aep.NewID(), sessionID, 0, events.Reasoning, events.ReasoningData{
					ID:      evt.PartID,
					Content: evt.Delta,
				}),
			}
		}
		return []*events.Envelope{
			events.NewEnvelope(aep.NewID(), sessionID, 0, events.MessageDelta, events.MessageDeltaData{Content: evt.Delta}),
		}
	}
}

func (c *Converter) handleToolCalled(sessionID string, props json.RawMessage) []*events.Envelope {
	var evt struct {
		CallID string         `json:"callID"`
		Tool   string         `json:"tool"`
		Input  map[string]any `json:"input"`
	}
	if err := json.Unmarshal(props, &evt); err != nil {
		return nil
	}

	return []*events.Envelope{
		events.NewEnvelope(aep.NewID(), sessionID, 0, events.ToolCall, events.ToolCallData{
			ID:    evt.CallID,
			Name:  evt.Tool,
			Input: evt.Input,
		}),
	}
}

func (c *Converter) handleToolOutcome(sessionID string, props json.RawMessage, isFailed bool) []*events.Envelope {
	var evt struct {
		CallID  string `json:"callID"`
		Content []any  `json:"content,omitempty"`
		Error   *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.Unmarshal(props, &evt); err != nil {
		return nil
	}

	data := events.ToolResultData{ID: evt.CallID}
	if isFailed {
		data.Error = "tool failed"
		if evt.Error != nil {
			data.Error = evt.Error.Message
		}
	} else {
		data.Output = evt.Content
	}

	return []*events.Envelope{
		events.NewEnvelope(aep.NewID(), sessionID, 0, events.ToolResult, data),
	}
}

// --- V1 legacy handlers ---

func (c *Converter) convertV1(sessionID, eventType string, props json.RawMessage) []*events.Envelope {
	switch eventType {
	case ocsSessionStatus:
		return c.handleSessionStatus(sessionID, props)
	case ocsSessionIdle:
		return c.handleSessionIdle(sessionID)
	case ocsSessionError:
		return c.handleSessionError(sessionID, props)
	case ocsPermAsked:
		return []*events.Envelope{
			events.NewEnvelope(aep.NewID(), sessionID, 0, events.Raw,
				events.RawData{Kind: "ocs:permission.asked", Raw: props}),
		}
	case ocsQuestionAsked:
		return []*events.Envelope{
			events.NewEnvelope(aep.NewID(), sessionID, 0, events.Raw,
				events.RawData{Kind: "ocs:question.asked", Raw: props}),
		}
	default:
		return nil
	}
}

func (c *Converter) handleSessionStatus(sessionID string, props json.RawMessage) []*events.Envelope {
	var data struct {
		Status struct {
			Type string `json:"type"`
		} `json:"status"`
	}
	if err := json.Unmarshal(props, &data); err != nil {
		return nil
	}

	switch data.Status.Type {
	case "idle":
		stats := c.takeStats(sessionID)
		return []*events.Envelope{
			events.NewEnvelope(aep.NewID(), sessionID, 0, events.Done,
				events.DoneData{Success: true, Stats: stats}),
		}
	case "busy":
		return []*events.Envelope{
			events.NewEnvelope(aep.NewID(), sessionID, 0, events.State,
				map[string]any{"state": "running"}),
		}
	case "retry":
		return []*events.Envelope{
			events.NewEnvelope(aep.NewID(), sessionID, 0, events.State,
				map[string]any{"state": "retry"}),
		}
	default:
		return nil
	}
}

func (c *Converter) handleSessionIdle(sessionID string) []*events.Envelope {
	stats := c.takeStats(sessionID)
	return []*events.Envelope{
		events.NewEnvelope(aep.NewID(), sessionID, 0, events.Done,
			events.DoneData{Success: true, Stats: stats}),
	}
}

func (c *Converter) handleSessionError(sessionID string, props json.RawMessage) []*events.Envelope {
	var data struct {
		Error struct {
			Name string `json:"name"`
			Data struct {
				Message string `json:"message"`
			} `json:"data"`
		} `json:"error"`
	}
	_ = json.Unmarshal(props, &data)

	msg := "opencode session error"
	if data.Error.Data.Message != "" {
		msg = data.Error.Data.Message
	} else if data.Error.Name != "" {
		msg = data.Error.Name
	}
	delete(c.states, sessionID)
	return []*events.Envelope{
		events.NewEnvelope(aep.NewID(), sessionID, 0, events.Error, events.ErrorData{Message: msg}),
	}
}

// --- state helpers ---

// Reset clears all per-session turn state. Call when the OCS process restarts
// to prevent stale state from leaking into the new process lifecycle.
func (c *Converter) Reset() {
	clear(c.states)
}

func (c *Converter) getOrCreateState(sessionID string) *turnState {
	st, ok := c.states[sessionID]
	if !ok {
		st = &turnState{}
		c.states[sessionID] = st
	}
	return st
}

// takeStats returns accumulated usage as a Stats map for DoneData and clears the entry.
// Returns nil if no usage was recorded.
func (c *Converter) takeStats(sessionID string) map[string]any {
	st, ok := c.states[sessionID]
	if !ok {
		return nil
	}
	delete(c.states, sessionID)

	if st.cost == 0 && st.tokens == (tokenAccum{}) {
		return nil
	}
	return map[string]any{
		"tokens": map[string]any{
			"input":       st.tokens.input,
			"output":      st.tokens.output,
			"reasoning":   st.tokens.reasoning,
			"cache_read":  st.tokens.cacheRead,
			"cache_write": st.tokens.cacheWrite,
		},
		"cost": st.cost,
	}
}
