package feishu

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/pkg/events"
)

func TestFeishuConn_ThinkTimer_StartStop(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := NewFeishuConn(adapter, "chat123", "")

	conn.startThinkTimer()
	conn.mu.RLock()
	timer := conn.pendingThinkTimer
	conn.mu.RUnlock()
	require.NotNil(t, timer)

	conn.stopThinkTimer()
	conn.mu.RLock()
	timer = conn.pendingThinkTimer
	conn.mu.RUnlock()
	require.Nil(t, timer)
}

func TestFeishuConn_ThinkTimer_ResetClears(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := NewFeishuConn(adapter, "chat123", "")

	conn.startThinkTimer()
	conn.mu.RLock()
	require.NotNil(t, conn.pendingThinkTimer)
	conn.mu.RUnlock()

	conn.resetThinkTimer()
	conn.mu.RLock()
	require.Nil(t, conn.pendingThinkTimer)
	conn.mu.RUnlock()
}

func TestFeishuConn_ThinkTimer_ResetWhenNil(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := NewFeishuConn(adapter, "chat123", "")

	require.NotPanics(t, func() { conn.resetThinkTimer() })
}

func TestFeishuConn_ThinkTimer_StopWhenNil(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := NewFeishuConn(adapter, "chat123", "")

	require.NotPanics(t, func() { conn.stopThinkTimer() })
}

func TestFeishuConn_ThinkTimer_StartReplacesExisting(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := NewFeishuConn(adapter, "chat123", "")

	conn.startThinkTimer()
	conn.startThinkTimer()

	conn.mu.RLock()
	timer := conn.pendingThinkTimer
	conn.mu.RUnlock()
	require.NotNil(t, timer)

	conn.stopThinkTimer()
}

func TestFeishuConn_SendThinkPing_NoPlatformMsgID(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := NewFeishuConn(adapter, "chat123", "")

	require.NotPanics(t, func() { conn.sendThinkPing() })
}

func TestFeishuConn_SendThinkPing_NilLarkClient(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := NewFeishuConn(adapter, "chat123", "")
	conn.mu.Lock()
	conn.platformMsgID = "msg123"
	conn.mu.Unlock()

	require.NotPanics(t, func() { conn.sendThinkPing() })
}

func TestFeishuConn_WriteCtx_Done_StopsThinkTimer(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	adapter.interactions = messaging.NewInteractionManager(discardLogger)
	conn := NewFeishuConn(adapter, "chat123", "")

	conn.startThinkTimer()
	conn.mu.RLock()
	require.NotNil(t, conn.pendingThinkTimer)
	conn.mu.RUnlock()

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-done-timer",
		Event: events.Event{
			Type: events.Done,
			Data: events.DoneData{Success: true},
		},
	}
	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)

	conn.mu.RLock()
	require.Nil(t, conn.pendingThinkTimer)
	conn.mu.RUnlock()
}

func TestFeishuConn_WriteCtx_Error_StopsThinkTimer(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	adapter.interactions = messaging.NewInteractionManager(discardLogger)
	conn := NewFeishuConn(adapter, "chat123", "")

	conn.startThinkTimer()
	conn.mu.RLock()
	require.NotNil(t, conn.pendingThinkTimer)
	conn.mu.RUnlock()

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-err-timer",
		Event: events.Event{
			Type: events.Error,
			Data: events.ErrorData{Code: "TEST", Message: "test error"},
		},
	}
	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)

	conn.mu.RLock()
	require.Nil(t, conn.pendingThinkTimer)
	conn.mu.RUnlock()
}

func TestFeishuConn_WriteCtx_Error_ExtractsMessage(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	adapter.interactions = messaging.NewInteractionManager(discardLogger)
	conn := NewFeishuConn(adapter, "chat123", "")

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-err-msg",
		Event: events.Event{
			Type: events.Error,
			Data: events.ErrorData{Code: "TIMEOUT", Message: "turn timeout"},
		},
	}
	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)
}

func TestFeishuConn_WriteCtx_ToolCall_ResetsThinkTimer(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := NewFeishuConn(adapter, "chat123", "")
	conn.mu.Lock()
	conn.platformMsgID = "msg123"
	conn.startedAt = time.Now()
	conn.mu.Unlock()

	conn.startThinkTimer()
	conn.mu.RLock()
	require.NotNil(t, conn.pendingThinkTimer)
	conn.mu.RUnlock()

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-tool-timer",
		Event: events.Event{
			Type: events.ToolCall,
			Data: events.ToolCallData{ID: "tc1", Name: "ReadFile"},
		},
	}
	_ = conn.WriteCtx(context.Background(), env)

	conn.mu.RLock()
	require.Nil(t, conn.pendingThinkTimer)
	conn.mu.RUnlock()
}

func TestFeishuConn_WriteCtx_ToolResult_ResetsThinkTimer(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := NewFeishuConn(adapter, "chat123", "")
	conn.mu.Lock()
	conn.platformMsgID = "msg123"
	conn.startedAt = time.Now()
	conn.mu.Unlock()

	conn.startThinkTimer()
	conn.mu.RLock()
	require.NotNil(t, conn.pendingThinkTimer)
	conn.mu.RUnlock()

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-result-timer",
		Event: events.Event{
			Type: events.ToolResult,
			Data: events.ToolResultData{ID: "tc1"},
		},
	}
	_ = conn.WriteCtx(context.Background(), env)

	conn.mu.RLock()
	require.Nil(t, conn.pendingThinkTimer)
	conn.mu.RUnlock()
}

func TestFeishuConn_Close_StopsThinkTimer(t *testing.T) {
	t.Parallel()
	adapter := newTestAdapter(t)
	conn := NewFeishuConn(adapter, "chat123", "")

	conn.startThinkTimer()
	conn.mu.RLock()
	require.NotNil(t, conn.pendingThinkTimer)
	conn.mu.RUnlock()

	conn.Close()

	conn.mu.RLock()
	require.Nil(t, conn.pendingThinkTimer)
	conn.mu.RUnlock()
}
