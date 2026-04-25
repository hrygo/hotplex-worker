package gateway

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// ─── Mock Store (for tests that need to stub specific store methods) ─────────────

type mockStore struct {
	mock.Mock
}

func (m *mockStore) Upsert(ctx context.Context, info *session.SessionInfo) error {
	args := m.Called(ctx, info)
	if args.Error(0) == nil {
		if ms, ok := args.Get(0).(*session.SessionInfo); ok {
			*info = *ms
		}
	}
	return args.Error(0)
}

func (m *mockStore) Get(ctx context.Context, id string) (*session.SessionInfo, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*session.SessionInfo), args.Error(1)
}

func (m *mockStore) List(ctx context.Context, userID, platform string, limit, offset int) ([]*session.SessionInfo, error) {
	args := m.Called(ctx, userID, platform, limit, offset)
	return args.Get(0).([]*session.SessionInfo), args.Error(1)
}

func (m *mockStore) GetExpiredMaxLifetime(ctx context.Context, now time.Time) ([]string, error) {
	args := m.Called(ctx, now)
	return args.Get(0).([]string), args.Error(1)
}

func (m *mockStore) GetExpiredIdle(ctx context.Context, now time.Time) ([]string, error) {
	args := m.Called(ctx, now)
	return args.Get(0).([]string), args.Error(1)
}

func (m *mockStore) DeleteTerminated(ctx context.Context, cutoff time.Time) error {
	args := m.Called(ctx, cutoff)
	return args.Error(0)
}

func (m *mockStore) DeletePhysical(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *mockStore) Close() error {
	return m.Called().Error(0)
}

// ─── Test Hub Factory ─────────────────────────────────────────────────────────

func newCtrlHub(t *testing.T) *Hub {
	t.Helper()
	return NewHub(slog.Default(), config.NewConfigStore(&config.Config{}, nil))
}

// ─── Real-store Handler Factory (for session-state-dependent tests) ──────────

func newHandlerWithRealStore(t *testing.T) (*Handler, *session.Manager, *Hub, func()) {
	t.Helper()
	h := newCtrlHub(t)
	cfg := config.Default()

	f, err := os.CreateTemp("", "gateway_ctrl_*.db")
	require.NoError(t, err)
	f.Close()
	tmpPath := f.Name()
	cleanup := func() { os.Remove(tmpPath) }

	cfg.DB.Path = tmpPath
	store, err := session.NewSQLiteStore(context.Background(), cfg)
	require.NoError(t, err)
	mgr, err := session.NewManager(context.Background(), slog.Default(), cfg, nil, store, nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		mgr.Close()
		store.Close()
		cleanup()
	})
	return NewHandler(slog.Default(), h, mgr, nil), mgr, h, cleanup
}

// ─── Mock-store Handler Factory (for tests that stub store methods) ───────────

func newHandlerWithMockStore(t *testing.T, store *mockStore) (*Handler, *Hub) {
	t.Helper()
	h := newCtrlHub(t)
	cfg := config.Default()
	store.On("Close").Return(nil)
	mgr, err := session.NewManager(context.Background(), slog.Default(), cfg, nil, store, nil)
	require.NoError(t, err)
	t.Cleanup(func() { mgr.Close() })
	return NewHandler(slog.Default(), h, mgr, nil), h
}

// ─── Envelope Helpers ─────────────────────────────────────────────────────────

func inputEnvelope(sessionID, content string) *events.Envelope {
	return events.NewEnvelope(aep.NewID(), sessionID, 1, events.Input, map[string]any{
		"content": content,
	})
}

func pingEnvelope(sessionID string) *events.Envelope {
	return events.NewEnvelope(aep.NewID(), sessionID, 1, events.Ping, nil)
}

func controlEnvelope(sessionID, action string) *events.Envelope {
	return events.NewEnvelope(aep.NewID(), sessionID, 1, events.Control, map[string]any{
		"action": action,
	})
}

// ─── handleInput tests ────────────────────────────────────────────────────────

func TestHandleInput_Success(t *testing.T) {
	t.Parallel()
	handler, mgr, _, _ := newHandlerWithRealStore(t)

	const sid = "sess_input_ok"
	// Create session in StateCreated so transition CREATED→RUNNING succeeds.
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)

	env := inputEnvelope(sid, "hello")
	err = handler.handleInput(context.Background(), env)
	require.NoError(t, err)
}

func TestHandleInput_SessionNotFound(t *testing.T) {
	t.Parallel()
	store := new(mockStore)
	store.Test(t)
	bg := context.Background()
	store.On("Get", bg, "sess_missing").Return(nil, session.ErrSessionNotFound).Maybe()

	handler, _ := newHandlerWithMockStore(t, store)
	env := inputEnvelope("sess_missing", "hello")
	err := handler.handleInput(bg, env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "session not found")
}

func TestHandleInput_SessionNotActive(t *testing.T) {
	t.Parallel()
	handler, mgr, _, _ := newHandlerWithRealStore(t)

	const sid = "sess_inactive"
	// Create session and transition to TERMINATED state.
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)
	err = mgr.Transition(context.Background(), sid, events.StateTerminated)
	require.NoError(t, err)

	env := inputEnvelope(sid, "hello")
	err = handler.handleInput(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not active")
}

func TestHandleInput_RunningSession_AcceptsInput(t *testing.T) {
	t.Parallel()
	handler, mgr, _, _ := newHandlerWithRealStore(t)

	const sid = "sess_inv_trans"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)
	// Transition CREATED→RUNNING (valid).
	err = mgr.Transition(context.Background(), sid, events.StateRunning)
	require.NoError(t, err)

	// When session is RUNNING, handleInput delivers input to worker without error.
	// This tests the fix: handleInput no longer tries TransitionWithInput for RUNNING,
	// avoiding the invalid transition error.
	env := inputEnvelope(sid, "hello")
	err = handler.handleInput(context.Background(), env)
	// No error — input is accepted and delivered to worker.
	require.NoError(t, err)
}

// ─── handlePing tests ─────────────────────────────────────────────────────────

func TestHandlePing_KnownSession(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_ping"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)

	conn, _ := newTestWSConnPair(t)
	t.Cleanup(func() { conn.Close() })
	hub.JoinSession(sid, newConn(hub, conn, sid, nil))

	env := pingEnvelope(sid)
	err = handler.handlePing(context.Background(), env)
	require.NoError(t, err)
}

func TestHandlePing_UnknownSession(t *testing.T) {
	t.Parallel()
	store := new(mockStore)
	store.Test(t)
	store.On("Get", mock.Anything, "sess_ping_missing").Return(nil, session.ErrSessionNotFound)

	handler, _ := newHandlerWithMockStore(t, store)

	env := pingEnvelope("sess_ping_missing")
	err := handler.handlePing(context.Background(), env)
	// Ping always succeeds even if session unknown; state falls back to "unknown".
	require.NoError(t, err)
}

// ─── handleControl tests ──────────────────────────────────────────────────────

func TestHandleControl_Terminate_Success(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_term"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)
	err = mgr.Transition(context.Background(), sid, events.StateRunning)
	require.NoError(t, err)

	conn, _ := newTestWSConnPair(t)
	t.Cleanup(func() { conn.Close() })
	hub.JoinSession(sid, newConn(hub, conn, sid, nil))

	env := controlEnvelope(sid, string(events.ControlActionTerminate))
	env.OwnerID = "user1"
	err = handler.handleControl(context.Background(), env)
	require.NoError(t, err)
}

func TestHandleControl_Terminate_Unauthorized(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_term_unauth"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)
	err = mgr.Transition(context.Background(), sid, events.StateRunning)
	require.NoError(t, err)

	conn, _ := newTestWSConnPair(t)
	t.Cleanup(func() { conn.Close() })
	hub.JoinSession(sid, newConn(hub, conn, sid, nil))

	env := controlEnvelope(sid, string(events.ControlActionTerminate))
	env.OwnerID = "other_user"
	err = handler.handleControl(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ownership required")
}

func TestHandleControl_Delete_Success(t *testing.T) {
	t.Parallel()
	handler, mgr, _, _ := newHandlerWithRealStore(t)

	const sid = "sess_del"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)
	err = mgr.Transition(context.Background(), sid, events.StateRunning)
	require.NoError(t, err)

	env := controlEnvelope(sid, string(events.ControlActionDelete))
	env.OwnerID = "user1"
	err = handler.handleControl(context.Background(), env)
	require.NoError(t, err)
}

func TestHandleControl_Delete_Unauthorized(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_del_unauth"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)
	err = mgr.Transition(context.Background(), sid, events.StateRunning)
	require.NoError(t, err)

	conn, _ := newTestWSConnPair(t)
	t.Cleanup(func() { conn.Close() })
	hub.JoinSession(sid, newConn(hub, conn, sid, nil))

	env := controlEnvelope(sid, string(events.ControlActionDelete))
	env.OwnerID = "hacker"
	err = handler.handleControl(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "ownership required")
}

func TestHandleControl_UnknownAction(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_unknown"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)
	err = mgr.Transition(context.Background(), sid, events.StateRunning)
	require.NoError(t, err)

	conn, _ := newTestWSConnPair(t)
	t.Cleanup(func() { conn.Close() })
	hub.JoinSession(sid, newConn(hub, conn, sid, nil))

	env := controlEnvelope(sid, "fly_to_the_moon")
	env.OwnerID = "user1"
	err = handler.handleControl(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown control action")
}

func TestHandleControl_InvalidData(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_bad"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)

	conn, _ := newTestWSConnPair(t)
	t.Cleanup(func() { conn.Close() })
	hub.JoinSession(sid, newConn(hub, conn, sid, nil))

	badEnv := events.NewEnvelope(aep.NewID(), sid, 1, events.Control, "not a map")
	badEnv.OwnerID = "user1"
	err = handler.handleControl(context.Background(), badEnv)
	require.Error(t, err)
	require.Contains(t, err.Error(), "invalid data")
}

// ─── Handle (event router) tests ─────────────────────────────────────────────

func TestHandle_InputRoute(t *testing.T) {
	t.Parallel()
	handler, mgr, _, _ := newHandlerWithRealStore(t)

	const sid = "sess_handle"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)

	env := inputEnvelope(sid, "test")
	err = handler.Handle(context.Background(), env)
	require.NoError(t, err)
}

func TestHandle_PingRoute(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_hping"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)

	conn, _ := newTestWSConnPair(t)
	t.Cleanup(func() { conn.Close() })
	hub.JoinSession(sid, newConn(hub, conn, sid, nil))

	env := pingEnvelope(sid)
	err = handler.Handle(context.Background(), env)
	require.NoError(t, err)
}

func TestHandle_ControlRoute(t *testing.T) {
	t.Parallel()
	handler, mgr, _, _ := newHandlerWithRealStore(t)

	const sid = "sess_hctrl"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)
	err = mgr.Transition(context.Background(), sid, events.StateRunning)
	require.NoError(t, err)

	env := controlEnvelope(sid, string(events.ControlActionDelete))
	env.OwnerID = "user1"
	err = handler.Handle(context.Background(), env)
	require.NoError(t, err)
}

func TestHandle_UnknownEventType(t *testing.T) {
	t.Parallel()
	handler, mgr, _, _ := newHandlerWithRealStore(t)

	const sid = "sess_unknown_evt"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)

	// Unknown event type: Handle now returns the error from sendErrorf.
	env := events.NewEnvelope(aep.NewID(), sid, 1, "fly_to_the_moon", nil)
	env.OwnerID = "user1"
	err = handler.Handle(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown event type")
}

func TestHandle_InputMalformedData(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_mal"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)

	conn, _ := newTestWSConnPair(t)
	t.Cleanup(func() { conn.Close() })
	hub.JoinSession(sid, newConn(hub, conn, sid, nil))

	badEnv := events.NewEnvelope(aep.NewID(), sid, 1, events.Input, "not a map")
	badEnv.OwnerID = "user1"
	err = handler.Handle(context.Background(), badEnv)
	require.Error(t, err)
}

// ─── SendControlToSession tests ───────────────────────────────────────────────

func TestSendControlToSession_Reconnect(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_reconn"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)

	clientConn, serverConn := newTestWSConnPair(t)
	t.Cleanup(func() { clientConn.Close() })
	hub.JoinSession(sid, newConn(hub, clientConn, sid, nil))

	err = handler.SendControlToSession(context.Background(), sid,
		events.ControlActionReconnect, "worker restarted", map[string]any{"delay_ms": 1000})
	require.NoError(t, err)

	_ = serverConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, data, err := serverConn.ReadMessage()
	require.NoError(t, err)
	var env events.Envelope
	err = json.Unmarshal(data, &env)
	require.NoError(t, err)
	require.Equal(t, events.Control, env.Event.Type)
}

func TestSendControlToSession_SessionInvalid(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_inval"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)

	clientConn, serverConn := newTestWSConnPair(t)
	t.Cleanup(func() { clientConn.Close() })
	hub.JoinSession(sid, newConn(hub, clientConn, sid, nil))

	err = handler.SendSessionInvalid(context.Background(), sid, "session deleted", true)
	require.NoError(t, err)

	_ = serverConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, data, err := serverConn.ReadMessage()
	require.NoError(t, err)
	var env events.Envelope
	err = json.Unmarshal(data, &env)
	require.NoError(t, err)
	require.Equal(t, events.Control, env.Event.Type)
}

func TestSendControlToSession_Throttle(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_throttle"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)

	clientConn, serverConn := newTestWSConnPair(t)
	t.Cleanup(func() { clientConn.Close() })
	hub.JoinSession(sid, newConn(hub, clientConn, sid, nil))

	err = handler.SendThrottle(context.Background(), sid, 5000, 10)
	require.NoError(t, err)

	_ = serverConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, data, err := serverConn.ReadMessage()
	require.NoError(t, err)
	var env events.Envelope
	err = json.Unmarshal(data, &env)
	require.NoError(t, err)
	require.Equal(t, events.Control, env.Event.Type)
}

// ─── SendReconnect test ────────────────────────────────────────────────────────

func TestSendReconnect(t *testing.T) {
	t.Parallel()
	handler, mgr, hub, _ := newHandlerWithRealStore(t)

	const sid = "sess_send_reconn"
	_, err := mgr.Create(context.Background(), sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)

	clientConn, serverConn := newTestWSConnPair(t)
	t.Cleanup(func() { clientConn.Close() })
	hub.JoinSession(sid, newConn(hub, clientConn, sid, nil))

	err = handler.SendReconnect(context.Background(), sid, "worker restarted", 500)
	require.NoError(t, err)

	_ = serverConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, data, err := serverConn.ReadMessage()
	require.NoError(t, err)
	var env2 events.Envelope
	err = json.Unmarshal(data, &env2)
	require.NoError(t, err)
	require.Equal(t, events.Control, env2.Event.Type)
}

// ─── Heartbeat tests ──────────────────────────────────────────────────────────

func TestHeartbeat_MarkMissed_ResetsOnMax(t *testing.T) {
	t.Parallel()
	h := newHeartbeat(slog.Default())
	// First 2 misses should not trigger max
	require.False(t, h.MarkMissed())
	require.False(t, h.MarkMissed())
	// Third miss reaches max (maxMiss=3)
	require.True(t, h.MarkMissed())
	// After MarkAlive resets, counter goes back to 0
	h.MarkAlive()
	require.Equal(t, 0, h.MissedCount())
}

func TestHeartbeat_MarkMissed_AfterStop(t *testing.T) {
	t.Parallel()
	h := newHeartbeat(slog.Default())
	h.Stop()
	// After stop, MarkMissed returns false (max not exceeded)
	require.False(t, h.MarkMissed())
}

func TestHeartbeat_MarkAlive_ResetsCounter(t *testing.T) {
	t.Parallel()
	h := newHeartbeat(slog.Default())
	require.Equal(t, 0, h.MissedCount())
	h.MarkMissed()
	require.Equal(t, 1, h.MissedCount())
	h.MarkAlive()
	require.Equal(t, 0, h.MissedCount())
}

func TestHeartbeat_Stop_Idempotent(t *testing.T) {
	t.Parallel()
	h := newHeartbeat(slog.Default())
	h.Stop()
	// Second stop: should be no-op (idempotent)
	h.Stop()
	select {
	case _, ok := <-h.Stopped():
		require.False(t, ok, "channel should be closed")
	default:
		t.Fatal("expected channel to be closed")
	}
}
