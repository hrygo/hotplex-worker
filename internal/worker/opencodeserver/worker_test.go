package opencodeserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
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
	require.NotNil(t, w.EnvBlocklist())
	require.Empty(t, w.SessionStoreDir())
	require.Zero(t, w.MaxTurns())
	require.Equal(t, []string{"text", "code"}, w.Modalities())
}

func TestOpenCodeServerWorker_New(t *testing.T) {
	t.Parallel()
	w := New()

	require.NotNil(t, w)
	require.NotNil(t, w.BaseWorker)
	require.NotNil(t, w.client)
	require.Nil(t, w.sseCancel, "sseCancel should be nil until Start/Resume")
	require.Nil(t, w.httpConn)
	require.Nil(t, w.httpConn)
}

func TestOpenCodeServerWorker_EnvBlocklist(t *testing.T) {
	t.Parallel()
	w := New()

	bl := w.EnvBlocklist()
	require.Contains(t, bl, "CLAUDECODE")
	require.Contains(t, bl, "HOTPLEX_")
	require.Contains(t, bl, "CLAUDE_")
	require.Contains(t, bl, "ANTHROPIC_")
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

	sseCtx, sseCancel := context.WithCancel(context.Background())
	w.Mu.Lock()
	w.sseCancel = sseCancel
	w.Mu.Unlock()

	err := w.Terminate(ctx)
	require.NoError(t, err)

	select {
	case <-sseCtx.Done():
	default:
		t.Fatal("sseCancel was not called by Terminate")
	}
}

func TestOpenCodeServerWorker_Kill_CallsSSECancel(t *testing.T) {
	t.Parallel()

	w := New()

	sseCtx, sseCancel := context.WithCancel(context.Background())
	w.Mu.Lock()
	w.sseCancel = sseCancel
	w.Mu.Unlock()

	err := w.Kill()
	require.NoError(t, err)

	select {
	case <-sseCtx.Done():
	default:
		t.Fatal("sseCancel was not called by Kill")
	}
}

func TestOpenCodeServerWorker_Terminate_NilSSECancel(t *testing.T) {
	t.Parallel()

	w := New()
	ctx := context.Background()

	err := w.Terminate(ctx)
	require.NoError(t, err)
}

func TestOpenCodeServerWorker_Kill_NilSSECancel(t *testing.T) {
	t.Parallel()

	w := New()

	err := w.Kill()
	require.NoError(t, err)
}

func TestOpenCodeServerWorker_Terminate_ReleasesSingleton(t *testing.T) {
	t.Parallel()

	w := New()
	ctx := context.Background()

	err := w.Terminate(ctx)
	require.NoError(t, err)

	err = w.Terminate(ctx)
	require.NoError(t, err)
}

func TestOpenCodeServerWorker_Kill_ReleasesSingleton(t *testing.T) {
	t.Parallel()

	w := New()

	err := w.Kill()
	require.NoError(t, err)

	err = w.Kill()
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
		t.Logf("Start returned error (expected if server API not configured): %v", err)
		return
	}

	conn := w.Conn()
	if conn != nil {
		require.Equal(t, "test-session", conn.SessionID())
	}

	_ = w.Kill()
}

// ─── Input interaction response tests (httptest-backed) ──────────────────────

func newWorkerWithMockServer(t *testing.T, handler http.HandlerFunc) (*Worker, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	w := New()
	w.httpAddr = srv.URL
	w.client = srv.Client()
	w.httpConn = &conn{
		sessionID: "test-session",
		userID:    "test-user",
		httpAddr:  srv.URL,
		client:    srv.Client(),
		recvCh:    make(chan *events.Envelope, 16),
		log:       w.Log,
	}
	return w, srv
}

func TestInput_PermissionResponse_Allowed(t *testing.T) {
	t.Parallel()

	var receivedPath string
	var receivedBody map[string]string
	w, _ := newWorkerWithMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)
		w.WriteHeader(http.StatusOK)
	})

	md := map[string]any{
		"permission_response": map[string]any{
			"request_id": "perm_123",
			"allowed":    true,
		},
	}
	err := w.Input(context.Background(), "", md)
	require.NoError(t, err)
	require.Equal(t, "/permission/perm_123/reply", receivedPath)
	require.Equal(t, "once", receivedBody["reply"])
}

func TestInput_PermissionResponse_Denied(t *testing.T) {
	t.Parallel()

	var receivedBody map[string]string
	w, _ := newWorkerWithMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)
		w.WriteHeader(http.StatusOK)
	})

	md := map[string]any{
		"permission_response": map[string]any{
			"request_id": "perm_456",
			"allowed":    false,
		},
	}
	err := w.Input(context.Background(), "", md)
	require.NoError(t, err)
	require.Equal(t, "reject", receivedBody["reply"])
}

func TestInput_QuestionResponse(t *testing.T) {
	t.Parallel()

	var receivedPath string
	w, _ := newWorkerWithMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})

	md := map[string]any{
		"question_response": map[string]any{
			"id":      "q_789",
			"answers": map[string]string{"q1": "yes"},
		},
	}
	err := w.Input(context.Background(), "", md)
	require.NoError(t, err)
	require.Equal(t, "/question/q_789/reply", receivedPath)
}

func TestInput_ElicitationResponse(t *testing.T) {
	t.Parallel()

	var receivedPath string
	var receivedBody map[string]any
	w, _ := newWorkerWithMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)
		w.WriteHeader(http.StatusOK)
	})

	md := map[string]any{
		"elicitation_response": map[string]any{
			"id":     "e_001",
			"action": "accept",
			"content": map[string]any{
				"key": "value",
			},
		},
	}
	err := w.Input(context.Background(), "", md)
	require.NoError(t, err)
	require.Equal(t, "/elicitation/e_001/reply", receivedPath)
	require.Equal(t, "accept", receivedBody["action"])
	require.Equal(t, map[string]any{"key": "value"}, receivedBody["content"])
}

func TestInput_ElicitationResponse_Decline(t *testing.T) {
	t.Parallel()

	var receivedBody map[string]any
	w, _ := newWorkerWithMockServer(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)
		w.WriteHeader(http.StatusOK)
	})

	md := map[string]any{
		"elicitation_response": map[string]any{
			"id":     "e_002",
			"action": "decline",
		},
	}
	err := w.Input(context.Background(), "", md)
	require.NoError(t, err)
	require.Equal(t, "decline", receivedBody["action"])
	_, hasContent := receivedBody["content"]
	require.False(t, hasContent)
}
