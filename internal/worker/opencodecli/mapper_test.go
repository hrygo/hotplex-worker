package opencodecli

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hotplex/hotplex-worker/pkg/events"
)

func TestMapper_Map_Text(t *testing.T) {
	mapper := NewMapper(newTestLogger(), "sess_abc", func() int64 { return 1 })

	evt := &WorkerEvent{
		Type: EventText,
		Payload: &TextPayload{
			Content:   "Hello, world!",
			MessageID: "msg_001",
		},
	}

	envs, err := mapper.Map(evt)
	require.NoError(t, err)
	require.Len(t, envs, 1)

	env := envs[0]
	require.Equal(t, "sess_abc", env.SessionID)
	require.Equal(t, events.MessageDelta, env.Event.Type)
	data, ok := env.Event.Data.(events.MessageDeltaData)
	require.True(t, ok)
	require.Equal(t, "Hello, world!", data.Content)
	require.Equal(t, "msg_001", data.MessageID)
}

func TestMapper_Map_Reasoning(t *testing.T) {
	mapper := NewMapper(newTestLogger(), "sess_abc", func() int64 { return 2 })

	evt := &WorkerEvent{
		Type: EventReasoning,
		Payload: &ReasoningPayload{
			Content:   "Let me think about this...",
			MessageID: "msg_002",
		},
	}

	envs, err := mapper.Map(evt)
	require.NoError(t, err)
	require.Len(t, envs, 1)

	env := envs[0]
	require.Equal(t, events.Reasoning, env.Event.Type)
	data, ok := env.Event.Data.(events.ReasoningData)
	require.True(t, ok)
	require.Equal(t, "Let me think about this...", data.Content)
}

func TestMapper_Map_ToolCall(t *testing.T) {
	mapper := NewMapper(newTestLogger(), "sess_abc", func() int64 { return 3 })

	evt := &WorkerEvent{
		Type: EventToolUse,
		Payload: &ToolCallPayload{
			ID:    "call_xyz",
			Name:  "bash",
			Input: map[string]any{"command": "ls -la"},
		},
	}

	envs, err := mapper.Map(evt)
	require.NoError(t, err)
	require.Len(t, envs, 1)

	env := envs[0]
	require.Equal(t, events.ToolCall, env.Event.Type)
	data, ok := env.Event.Data.(events.ToolCallData)
	require.True(t, ok)
	require.Equal(t, "call_xyz", data.ID)
	require.Equal(t, "bash", data.Name)
	require.Equal(t, "ls -la", data.Input["command"])
}

func TestMapper_Map_UnknownEvent(t *testing.T) {
	mapper := NewMapper(newTestLogger(), "sess_abc", nil)

	evt := &WorkerEvent{
		Type:    EventStepStart,
		Payload: &StepStartPayload{SessionID: "ses_abc", MessageID: "msg_001"},
	}

	envs, err := mapper.Map(evt)
	require.NoError(t, err)
	require.Len(t, envs, 0) // step_start is internal, not sent to client
}

func TestMapper_UpdateSessionID(t *testing.T) {
	mapper := NewMapper(newTestLogger(), "sess_initial", func() int64 { return 1 })

	require.Equal(t, "sess_initial", mapper.SessionID())

	mapper.UpdateSessionID("ses_updated")
	require.Equal(t, "ses_updated", mapper.SessionID())
}
