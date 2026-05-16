package opencodeserver

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

func newTestConverter() *Converter {
	return NewConverter()
}

func rawProps(t *testing.T, v map[string]any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

// ─── V2 Step Events ───────────────────────────────────────────────────────────

func TestConverter_StepStarted_NoOutput(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{
		"model": map[string]any{
			"providerID": "anthropic",
			"modelID":    "claude-sonnet-4-6",
		},
	})
	envs := c.Convert("s1", ocsStepStarted, props)
	require.Empty(t, envs)
}

func TestConverter_StepEnded_Accumulates(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{
		"cost": 0.003,
		"tokens": map[string]any{
			"input":     1500,
			"output":    200,
			"reasoning": 50,
			"cache":     map[string]any{"read": 300, "write": 100},
		},
	})
	envs := c.Convert("s1", ocsStepEnded, props)
	require.Empty(t, envs)

	st := c.states["s1"]
	require.InDelta(t, 0.003, st.cost, 0.0001)
	require.Equal(t, int64(1500), st.tokens.input)
	require.Equal(t, int64(200), st.tokens.output)
	require.Equal(t, int64(50), st.tokens.reasoning)
	require.Equal(t, int64(300), st.tokens.cacheRead)
	require.Equal(t, int64(100), st.tokens.cacheWrite)
}

func TestConverter_StepEnded_MultipleAccumulates(t *testing.T) {
	c := newTestConverter()
	p1 := rawProps(t, map[string]any{
		"cost": 0.001,
		"tokens": map[string]any{
			"input": 500, "output": 50, "reasoning": 0,
			"cache": map[string]any{"read": 100, "write": 0},
		},
	})
	p2 := rawProps(t, map[string]any{
		"cost": 0.002,
		"tokens": map[string]any{
			"input": 800, "output": 100, "reasoning": 30,
			"cache": map[string]any{"read": 200, "write": 50},
		},
	})
	c.Convert("s1", ocsStepEnded, p1)
	c.Convert("s1", ocsStepEnded, p2)

	st := c.states["s1"]
	require.InDelta(t, 0.003, st.cost, 0.0001)
	require.Equal(t, int64(1300), st.tokens.input)
	require.Equal(t, int64(150), st.tokens.output)
}

func TestConverter_StepFailed(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{
		"error": map[string]any{"message": "API timeout"},
	})
	envs := c.Convert("s1", ocsStepFailed, props)
	require.Len(t, envs, 1)
	require.Equal(t, events.Error, envs[0].Event.Type)
	require.Equal(t, "API timeout", envs[0].Event.Data.(events.ErrorData).Message)
}

// ─── message.part.delta (OCS 1.15+) ──────────────────────────────────────────

func TestConverter_PartDelta_Text(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{
		"partID": "p1",
		"field":  "text",
		"delta":  "Hel",
	})
	envs := c.Convert("s1", ocsPartDelta, props)
	require.Len(t, envs, 1)
	require.Equal(t, events.MessageDelta, envs[0].Event.Type)
	require.Equal(t, "Hel", envs[0].Event.Data.(events.MessageDeltaData).Content)
}

func TestConverter_PartDelta_Text_Empty(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{
		"partID": "p1",
		"field":  "text",
		"delta":  "",
	})
	envs := c.Convert("s1", ocsPartDelta, props)
	require.Empty(t, envs)
}

func TestConverter_PartDelta_Reasoning(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{
		"partID": "p1",
		"field":  "reasoning",
		"delta":  "thinking...",
	})
	envs := c.Convert("s1", ocsPartDelta, props)
	require.Len(t, envs, 1)
	require.Equal(t, events.Reasoning, envs[0].Event.Type)
	require.Equal(t, "p1", envs[0].Event.Data.(events.ReasoningData).ID)
	require.Equal(t, "thinking...", envs[0].Event.Data.(events.ReasoningData).Content)
}

func TestConverter_PartDelta_DefaultField(t *testing.T) {
	c := newTestConverter()
	// Missing field → treated as text
	props := rawProps(t, map[string]any{
		"partID": "p1",
		"delta":  "hello",
	})
	envs := c.Convert("s1", ocsPartDelta, props)
	require.Len(t, envs, 1)
	require.Equal(t, events.MessageDelta, envs[0].Event.Type)
}

func TestConverter_PartDelta_Reasoning_Empty(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{
		"partID": "p1",
		"field":  "reasoning",
		"delta":  "",
	})
	envs := c.Convert("s1", ocsPartDelta, props)
	require.Empty(t, envs)
}

// ─── V2 Tool Events ───────────────────────────────────────────────────────────

func TestConverter_ToolCalled(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{
		"callID": "tc1",
		"tool":   "Read",
		"input":  map[string]any{"path": "/tmp/x"},
	})
	envs := c.Convert("s1", ocsToolCalled, props)
	require.Len(t, envs, 1)
	require.Equal(t, events.ToolCall, envs[0].Event.Type)
	tc := envs[0].Event.Data.(events.ToolCallData)
	require.Equal(t, "tc1", tc.ID)
	require.Equal(t, "Read", tc.Name)
	require.Equal(t, "/tmp/x", tc.Input["path"])
}

func TestConverter_ToolSuccess(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{
		"callID":  "tc1",
		"content": []any{"file contents here"},
	})
	envs := c.Convert("s1", ocsToolSuccess, props)
	require.Len(t, envs, 1)
	require.Equal(t, events.ToolResult, envs[0].Event.Type)
	tr := envs[0].Event.Data.(events.ToolResultData)
	require.Equal(t, "tc1", tr.ID)
	require.Empty(t, tr.Error)
}

func TestConverter_ToolFailed(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{
		"callID": "tc1",
		"error":  map[string]any{"message": "file not found"},
	})
	envs := c.Convert("s1", ocsToolFailed, props)
	require.Len(t, envs, 1)
	require.Equal(t, events.ToolResult, envs[0].Event.Type)
	tr := envs[0].Event.Data.(events.ToolResultData)
	require.Equal(t, "tc1", tr.ID)
	require.Equal(t, "file not found", tr.Error)
}

// ─── V2 Unknown Event ─────────────────────────────────────────────────────────

func TestConverter_V2UnknownEvent(t *testing.T) {
	c := newTestConverter()
	envs := c.Convert("s1", "session.next.text.started", rawProps(t, nil))
	require.Empty(t, envs)
}

// ─── V1 Legacy Events ─────────────────────────────────────────────────────────

func TestConverter_SessionStatus_Idle(t *testing.T) {
	c := newTestConverter()
	// Accumulate usage first
	c.Convert("s1", ocsStepEnded, rawProps(t, map[string]any{
		"cost":   0.005,
		"tokens": map[string]any{"input": 2000, "output": 300, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}},
	}))

	props := rawProps(t, map[string]any{
		"status": map[string]any{"type": "idle"},
	})
	envs := c.Convert("s1", ocsSessionStatus, props)
	require.Len(t, envs, 1)
	require.Equal(t, events.Done, envs[0].Event.Type)

	dd, ok := envs[0].Event.Data.(events.DoneData)
	require.True(t, ok)
	require.True(t, dd.Success)
	require.NotNil(t, dd.Stats)

	tokens := dd.Stats["tokens"].(map[string]any)
	require.Equal(t, int64(2000), tokens["input"])
	require.Equal(t, int64(300), tokens["output"])
	require.InDelta(t, 0.005, dd.Stats["cost"], 0.0001)

	// Usage cleared after take
	_, exists := c.states["s1"]
	require.False(t, exists)
}

func TestConverter_SessionStatus_Idle_NoUsage(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{"status": map[string]any{"type": "idle"}})
	envs := c.Convert("s1", ocsSessionStatus, props)
	require.Len(t, envs, 1)
	require.Equal(t, events.Done, envs[0].Event.Type)
	dd := envs[0].Event.Data.(events.DoneData)
	require.Nil(t, dd.Stats, "no usage → Stats is nil")
}

func TestConverter_SessionStatus_Busy(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{"status": map[string]any{"type": "busy"}})
	envs := c.Convert("s1", ocsSessionStatus, props)
	require.Len(t, envs, 1)
	require.Equal(t, events.State, envs[0].Event.Type)
}

func TestConverter_SessionStatus_Retry(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{"status": map[string]any{"type": "retry"}})
	envs := c.Convert("s1", ocsSessionStatus, props)
	require.Len(t, envs, 1)
	require.Equal(t, events.State, envs[0].Event.Type)
}

func TestConverter_SessionIdle(t *testing.T) {
	c := newTestConverter()
	envs := c.Convert("s1", ocsSessionIdle, nil)
	require.Len(t, envs, 1)
	require.Equal(t, events.Done, envs[0].Event.Type)
	dd := envs[0].Event.Data.(events.DoneData)
	require.True(t, dd.Success)
	require.Nil(t, dd.Stats, "no usage accumulated → Stats is nil")
}

func TestConverter_SessionIdle_WithUsage(t *testing.T) {
	c := newTestConverter()
	c.Convert("s1", ocsStepEnded, rawProps(t, map[string]any{
		"cost": 0.01, "tokens": map[string]any{"input": 1000, "output": 100, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}},
	}))
	envs := c.Convert("s1", ocsSessionIdle, nil)
	require.Len(t, envs, 1)
	dd := envs[0].Event.Data.(events.DoneData)
	require.NotNil(t, dd.Stats)
	require.Equal(t, int64(1000), dd.Stats["tokens"].(map[string]any)["input"])
}

func TestConverter_SessionError(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{
		"error": map[string]any{"name": "APIError", "data": map[string]any{"message": "rate limited"}},
	})
	envs := c.Convert("s1", ocsSessionError, props)
	require.Len(t, envs, 1)
	require.Equal(t, events.Error, envs[0].Event.Type)
	require.Equal(t, "rate limited", envs[0].Event.Data.(events.ErrorData).Message)
	_, exists := c.states["s1"]
	require.False(t, exists, "session.error should clear state")
}

func TestConverter_SessionError_NameOnly(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{
		"error": map[string]any{"name": "TimeoutError"},
	})
	envs := c.Convert("s1", ocsSessionError, props)
	require.Len(t, envs, 1)
	require.Equal(t, "TimeoutError", envs[0].Event.Data.(events.ErrorData).Message)
}

func TestConverter_PermissionAsked(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{"id": "p1", "metadata": map[string]any{"tool": "bash"}})
	envs := c.Convert("s1", ocsPermAsked, props)
	require.Len(t, envs, 1)
	require.Equal(t, events.Raw, envs[0].Event.Type)
	data := envs[0].Event.Data.(events.RawData)
	require.Equal(t, "ocs:permission.asked", data.Kind)
}

func TestConverter_QuestionAsked(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{"id": "q1"})
	envs := c.Convert("s1", ocsQuestionAsked, props)
	require.Len(t, envs, 1)
	require.Equal(t, events.Raw, envs[0].Event.Type)
	data := envs[0].Event.Data.(events.RawData)
	require.Equal(t, "ocs:question.asked", data.Kind)
}

func TestConverter_V1UnknownEvent(t *testing.T) {
	c := newTestConverter()
	envs := c.Convert("s1", "some.unknown.event", rawProps(t, nil))
	require.Empty(t, envs)
}

// ─── Full Turn Lifecycle ──────────────────────────────────────────────────────

func TestConverter_FullTurnLifecycle(t *testing.T) {
	c := newTestConverter()
	sid := "ses-lifecycle"

	// Step 1: busy
	envs := c.Convert(sid, ocsSessionStatus, rawProps(t, map[string]any{"status": map[string]any{"type": "busy"}}))
	require.Len(t, envs, 1)
	require.Equal(t, events.State, envs[0].Event.Type)

	// Step 2: step started
	envs = c.Convert(sid, ocsStepStarted, rawProps(t, map[string]any{
		"model": map[string]any{"providerID": "anthropic", "modelID": "claude-sonnet-4-6"},
	}))
	require.Empty(t, envs)

	// Step 3: text delta
	envs = c.Convert(sid, ocsPartDelta, rawProps(t, map[string]any{"partID": "p1", "field": "text", "delta": "Hello"}))
	require.Len(t, envs, 1)
	require.Equal(t, events.MessageDelta, envs[0].Event.Type)

	// Step 4: tool called
	envs = c.Convert(sid, ocsToolCalled, rawProps(t, map[string]any{
		"callID": "tc1", "tool": "Bash", "input": map[string]any{"cmd": "ls"},
	}))
	require.Len(t, envs, 1)
	require.Equal(t, events.ToolCall, envs[0].Event.Type)

	// Step 5: tool success
	envs = c.Convert(sid, ocsToolSuccess, rawProps(t, map[string]any{
		"callID": "tc1", "content": []any{"file1.go\nfile2.go"},
	}))
	require.Len(t, envs, 1)
	require.Equal(t, events.ToolResult, envs[0].Event.Type)

	// Step 6: step ended
	envs = c.Convert(sid, ocsStepEnded, rawProps(t, map[string]any{
		"cost": 0.02, "tokens": map[string]any{"input": 5000, "output": 600, "reasoning": 100, "cache": map[string]any{"read": 2000, "write": 500}},
	}))
	require.Empty(t, envs)

	// Step 7: more text
	envs = c.Convert(sid, ocsPartDelta, rawProps(t, map[string]any{"partID": "p1", "field": "text", "delta": "Done!"}))
	require.Len(t, envs, 1)
	require.Equal(t, events.MessageDelta, envs[0].Event.Type)

	// Step 8: idle → Done with stats
	envs = c.Convert(sid, ocsSessionStatus, rawProps(t, map[string]any{"status": map[string]any{"type": "idle"}}))
	require.Len(t, envs, 1)
	require.Equal(t, events.Done, envs[0].Event.Type)

	dd := envs[0].Event.Data.(events.DoneData)
	require.True(t, dd.Success)
	require.NotNil(t, dd.Stats)

	tokens := dd.Stats["tokens"].(map[string]any)
	require.Equal(t, int64(5000), tokens["input"])
	require.Equal(t, int64(600), tokens["output"])
	require.Equal(t, int64(100), tokens["reasoning"])
	require.Equal(t, int64(2000), tokens["cache_read"])
	require.Equal(t, int64(500), tokens["cache_write"])
	require.InDelta(t, 0.02, dd.Stats["cost"], 0.0001)

	// State cleared
	_, exists := c.states[sid]
	require.False(t, exists)
}

// ─── Review Fix Tests ─────────────────────────────────────────────────────────

func TestConverter_ToolFailed_NilError(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{
		"callID": "tc1",
	})
	envs := c.Convert("s1", ocsToolFailed, props)
	require.Len(t, envs, 1)
	require.Equal(t, events.ToolResult, envs[0].Event.Type)
	tr := envs[0].Event.Data.(events.ToolResultData)
	require.Equal(t, "tc1", tr.ID)
	require.Equal(t, "tool failed", tr.Error)
	require.Nil(t, tr.Output)
}

func TestConverter_ToolFailed_WithErrorMessage(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{
		"callID": "tc1",
		"error":  map[string]any{"message": "permission denied"},
	})
	envs := c.Convert("s1", ocsToolFailed, props)
	require.Len(t, envs, 1)
	tr := envs[0].Event.Data.(events.ToolResultData)
	require.Equal(t, "permission denied", tr.Error)
}

func TestConverter_MalformedJSON(t *testing.T) {
	c := newTestConverter()
	bad := json.RawMessage(`{invalid json`)
	for _, eventType := range []string{
		ocsStepStarted,
		ocsStepEnded,
		ocsPartDelta,
		ocsToolCalled,
		ocsToolSuccess,
		ocsToolFailed,
		ocsSessionStatus,
	} {
		envs := c.Convert("s1", eventType, bad)
		require.Empty(t, envs, "malformed JSON should produce nil for %s", eventType)
	}

	// step.failed and session.error always emit Error (use default message on bad JSON)
	for _, eventType := range []string{
		ocsStepFailed,
		ocsSessionError,
	} {
		envs := c.Convert("s1", eventType, bad)
		require.Len(t, envs, 1, "%s should emit Error even on malformed JSON", eventType)
		require.Equal(t, events.Error, envs[0].Event.Type)
	}
}

func TestConverter_InterleavedSessions(t *testing.T) {
	c := newTestConverter()
	s1, s2 := "session-a", "session-b"

	c.Convert(s1, ocsStepEnded, rawProps(t, map[string]any{
		"cost": 0.01, "tokens": map[string]any{"input": 500, "output": 50, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}},
	}))
	c.Convert(s2, ocsStepEnded, rawProps(t, map[string]any{
		"cost": 0.05, "tokens": map[string]any{"input": 2000, "output": 200, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}},
	}))

	envs := c.Convert(s1, ocsSessionStatus, rawProps(t, map[string]any{"status": map[string]any{"type": "idle"}}))
	require.Len(t, envs, 1)
	dd := envs[0].Event.Data.(events.DoneData)
	require.InDelta(t, 0.01, dd.Stats["cost"], 0.0001)

	_, exists := c.states[s1]
	require.False(t, exists)
	require.NotNil(t, c.states[s2])

	envs = c.Convert(s2, ocsSessionStatus, rawProps(t, map[string]any{"status": map[string]any{"type": "idle"}}))
	dd = envs[0].Event.Data.(events.DoneData)
	require.InDelta(t, 0.05, dd.Stats["cost"], 0.0001)
}

func TestConverter_DualDone_IdleFirst(t *testing.T) {
	c := newTestConverter()
	sid := "ses-dual"

	c.Convert(sid, ocsStepEnded, rawProps(t, map[string]any{
		"cost": 0.01, "tokens": map[string]any{"input": 500, "output": 50, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}},
	}))

	envs := c.Convert(sid, ocsSessionStatus, rawProps(t, map[string]any{"status": map[string]any{"type": "idle"}}))
	require.Len(t, envs, 1)
	require.Equal(t, events.Done, envs[0].Event.Type)

	envs = c.Convert(sid, ocsSessionIdle, nil)
	require.Len(t, envs, 1)
	require.Equal(t, events.Done, envs[0].Event.Type)
	dd := envs[0].Event.Data.(events.DoneData)
	require.Nil(t, dd.Stats, "state already cleared → Stats is nil")
}

func TestConverter_Reset(t *testing.T) {
	c := newTestConverter()
	c.Convert("s1", ocsStepEnded, rawProps(t, map[string]any{
		"cost": 0.01, "tokens": map[string]any{"input": 500, "output": 50, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}},
	}))
	require.NotNil(t, c.states["s1"])

	c.Reset()
	_, exists := c.states["s1"]
	require.False(t, exists, "Reset should clear all state")

	envs := c.Convert("s2", ocsPartDelta, rawProps(t, map[string]any{"partID": "p1", "field": "text", "delta": "hello"}))
	require.Len(t, envs, 1)
}
