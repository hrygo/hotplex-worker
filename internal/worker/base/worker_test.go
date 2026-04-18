package base

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hotplex/hotplex-worker/internal/worker"
)

func TestBaseWorker_HealthNotStarted(t *testing.T) {
	w := NewBaseWorker(slog.Default(), nil)

	health := w.Health(worker.TypeClaudeCode)

	require.Equal(t, worker.TypeClaudeCode, health.Type)
	require.Empty(t, health.SessionID)
	require.False(t, health.Running)
	require.True(t, health.Healthy)
	require.Equal(t, "0s", health.Uptime)
	// When Proc is nil, PID defaults to 0 (not -1 since we don't call PID())
	require.Equal(t, 0, health.PID)
}

func TestBaseWorker_LastIO(t *testing.T) {
	w := NewBaseWorker(slog.Default(), nil)

	initial := w.LastIO()
	require.True(t, initial.IsZero(), "initial LastIO should be zero time")

	now := time.Now()
	w.SetLastIO(now)
	got := w.LastIO()

	require.WithinDuration(t, now, got, time.Second)
}

func TestBaseWorker_SetConn(t *testing.T) {
	w := NewBaseWorker(slog.Default(), nil)

	// Conn should be nil initially.
	require.Nil(t, w.Conn())

	// Create a mock conn (we can't fully test without stdin, but nil-safe access works).
	conn := NewConn(slog.Default(), nil, "user1", "sess1")
	w.SetConn(conn)

	got := w.Conn()
	require.NotNil(t, got)
	require.Equal(t, "user1", got.UserID())
	require.Equal(t, "sess1", got.SessionID())
}

func TestBaseWorker_Conn_NilSafe(t *testing.T) {
	w := NewBaseWorker(slog.Default(), nil)

	// Accessing Conn() when conn is nil should return nil, not panic.
	require.Nil(t, w.Conn())

	// SetConn(nil) should also be safe.
	w.SetConn(nil)
	require.Nil(t, w.Conn())
}

func TestBaseWorker_Terminate_NilProc(t *testing.T) {
	w := NewBaseWorker(slog.Default(), nil)

	// Terminating when proc is nil should not error.
	err := w.Terminate(context.Background())
	require.NoError(t, err)
}

func TestBaseWorker_Kill_NilProc(t *testing.T) {
	w := NewBaseWorker(slog.Default(), nil)

	// Kill when proc is nil should not error.
	err := w.Kill()
	require.NoError(t, err)
}

func TestBaseWorker_Wait_NilProc(t *testing.T) {
	w := NewBaseWorker(slog.Default(), nil)

	// Wait when proc is nil should error.
	code, err := w.Wait()
	require.Equal(t, -1, code)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not started")
}

func TestConn_UserID(t *testing.T) {
	conn := NewConn(slog.Default(), nil, "test-user", "test-session")
	require.Equal(t, "test-user", conn.UserID())
}

func TestConn_SessionID(t *testing.T) {
	conn := NewConn(slog.Default(), nil, "test-user", "test-session")
	require.Equal(t, "test-session", conn.SessionID())
}

func TestConn_SetSessionID(t *testing.T) {
	conn := NewConn(slog.Default(), nil, "test-user", "original-session")

	// Update session ID.
	conn.SetSessionID("new-session")
	require.Equal(t, "new-session", conn.SessionID())
}

func TestConn_Recv(t *testing.T) {
	conn := NewConn(slog.Default(), nil, "test-user", "test-session")

	// Recv returns the channel.
	ch := conn.Recv()
	require.NotNil(t, ch)
}

func TestConn_Interface(t *testing.T) {
	// Compile-time interface satisfaction check.
	var _ worker.SessionConn = (*Conn)(nil)
}
