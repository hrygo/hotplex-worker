package messaging

import (
	"log/slog"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

// ---------------------------------------------------------------------------
// InteractionManager tests
// ---------------------------------------------------------------------------

func TestNewInteractionManager(t *testing.T) {
	t.Parallel()

	m := NewInteractionManager(slog.Default())
	require.NotNil(t, m)
	require.NotNil(t, m.pending)
	require.Equal(t, 0, m.Len())
}

func TestInteractionManager_RegisterAndGet(t *testing.T) {
	t.Parallel()

	m := NewInteractionManager(slog.Default())
	pi := &PendingInteraction{
		ID:        "req-1",
		SessionID: "sess-1",
		Type:      events.PermissionRequest,
		CreatedAt: time.Now(),
		Timeout:   DefaultInteractionTimeout,
	}

	m.Register(pi)

	got, ok := m.Get("req-1")
	require.True(t, ok)
	require.Equal(t, "req-1", got.ID)
	require.Equal(t, "sess-1", got.SessionID)
	require.Equal(t, events.PermissionRequest, got.Type)
}

func TestInteractionManager_Get_NotFound(t *testing.T) {
	t.Parallel()

	m := NewInteractionManager(slog.Default())
	_, ok := m.Get("nonexistent")
	require.False(t, ok)
}

func TestInteractionManager_Register_Dedup(t *testing.T) {
	t.Parallel()

	m := NewInteractionManager(slog.Default())
	pi := &PendingInteraction{
		ID:           "dup-1",
		SessionID:    "sess-1",
		Type:         events.PermissionRequest,
		CreatedAt:    time.Now(),
		Timeout:      time.Hour, // long timeout; won't fire during test
		SendResponse: func(map[string]any) {},
	}

	// Register twice — second should be a no-op.
	m.Register(pi)
	m.Register(pi)

	require.Equal(t, 1, m.Len())
}

func TestInteractionManager_Complete(t *testing.T) {
	t.Parallel()

	m := NewInteractionManager(slog.Default())
	pi := &PendingInteraction{
		ID:           "req-complete",
		SessionID:    "sess-1",
		Type:         events.QuestionRequest,
		CreatedAt:    time.Now(),
		Timeout:      time.Hour,
		SendResponse: func(map[string]any) {},
	}

	m.Register(pi)
	require.Equal(t, 1, m.Len())

	completed, ok := m.Complete("req-complete")
	require.True(t, ok)
	require.Equal(t, "req-complete", completed.ID)
	require.Equal(t, 0, m.Len())

	// Second complete returns false.
	_, ok = m.Complete("req-complete")
	require.False(t, ok)
}

func TestInteractionManager_Complete_NotFound(t *testing.T) {
	t.Parallel()

	m := NewInteractionManager(slog.Default())
	_, ok := m.Complete("nonexistent")
	require.False(t, ok)
}

func TestInteractionManager_Len(t *testing.T) {
	t.Parallel()

	m := NewInteractionManager(slog.Default())
	require.Equal(t, 0, m.Len())

	for i := range 5 {
		m.Register(&PendingInteraction{
			ID:           "req-len-" + strings.Repeat("0", 1) + string(rune('0'+i)),
			SessionID:    "sess-1",
			Type:         events.PermissionRequest,
			CreatedAt:    time.Now(),
			Timeout:      time.Hour,
			SendResponse: func(map[string]any) {},
		})
	}
	require.Equal(t, 5, m.Len())
}

func TestInteractionManager_GetAll_SortedByCreatedAtDesc(t *testing.T) {
	t.Parallel()

	m := NewInteractionManager(slog.Default())
	base := time.Now()

	m.Register(&PendingInteraction{
		ID: "oldest", SessionID: "s1", Type: events.PermissionRequest,
		CreatedAt: base, Timeout: time.Hour, SendResponse: func(map[string]any) {},
	})
	m.Register(&PendingInteraction{
		ID: "middle", SessionID: "s1", Type: events.QuestionRequest,
		CreatedAt: base.Add(10 * time.Second), Timeout: time.Hour, SendResponse: func(map[string]any) {},
	})
	m.Register(&PendingInteraction{
		ID: "newest", SessionID: "s1", Type: events.ElicitationRequest,
		CreatedAt: base.Add(20 * time.Second), Timeout: time.Hour, SendResponse: func(map[string]any) {},
	})

	all := m.GetAll()
	require.Len(t, all, 3)
	require.Equal(t, "newest", all[0].ID)
	require.Equal(t, "middle", all[1].ID)
	require.Equal(t, "oldest", all[2].ID)
}

func TestInteractionManager_GetAll_Empty(t *testing.T) {
	t.Parallel()

	m := NewInteractionManager(slog.Default())
	all := m.GetAll()
	require.NotNil(t, all)
	require.Empty(t, all)
}

func TestInteractionManager_GetBySession(t *testing.T) {
	t.Parallel()

	m := NewInteractionManager(slog.Default())
	base := time.Now()

	m.Register(&PendingInteraction{
		ID: "req-a", SessionID: "sess-A", Type: events.PermissionRequest,
		CreatedAt: base, Timeout: time.Hour, SendResponse: func(map[string]any) {},
	})
	m.Register(&PendingInteraction{
		ID: "req-b", SessionID: "sess-B", Type: events.QuestionRequest,
		CreatedAt: base.Add(5 * time.Second), Timeout: time.Hour, SendResponse: func(map[string]any) {},
	})
	m.Register(&PendingInteraction{
		ID: "req-c", SessionID: "sess-A", Type: events.ElicitationRequest,
		CreatedAt: base.Add(10 * time.Second), Timeout: time.Hour, SendResponse: func(map[string]any) {},
	})

	// Filter by sess-A: should return req-c (newest) then req-a (oldest).
	result := m.GetBySession("sess-A")
	require.Len(t, result, 2)
	require.Equal(t, "req-c", result[0].ID)
	require.Equal(t, "req-a", result[1].ID)

	// Filter by sess-B: single item.
	resultB := m.GetBySession("sess-B")
	require.Len(t, resultB, 1)
	require.Equal(t, "req-b", resultB[0].ID)

	// No match.
	resultNone := m.GetBySession("sess-Z")
	require.Nil(t, resultNone)
}

func TestInteractionManager_GetBySession_EmptySessionID(t *testing.T) {
	t.Parallel()

	m := NewInteractionManager(slog.Default())
	m.Register(&PendingInteraction{
		ID: "req-1", SessionID: "sess-1", Type: events.PermissionRequest,
		CreatedAt: time.Now(), Timeout: time.Hour, SendResponse: func(map[string]any) {},
	})

	// Empty sessionID returns nil.
	result := m.GetBySession("")
	require.Nil(t, result)
}

func TestInteractionManager_CancelAll(t *testing.T) {
	t.Parallel()

	m := NewInteractionManager(slog.Default())

	m.Register(&PendingInteraction{
		ID: "r1", SessionID: "sess-X", Type: events.PermissionRequest,
		CreatedAt: time.Now(), Timeout: time.Hour, SendResponse: func(map[string]any) {},
	})
	m.Register(&PendingInteraction{
		ID: "r2", SessionID: "sess-X", Type: events.QuestionRequest,
		CreatedAt: time.Now(), Timeout: time.Hour, SendResponse: func(map[string]any) {},
	})
	m.Register(&PendingInteraction{
		ID: "r3", SessionID: "sess-Y", Type: events.ElicitationRequest,
		CreatedAt: time.Now(), Timeout: time.Hour, SendResponse: func(map[string]any) {},
	})

	require.Equal(t, 3, m.Len())

	m.CancelAll("sess-X")
	require.Equal(t, 1, m.Len())

	_, ok := m.Get("r3")
	require.True(t, ok, "sess-Y interaction should remain")
}

func TestInteractionManager_CancelAll_NoMatch(t *testing.T) {
	t.Parallel()

	m := NewInteractionManager(slog.Default())
	m.Register(&PendingInteraction{
		ID: "r1", SessionID: "sess-1", Type: events.PermissionRequest,
		CreatedAt: time.Now(), Timeout: time.Hour, SendResponse: func(map[string]any) {},
	})

	m.CancelAll("nonexistent")
	require.Equal(t, 1, m.Len())
}

// ---------------------------------------------------------------------------
// watchTimeout — auto-deny on timeout
// ---------------------------------------------------------------------------

func TestInteractionManager_WatchTimeout_PermissionRequest(t *testing.T) {
	t.Parallel()

	m := NewInteractionManager(slog.Default())

	var responded atomic.Int32

	pi := &PendingInteraction{
		ID:        "timeout-perm",
		SessionID: "sess-1",
		Type:      events.PermissionRequest,
		CreatedAt: time.Now(),
		Timeout:   50 * time.Millisecond,
		SendResponse: func(metadata map[string]any) {
			responded.Add(1)
		},
	}
	m.Register(pi)

	// Wait for timeout to fire.
	require.Eventually(t, func() bool {
		return responded.Load() == 1
	}, 2*time.Second, 10*time.Millisecond, "SendResponse should be called once after timeout")

	// After timeout, interaction should be removed.
	_, ok := m.Get("timeout-perm")
	require.False(t, ok, "interaction should be removed after timeout")
}

func TestInteractionManager_WatchTimeout_QuestionRequest(t *testing.T) {
	t.Parallel()

	m := NewInteractionManager(slog.Default())

	respCh := make(chan map[string]any, 1)
	pi := &PendingInteraction{
		ID:        "timeout-question",
		SessionID: "sess-1",
		Type:      events.QuestionRequest,
		CreatedAt: time.Now(),
		Timeout:   50 * time.Millisecond,
		SendResponse: func(metadata map[string]any) {
			select {
			case respCh <- metadata:
			default:
			}
		},
	}
	m.Register(pi)

	select {
	case lastResponse := <-respCh:
		// Verify auto-deny payload for question.
		qr, ok := lastResponse["question_response"]
		require.True(t, ok, "should contain question_response key")
		qrMap, ok := qr.(map[string]any)
		require.True(t, ok)
		require.Equal(t, "timeout-question", qrMap["id"])
		require.Equal(t, map[string]string{}, qrMap["answers"])
	case <-time.After(2 * time.Second):
		require.Fail(t, "SendResponse should be called after timeout")
	}
}

func TestInteractionManager_WatchTimeout_ElicitationRequest(t *testing.T) {
	t.Parallel()

	m := NewInteractionManager(slog.Default())

	respCh := make(chan map[string]any, 1)
	pi := &PendingInteraction{
		ID:        "timeout-elicitation",
		SessionID: "sess-1",
		Type:      events.ElicitationRequest,
		CreatedAt: time.Now(),
		Timeout:   50 * time.Millisecond,
		SendResponse: func(metadata map[string]any) {
			select {
			case respCh <- metadata:
			default:
			}
		},
	}
	m.Register(pi)

	select {
	case lastResponse := <-respCh:
		// Verify auto-deny payload for elicitation (action=cancel).
		er, ok := lastResponse["elicitation_response"]
		require.True(t, ok, "should contain elicitation_response key")
		erMap, ok := er.(map[string]any)
		require.True(t, ok)
		require.Equal(t, "timeout-elicitation", erMap["id"])
		require.Equal(t, "cancel", erMap["action"])
	case <-time.After(2 * time.Second):
		require.Fail(t, "SendResponse should be called after timeout")
	}
}

func TestInteractionManager_WatchTimeout_AlreadyCompleted(t *testing.T) {
	t.Parallel()

	m := NewInteractionManager(slog.Default())

	var responded atomic.Int32
	pi := &PendingInteraction{
		ID:        "timeout-already",
		SessionID: "sess-1",
		Type:      events.PermissionRequest,
		CreatedAt: time.Now(),
		Timeout:   50 * time.Millisecond,
		SendResponse: func(metadata map[string]any) {
			responded.Add(1)
		},
	}
	m.Register(pi)

	m.Complete("timeout-already")

	require.Never(t, func() bool {
		return responded.Load() > 0
	}, 200*time.Millisecond, 10*time.Millisecond, "SendResponse should not be called when already completed")
}

func TestInteractionManager_WatchTimeout_PermissionAutoDenyPayload(t *testing.T) {
	t.Parallel()

	m := NewInteractionManager(slog.Default())

	respCh := make(chan map[string]any, 1)
	pi := &PendingInteraction{
		ID:        "timeout-payload",
		SessionID: "sess-1",
		Type:      events.PermissionRequest,
		CreatedAt: time.Now(),
		Timeout:   50 * time.Millisecond,
		SendResponse: func(metadata map[string]any) {
			select {
			case respCh <- metadata:
			default:
			}
		},
	}
	m.Register(pi)

	select {
	case lastResponse := <-respCh:
		pr, ok := lastResponse["permission_response"]
		require.True(t, ok, "should contain permission_response key")
		prMap, ok := pr.(map[string]any)
		require.True(t, ok)
		require.Equal(t, "timeout-payload", prMap["request_id"])
		require.Equal(t, false, prMap["allowed"])
		require.Equal(t, "interaction timed out", prMap["reason"])
	case <-time.After(2 * time.Second):
		require.Fail(t, "SendResponse should be called after timeout")
	}
}

// ---------------------------------------------------------------------------
// ExtractPermissionData
// ---------------------------------------------------------------------------

func TestExtractPermissionData_TypedData(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Event: events.Event{
			Type: events.PermissionRequest,
			Data: events.PermissionRequestData{
				ID:          "perm-1",
				ToolName:    "bash",
				Description: "run a command",
				Args:        []string{"ls", "-la"},
			},
		},
	}

	data, err := ExtractPermissionData(env)
	require.NoError(t, err)
	require.Equal(t, "perm-1", data.ID)
	require.Equal(t, "bash", data.ToolName)
	require.Equal(t, "run a command", data.Description)
	require.Equal(t, []string{"ls", "-la"}, data.Args)
}

func TestExtractPermissionData_MapData(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Event: events.Event{
			Type: events.PermissionRequest,
			Data: map[string]any{
				"id":          "perm-2",
				"tool_name":   "write_file",
				"description": "write content to file",
				"args":        []any{"hello.txt", "world"},
			},
		},
	}

	data, err := ExtractPermissionData(env)
	require.NoError(t, err)
	require.Equal(t, "perm-2", data.ID)
	require.Equal(t, "write_file", data.ToolName)
	require.Equal(t, "write content to file", data.Description)
	require.Equal(t, []string{"hello.txt", "world"}, data.Args)
}

func TestExtractPermissionData_MapData_Partial(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Event: events.Event{
			Type: events.PermissionRequest,
			Data: map[string]any{
				"id": "perm-3",
				// missing tool_name, description, args
			},
		},
	}

	data, err := ExtractPermissionData(env)
	require.NoError(t, err)
	require.Equal(t, "perm-3", data.ID)
	require.Empty(t, data.ToolName)
	require.Empty(t, data.Description)
	require.Nil(t, data.Args)
}

func TestExtractPermissionData_MapData_NonStringArgs(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Event: events.Event{
			Type: events.PermissionRequest,
			Data: map[string]any{
				"id":   "perm-4",
				"args": []any{123, true, "valid", nil},
			},
		},
	}

	data, err := ExtractPermissionData(env)
	require.NoError(t, err)
	// Only string elements from the args slice are extracted.
	require.Equal(t, []string{"valid"}, data.Args)
}

func TestExtractPermissionData_UnexpectedType(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Event: events.Event{
			Type: events.PermissionRequest,
			Data: "not a valid type",
		},
	}

	data, err := ExtractPermissionData(env)
	require.Nil(t, data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected permission data type")
}

// ---------------------------------------------------------------------------
// ExtractQuestionData
// ---------------------------------------------------------------------------

func TestExtractQuestionData_TypedData(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Event: events.Event{
			Type: events.QuestionRequest,
			Data: events.QuestionRequestData{
				ID:       "q-1",
				ToolName: "AskUserQuestion",
			},
		},
	}

	data, err := ExtractQuestionData(env)
	require.NoError(t, err)
	require.Equal(t, "q-1", data.ID)
	require.Equal(t, "AskUserQuestion", data.ToolName)
}

func TestExtractQuestionData_MapData(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Event: events.Event{
			Type: events.QuestionRequest,
			Data: map[string]any{
				"id":        "q-2",
				"tool_name": "question",
			},
		},
	}

	data, err := ExtractQuestionData(env)
	require.NoError(t, err)
	require.Equal(t, "q-2", data.ID)
	require.Equal(t, "question", data.ToolName)
}

func TestExtractQuestionData_MapData_Partial(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Event: events.Event{
			Type: events.QuestionRequest,
			Data: map[string]any{
				"id": "q-3",
			},
		},
	}

	data, err := ExtractQuestionData(env)
	require.NoError(t, err)
	require.Equal(t, "q-3", data.ID)
	require.Empty(t, data.ToolName)
}

func TestExtractQuestionData_UnexpectedType(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Event: events.Event{
			Type: events.QuestionRequest,
			Data: 42,
		},
	}

	data, err := ExtractQuestionData(env)
	require.Nil(t, data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected question data type")
}

// ---------------------------------------------------------------------------
// ExtractElicitationData
// ---------------------------------------------------------------------------

func TestExtractElicitationData_TypedData(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Event: events.Event{
			Type: events.ElicitationRequest,
			Data: events.ElicitationRequestData{
				ID:            "el-1",
				MCPServerName: "github",
				Message:       "Please authorize",
				Mode:          "default",
				URL:           "https://github.com",
			},
		},
	}

	data, err := ExtractElicitationData(env)
	require.NoError(t, err)
	require.Equal(t, "el-1", data.ID)
	require.Equal(t, "github", data.MCPServerName)
	require.Equal(t, "Please authorize", data.Message)
	require.Equal(t, "default", data.Mode)
	require.Equal(t, "https://github.com", data.URL)
}

func TestExtractElicitationData_MapData(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Event: events.Event{
			Type: events.ElicitationRequest,
			Data: map[string]any{
				"id":              "el-2",
				"mcp_server_name": "slack",
				"message":         "Enter token",
				"mode":            "brief",
				"url":             "https://slack.com",
			},
		},
	}

	data, err := ExtractElicitationData(env)
	require.NoError(t, err)
	require.Equal(t, "el-2", data.ID)
	require.Equal(t, "slack", data.MCPServerName)
	require.Equal(t, "Enter token", data.Message)
	require.Equal(t, "brief", data.Mode)
	require.Equal(t, "https://slack.com", data.URL)
}

func TestExtractElicitationData_MapData_Partial(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Event: events.Event{
			Type: events.ElicitationRequest,
			Data: map[string]any{
				"id": "el-3",
			},
		},
	}

	data, err := ExtractElicitationData(env)
	require.NoError(t, err)
	require.Equal(t, "el-3", data.ID)
	require.Empty(t, data.MCPServerName)
	require.Empty(t, data.Message)
	require.Empty(t, data.Mode)
	require.Empty(t, data.URL)
}

func TestExtractElicitationData_UnexpectedType(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Event: events.Event{
			Type: events.ElicitationRequest,
			Data: []string{"invalid"},
		},
	}

	data, err := ExtractElicitationData(env)
	require.Nil(t, data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unexpected elicitation data type")
}

// ---------------------------------------------------------------------------
// IsHelpCommand
// ---------------------------------------------------------------------------

func TestIsHelpCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"slash help", "/help", true},
		{"slash help uppercase", "/Help", true},
		{"slash help mixed case", "/HELP", true},
		{"dollar help", "$help", true},
		{"dollar help uppercase", "$Help", true},
		{"question mark ascii", "?", true},
		{"question mark fullwidth", "？", true},
		{"slash help with spaces", "  /help  ", true},
		{"not help - random text", "hello", false},
		{"not help - empty", "", false},
		{"not help - help without prefix", "help", false},
		{"not help - /hel", "/hel", false},
		{"not help - /helping", "/helping", false},
		{"not help - ??", "??", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, IsHelpCommand(tt.input))
		})
	}
}

// ---------------------------------------------------------------------------
// HelpText
// ---------------------------------------------------------------------------

func TestHelpText_NotEmpty(t *testing.T) {
	t.Parallel()

	text := HelpText()
	require.NotEmpty(t, text)
}

func TestHelpText_ContainsSections(t *testing.T) {
	t.Parallel()

	text := HelpText()

	// Should contain all major section titles.
	require.Contains(t, text, "命令帮助")
	require.Contains(t, text, "会话控制")
	require.Contains(t, text, "信息与状态")
	require.Contains(t, text, "配置")
	require.Contains(t, text, "对话")
	require.Contains(t, text, "提示")
}

func TestHelpText_ContainsKeyCommands(t *testing.T) {
	t.Parallel()

	text := HelpText()

	// Should mention the key commands.
	require.Contains(t, text, "`/gc`")
	require.Contains(t, text, "`/park`")
	require.Contains(t, text, "`/reset`")
	require.Contains(t, text, "`/new`")
	require.Contains(t, text, "`/context`")
	require.Contains(t, text, "`/mcp`")
	require.Contains(t, text, "`/model`")
	require.Contains(t, text, "`?`")
}

// ---------------------------------------------------------------------------
// Register / RegisteredTypes (platform_adapter.go)
// ---------------------------------------------------------------------------

func TestRegister_NilBuilder_Panics(t *testing.T) {
	t.Parallel()

	require.Panics(t, func() {
		Register(PlatformType("test_nil"), nil)
	}, "Register with nil builder should panic")
}

func TestRegister_Duplicate_Panics(t *testing.T) {
	// Register a test type first, then try to re-register it.
	testDup := PlatformType("test_dup_panic")
	Register(testDup, func(log *slog.Logger) PlatformAdapterInterface { return nil })
	t.Cleanup(func() {
		registryMu.Lock()
		delete(registry, testDup)
		registryMu.Unlock()
	})

	require.Panics(t, func() {
		Register(testDup, func(log *slog.Logger) PlatformAdapterInterface { return nil })
	}, "Register with duplicate platform type should panic")
}

func TestRegisteredTypes(t *testing.T) {
	t.Parallel()

	types := RegisteredTypes()
	// When run as part of the messaging package tests only (no blank imports
	// from main.go), the adapter registry may be empty. Verify the function
	// returns a non-nil slice in all cases.
	require.NotNil(t, types)
}

func TestRegister_AndRegisteredTypes(t *testing.T) {
	testType := PlatformType("test_unit_verify")

	Register(testType, func(log *slog.Logger) PlatformAdapterInterface {
		return nil
	})
	t.Cleanup(func() {
		registryMu.Lock()
		delete(registry, testType)
		registryMu.Unlock()
	})

	types := RegisteredTypes()
	require.True(t, slices.Contains(types, testType), "test_unit_verify should appear in RegisteredTypes")
}
