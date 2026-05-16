package opencodeserver

import (
	"encoding/json"
	"strings"

	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
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
// Reset when session.status(idle) fires.
type turnState struct {
	model     string
	steps     int
	cost      float64
	tokens    tokenAccum
	toolNames map[string]int
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
	// V2 events: session.next.* prefix
	if strings.HasPrefix(eventType, "session.next.") {
		return c.convertV2(sessionID, eventType, props)
	}
	// Legacy V1 events
	return c.convertV1(sessionID, eventType, props)
}

// --- V2 event handlers ---

func (c *Converter) convertV2(sessionID, eventType string, props json.RawMessage) []*events.Envelope {
	switch eventType {
	case "session.next.step.started":
		return c.handleStepStarted(sessionID, props)
	case "session.next.step.ended":
		return c.handleStepEnded(sessionID, props)
	case "session.next.step.failed":
		return c.handleStepFailed(sessionID, props)
	case "session.next.text.delta":
		return c.handleTextDelta(sessionID, props)
	case "session.next.reasoning.delta":
		return c.handleReasoningDelta(sessionID, props)
	case "session.next.reasoning.ended":
		return c.handleReasoningEnded(sessionID, props)
	case "session.next.tool.called":
		return c.handleToolCalled(sessionID, props)
	case "session.next.tool.success":
		return c.handleToolOutcome(sessionID, props, false)
	case "session.next.tool.failed":
		return c.handleToolOutcome(sessionID, props, true)
	default:
		return nil
	}
}

func (c *Converter) handleStepStarted(sessionID string, props json.RawMessage) []*events.Envelope {
	var evt struct {
		Model struct {
			ProviderID string `json:"providerID"`
			ModelID    string `json:"modelID"`
			Variant    string `json:"variant,omitempty"`
		} `json:"model"`
	}
	if err := json.Unmarshal(props, &evt); err != nil {
		return nil
	}

	st := c.getOrCreateState(sessionID)
	st.model = evt.Model.ModelID
	st.steps++
	return nil // no AEP output, just state update
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
	return nil // no AEP output, just state update
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

func (c *Converter) handleTextDelta(sessionID string, props json.RawMessage) []*events.Envelope {
	var evt struct {
		Delta string `json:"delta"`
	}
	if err := json.Unmarshal(props, &evt); err != nil {
		return nil
	}
	if evt.Delta == "" {
		return nil
	}
	return []*events.Envelope{
		events.NewEnvelope(aep.NewID(), sessionID, 0, events.MessageDelta, events.MessageDeltaData{Content: evt.Delta}),
	}
}

func (c *Converter) handleReasoningDelta(sessionID string, props json.RawMessage) []*events.Envelope {
	var evt struct {
		ReasoningID string `json:"reasoningID"`
		Delta       string `json:"delta"`
	}
	if err := json.Unmarshal(props, &evt); err != nil {
		return nil
	}
	if evt.Delta == "" {
		return nil
	}
	return []*events.Envelope{
		events.NewEnvelope(aep.NewID(), sessionID, 0, events.Reasoning, events.ReasoningData{
			ID:      evt.ReasoningID,
			Content: evt.Delta,
		}),
	}
}

func (c *Converter) handleReasoningEnded(sessionID string, props json.RawMessage) []*events.Envelope {
	var evt struct {
		ReasoningID string `json:"reasoningID"`
		Text        string `json:"text"`
	}
	if err := json.Unmarshal(props, &evt); err != nil {
		return nil
	}
	if evt.Text == "" {
		return nil
	}
	return []*events.Envelope{
		events.NewEnvelope(aep.NewID(), sessionID, 0, events.Reasoning, events.ReasoningData{
			ID:      evt.ReasoningID,
			Content: evt.Text,
		}),
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

	st := c.getOrCreateState(sessionID)
	st.toolNames[evt.Tool]++

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
	case "session.status":
		return c.handleSessionStatus(sessionID, props)
	case "session.idle":
		return c.handleSessionIdle(sessionID)
	case "session.error":
		return c.handleSessionError(sessionID, props)
	case "permission.asked":
		return []*events.Envelope{
			events.NewEnvelope(aep.NewID(), sessionID, 0, events.Raw,
				events.RawData{Kind: "ocs:permission.asked", Raw: props}),
		}
	case "question.asked":
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
	if stats == nil {
		return nil
	}
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
		st = &turnState{toolNames: make(map[string]int)}
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
