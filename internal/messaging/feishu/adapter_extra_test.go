package feishu

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/pkg/events"
)

func TestAdapter_SetReconnectDelays(t *testing.T) {
	t.Parallel()
	a := &Adapter{log: slog.New(slog.NewTextHandler(io.Discard, nil))}

	require.Equal(t, time.Duration(0), a.backoffBaseDelay)
	require.Equal(t, time.Duration(0), a.backoffMaxDelay)

	a.SetReconnectDelays(2*time.Second, 60*time.Second)
	require.Equal(t, 2*time.Second, a.backoffBaseDelay)
	require.Equal(t, 60*time.Second, a.backoffMaxDelay)
}

func TestAdapter_GetOrCreateConn_SameKeyReturnsSame(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	conn1 := a.GetOrCreateConn("chat123", "thread1")
	conn2 := a.GetOrCreateConn("chat123", "thread1")
	require.Same(t, conn1, conn2)
	require.Len(t, a.activeConns, 1)
}

func TestAdapter_GetOrCreateConn_DifferentKeyReturnsDifferent(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	conn1 := a.GetOrCreateConn("chat123", "thread1")
	conn2 := a.GetOrCreateConn("chat456", "thread1")
	conn3 := a.GetOrCreateConn("chat123", "thread2")
	require.NotSame(t, conn1, conn2)
	require.NotSame(t, conn1, conn3)
	require.NotSame(t, conn2, conn3)
	require.Len(t, a.activeConns, 3)
}

func TestAdapter_GetOrCreateConn_ThreadKeyPassedThrough(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	conn := a.GetOrCreateConn("chat_abc", "thread_xyz")
	require.Equal(t, "chat_abc", conn.chatID)
	require.Equal(t, "thread_xyz", conn.threadKey)
}

func TestFeishuConn_Close_ClearsFields(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	_ = a.Start(context.Background())
	t.Cleanup(func() { _ = a.Close(context.Background()) })

	conn := a.GetOrCreateConn("chat123", "")
	conn.mu.Lock()
	conn.streamCtrl = NewStreamingCardController(nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	conn.typingRid = "typing_abc"
	conn.toolRid = "tool_def"
	conn.platformMsgID = "msg_xyz"
	conn.mu.Unlock()

	err := conn.Close()
	require.NoError(t, err)

	conn.mu.RLock()
	require.Nil(t, conn.streamCtrl)
	require.Empty(t, conn.typingRid)
	require.Empty(t, conn.toolRid)
	require.Empty(t, conn.toolEmoji)
	conn.mu.RUnlock()

	a.mu.RLock()
	_, exists := a.activeConns["chat123#"]
	a.mu.RUnlock()
	require.False(t, exists)
}

func TestFeishuConn_Close_NilStreamCtrl(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	_ = a.Start(context.Background())
	t.Cleanup(func() { _ = a.Close(context.Background()) })

	conn := a.GetOrCreateConn("chat_nil", "")
	err := conn.Close()
	require.NoError(t, err)
}

func TestFeishuConn_Close_NilLarkClient(t *testing.T) {
	t.Parallel()
	a := &Adapter{
		log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		dedup:        NewDedup(100, time.Hour),
		activeConns:  make(map[string]*FeishuConn),
		dedupDone:    make(chan struct{}),
		interactions: messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil))),
	}

	conn := NewFeishuConn(a, "chat_close", "")
	conn.mu.Lock()
	conn.typingRid = "typing_rid"
	conn.toolRid = "tool_rid"
	conn.platformMsgID = "msg_id"
	conn.mu.Unlock()

	err := conn.Close()
	require.NoError(t, err)
}

func TestAdapter_HandleTextMessage_NilBridge(t *testing.T) {
	t.Parallel()
	a := &Adapter{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	err := a.HandleTextMessage(context.Background(), "msg1", "ch1", "team1", "thread1", "user1", "hello")
	require.NoError(t, err)
}

func TestFeishuConn_WriteCtx_PermissionRequest_NilClient(t *testing.T) {
	t.Parallel()
	a := &Adapter{
		log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		dedup:        NewDedup(100, time.Hour),
		activeConns:  make(map[string]*FeishuConn),
		dedupDone:    make(chan struct{}),
		interactions: messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil))),
	}

	conn := NewFeishuConn(a, "chat123", "")
	conn.platformMsgID = "msg123"

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess1",
		Event: events.Event{
			Type: events.PermissionRequest,
			Data: events.PermissionRequestData{
				ID:          "perm-001",
				ToolName:    "WriteFile",
				Description: "Write to file /tmp/test.txt",
				Args:        []string{`{"path":"/tmp/test.txt","content":"hello"}`},
			},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "send permission card")
}

func TestFeishuConn_WriteCtx_QuestionRequest_NilClient(t *testing.T) {
	t.Parallel()
	a := &Adapter{
		log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		dedup:        NewDedup(100, time.Hour),
		activeConns:  make(map[string]*FeishuConn),
		dedupDone:    make(chan struct{}),
		interactions: messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil))),
	}

	conn := NewFeishuConn(a, "chat123", "")
	conn.platformMsgID = "msg123"

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess1",
		Event: events.Event{
			Type: events.QuestionRequest,
			Data: events.QuestionRequestData{
				ID:       "q-001",
				ToolName: "AskUserQuestion",
			},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "send question card")
}

func TestFeishuConn_WriteCtx_ElicitationRequest_NilClient(t *testing.T) {
	t.Parallel()
	a := &Adapter{
		log:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		dedup:        NewDedup(100, time.Hour),
		activeConns:  make(map[string]*FeishuConn),
		dedupDone:    make(chan struct{}),
		interactions: messaging.NewInteractionManager(slog.New(slog.NewTextHandler(io.Discard, nil))),
	}

	conn := NewFeishuConn(a, "chat123", "")
	conn.platformMsgID = "msg123"

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess1",
		Event: events.Event{
			Type: events.ElicitationRequest,
			Data: events.ElicitationRequestData{
				ID:            "elicit-001",
				MCPServerName: "filesystem",
				Message:       "Please provide input",
				URL:           "https://example.com/form",
			},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "send elicitation card")
}
