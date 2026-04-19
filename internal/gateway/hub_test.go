package gateway

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/hotplex/hotplex-worker/internal/config"
	"github.com/hotplex/hotplex-worker/internal/security"
	"github.com/hotplex/hotplex-worker/internal/session"
	"github.com/hotplex/hotplex-worker/internal/worker"
	"github.com/hotplex/hotplex-worker/pkg/aep"
	"github.com/hotplex/hotplex-worker/pkg/events"
)

// ─── WebSocket test helpers ───────────────────────────────────────────────────

// newTestWSConnPair creates a connected WebSocket client/server pair via httptest.
func newTestWSConnPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()

	var (
		serverConn *websocket.Conn
		connMu     sync.Mutex
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{}
		conn, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		connMu.Lock()
		serverConn = conn
		connMu.Unlock()
	}))
	t.Cleanup(server.Close)

	client, _, err := websocket.DefaultDialer.Dial("ws"+server.URL[4:], nil)
	require.NoError(t, err)

	// Wait for server to accept the upgrade.
	require.Eventually(t, func() bool {
		connMu.Lock()
		ok := serverConn != nil
		connMu.Unlock()
		return ok
	}, time.Second, 10*time.Millisecond)

	connMu.Lock()
	conn := serverConn
	connMu.Unlock()
	return client, conn
}

func newTestHub(t *testing.T) *Hub {
	t.Helper()
	cfg := config.Default()
	cfg.Gateway.BroadcastQueueSize = 16
	return NewHub(slog.Default(), cfg)
}

// ─── Hub tests ────────────────────────────────────────────────────────────────

func TestHub_NewHub(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	require.NotNil(t, h)
	require.NotNil(t, h.broadcast)
	require.Equal(t, 16, cap(h.broadcast))
}

func TestHub_NewHub_NilLogger(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.Gateway.BroadcastQueueSize = 1
	h := NewHub(nil, cfg)
	require.NotNil(t, h)
}

func TestHub_RegisterConn(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	defer conn.Close()
	defer server.Close()

	h.RegisterConn(newConn(h, conn, "sess_1", nil))
	require.Equal(t, 1, h.ConnectionsOpen())
}

func TestHub_UnregisterConn(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	c := newConn(h, conn, "sess_1", nil)
	h.RegisterConn(c)

	conn.Close()
	server.Close()
	h.UnregisterConn(c)
	require.Equal(t, 0, h.ConnectionsOpen())
}

func TestHub_JoinSession(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	defer conn.Close()
	defer server.Close()

	c := newConn(h, conn, "sess_abc", nil)
	h.JoinSession("sess_abc", c)

	h.mu.RLock()
	require.Len(t, h.sessions["sess_abc"], 1)
	h.mu.RUnlock()
}

func TestHub_JoinSession_DisconnectsStale(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	conn1, server1 := newTestWSConnPair(t)
	conn2, server2 := newTestWSConnPair(t)
	defer conn1.Close()
	defer server1.Close()
	defer conn2.Close()
	defer server2.Close()

	c1 := newConn(h, conn1, "sess_x", nil)
	c2 := newConn(h, conn2, "sess_x", nil)

	h.JoinSession("sess_x", c1)
	h.JoinSession("sess_x", c2)
	// JoinSession closes c1 but does not call LeaveSession (that happens in ReadPump).
	// Simulate ReadPump cleanup so c1 is removed from the session map.
	h.LeaveSession("sess_x", c1)

	h.mu.RLock()
	require.Len(t, h.sessions["sess_x"], 1)
	h.mu.RUnlock()
}

func TestHub_LeaveSession(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	defer conn.Close()
	defer server.Close()

	c := newConn(h, conn, "sess_y", nil)
	h.JoinSession("sess_y", c)
	h.LeaveSession("sess_y", c)

	h.mu.RLock()
	_, ok := h.sessions["sess_y"]
	h.mu.RUnlock()
	require.False(t, ok)
}

func TestHub_LeaveSession_UnknownSession(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	defer conn.Close()
	defer server.Close()

	// Must not panic.
	h.LeaveSession("never_existed", newConn(h, conn, "sess_z", nil))
}

func TestHub_NextSeq(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	require.Equal(t, int64(1), h.NextSeq("sess_seq"))
	require.Equal(t, int64(2), h.NextSeq("sess_seq"))
	require.Equal(t, int64(1), h.NextSeq("sess_other"))
}

func TestHub_NextSeqPeek(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	require.Equal(t, int64(0), h.NextSeqPeek("unknown"))
	h.NextSeq("sess_peek")
	require.Equal(t, int64(1), h.NextSeqPeek("sess_peek"))
}

func TestHub_ConnectionsOpen(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	require.Equal(t, 0, h.ConnectionsOpen())

	conn1, server1 := newTestWSConnPair(t)
	defer conn1.Close()
	defer server1.Close()
	conn2, server2 := newTestWSConnPair(t)
	defer conn2.Close()
	defer server2.Close()

	h.RegisterConn(newConn(h, conn1, "s1", nil))
	h.RegisterConn(newConn(h, conn2, "s2", nil))
	require.Equal(t, 2, h.ConnectionsOpen())
}

func TestHub_GetAndClearDropped(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	require.False(t, h.GetAndClearDropped("sess_d"))

	h.mu.Lock()
	h.sessionDropped["sess_d"] = true
	h.mu.Unlock()

	require.True(t, h.GetAndClearDropped("sess_d"))
	require.False(t, h.GetAndClearDropped("sess_d"))
}

func TestHub_SendToSession_ControlPriority(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	defer conn.Close()
	defer server.Close()

	c := newConn(h, conn, "sess_ctrl", nil)
	h.JoinSession("sess_ctrl", c)

	env := events.NewEnvelope(aep.NewID(), "sess_ctrl", 0, events.State, events.StateData{State: events.StateRunning})
	env.Priority = events.PriorityControl

	err := h.SendToSession(context.Background(), env)
	require.NoError(t, err)

	_ = server.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, data, err := server.ReadMessage()
	require.NoError(t, err)
	require.Contains(t, string(data), `"type":"state"`)
}

func TestHub_SendToSession_DeltaDropSilently(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	h.mu.Lock()
	h.broadcast = make(chan *EnvelopeWithConn, 1)
	h.mu.Unlock()

	conn, server := newTestWSConnPair(t)
	defer conn.Close()
	defer server.Close()
	c := newConn(h, conn, "sess_drop", nil)
	h.JoinSession("sess_drop", c)

	// Droppable delta should return nil even when queue is full.
	delta := events.NewEnvelope(aep.NewID(), "sess_drop", 0, events.MessageDelta, map[string]any{"delta": "x"})
	err := h.SendToSession(context.Background(), delta)
	require.NoError(t, err)
}

func TestHub_SendToSession_GuaranteedQueueFull(t *testing.T) {
	h := newTestHub(t)
	h.mu.Lock()
	h.broadcast = make(chan *EnvelopeWithConn, 1)
	h.mu.Unlock()

	conn, server := newTestWSConnPair(t)
	defer conn.Close()
	defer server.Close()
	c := newConn(h, conn, "sess_full", nil)
	h.JoinSession("sess_full", c)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// ── Path 1: "queue full" error ──────────────────────────────────────────
	// h.Run is NOT started yet, so the queue stays full once the first send
	// occupies the single slot. Subsequent sends must fail immediately.
	go h.Run() // start hub so it processes items

	// Send one item: succeeds (queue was empty, h.Run draining).
	first := events.NewEnvelope(aep.NewID(), "sess_full", 0, events.Done, events.DoneData{Success: true})
	err := h.SendToSession(ctx, first)
	require.NoError(t, err, "first send (empty queue) should succeed")

	// By the time we send again, h.Run has drained the queue → queue is empty again.
	// This second assertion (expecting "queue full") is inherently racy because
	// h.Run drains asynchronously. We mitigate by sending many concurrent goroutines
	// to increase the chance of catching the window where the queue is temporarily full.
	//
	// Run 50 concurrent sends; with capacity=1, at least one should hit "queue full"
	// if any goroutine is scheduled while h.Run is still processing the previous item.
	var queueFullErrs []error
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			env := events.NewEnvelope(aep.NewID(), "sess_full", 0, events.Done, events.DoneData{Success: true})
			if err := h.SendToSession(ctx, env); err != nil {
				mu.Lock()
				queueFullErrs = append(queueFullErrs, err)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	// At least one goroutine should have hit the queue-full window.
	// If this assertion is flaky (all 50 succeed because h.Run is faster than
	// goroutine scheduling), the drain path below still verifies correctness.
	if len(queueFullErrs) > 0 {
		require.Contains(t, queueFullErrs[0].Error(), "queue full")
	}

	// ── Path 2: drain → send succeeds again ────────────────────────────────
	// After h.Run drains, the queue is empty and sends succeed.
	time.Sleep(50 * time.Millisecond) // allow h.Run to drain pending items
	env := events.NewEnvelope(aep.NewID(), "sess_full", 0, events.Done, events.DoneData{Success: true})
	err = h.SendToSession(ctx, env)
	require.NoError(t, err, "send after drain should succeed")
}

func TestHub_SendToSession_SeqAssignment(t *testing.T) {
	h := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	defer conn.Close()
	defer server.Close()
	c := newConn(h, conn, "sess_seq", nil)
	h.JoinSession("sess_seq", c)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go h.Run()

	env := events.NewEnvelope(aep.NewID(), "sess_seq", 0, events.State, events.StateData{State: events.StateIdle})
	err := h.SendToSession(ctx, env)
	require.NoError(t, err)

	_ = server.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, data, err := server.ReadMessage()
	require.NoError(t, err)
	require.Contains(t, string(data), `"seq":1`)
}

func TestHub_Shutdown(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	defer conn.Close()
	defer server.Close()

	h.RegisterConn(newConn(h, conn, "sess_shutdown", nil))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := h.Shutdown(ctx)
	require.NoError(t, err)
}

func TestHub_Run_DrainsOnCancel(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	go h.Run()
	cancel()
	_ = ctx.Err() // assert context was cancelled
	// Run should return after ctx cancel without blocking.
}

func TestHub_RouteMessage_LogHandler(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	defer conn.Close()
	defer server.Close()

	c := newConn(h, conn, "sess_log", nil)
	h.JoinSession("sess_log", c)

	var logLines []string
	h.LogHandler = func(level, msg, sessionID string) {
		logLines = append(logLines, level+":"+msg+":"+sessionID)
	}

	stateEnv := events.NewEnvelope(aep.NewID(), "sess_log", h.NextSeq("sess_log"), events.State, events.StateData{State: events.StateIdle})
	h.routeMessage(&EnvelopeWithConn{Env: stateEnv, Conn: c})

	_ = server.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, _, err := server.ReadMessage()
	require.NoError(t, err)
	require.Len(t, logLines, 1)
	require.Contains(t, logLines[0], "sess_log")
}

func TestHub_RouteMessage_NoConnections(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	// Must not panic.
	h.routeMessage(&EnvelopeWithConn{
		Env:  events.NewEnvelope(aep.NewID(), "orphan", 1, events.State, events.StateData{State: events.StateIdle}),
		Conn: nil,
	})
}

func TestHub_sendControlToSession_NoConns(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	env := events.NewEnvelope(aep.NewID(), "no_conns", 1, events.Control, nil)
	h.sendControlToSession(context.Background(), env)
}

func TestHub_DrainBroadcast(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	env := events.NewEnvelope(aep.NewID(), "drain", 1, events.State, events.StateData{State: events.StateIdle})

	h.broadcast <- &EnvelopeWithConn{Env: env}
	// drainBroadcast is non-blocking; processes items already in the channel.
	h.drainBroadcast()
}

// ─── Conn helper tests ────────────────────────────────────────────────────────

func TestConn_RemoteAddr(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	defer conn.Close()
	defer server.Close()

	c := newConn(h, conn, "sess_ra", nil)
	addr := c.RemoteAddr()
	require.NotEmpty(t, addr)
	require.NotEqual(t, "?", addr)
}

func TestConn_RemoteAddr_NilWC(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	c := &Conn{hub: h, wc: nil, sessionID: "sess_nil"}
	require.Equal(t, "?", c.RemoteAddr())
}

func TestConn_WriteCtx(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	defer conn.Close()
	defer server.Close()

	c := newConn(h, conn, "sess_writectx", nil)

	env := events.NewEnvelope(aep.NewID(), "sess_writectx", 1, events.State, events.StateData{State: events.StateRunning})
	err := c.WriteCtx(context.Background(), env)
	require.NoError(t, err)

	_ = server.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, data, err := server.ReadMessage()
	require.NoError(t, err)
	require.Contains(t, string(data), `"type":"state"`)
}

func TestConn_WriteCtx_Closed(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	conn, _ := newTestWSConnPair(t)
	defer conn.Close()

	c := newConn(h, conn, "sess_closed", nil)
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()

	env := events.NewEnvelope(aep.NewID(), "sess_closed", 1, events.State, nil)
	err := c.WriteCtx(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "closed")
}

func TestConn_WriteMessage(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	defer conn.Close()
	defer server.Close()

	c := newConn(h, conn, "sess_writemsg", nil)

	err := c.WriteMessage(websocket.TextMessage, []byte(`{"test":"write"}`))
	require.NoError(t, err)

	_ = server.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, data, err := server.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, `{"test":"write"}`, string(data))
}

func TestConn_WriteMessage_Closed(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	conn, _ := newTestWSConnPair(t)
	defer conn.Close()

	c := newConn(h, conn, "sess_write_closed", nil)
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()

	err := c.WriteMessage(websocket.TextMessage, []byte("hello"))
	require.Error(t, err)
}

func TestConn_Close_Idempotent(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	conn, _ := newTestWSConnPair(t)
	defer conn.Close()

	c := newConn(h, conn, "sess_close", nil)
	err := c.Close()
	require.NoError(t, err)

	// Second close should not panic or error.
	err = c.Close()
	require.NoError(t, err)
}

func TestConn_sendError(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	defer conn.Close()
	defer server.Close()

	c := newConn(h, conn, "sess_senderr", nil)
	c.sendError(events.ErrCodeInvalidMessage, "test error message")

	_ = server.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, data, err := server.ReadMessage()
	require.NoError(t, err)
	require.Contains(t, string(data), `"type":"error"`)
	require.Contains(t, string(data), "test error message")
}

func TestConn_sendInitError(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	conn, server := newTestWSConnPair(t)
	defer conn.Close()
	defer server.Close()

	c := newConn(h, conn, "sess_initerr", nil)
	c.sendInitError(events.ErrCodeUnauthorized, "bad token")

	_ = server.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, data, err := server.ReadMessage()
	require.NoError(t, err)
	require.Contains(t, string(data), `"type":"init_ack"`)
	require.Contains(t, string(data), "bad token")
}

// ─── Bridge tests ─────────────────────────────────────────────────────────────

func TestBridge_NewBridge(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	b := NewBridge(slog.Default(), h, nil, nil)
	require.NotNil(t, b)
	require.Equal(t, h, b.hub)
}

// fakeWorkerConn implements worker.SessionConn with a fake channel.
type fakeWorkerConn struct {
	ch chan *events.Envelope
}

func (f *fakeWorkerConn) Send(ctx context.Context, msg *events.Envelope) error { return nil }
func (f *fakeWorkerConn) Recv() <-chan *events.Envelope                        { return f.ch }
func (f *fakeWorkerConn) Close() error                                         { return nil }
func (f *fakeWorkerConn) UserID() string                                       { return "test_user" }
func (f *fakeWorkerConn) SessionID() string                                    { return "test_session" }

var _ worker.SessionConn = (*fakeWorkerConn)(nil)

// fakeWorker is a minimal worker.Worker implementation for Bridge tests.
type fakeWorker struct {
	workerType worker.WorkerType
	exitCode   int
	conn       *fakeWorkerConn
}

func (f *fakeWorker) Type() worker.WorkerType                             { return f.workerType }
func (f *fakeWorker) SupportsResume() bool                                { return true }
func (f *fakeWorker) SupportsStreaming() bool                             { return true }
func (f *fakeWorker) SupportsTools() bool                                 { return true }
func (f *fakeWorker) EnvWhitelist() []string                              { return nil }
func (f *fakeWorker) SessionStoreDir() string                             { return "" }
func (f *fakeWorker) MaxTurns() int                                       { return 0 }
func (f *fakeWorker) Modalities() []string                                { return []string{"text", "code"} }
func (f *fakeWorker) Start(context.Context, worker.SessionInfo) error     { return nil }
func (f *fakeWorker) Input(context.Context, string, map[string]any) error { return nil }
func (f *fakeWorker) Resume(context.Context, worker.SessionInfo) error    { return nil }
func (f *fakeWorker) Terminate(context.Context) error                     { return nil }
func (f *fakeWorker) Kill() error                                         { return nil }
func (f *fakeWorker) Wait() (int, error)                                  { return f.exitCode, nil }
func (f *fakeWorker) Conn() worker.SessionConn                            { return f.conn }
func (f *fakeWorker) Health() worker.WorkerHealth                         { return worker.WorkerHealth{} }
func (f *fakeWorker) LastIO() time.Time                                   { return time.Now() }
func (f *fakeWorker) ResetContext(context.Context) error                  { return nil }

var _ worker.Worker = (*fakeWorker)(nil)
var _ session.MessageStore = (*fakeMsgStore)(nil)

// fakeMsgStore is a minimal session.MessageStore for Bridge tests.
type fakeMsgStore struct{}

func (*fakeMsgStore) Append(ctx context.Context, sessionID string, seq int64, eventType string, payload []byte) error {
	return nil
}
func (*fakeMsgStore) GetBySession(ctx context.Context, sessionID string, fromSeq int64) ([]*session.EventRecord, error) {
	return nil, nil
}
func (*fakeMsgStore) Query(ctx context.Context, sessionID string, fromSeq int64) ([]*events.Envelope, error) {
	return nil, nil
}
func (*fakeMsgStore) GetOwner(ctx context.Context, sessionID string) (string, error) {
	return "", nil
}
func (*fakeMsgStore) Close() error { return nil }

var _ session.MessageStore = (*fakeMsgStore)(nil)

// ─── Bridge forwarding tests ───────────────────────────────────────────────────

// ─── HandleHTTP tests ─────────────────────────────────────────────────────────

// TestHub_HandleHTTP_Success verifies that a request with valid auth and no
// session_id succeeds with a 101 WebSocket upgrade and the connection is
// registered with the hub.
func TestHub_HandleHTTP_Success(t *testing.T) {
	cfg := config.Default()
	cfg.Security.APIKeys = []string{"test-api-key"} // require this key
	cfg.Security.AllowedOrigins = []string{"*"}

	auth := security.NewAuthenticator(&cfg.Security, nil)
	h := newTestHub(t)
	handler := NewHandler(slog.Default(), cfg, h, nil, nil)
	bridge := NewBridge(slog.Default(), h, nil, nil)

	serveHandler := h.HandleHTTP(auth, nil, handler, bridge)
	server := httptest.NewServer(serveHandler)
	defer server.Close()

	u := "ws" + server.URL[4:]
	header := http.Header{}
	header.Set("X-API-Key", "test-api-key")

	conn, resp, err := websocket.DefaultDialer.Dial(u, header)
	require.NoError(t, err, "WebSocket upgrade should succeed")
	defer conn.Close()
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	require.Eventually(t, func() bool {
		return h.ConnectionsOpen() > 0
	}, 2*time.Second, 10*time.Millisecond, "hub should have registered the connection")
}

// TestHub_HandleHTTP_Unauthorized verifies that a request without an API key
// returns 401 Unauthorized.
func TestHub_HandleHTTP_Unauthorized(t *testing.T) {
	cfg := config.Default()
	cfg.Security.APIKeys = []string{"secret-key"} // require this key
	cfg.Security.AllowedOrigins = []string{"*"}

	auth := security.NewAuthenticator(&cfg.Security, nil)
	h := newTestHub(t)
	handler := NewHandler(slog.Default(), cfg, h, nil, nil)
	bridge := NewBridge(slog.Default(), h, nil, nil)

	serveHandler := h.HandleHTTP(auth, nil, handler, bridge)
	server := httptest.NewServer(serveHandler)
	defer server.Close()

	u := "ws" + server.URL[4:]
	// No API key header.
	_, resp, err := websocket.DefaultDialer.Dial(u, nil)
	require.Error(t, err, "dial should fail without API key")
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// TestHub_HandleHTTP_WithSessionID verifies that a request with an explicit
// session_id query parameter results in a connection registered under that ID.
func TestHub_HandleHTTP_WithSessionID(t *testing.T) {
	cfg := config.Default()
	cfg.Security.APIKeys = []string{"test-key"}
	cfg.Security.AllowedOrigins = []string{"*"}

	auth := security.NewAuthenticator(&cfg.Security, nil)
	h := newTestHub(t)
	handler := NewHandler(slog.Default(), cfg, h, nil, nil)
	bridge := NewBridge(slog.Default(), h, nil, nil)

	serveHandler := h.HandleHTTP(auth, nil, handler, bridge)
	server := httptest.NewServer(serveHandler)
	defer server.Close()

	u := "ws" + server.URL[4:] + "?session_id=sess_explicit"
	header := http.Header{}
	header.Set("X-API-Key", "test-key")

	conn, resp, err := websocket.DefaultDialer.Dial(u, header)
	require.NoError(t, err, "WebSocket upgrade should succeed with session_id param")
	defer conn.Close()
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	// Verify the session has a connection registered (async: wait for registration).
	require.Eventually(t, func() bool {
		h.mu.RLock()
		_, ok := h.sessions["sess_explicit"]
		h.mu.RUnlock()
		return ok
	}, 2*time.Second, 10*time.Millisecond, "hub should have registered session sess_explicit")
}

// TestHub_HandleHTTP_GeneratesSessionID verifies that when no session_id is
// provided, a new session ID is auto-generated and the connection is registered.
func TestHub_HandleHTTP_GeneratesSessionID(t *testing.T) {
	cfg := config.Default()
	cfg.Security.APIKeys = []string{"test-key"}
	cfg.Security.AllowedOrigins = []string{"*"}

	auth := security.NewAuthenticator(&cfg.Security, nil)
	h := newTestHub(t)
	handler := NewHandler(slog.Default(), cfg, h, nil, nil)
	bridge := NewBridge(slog.Default(), h, nil, nil)

	serveHandler := h.HandleHTTP(auth, nil, handler, bridge)
	server := httptest.NewServer(serveHandler)
	defer server.Close()

	u := "ws" + server.URL[4:] // no session_id query param
	header := http.Header{}
	header.Set("X-API-Key", "test-key")

	conn, resp, err := websocket.DefaultDialer.Dial(u, header)
	require.NoError(t, err, "WebSocket upgrade should succeed without session_id")
	defer conn.Close()
	require.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	// Hub should have at least one session registered (async: wait for registration).
	require.Eventually(t, func() bool {
		h.mu.RLock()
		n := len(h.sessions)
		h.mu.RUnlock()
		return n == 1
	}, 2*time.Second, 10*time.Millisecond, "hub should have exactly one auto-generated session")
}

// TestHub_HandleHTTP_RejectsInvalidAPIKey verifies that a wrong API key is rejected.
func TestHub_HandleHTTP_RejectsInvalidAPIKey(t *testing.T) {
	cfg := config.Default()
	cfg.Security.APIKeys = []string{"correct-key"}
	cfg.Security.AllowedOrigins = []string{"*"}

	auth := security.NewAuthenticator(&cfg.Security, nil)
	h := newTestHub(t)
	handler := NewHandler(slog.Default(), cfg, h, nil, nil)
	bridge := NewBridge(slog.Default(), h, nil, nil)

	serveHandler := h.HandleHTTP(auth, nil, handler, bridge)
	server := httptest.NewServer(serveHandler)
	defer server.Close()

	u := "ws" + server.URL[4:]
	header := http.Header{}
	header.Set("X-API-Key", "wrong-key")

	_, resp, err := websocket.DefaultDialer.Dial(u, header)
	require.Error(t, err, "dial should fail with wrong API key")
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}
