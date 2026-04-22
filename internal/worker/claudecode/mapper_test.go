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

func TestMapContextUsageResponse(t *testing.T) {
	raw := map[string]any{
		"totalTokens": float64(76284),
		"maxTokens":   float64(200000),
		"percentage":  float64(38),
		"model":       "claude-sonnet-4",
		"memoryFiles": float64(5),
		"mcpTools":    float64(124),
		"agents":      float64(157),
		"categories": []any{
			map[string]any{"name": "System Prompt", "tokens": float64(603)},
			map[string]any{"name": "Messages", "tokens": float64(55228)},
		},
		"skills": map[string]any{
			"totalSkills":    float64(147),
			"includedSkills": float64(147),
			"tokens":         float64(9073),
		},
	}
	result := mapContextUsageResponse(raw)
	require.NotNil(t, result)
	require.Equal(t, 76284, result.TotalTokens)
	require.Equal(t, 200000, result.MaxTokens)
	require.Equal(t, 38, result.Percentage)
	require.Equal(t, "claude-sonnet-4", result.Model)
	require.Equal(t, 5, result.MemoryFiles)
	require.Equal(t, 124, result.MCPTools)
	require.Equal(t, 157, result.Agents)
	require.Len(t, result.Categories, 2)
	require.Equal(t, "System Prompt", result.Categories[0].Name)
	require.Equal(t, 603, result.Categories[0].Tokens)
	require.Equal(t, "Messages", result.Categories[1].Name)
	require.Equal(t, 55228, result.Categories[1].Tokens)
	require.Equal(t, 147, result.Skills.Total)
	require.Equal(t, 147, result.Skills.Included)
	require.Equal(t, 9073, result.Skills.Tokens)
}

func TestMapContextUsageResponseNil(t *testing.T) {
	result := mapContextUsageResponse(nil)
	require.NotNil(t, result)
	require.Equal(t, 0, result.TotalTokens)
	require.Empty(t, result.Categories)
}

func TestMapMCPStatusResponse(t *testing.T) {
	raw := map[string]any{
		"servers": []any{
			map[string]any{"name": "context7", "status": "connected"},
			map[string]any{"name": "playwright", "status": "connected"},
			map[string]any{"name": "slack", "status": "disconnected"},
		},
	}
	result := mapMCPStatusResponse(raw)
	require.Len(t, result.Servers, 3)
	require.Equal(t, "context7", result.Servers[0].Name)
	require.Equal(t, "connected", result.Servers[0].Status)
	require.Equal(t, "playwright", result.Servers[1].Name)
	require.Equal(t, "slack", result.Servers[2].Name)
	require.Equal(t, "disconnected", result.Servers[2].Status)
}

func TestMapMCPStatusResponseNil(t *testing.T) {
	result := mapMCPStatusResponse(nil)
	require.NotNil(t, result)
	require.Empty(t, result.Servers)
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
