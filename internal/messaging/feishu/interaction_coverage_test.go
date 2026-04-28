package feishu

import (
	"context"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/pkg/events"
)

func TestCheckPendingInteraction_QuestionResponse(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.interactions = messaging.NewInteractionManager(discardLogger)
	a.rateLimiter = NewFeishuRateLimiter()
	t.Cleanup(func() { a.rateLimiter.Stop() })

	conn := a.GetOrCreateConn("chat_qr", "")
	conn.mu.Lock()
	conn.sessionID = "sess-qr"
	conn.mu.Unlock()

	var capturedMetadata map[string]any
	a.interactions.Register(&messaging.PendingInteraction{
		ID:        "q-1",
		SessionID: "sess-qr",
		Type:      events.QuestionRequest,
		Timeout:   5 * time.Minute,
		SendResponse: func(metadata map[string]any) {
			capturedMetadata = metadata
		},
	})

	consumed := a.checkPendingInteraction(context.Background(), "my answer", conn)
	require.True(t, consumed)
	require.NotNil(t, capturedMetadata)
	qr, ok := capturedMetadata["question_response"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "q-1", qr["id"])
}

func TestCheckPendingInteraction_ElicitationAccept(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.interactions = messaging.NewInteractionManager(discardLogger)
	a.rateLimiter = NewFeishuRateLimiter()
	t.Cleanup(func() { a.rateLimiter.Stop() })

	conn := a.GetOrCreateConn("chat_el", "")
	conn.mu.Lock()
	conn.sessionID = "sess-el"
	conn.mu.Unlock()

	var capturedMetadata map[string]any
	a.interactions.Register(&messaging.PendingInteraction{
		ID:        "el-1",
		SessionID: "sess-el",
		Type:      events.ElicitationRequest,
		Timeout:   5 * time.Minute,
		SendResponse: func(metadata map[string]any) {
			capturedMetadata = metadata
		},
	})

	consumed := a.checkPendingInteraction(context.Background(), "accept", conn)
	require.True(t, consumed)
	er := capturedMetadata["elicitation_response"].(map[string]any)
	require.Equal(t, "accept", er["action"])
}

func TestCheckPendingInteraction_ElicitationDecline(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.interactions = messaging.NewInteractionManager(discardLogger)
	a.rateLimiter = NewFeishuRateLimiter()
	t.Cleanup(func() { a.rateLimiter.Stop() })

	conn := a.GetOrCreateConn("chat_el2", "")
	conn.mu.Lock()
	conn.sessionID = "sess-el2"
	conn.mu.Unlock()

	var capturedMetadata map[string]any
	a.interactions.Register(&messaging.PendingInteraction{
		ID:        "el-2",
		SessionID: "sess-el2",
		Type:      events.ElicitationRequest,
		Timeout:   5 * time.Minute,
		SendResponse: func(metadata map[string]any) {
			capturedMetadata = metadata
		},
	})

	consumed := a.checkPendingInteraction(context.Background(), "decline", conn)
	require.True(t, consumed)
	er := capturedMetadata["elicitation_response"].(map[string]any)
	require.Equal(t, "decline", er["action"])
}

func TestCheckPendingInteraction_PermissionDeny(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.interactions = messaging.NewInteractionManager(discardLogger)
	a.rateLimiter = NewFeishuRateLimiter()
	t.Cleanup(func() { a.rateLimiter.Stop() })

	conn := a.GetOrCreateConn("chat_deny", "")
	conn.mu.Lock()
	conn.sessionID = "sess-deny"
	conn.mu.Unlock()

	var capturedMetadata map[string]any
	a.interactions.Register(&messaging.PendingInteraction{
		ID:        "perm-deny",
		SessionID: "sess-deny",
		Type:      events.PermissionRequest,
		Timeout:   5 * time.Minute,
		SendResponse: func(metadata map[string]any) {
			capturedMetadata = metadata
		},
	})

	consumed := a.checkPendingInteraction(context.Background(), "拒绝", conn)
	require.True(t, consumed)
	pr := capturedMetadata["permission_response"].(map[string]any)
	require.False(t, pr["allowed"].(bool))
	require.Equal(t, "user denied", pr["reason"])
}

func TestCheckPendingInteraction_NotPermissionText(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.interactions = messaging.NewInteractionManager(discardLogger)

	conn := a.GetOrCreateConn("chat_np", "")
	conn.mu.Lock()
	conn.sessionID = "sess-np"
	conn.mu.Unlock()

	a.interactions.Register(&messaging.PendingInteraction{
		ID:           "perm-np",
		SessionID:    "sess-np",
		Type:         events.PermissionRequest,
		Timeout:      5 * time.Minute,
		SendResponse: func(metadata map[string]any) {},
	})

	consumed := a.checkPendingInteraction(context.Background(), "random text", conn)
	require.False(t, consumed)
}

func TestCheckPendingInteraction_NoMatchingSession(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.interactions = messaging.NewInteractionManager(discardLogger)

	conn := a.GetOrCreateConn("chat_nomatch", "")
	conn.mu.Lock()
	conn.sessionID = "sess-other"
	conn.mu.Unlock()

	a.interactions.Register(&messaging.PendingInteraction{
		ID:           "perm-nm",
		SessionID:    "sess-different",
		Type:         events.PermissionRequest,
		Timeout:      5 * time.Minute,
		SendResponse: func(metadata map[string]any) {},
	})

	consumed := a.checkPendingInteraction(context.Background(), "allow", conn)
	require.False(t, consumed)
}

func TestChatQueue_TaskExecution(t *testing.T) {
	t.Parallel()
	q := NewChatQueue(discardLogger)
	t.Cleanup(func() { q.Close() })

	var executed atomic.Bool
	err := q.Enqueue("chat_exec_1", func(ctx context.Context) error {
		executed.Store(true)
		return nil
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
	require.True(t, executed.Load())
}

func TestChatQueue_TaskError(t *testing.T) {
	t.Parallel()
	q := NewChatQueue(discardLogger)
	t.Cleanup(func() { q.Close() })

	err := q.Enqueue("chat_err_1", func(ctx context.Context) error {
		return io.ErrUnexpectedEOF
	})
	require.NoError(t, err)

	time.Sleep(100 * time.Millisecond)
}

func TestChatQueue_AbortNonexistentChat(t *testing.T) {
	t.Parallel()
	q := NewChatQueue(discardLogger)
	t.Cleanup(func() { q.Close() })

	require.NotPanics(t, func() { q.Abort("nonexistent") })
}

func TestCheckPendingInteraction_ElicitationDecline_CN(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.interactions = messaging.NewInteractionManager(discardLogger)
	a.rateLimiter = NewFeishuRateLimiter()
	t.Cleanup(func() { a.rateLimiter.Stop() })

	conn := a.GetOrCreateConn("chat_el_cn", "")
	conn.mu.Lock()
	conn.sessionID = "sess-el-cn"
	conn.mu.Unlock()

	var capturedMetadata map[string]any
	a.interactions.Register(&messaging.PendingInteraction{
		ID:        "el-cn",
		SessionID: "sess-el-cn",
		Type:      events.ElicitationRequest,
		Timeout:   5 * time.Minute,
		SendResponse: func(metadata map[string]any) {
			capturedMetadata = metadata
		},
	})

	consumed := a.checkPendingInteraction(context.Background(), "取消", conn)
	require.True(t, consumed)
	er := capturedMetadata["elicitation_response"].(map[string]any)
	require.Equal(t, "decline", er["action"])
}

func TestCheckPendingInteraction_PermissionAllow_Variants(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		text string
	}{
		{"allow", "allow"},
		{"yes", "yes"},
		{"是", "是"},
		{"允许", "允许"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			a := newTestAdapter(t)
			a.interactions = messaging.NewInteractionManager(discardLogger)
			a.rateLimiter = NewFeishuRateLimiter()
			t.Cleanup(func() { a.rateLimiter.Stop() })

			conn := a.GetOrCreateConn("chat_"+tt.name, "")
			conn.mu.Lock()
			conn.sessionID = "sess-" + tt.name
			conn.mu.Unlock()

			var capturedMetadata map[string]any
			a.interactions.Register(&messaging.PendingInteraction{
				ID:        "perm-" + tt.name,
				SessionID: "sess-" + tt.name,
				Type:      events.PermissionRequest,
				Timeout:   5 * time.Minute,
				SendResponse: func(metadata map[string]any) {
					capturedMetadata = metadata
				},
			})

			consumed := a.checkPendingInteraction(context.Background(), tt.text, conn)
			require.True(t, consumed)
			pr := capturedMetadata["permission_response"].(map[string]any)
			require.True(t, pr["allowed"].(bool))
		})
	}
}
