package claudecode

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hotplex/hotplex-worker/pkg/events"
)

func TestMapper_Map_StreamEvent(t *testing.T) {
	log := newTestLogger()
	seqGen := func() int64 { return 1 }
	mapper := NewMapper(log, "session_123", seqGen)

	tests := []struct {
		name     string
		event    *WorkerEvent
		wantType events.Kind
		wantSeq  int64
	}{
		{
			name: "thinking → events.Reasoning",
			event: &WorkerEvent{
				Type: EventStream,
				Payload: &StreamPayload{
					Type:      "thinking",
					MessageID: "msg_123",
					Content:   "Let me analyze...",
				},
			},
			wantType: events.Reasoning,
			wantSeq:  1,
		},
		{
			name: "text → message.delta",
			event: &WorkerEvent{
				Type: EventStream,
				Payload: &StreamPayload{
					Type:      "text",
					MessageID: "msg_456",
					Content:   "Hello world",
				},
			},
			wantType: events.MessageDelta,
			wantSeq:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envs, err := mapper.Map(tt.event)
			require.NoError(t, err)
			require.Len(t, envs, 1)
			env := envs[0]
			require.Equal(t, tt.wantType, env.Event.Type)
			require.Equal(t, tt.wantSeq, env.Seq)
			require.Equal(t, "session_123", env.SessionID)
		})
	}

	t.Run("thinking content in ReasoningData", func(t *testing.T) {
		event := &WorkerEvent{
			Type: EventStream,
			Payload: &StreamPayload{
				Type:      "thinking",
				MessageID: "msg_think",
				Content:   "Using chain-of-thought...",
			},
		}
		envs, err := mapper.Map(event)
		require.NoError(t, err)
		require.Len(t, envs, 1)
		require.Equal(t, events.Reasoning, envs[0].Event.Type)
		data, ok := envs[0].Event.Data.(events.ReasoningData)
		require.True(t, ok)
		require.Equal(t, "msg_think", data.ID)
		require.Equal(t, "Using chain-of-thought...", data.Content)
	})
}

func TestMapper_Map_ToolCall(t *testing.T) {
	log := newTestLogger()
	seqGen := func() int64 { return 1 }
	mapper := NewMapper(log, "session_123", seqGen)

	event := &WorkerEvent{
		Type: EventAssistant,
		Payload: &ToolCallPayload{
			ID:   "call_123",
			Name: "read_file",
			Input: map[string]any{
				"path": "/app/main.go",
			},
		},
	}

	envs, err := mapper.Map(event)
	require.NoError(t, err)
	require.Len(t, envs, 1)
	env := envs[0]
	require.Equal(t, events.ToolCall, env.Event.Type)

	data, ok := env.Event.Data.(events.ToolCallData)
	require.True(t, ok)
	require.Equal(t, "call_123", data.ID)
	require.Equal(t, "read_file", data.Name)
	require.Equal(t, "/app/main.go", data.Input["path"])
}

func TestMapper_Map_ToolResult(t *testing.T) {
	log := newTestLogger()
	seqGen := func() int64 { return 1 }
	mapper := NewMapper(log, "session_123", seqGen)

	event := &WorkerEvent{
		Type: EventToolProgress,
		Payload: &ToolResultPayload{
			ToolUseID: "call_123",
			Output:    "file content...",
		},
	}

	envs, err := mapper.Map(event)
	require.NoError(t, err)
	require.Len(t, envs, 1)
	env := envs[0]
	require.Equal(t, events.ToolResult, env.Event.Type)

	data, ok := env.Event.Data.(events.ToolResultData)
	require.True(t, ok)
	require.Equal(t, "call_123", data.ID)
	require.Equal(t, "file content...", data.Output)
}

func TestMapper_Map_Result(t *testing.T) {
	log := newTestLogger()
	seqGen := func() int64 { return 1 }
	mapper := NewMapper(log, "session_123", seqGen)

	t.Run("success", func(t *testing.T) {
		event := &WorkerEvent{
			Type: EventResult,
			Payload: &ResultPayload{
				Success: true,
				Message: "Task completed",
				Stats: map[string]any{
					"duration_ms": 5200,
				},
			},
		}

		envs, err := mapper.Map(event)
		require.NoError(t, err)
		require.Len(t, envs, 1)
		env := envs[0]
		require.Equal(t, events.Done, env.Event.Type)

		data, ok := env.Event.Data.(events.DoneData)
		require.True(t, ok)
		require.True(t, data.Success)
	})

	t.Run("error sends both error and done", func(t *testing.T) {
		event := &WorkerEvent{
			Type: EventResult,
			Payload: &ResultPayload{
				Success: false,
				Message: "Error occurred",
			},
		}

		envs, err := mapper.Map(event)
		require.NoError(t, err)
		require.Len(t, envs, 2)

		// First envelope: error
		require.Equal(t, events.Error, envs[0].Event.Type)
		errData, ok := envs[0].Event.Data.(events.ErrorData)
		require.True(t, ok)
		require.Equal(t, events.ErrCodeInternalError, errData.Code)
		require.Equal(t, "Error occurred", errData.Message)

		// Second envelope: done { success: false }
		require.Equal(t, events.Done, envs[1].Event.Type)
		doneData, ok := envs[1].Event.Data.(events.DoneData)
		require.True(t, ok)
		require.False(t, doneData.Success)
	})
}
