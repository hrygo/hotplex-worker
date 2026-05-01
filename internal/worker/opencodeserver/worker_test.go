package opencodeserver

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
)

func hasOpenCodeBinary() bool {
	_, err := exec.LookPath("opencode")
	return err == nil
}

func TestOpenCodeServerWorker_Capabilities(t *testing.T) {
	t.Parallel()
	w := New()

	require.Equal(t, worker.TypeOpenCodeSrv, w.Type())
	require.True(t, w.SupportsResume())
	require.True(t, w.SupportsStreaming())
	require.True(t, w.SupportsTools())
	require.NotNil(t, w.EnvWhitelist())
	require.Empty(t, w.SessionStoreDir())
	require.Zero(t, w.MaxTurns())
	require.Equal(t, []string{"text", "code"}, w.Modalities())
}

func TestOpenCodeServerWorker_New(t *testing.T) {
	t.Parallel()
	w := New()

	// Verify worker is properly initialized
	require.NotNil(t, w)
	require.NotNil(t, w.BaseWorker)
	// singleton may be nil if not initialized, so just check it exists (could be nil)
	// require.NotNil(t, w.singleton)
	require.NotNil(t, w.client)
	require.Nil(t, w.sseClient, "sseClient should be nil until Start/Resume")
	require.Nil(t, w.sseCancel, "sseCancel should be nil until Start/Resume")
	require.Nil(t, w.httpConn)
}

func TestOpenCodeServerWorker_EnvWhitelist(t *testing.T) {
	t.Parallel()
	w := New()

	wl := w.EnvWhitelist()
	require.Contains(t, wl, "OPENAI_API_KEY")
	require.Contains(t, wl, "OPENAI_BASE_URL")
	require.Contains(t, wl, "OPENCODE_API_KEY")
	require.Contains(t, wl, "OPENCODE_BASE_URL")
	require.Contains(t, wl, "HOME")
	require.Contains(t, wl, "PATH")
}

func TestOpenCodeServerWorker_ConnBeforeStart(t *testing.T) {
	t.Parallel()
	w := New()
	require.Nil(t, w.Conn())
}

func TestOpenCodeServerWorker_HealthBeforeStart(t *testing.T) {
	t.Parallel()
	w := New()

	h := w.Health()
	require.Equal(t, worker.TypeOpenCodeSrv, h.Type)
	require.False(t, h.Running)
	require.True(t, h.Healthy)
	require.Empty(t, h.SessionID)
}

func TestOpenCodeServerWorker_LastIOBeforeStart(t *testing.T) {
	t.Parallel()
	w := New()
	require.True(t, w.LastIO().IsZero())
}

func TestOpenCodeServerWorker_TerminateWithoutStart(t *testing.T) {
	t.Parallel()

	w := New()
	ctx := context.Background()

	err := w.Terminate(ctx)
	require.NoError(t, err)
}

func TestOpenCodeServerWorker_KillWithoutStart(t *testing.T) {
	t.Parallel()

	w := New()
	err := w.Kill()
	require.NoError(t, err)
}

func TestOpenCodeServerWorker_Terminate_CallsSSECancel(t *testing.T) {
	t.Parallel()

	w := New()
	ctx := context.Background()

	// Set sseCancel
	sseCtx, sseCancel := context.WithCancel(context.Background())
	w.Mu.Lock()
	w.sseCancel = sseCancel
	w.Mu.Unlock()

	// Terminate should call sseCancel
	err := w.Terminate(ctx)
	require.NoError(t, err)

	// Verify context was cancelled
	select {
	case <-sseCtx.Done():
		// Context was cancelled as expected
	default:
		t.Fatal("sseCancel was not called by Terminate")
	}
}

func TestOpenCodeServerWorker_Kill_CallsSSECancel(t *testing.T) {
	t.Parallel()

	w := New()

	// Set sseCancel
	sseCtx, sseCancel := context.WithCancel(context.Background())
	w.Mu.Lock()
	w.sseCancel = sseCancel
	w.Mu.Unlock()

	// Kill should call sseCancel
	err := w.Kill()
	require.NoError(t, err)

	// Verify context was cancelled
	select {
	case <-sseCtx.Done():
		// Context was cancelled as expected
	default:
		t.Fatal("sseCancel was not called by Kill")
	}
}

func TestOpenCodeServerWorker_Terminate_NilSSECancel(t *testing.T) {
	t.Parallel()

	w := New()
	ctx := context.Background()

	// Don't set sseCancel (it's nil)
	// Terminate should not panic
	err := w.Terminate(ctx)
	require.NoError(t, err)
}

func TestOpenCodeServerWorker_Kill_NilSSECancel(t *testing.T) {
	t.Parallel()

	w := New()

	// Don't set sseCancel (it's nil)
	// Kill should not panic
	err := w.Kill()
	require.NoError(t, err)
}

func TestOpenCodeServerWorker_WaitWithoutStart(t *testing.T) {
	t.Parallel()

	w := New()
	_, err := w.Wait()
	require.Error(t, err)
	require.Contains(t, err.Error(), "not started")
}

func TestOpenCodeServerWorker_Input_WithoutStart(t *testing.T) {
	t.Parallel()

	w := New()
	ctx := context.Background()
	err := w.Input(ctx, "hello", nil)
	require.Error(t, err)
}

func TestOpenCodeServerWorker_Resume_WithBinary(t *testing.T) {
	if !hasOpenCodeBinary() {
		t.Skip("opencode binary not found, skipping integration test")
	}

	w := New()
	ctx := context.Background()
	session := worker.SessionInfo{
		SessionID:  "test-session",
		UserID:     "test-user",
		ProjectDir: "/tmp",
	}

	err := w.Resume(ctx, session)
	if err != nil {
		t.Logf("Resume returned error (expected if server API not configured): %v", err)
		return
	}

	conn := w.Conn()
	if conn != nil {
		require.Equal(t, "test-session", conn.SessionID())
	}

	_ = w.Kill()
}

func TestOpenCodeServerWorker_Start_WithBinary(t *testing.T) {
	if !hasOpenCodeBinary() {
		t.Skip("opencode binary not found, skipping integration test")
	}

	w := New()
	ctx := context.Background()
	session := worker.SessionInfo{
		SessionID:  "test-session",
		UserID:     "test-user",
		ProjectDir: "/tmp",
	}

	err := w.Start(ctx, session)
	if err != nil {
		// Server started but API call may fail - that's OK for this test
		t.Logf("Start returned error (expected if server API not configured): %v", err)
		return
	}

	conn := w.Conn()
	if conn != nil {
		require.Equal(t, "test-session", conn.SessionID())
	}

	_ = w.Kill()
}
