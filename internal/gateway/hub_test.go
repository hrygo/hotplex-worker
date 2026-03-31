package gateway

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"hotplex-worker/internal/config"
	"hotplex-worker/pkg/events"
)

// ─── isReadTimeout tests ────────────────────────────────────────────────────────

func TestIsReadTimeout(t *testing.T) {
	t.Parallel()

	require.False(t, isReadTimeout(nil))
	require.False(t, isReadTimeout(os.ErrClosed))
	require.True(t, isReadTimeout(os.ErrDeadlineExceeded))
}

// ─── broadcastQueueSize tests ──────────────────────────────────────────────────

func TestBroadcastQueueSize_Default(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Gateway.BroadcastQueueSize = 0
	require.Equal(t, 256, broadcastQueueSize(cfg))
}

func TestBroadcastQueueSize_Negative(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Gateway.BroadcastQueueSize = -1
	require.Equal(t, 256, broadcastQueueSize(cfg))
}

func TestBroadcastQueueSize_Positive(t *testing.T) {
	t.Parallel()

	cfg := config.Default()
	cfg.Gateway.BroadcastQueueSize = 512
	require.Equal(t, 512, broadcastQueueSize(cfg))
}

// ─── isDroppable tests ─────────────────────────────────────────────────────────

func TestIsDroppable(t *testing.T) {
	t.Parallel()

	require.True(t, isDroppable(events.MessageDelta))
	require.True(t, isDroppable(events.Raw))
	require.False(t, isDroppable(events.Error))
	require.False(t, isDroppable(events.Input))
	require.False(t, isDroppable(events.ToolCall))
	require.False(t, isDroppable(events.ToolResult))
	require.False(t, isDroppable(events.Control))
}

// ─── heartbeat tests ───────────────────────────────────────────────────────────

func TestHeartbeat_MarkAlive(t *testing.T) {
	t.Parallel()

	h := newHeartbeat(nil)
	h.MarkMissed()
	h.MarkMissed()
	require.Equal(t, 2, h.missed)

	h.MarkAlive()
	require.Equal(t, 0, h.missed)
}

func TestHeartbeat_MarkMissed_UnderLimit(t *testing.T) {
	t.Parallel()

	h := newHeartbeat(nil)
	exceeded := h.MarkMissed()
	require.False(t, exceeded)
	require.Equal(t, 1, h.missed)
}

func TestHeartbeat_MarkMissed_AtLimit(t *testing.T) {
	t.Parallel()

	h := newHeartbeat(nil)
	// maxMiss = 3
	h.MarkMissed()
	h.MarkMissed()
	h.MarkMissed()
	exceeded := h.MarkMissed() // 4th miss → exceeds maxMiss
	require.True(t, exceeded)
	require.Equal(t, 4, h.missed)
}

func TestHeartbeat_MarkMissed_AfterStop(t *testing.T) {
	t.Parallel()

	h := newHeartbeat(nil)
	h.Stop()

	exceeded := h.MarkMissed()
	require.False(t, exceeded) // stopped → always returns false
	require.Equal(t, 0, h.missed)
}

func TestHeartbeat_MissedCount(t *testing.T) {
	t.Parallel()

	h := newHeartbeat(nil)
	h.MarkMissed()
	h.MarkMissed()
	require.Equal(t, 2, h.MissedCount())
}

func TestHeartbeat_Stop_Idempotent(t *testing.T) {
	t.Parallel()

	h := newHeartbeat(nil)
	h.Stop()
	h.Stop() // second call should not panic

	select {
	case <-h.Stopped():
	default:
		t.Fatal("Stopped() should return closed channel")
	}
}

func TestHeartbeat_MarkAlive_AfterStop(t *testing.T) {
	t.Parallel()

	h := newHeartbeat(nil)
	h.MarkMissed()
	h.Stop()

	h.MarkAlive() // should be safe, no panic
	require.Equal(t, 0, h.missed)
}

// ─── SeqGen tests ───────────────────────────────────────────────────────────────

func TestSeqGen_Next_StartsAtOne(t *testing.T) {
	t.Parallel()

	g := NewSeqGen()
	n := g.Next("sess1")
	require.Equal(t, int64(1), n)
}

func TestSeqGen_Next_Increments(t *testing.T) {
	t.Parallel()

	g := NewSeqGen()
	g.Next("sess1")
	g.Next("sess1")
	n := g.Next("sess1")
	require.Equal(t, int64(3), n)
}

func TestSeqGen_Next_IndependentSessions(t *testing.T) {
	t.Parallel()

	g := NewSeqGen()
	require.Equal(t, int64(1), g.Next("sess_a"))
	require.Equal(t, int64(1), g.Next("sess_b"))
	require.Equal(t, int64(2), g.Next("sess_a"))
	require.Equal(t, int64(2), g.Next("sess_b"))
}

func TestSeqGen_Peek_ZeroForUnknown(t *testing.T) {
	t.Parallel()

	g := NewSeqGen()
	require.Equal(t, int64(0), g.Peek("unknown"))
}

func TestSeqGen_Peek_DoesNotIncrement(t *testing.T) {
	t.Parallel()

	g := NewSeqGen()
	g.Next("sess1")
	g.Next("sess1")
	require.Equal(t, int64(2), g.Peek("sess1"))
	require.Equal(t, int64(3), g.Next("sess1")) // increments to 3
	require.Equal(t, int64(3), g.Peek("sess1"))
}
