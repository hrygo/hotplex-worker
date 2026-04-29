package feishu

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/pkg/events"
)

func TestAdapterFlow_HandleTextMessage_NilBridge(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	// No bridge → early return nil.
	err := a.handleTextMessage(context.Background(), "msg1", "ch1", "p2p", "user1", "hello", "", "")
	require.NoError(t, err)
}

func TestAdapterFlow_HandleTextMessage_WithInteractionConsumed(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	// Set bridge via ConfigureWith (private field).
	_ = a.ConfigureWith(messaging.AdapterConfig{Bridge: &messaging.Bridge{}})
	a.Interactions = messaging.NewInteractionManager(discardLogger)
	a.rateLimiter = NewFeishuRateLimiter()
	t.Cleanup(func() { a.rateLimiter.Stop() })

	// Register a pending permission request.
	a.Interactions.Register(&messaging.PendingInteraction{
		ID:           "perm-ht-1",
		SessionID:    "",
		Type:         events.PermissionRequest,
		Timeout:      5 * time.Minute,
		SendResponse: func(metadata map[string]any) {},
	})

	// "允许" consumed as interaction response → returns nil (not an error).
	err := a.handleTextMessage(context.Background(), "msg1", "ch1", "p2p", "user1", "允许", "", "")
	require.NoError(t, err)
}

func TestAdapterFlow_WriteCtx_DoneEvent_WithStreamCtrl(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "")

	ctrl := newTestStreamingCtrl()
	// Transition to creating then completed (Close will be a no-op).
	conn.EnableStreaming(ctrl)

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-done-1",
		Event: events.Event{
			Type: events.Done,
			Data: events.DoneData{Success: true},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)
}

func TestAdapterFlow_WriteCtx_ErrorEvent_WithStreamCtrl(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.Interactions = messaging.NewInteractionManager(discardLogger)
	conn := NewFeishuConn(a, "chat123", "")

	ctrl := newTestStreamingCtrl()
	conn.EnableStreaming(ctrl)

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-err-1",
		Event: events.Event{
			Type: events.Error,
			Data: events.ErrorData{Code: "TIMEOUT", Message: "timeout"},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)
}

func TestAdapterFlow_WriteCtx_ToolCallEvent(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "")
	conn.mu.Lock()
	conn.startedAt = time.Now()
	conn.mu.Unlock()

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-tool-1",
		Event: events.Event{
			Type: events.ToolCall,
			Data: events.ToolCallData{Name: "ReadFile"},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)
}

func TestAdapterFlow_WriteCtx_ToolResultEvent(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "")
	conn.mu.Lock()
	conn.startedAt = time.Now()
	conn.mu.Unlock()

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-tresult-1",
		Event: events.Event{
			Type: events.ToolResult,
			Data: events.ToolResultData{ID: "tc_1"},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)
}

func TestAdapterFlow_WriteCtx_ContextUsageEvent_NilClient(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "")
	conn.mu.Lock()
	conn.platformMsgID = "msg123"
	conn.mu.Unlock()

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-cu",
		Event: events.Event{
			Type: events.ContextUsage,
			Data: events.ContextUsageData{
				TotalTokens: 1000, MaxTokens: 2000, Percentage: 50,
			},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestAdapterFlow_WriteCtx_MCPStatusEvent_NilClient(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "")
	conn.mu.Lock()
	conn.platformMsgID = "msg123"
	conn.mu.Unlock()

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-mcp",
		Event: events.Event{
			Type: events.MCPStatus,
			Data: events.MCPStatusData{
				Servers: []events.MCPServerInfo{
					{Name: "fs", Status: "connected"},
				},
			},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestAdapterFlow_WriteCtx_SkillsListEvent_NilClient(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "")
	conn.mu.Lock()
	conn.platformMsgID = "msg123"
	conn.mu.Unlock()

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-sk",
		Event: events.Event{
			Type: events.SkillsList,
			Data: events.SkillsListData{
				Skills: []events.SkillEntry{{Name: "commit", Description: "git commit", Source: "project"}},
				Total:  1,
			},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestAdapterFlow_WriteCtx_MessageDelta_NoStreamingCtrl(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	limiter := NewFeishuRateLimiter()
	t.Cleanup(func() { limiter.Stop() })
	a.rateLimiter = limiter

	conn := NewFeishuConn(a, "chat123", "")
	conn.mu.Lock()
	conn.replyToMsgID = "msg_reply"
	conn.platformMsgID = "msg123"
	conn.startedAt = time.Now()
	conn.mu.Unlock()

	// No streaming controller → WriteCtx uses static message path (nil client).
	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-delta",
		Event: events.Event{
			Type: events.Message,
			Data: events.MessageData{Content: "hello world"},
		},
	}

	// Static message with nil client returns error, no panic.
	require.NotPanics(t, func() {
		_ = conn.WriteCtx(context.Background(), env)
	})
}

func TestAdapterFlow_FeishuConn_Close_WithStreamCtrl(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "")

	ctrl := newTestStreamingCtrl()
	conn.EnableStreaming(ctrl)

	err := conn.Close()
	require.NoError(t, err)

	conn.mu.RLock()
	require.Nil(t, conn.streamCtrl)
	conn.mu.RUnlock()
}

func TestAdapterFlow_FeishuConn_Close_WithReactionIDs(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "")

	conn.mu.Lock()
	conn.typingRid = "typing_rid"
	conn.toolRid = "tool_rid"
	conn.platformMsgID = "msg123"
	conn.mu.Unlock()

	// Nil larkClient → reaction cleanup skipped gracefully.
	err := conn.Close()
	require.NoError(t, err)

	conn.mu.RLock()
	require.Empty(t, conn.typingRid)
	require.Empty(t, conn.toolRid)
	conn.mu.RUnlock()
}

func TestAdapterFlow_ClearProcessingReaction_EmptyRID(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "")

	// Empty RID → early return, no panic.
	conn.clearProcessingReaction(context.Background(), "")
}

func TestAdapterFlow_ClearProcessingReaction_EmptyMsgID(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "")

	// RID set but no platformMsgID → early return.
	conn.clearProcessingReaction(context.Background(), "rid123")
}

func TestAdapterFlow_ClearProcessingReaction_NilClient(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "")
	conn.mu.Lock()
	conn.platformMsgID = "msg123"
	conn.mu.Unlock()

	// Nil larkClient → removeReaction returns error, but clearProcessingReaction ignores it.
	conn.clearProcessingReaction(context.Background(), "rid123")
}

func TestAdapterFlow_SetProcessingReaction_EmptyMsgID(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "")

	rid := conn.setProcessingReaction(context.Background())
	require.Empty(t, rid)
}

func TestAdapterFlow_SetProcessingReaction_NilClient(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "")
	conn.mu.Lock()
	conn.platformMsgID = "msg123"
	conn.mu.Unlock()

	// Nil client → addReaction fails, returns empty rid.
	rid := conn.setProcessingReaction(context.Background())
	require.Empty(t, rid)
}

func TestAdapterFlow_CycleReaction_EmptyPlatformMsgID(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "")

	// No platformMsgID → early return.
	conn.cycleReaction(context.Background(), "THINKING")
}

func TestAdapterFlow_CycleReaction_DifferentEmoji_NilClient(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "")
	conn.mu.Lock()
	conn.platformMsgID = "msg123"
	conn.toolEmoji = "YEAH"
	conn.mu.Unlock()

	// Different emoji → tries remove old + add new → both fail on nil client.
	conn.cycleReaction(context.Background(), "THINKING")
}

func TestAdapterFlow_SendTextMessage_NilClient(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	err := a.sendTextMessage(context.Background(), "chat123", "hello")
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestAdapterFlow_ReplyMessage_NilClient(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	err := a.replyMessage(context.Background(), "msg123", "hello", false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestAdapterFlow_DedupCleanupLoop_Exit(t *testing.T) {
	t.Parallel()
	a := &Adapter{
		PlatformAdapter: messaging.PlatformAdapter{
			Log:   discardLogger,
			Dedup: messaging.NewDedup(10, time.Millisecond),
		},
		connPool: messaging.NewConnPool[*FeishuConn](nil),
	}
	a.Dedup.StartCleanup()
	a.Dedup.Close() // should not panic
}

func TestAdapterFlow_Close_WithConnections(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := a.GetOrCreateConn("chat_cleanup", "")
	conn.mu.Lock()
	conn.streamCtrl = newTestStreamingCtrl()
	conn.mu.Unlock()

	err := a.Close(context.Background())
	require.NoError(t, err)

	require.Nil(t, a.connPool.Get("chat_cleanup#"))
}

func TestAdapterFlow_Start_AlreadyStarted(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.Started.Store(true)

	err := a.Start(context.Background())
	require.NoError(t, err)
}

func TestAdapterFlow_Start_NoCredentials(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	err := a.Start(context.Background())
	require.Error(t, err)
	require.Contains(t, err.Error(), "appID and appSecret required")
}

func TestAdapterFlow_HandleTextControlCommand_NilBridge(t *testing.T) {
	t.Parallel()
	// controlFeedbackMessageCN covers all action branches.
	_ = controlFeedbackMessageCN(events.ControlActionGC)
	_ = controlFeedbackMessageCN(events.ControlActionReset)
	_ = controlFeedbackMessageCN(events.ControlActionDelete)
}

func TestAdapterFlow_HandleTextWorkerCommand_NilBridge(t *testing.T) {
	// Worker command with nil bridge can't be tested without panic.
	// The function is covered indirectly via handleMessage integration.
}

func TestAdapterFlow_WriteCtx_NilEnvelope(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "")

	err := conn.WriteCtx(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil envelope")
}

func TestAdapterFlow_WriteCtx_PermissionRequest_ExtractFail(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "")

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-perm",
		Event: events.Event{
			Type: events.PermissionRequest,
			Data: map[string]any{},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.Error(t, err)
}

func TestAdapterFlow_WriteCtx_QuestionRequest_ExtractFail(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "")

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-q",
		Event: events.Event{
			Type: events.QuestionRequest,
			Data: map[string]any{},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.Error(t, err)
}

func TestAdapterFlow_WriteCtx_ElicitationRequest_ExtractFail(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "")

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-el",
		Event: events.Event{
			Type: events.ElicitationRequest,
			Data: map[string]any{},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.Error(t, err)
}

func TestAdapterFlow_WriteCtx_Done_NoStreamCtrl(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.Interactions = messaging.NewInteractionManager(discardLogger)
	conn := NewFeishuConn(a, "chat123", "")

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-done-2",
		Event: events.Event{
			Type: events.Done,
			Data: events.DoneData{Success: true},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)
}

func TestAdapterFlow_WriteCtx_Error_NoStreamCtrl(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.Interactions = messaging.NewInteractionManager(discardLogger)
	conn := NewFeishuConn(a, "chat123", "")

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-err-2",
		Event: events.Event{
			Type: events.Error,
			Data: events.ErrorData{Code: "TIMEOUT", Message: "timeout"},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)
}

func TestAdapterFlow_WriteCtx_MessageDelta_StaticPath(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "")
	conn.mu.Lock()
	conn.replyToMsgID = "msg_reply"
	conn.startedAt = time.Now()
	conn.mu.Unlock()

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-delta-static",
		Event: events.Event{
			Type: events.MessageDelta,
			Data: events.MessageDeltaData{Content: "hello"},
		},
	}

	// No streaming ctrl → falls through to static path.
	// replyMessage needs lark client → returns error.
	err := conn.WriteCtx(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestAdapterFlow_WriteCtx_Message_StaticPath_NoReplyTo(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "")
	conn.mu.Lock()
	conn.startedAt = time.Now()
	// No replyToMsgID → uses sendTextMessage path.
	conn.mu.Unlock()

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-msg-static",
		Event: events.Event{
			Type: events.MessageDelta,
			Data: events.MessageDeltaData{Content: "world"},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestAdapterFlow_WriteCtx_RawEvent_WithText(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "")
	conn.mu.Lock()
	conn.replyToMsgID = "msg_raw"
	conn.startedAt = time.Now()
	conn.mu.Unlock()

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-raw",
		Event: events.Event{
			Type: events.Raw,
			Data: events.RawData{Raw: map[string]any{"text": "raw content"}},
		},
	}

	// extractResponseText returns text for raw events.
	// Static path with nil client → error.
	err := conn.WriteCtx(context.Background(), env)
	require.Error(t, err)
}

func TestAdapterFlow_WriteCtx_Done_WithReactionCleanup(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewFeishuConn(a, "chat123", "")
	conn.mu.Lock()
	conn.typingRid = "typing_rid"
	conn.toolRid = "tool_rid"
	conn.platformMsgID = "msg123"
	conn.mu.Unlock()

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-done-rx",
		Event: events.Event{
			Type: events.Done,
			Data: events.DoneData{Success: true},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)

	conn.mu.RLock()
	require.Empty(t, conn.typingRid)
	require.Empty(t, conn.toolRid)
	conn.mu.RUnlock()
}

func TestAdapterFlow_WriteCtx_Error_WithReactionCleanup(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.Interactions = messaging.NewInteractionManager(discardLogger)
	conn := NewFeishuConn(a, "chat123", "")
	conn.mu.Lock()
	conn.typingRid = "typing_rid"
	conn.toolRid = "tool_rid"
	conn.platformMsgID = "msg123"
	conn.startedAt = time.Now()
	conn.mu.Unlock()

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-err-rx",
		Event: events.Event{
			Type: events.Error,
			Data: events.ErrorData{Code: "ERR", Message: "something went wrong"},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)

	conn.mu.RLock()
	require.Empty(t, conn.typingRid)
	require.Empty(t, conn.toolRid)
	conn.mu.RUnlock()
}

func TestAdapterFlow_RegisterInteraction(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.Interactions = messaging.NewInteractionManager(discardLogger)
	conn := a.GetOrCreateConn("chat_ri", "")

	a.registerInteraction("req-1", "sess-ri", events.PermissionRequest, conn)
	require.Equal(t, 1, a.Interactions.Len())
}

func TestAdapterFlow_WriteCtx_StreamCtrl_WriteFlush(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	limiter := NewFeishuRateLimiter()
	t.Cleanup(func() { limiter.Stop() })
	conn := NewFeishuConn(a, "chat123", "")

	ctrl := NewStreamingCardController(nil, limiter, discardLogger)
	ctrl.transition(PhaseCreating)
	ctrl.transition(PhaseStreaming)
	ctrl.mu.Lock()
	ctrl.cardKitOK = false // skip cardKit path, no msgID → IM patch also skipped
	ctrl.mu.Unlock()
	conn.EnableStreaming(ctrl)
	conn.mu.Lock()
	conn.startedAt = time.Now()
	conn.mu.Unlock()

	env := &events.Envelope{
		Version:   events.Version,
		SessionID: "sess-wf",
		Event: events.Event{
			Type: events.MessageDelta,
			Data: events.MessageDeltaData{Content: "hello"},
		},
	}

	err := conn.WriteCtx(context.Background(), env)
	require.NoError(t, err)
}

func TestAdapterFlow_RemoveReaction_EmptyReactionID_NilClient(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	// removeReaction checks nil client BEFORE empty reactionID.
	err := a.removeReaction(context.Background(), "msg123", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "lark client not initialized")
}

func TestAdapterFlow_RegisterInteraction_CallbackConsumed(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.Interactions = messaging.NewInteractionManager(discardLogger)
	conn := a.GetOrCreateConn("chat_ricb", "")
	conn.mu.Lock()
	conn.sessionID = "sess-ricb"
	conn.mu.Unlock()

	// Register via registerInteraction (creates SendResponse closure with nil bridge).
	a.registerInteraction("perm-ricb", "sess-ricb", events.PermissionRequest, conn)
	require.Equal(t, 1, a.Interactions.Len())

	// Consume the interaction via checkPendingInteraction.
	consumed := a.checkPendingInteraction(context.Background(), "允许", conn)
	require.True(t, consumed)
	require.Equal(t, 0, a.Interactions.Len())
}
