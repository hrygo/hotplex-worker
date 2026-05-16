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

func TestConverter_StepStarted_UpdatesModel(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{
		"model": map[string]any{
			"providerID": "anthropic",
			"modelID":    "claude-sonnet-4-6",
		},
	})
	envs := c.Convert("s1", "session.next.step.started", props)
	require.Empty(t, envs, "step.started produces no AEP output")
	require.Equal(t, "claude-sonnet-4-6", c.states["s1"].model)
	require.Equal(t, 1, c.states["s1"].steps)
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
	envs := c.Convert("s1", "session.next.step.ended", props)
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
	c.Convert("s1", "session.next.step.ended", p1)
	c.Convert("s1", "session.next.step.ended", p2)

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
	envs := c.Convert("s1", "session.next.step.failed", props)
	require.Len(t, envs, 1)
	require.Equal(t, events.Error, envs[0].Event.Type)
	require.Equal(t, "API timeout", envs[0].Event.Data.(events.ErrorData).Message)
}

// ─── V2 Text ──────────────────────────────────────────────────────────────────

func TestConverter_TextDelta(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{"delta": "Hel"})
	envs := c.Convert("s1", "session.next.text.delta", props)
	require.Len(t, envs, 1)
	require.Equal(t, events.MessageDelta, envs[0].Event.Type)
	require.Equal(t, "Hel", envs[0].Event.Data.(events.MessageDeltaData).Content)
}

func TestConverter_TextDelta_Empty(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{"delta": ""})
	envs := c.Convert("s1", "session.next.text.delta", props)
	require.Empty(t, envs)
}

// ─── V2 Reasoning ─────────────────────────────────────────────────────────────

func TestConverter_ReasoningDelta(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{
		"reasoningID": "r1",
		"delta":       "thinking...",
	})
	envs := c.Convert("s1", "session.next.reasoning.delta", props)
	require.Len(t, envs, 1)
	require.Equal(t, events.Reasoning, envs[0].Event.Type)
	require.Equal(t, "r1", envs[0].Event.Data.(events.ReasoningData).ID)
	require.Equal(t, "thinking...", envs[0].Event.Data.(events.ReasoningData).Content)
}

func TestConverter_ReasoningEnded(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{
		"reasoningID": "r1",
		"text":        "full reasoning text",
	})
	envs := c.Convert("s1", "session.next.reasoning.ended", props)
	require.Len(t, envs, 1)
	require.Equal(t, events.Reasoning, envs[0].Event.Type)
	require.Equal(t, "full reasoning text", envs[0].Event.Data.(events.ReasoningData).Content)
}

// ─── V2 Tool Events ───────────────────────────────────────────────────────────

func TestConverter_ToolCalled(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{
		"callID": "tc1",
		"tool":   "Read",
		"input":  map[string]any{"path": "/tmp/x"},
	})
	envs := c.Convert("s1", "session.next.tool.called", props)
	require.Len(t, envs, 1)
	require.Equal(t, events.ToolCall, envs[0].Event.Type)
	tc := envs[0].Event.Data.(events.ToolCallData)
	require.Equal(t, "tc1", tc.ID)
	require.Equal(t, "Read", tc.Name)
	require.Equal(t, "/tmp/x", tc.Input["path"])

	// Verify tool name tracked in state
	require.Equal(t, 1, c.states["s1"].toolNames["Read"])
}

func TestConverter_ToolSuccess(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{
		"callID":  "tc1",
		"content": []any{"file contents here"},
	})
	envs := c.Convert("s1", "session.next.tool.success", props)
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
	envs := c.Convert("s1", "session.next.tool.failed", props)
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
	c.Convert("s1", "session.next.step.ended", rawProps(t, map[string]any{
		"cost":   0.005,
		"tokens": map[string]any{"input": 2000, "output": 300, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}},
	}))

	props := rawProps(t, map[string]any{
		"status": map[string]any{"type": "idle"},
	})
	envs := c.Convert("s1", "session.status", props)
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

func TestConverter_SessionStatus_Busy(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{"status": map[string]any{"type": "busy"}})
	envs := c.Convert("s1", "session.status", props)
	require.Len(t, envs, 1)
	require.Equal(t, events.State, envs[0].Event.Type)
}

func TestConverter_SessionStatus_Retry(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{"status": map[string]any{"type": "retry"}})
	envs := c.Convert("s1", "session.status", props)
	require.Len(t, envs, 1)
	require.Equal(t, events.State, envs[0].Event.Type)
}

func TestConverter_SessionIdle(t *testing.T) {
	c := newTestConverter()
	envs := c.Convert("s1", "session.idle", nil)
	require.Empty(t, envs, "no usage accumulated → no Done emitted")
}

func TestConverter_SessionIdle_WithUsage(t *testing.T) {
	c := newTestConverter()
	c.Convert("s1", "session.next.step.ended", rawProps(t, map[string]any{
		"cost": 0.01, "tokens": map[string]any{"input": 1000, "output": 100, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}},
	}))
	envs := c.Convert("s1", "session.idle", nil)
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
	envs := c.Convert("s1", "session.error", props)
	require.Len(t, envs, 1)
	require.Equal(t, events.Error, envs[0].Event.Type)
	require.Equal(t, "rate limited", envs[0].Event.Data.(events.ErrorData).Message)
}

func TestConverter_SessionError_NameOnly(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{
		"error": map[string]any{"name": "TimeoutError"},
	})
	envs := c.Convert("s1", "session.error", props)
	require.Len(t, envs, 1)
	require.Equal(t, "TimeoutError", envs[0].Event.Data.(events.ErrorData).Message)
}

func TestConverter_PermissionAsked(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{"id": "p1", "metadata": map[string]any{"tool": "bash"}})
	envs := c.Convert("s1", "permission.asked", props)
	require.Len(t, envs, 1)
	require.Equal(t, events.Raw, envs[0].Event.Type)
	data := envs[0].Event.Data.(events.RawData)
	require.Equal(t, "ocs:permission.asked", data.Kind)
}

func TestConverter_QuestionAsked(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{"id": "q1"})
	envs := c.Convert("s1", "question.asked", props)
	require.Len(t, envs, 1)
	require.Equal(t, events.Raw, envs[0].Event.Type)
	data := envs[0].Event.Data.(events.RawData)
	require.Equal(t, "ocs:question.asked", data.Kind)
}

func TestConverter_V1UnknownEvent(t *testing.T) {
	c := newTestConverter()
	envs := c.Convert("s1", "message.part.delta", rawProps(t, nil))
	require.Empty(t, envs)
}

// ─── Full Turn Lifecycle ──────────────────────────────────────────────────────

func TestConverter_FullTurnLifecycle(t *testing.T) {
	c := newTestConverter()
	sid := "ses-lifecycle"

	// Step 1: busy
	envs := c.Convert(sid, "session.status", rawProps(t, map[string]any{"status": map[string]any{"type": "busy"}}))
	require.Len(t, envs, 1)
	require.Equal(t, events.State, envs[0].Event.Type)

	// Step 2: step started
	envs = c.Convert(sid, "session.next.step.started", rawProps(t, map[string]any{
		"model": map[string]any{"providerID": "anthropic", "modelID": "claude-sonnet-4-6"},
	}))
	require.Empty(t, envs)

	// Step 3: text delta
	envs = c.Convert(sid, "session.next.text.delta", rawProps(t, map[string]any{"delta": "Hello"}))
	require.Len(t, envs, 1)
	require.Equal(t, events.MessageDelta, envs[0].Event.Type)

	// Step 4: tool called
	envs = c.Convert(sid, "session.next.tool.called", rawProps(t, map[string]any{
		"callID": "tc1", "tool": "Bash", "input": map[string]any{"cmd": "ls"},
	}))
	require.Len(t, envs, 1)
	require.Equal(t, events.ToolCall, envs[0].Event.Type)

	// Step 5: tool success
	envs = c.Convert(sid, "session.next.tool.success", rawProps(t, map[string]any{
		"callID": "tc1", "content": []any{"file1.go\nfile2.go"},
	}))
	require.Len(t, envs, 1)
	require.Equal(t, events.ToolResult, envs[0].Event.Type)

	// Step 6: step ended
	envs = c.Convert(sid, "session.next.step.ended", rawProps(t, map[string]any{
		"cost": 0.02, "tokens": map[string]any{"input": 5000, "output": 600, "reasoning": 100, "cache": map[string]any{"read": 2000, "write": 500}},
	}))
	require.Empty(t, envs)

	// Step 7: more text
	envs = c.Convert(sid, "session.next.text.delta", rawProps(t, map[string]any{"delta": "Done!"}))
	require.Len(t, envs, 1)
	require.Equal(t, events.MessageDelta, envs[0].Event.Type)

	// Step 8: idle → Done with stats
	envs = c.Convert(sid, "session.status", rawProps(t, map[string]any{"status": map[string]any{"type": "idle"}}))
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
	envs := c.Convert("s1", "session.next.tool.failed", props)
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
	envs := c.Convert("s1", "session.next.tool.failed", props)
	require.Len(t, envs, 1)
	tr := envs[0].Event.Data.(events.ToolResultData)
	require.Equal(t, "permission denied", tr.Error)
}

func TestConverter_ReasoningEnded_Empty(t *testing.T) {
	c := newTestConverter()
	props := rawProps(t, map[string]any{
		"reasoningID": "r1",
		"text":        "",
	})
	envs := c.Convert("s1", "session.next.reasoning.ended", props)
	require.Empty(t, envs)
}

func TestConverter_MalformedJSON(t *testing.T) {
	c := newTestConverter()
	bad := json.RawMessage(`{invalid json`)
	for _, eventType := range []string{
		"session.next.step.started",
		"session.next.step.ended",
		"session.next.text.delta",
		"session.next.reasoning.delta",
		"session.next.reasoning.ended",
		"session.next.tool.called",
		"session.next.tool.success",
		"session.next.tool.failed",
		"session.status",
	} {
		envs := c.Convert("s1", eventType, bad)
		require.Empty(t, envs, "malformed JSON should produce nil for %s", eventType)
	}

	// step.failed and session.error always emit Error (use default message on bad JSON)
	for _, eventType := range []string{
		"session.next.step.failed",
		"session.error",
	} {
		envs := c.Convert("s1", eventType, bad)
		require.Len(t, envs, 1, "%s should emit Error even on malformed JSON", eventType)
		require.Equal(t, events.Error, envs[0].Event.Type)
	}
}

func TestConverter_InterleavedSessions(t *testing.T) {
	c := newTestConverter()
	s1, s2 := "session-a", "session-b"

	c.Convert(s1, "session.next.step.ended", rawProps(t, map[string]any{
		"cost": 0.01, "tokens": map[string]any{"input": 500, "output": 50, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}},
	}))
	c.Convert(s2, "session.next.step.ended", rawProps(t, map[string]any{
		"cost": 0.05, "tokens": map[string]any{"input": 2000, "output": 200, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}},
	}))

	envs := c.Convert(s1, "session.status", rawProps(t, map[string]any{"status": map[string]any{"type": "idle"}}))
	require.Len(t, envs, 1)
	dd := envs[0].Event.Data.(events.DoneData)
	require.InDelta(t, 0.01, dd.Stats["cost"], 0.0001)

	_, exists := c.states[s1]
	require.False(t, exists)
	require.NotNil(t, c.states[s2])

	envs = c.Convert(s2, "session.status", rawProps(t, map[string]any{"status": map[string]any{"type": "idle"}}))
	dd = envs[0].Event.Data.(events.DoneData)
	require.InDelta(t, 0.05, dd.Stats["cost"], 0.0001)
}

func TestConverter_DualDone_IdleFirst(t *testing.T) {
	c := newTestConverter()
	sid := "ses-dual"

	c.Convert(sid, "session.next.step.ended", rawProps(t, map[string]any{
		"cost": 0.01, "tokens": map[string]any{"input": 500, "output": 50, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}},
	}))

	envs := c.Convert(sid, "session.status", rawProps(t, map[string]any{"status": map[string]any{"type": "idle"}}))
	require.Len(t, envs, 1)
	require.Equal(t, events.Done, envs[0].Event.Type)

	envs = c.Convert(sid, "session.idle", nil)
	require.Empty(t, envs, "session.idle after state cleared should not emit second Done")
}

func TestConverter_Reset(t *testing.T) {
	c := newTestConverter()
	c.Convert("s1", "session.next.step.ended", rawProps(t, map[string]any{
		"cost": 0.01, "tokens": map[string]any{"input": 500, "output": 50, "reasoning": 0, "cache": map[string]any{"read": 0, "write": 0}},
	}))
	require.NotNil(t, c.states["s1"])

	c.Reset()
	_, exists := c.states["s1"]
	require.False(t, exists, "Reset should clear all state")

	envs := c.Convert("s2", "session.next.text.delta", rawProps(t, map[string]any{"delta": "hello"}))
	require.Len(t, envs, 1)
}
