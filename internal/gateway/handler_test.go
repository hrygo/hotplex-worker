package gateway

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
	noopworker "github.com/hrygo/hotplex/internal/worker/noop"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// mockHandlerSM is a mock session manager for handler tests.
type mockHandlerSM struct {
	mock.Mock
}

func (m *mockHandlerSM) ValidateOwnership(ctx context.Context, sessionID, userID, adminUserID string) error {
	args := m.Called(ctx, sessionID, userID, adminUserID)
	return args.Error(0)
}

func (m *mockHandlerSM) ClearContext(ctx context.Context, sessionID string) error {
	args := m.Called(ctx, sessionID)
	return args.Error(0)
}

func (m *mockHandlerSM) TransitionWithReason(ctx context.Context, id string, to events.SessionState, termReason string) error {
	args := m.Called(ctx, id, to, termReason)
	return args.Error(0)
}

func (m *mockHandlerSM) GetWorker(id string) worker.Worker {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(worker.Worker)
}

func (m *mockHandlerSM) DetachWorker(id string) {
	m.Called(id)
}

func (m *mockHandlerSM) Get(id string) (*session.SessionInfo, error) {
	args := m.Called(id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*session.SessionInfo), args.Error(1)
}

// mockHandlerHub captures sent envelopes for verification.
type mockHandlerHub struct {
	mu      sync.Mutex
	sent    []*events.Envelope
	nextSeq int64
}

func newMockHub() *mockHandlerHub {
	return &mockHandlerHub{nextSeq: 1}
}

func (h *mockHandlerHub) NextSeq(_ string) int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	seq := h.nextSeq
	h.nextSeq++
	return seq
}

func (h *mockHandlerHub) SendToSession(_ context.Context, env *events.Envelope, _ ...func()) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sent = append(h.sent, env)
	return nil
}

func (h *mockHandlerHub) Sent() []*events.Envelope {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.sent
}

func (h *mockHandlerHub) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.sent = nil
}

// testableHandler gives us access to handleReset/handleGC for testing
// by embedding the handler with a custom sm/hub.
type testableHandler struct {
	log *slog.Logger
	cfg *config.Config
	sm  interface {
		ValidateOwnership(ctx context.Context, sessionID, userID, adminUserID string) error
		ClearContext(ctx context.Context, sessionID string) error
		TransitionWithReason(ctx context.Context, id string, to events.SessionState, termReason string) error
		GetWorker(id string) worker.Worker
		DetachWorker(id string)
		Get(id string) (*session.SessionInfo, error)
	}
	hub interface {
		NextSeq(sessionID string) int64
		SendToSession(ctx context.Context, env *events.Envelope, afterDrain ...func()) error
	}
}

func (h *testableHandler) sendState(ctx context.Context, sessionID string, state events.SessionState, message string) error {
	env := events.NewEnvelope(aep.NewID(), sessionID, h.hub.NextSeq(sessionID), events.State, events.StateData{
		State:   state,
		Message: message,
	})
	return h.hub.SendToSession(ctx, env)
}

func (h *testableHandler) handleReset(ctx context.Context, sessionID, ownerID string) error {
	// 1. Ownership check
	if err := h.sm.ValidateOwnership(ctx, sessionID, ownerID, ""); err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			return errors.New("SESSION_NOT_FOUND")
		}
		return errors.New("UNAUTHORIZED")
	}
	// 1b. State precondition: reset only valid for active states.
	si, err := h.sm.Get(sessionID)
	if err != nil {
		return errors.New("SESSION_NOT_FOUND")
	}
	if si.State != events.StateCreated && si.State != events.StateRunning && si.State != events.StateIdle {
		return errors.New("PROTOCOL_VIOLATION")
	}
	// 2. Clear Context
	if err := h.sm.ClearContext(ctx, sessionID); err != nil {
		return err
	}
	// 3. Worker reset
	w := h.sm.GetWorker(sessionID)
	if w != nil {
		if err := w.ResetContext(ctx); err != nil {
			return err
		}
	}
	// 4. Transition to RUNNING
	if err := h.sm.TransitionWithReason(ctx, sessionID, events.StateRunning, "reset"); err != nil {
		return err
	}
	// 5. Send state notification
	return h.sendState(ctx, sessionID, events.StateRunning, "context_reset")
}

func (h *testableHandler) handleGC(ctx context.Context, sessionID, ownerID string) error {
	// 1. Ownership check
	if err := h.sm.ValidateOwnership(ctx, sessionID, ownerID, ""); err != nil {
		if errors.Is(err, session.ErrSessionNotFound) {
			return errors.New("SESSION_NOT_FOUND")
		}
		return errors.New("UNAUTHORIZED")
	}
	// 2. Get current state for idempotency check.
	si, err := h.sm.Get(sessionID)
	if err != nil {
		return errors.New("SESSION_NOT_FOUND")
	}
	// Idempotent: already terminated — return success without transitioning.
	if si.State == events.StateTerminated {
		return nil
	}
	// 3. Terminate + detach worker
	if w := h.sm.GetWorker(sessionID); w != nil {
		_ = w.Terminate(ctx)
		h.sm.DetachWorker(sessionID)
	}
	// 4. Transition to TERMINATED
	if err := h.sm.TransitionWithReason(ctx, sessionID, events.StateTerminated, "gc"); err != nil {
		return err
	}
	// 5. Send state notification
	return h.sendState(ctx, sessionID, events.StateTerminated, "session_archived")
}

// mockWorkerForHandler implements worker.Worker for handler tests.
type mockWorkerForHandler struct {
	mock.Mock
}

func (m *mockWorkerForHandler) Type() worker.WorkerType { return worker.TypeClaudeCode }
func (m *mockWorkerForHandler) SupportsResume() bool    { return true }
func (m *mockWorkerForHandler) SupportsStreaming() bool { return true }
func (m *mockWorkerForHandler) SupportsTools() bool     { return true }
func (m *mockWorkerForHandler) EnvWhitelist() []string  { return nil }
func (m *mockWorkerForHandler) SessionStoreDir() string { return "" }
func (m *mockWorkerForHandler) MaxTurns() int           { return 0 }
func (m *mockWorkerForHandler) Modalities() []string    { return []string{"text"} }

func (m *mockWorkerForHandler) Start(ctx context.Context, session worker.SessionInfo) error {
	args := m.Called(ctx, session)
	return args.Error(0)
}
func (m *mockWorkerForHandler) Input(ctx context.Context, content string, metadata map[string]any) error {
	args := m.Called(ctx, content, metadata)
	return args.Error(0)
}
func (m *mockWorkerForHandler) Resume(ctx context.Context, session worker.SessionInfo) error {
	args := m.Called(ctx, session)
	return args.Error(0)
}
func (m *mockWorkerForHandler) Terminate(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}
func (m *mockWorkerForHandler) Kill() error { return nil }
func (m *mockWorkerForHandler) Wait() (int, error) {
	args := m.Called()
	return args.Int(0), args.Error(1)
}
func (m *mockWorkerForHandler) Conn() worker.SessionConn { return nil }
func (m *mockWorkerForHandler) Health() worker.WorkerHealth {
	args := m.Called()
	return args.Get(0).(worker.WorkerHealth)
}
func (m *mockWorkerForHandler) LastIO() time.Time {
	args := m.Called()
	return args.Get(0).(time.Time)
}
func (m *mockWorkerForHandler) ResetContext(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

// ─── handleReset tests ──────────────────────────────────────────────────────

// mockControlWorker implements both worker.Worker and ControlRequester.
type mockControlWorker struct {
	mockWorkerForHandler
	controlResp    map[string]any
	controlErr     error
	controlCalled  bool
	controlSubtype string
}

func (m *mockControlWorker) SendControlRequest(_ context.Context, subtype string, _ map[string]any) (map[string]any, error) {
	m.controlCalled = true
	m.controlSubtype = subtype
	return m.controlResp, m.controlErr
}

func (m *mockControlWorker) Terminate(_ context.Context) error {
	return nil
}

// mockCommanderWorker implements worker.Worker and WorkerCommander.
type mockCommanderWorker struct {
	mockWorkerForHandler
	compactCalled bool
	clearCalled   bool
	rewindCalled  bool
}

func (m *mockCommanderWorker) Compact(_ context.Context, _ map[string]any) error {
	m.compactCalled = true
	return nil
}

func (m *mockCommanderWorker) Clear(_ context.Context) error {
	m.clearCalled = true
	return nil
}

func (m *mockCommanderWorker) Rewind(_ context.Context, _ string) error {
	m.rewindCalled = true
	return nil
}

func (m *mockCommanderWorker) Terminate(_ context.Context) error {
	return nil
}
func (m *mockCommanderWorker) SendControlRequest(_ context.Context, _ string, _ map[string]any) (map[string]any, error) {
	return nil, nil
}

// ─── handleWorkerCommand tests ──────────────────────────────────────────────

type workerCommandTestCtx struct {
	w            worker.Worker
	controlWkr   *mockControlWorker
	commanderWkr *mockCommanderWorker
	mockWkr      *mockWorkerForHandler
}

type workerCommandTestCase struct {
	name       string
	setup      func(tc *workerCommandTestCtx)
	command    events.WorkerStdioCommand
	rawData    map[string]any
	state      events.SessionState
	wantErr    bool
	errContain string
	verify     func(t *testing.T, tc *workerCommandTestCtx)
}

func (tc workerCommandTestCase) run(t *testing.T) {
	t.Helper()

	handler, mgr, _, _ := newHandlerWithRealStore(t)
	ctx := context.Background()
	const sid = "sess_wc"

	testCtx := &workerCommandTestCtx{}
	tc.setup(testCtx)

	_, err := mgr.Create(ctx, sid, "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)

	targetState := tc.state
	if targetState == "" {
		targetState = events.StateRunning
	}
	require.NoError(t, mgr.Transition(ctx, sid, targetState))

	if testCtx.w != nil && targetState == events.StateRunning {
		require.NoError(t, mgr.AttachWorker(sid, testCtx.w))
	}

	var data any = events.WorkerCommandData{Command: tc.command}
	if tc.rawData != nil {
		data = tc.rawData
	}

	env := &events.Envelope{
		SessionID: sid,
		OwnerID:   "user1",
		Event: events.Event{
			Type: events.WorkerCmd,
			Data: data,
		},
	}

	err = handler.handleWorkerCommand(ctx, env)
	if tc.wantErr {
		require.Error(t, err)
		require.Contains(t, err.Error(), tc.errContain)
	} else {
		require.NoError(t, err)
	}
	if tc.verify != nil {
		tc.verify(t, testCtx)
	}
}

func TestHandleWorkerCommand(t *testing.T) {
	t.Parallel()

	tests := []workerCommandTestCase{
		{
			name:    "passthrough_compact",
			command: events.StdioCompact,
			setup: func(tc *workerCommandTestCtx) {
				w := &mockCommanderWorker{}
				tc.w = w
				tc.commanderWkr = w
			},
			verify: func(t *testing.T, tc *workerCommandTestCtx) {
				t.Helper()
				require.True(t, tc.commanderWkr.compactCalled, "compact should be called")
			},
		},
		{
			name:    "passthrough_clear",
			command: events.StdioClear,
			setup: func(tc *workerCommandTestCtx) {
				w := &mockCommanderWorker{}
				tc.w = w
				tc.commanderWkr = w
			},
			verify: func(t *testing.T, tc *workerCommandTestCtx) {
				t.Helper()
				require.True(t, tc.commanderWkr.clearCalled, "clear should be called")
			},
		},
		{
			name:    "passthrough_rewind",
			command: events.StdioRewind,
			setup: func(tc *workerCommandTestCtx) {
				w := &mockCommanderWorker{}
				tc.w = w
				tc.commanderWkr = w
			},
			verify: func(t *testing.T, tc *workerCommandTestCtx) {
				t.Helper()
				require.True(t, tc.commanderWkr.rewindCalled, "rewind should be called")
			},
		},
		{
			name:    "control_context_usage",
			command: events.StdioContextUsage,
			setup: func(tc *workerCommandTestCtx) {
				w := &mockControlWorker{
					controlResp: map[string]any{
						"totalTokens": float64(50000),
						"maxTokens":   float64(200000),
						"percentage":  float64(25),
						"model":       "claude-sonnet-4",
						"categories":  []any{},
					},
				}
				tc.w = w
				tc.controlWkr = w
			},
			verify: func(t *testing.T, tc *workerCommandTestCtx) {
				t.Helper()
				require.True(t, tc.controlWkr.controlCalled, "SendControlRequest should be called")
				require.Equal(t, "get_context_usage", tc.controlWkr.controlSubtype)
			},
		},
		{
			name:    "control_mcp_status",
			command: events.StdioMCPStatus,
			setup: func(tc *workerCommandTestCtx) {
				w := &mockControlWorker{
					controlResp: map[string]any{
						"servers": []any{
							map[string]any{"name": "context7", "status": "connected"},
						},
					},
				}
				tc.w = w
				tc.controlWkr = w
			},
			verify: func(t *testing.T, tc *workerCommandTestCtx) {
				t.Helper()
				require.Equal(t, "mcp_status", tc.controlWkr.controlSubtype)
			},
		},
		{
			name:    "fallback_to_input",
			command: events.StdioCommit,
			setup: func(tc *workerCommandTestCtx) {
				w := new(mockWorkerForHandler)
				w.On("Input", mock.Anything, "/commit", mock.Anything).Return(nil)
				w.On("Terminate", mock.Anything).Return(nil)
				tc.w = w
				tc.mockWkr = w
			},
			verify: func(t *testing.T, tc *workerCommandTestCtx) {
				t.Helper()
				tc.mockWkr.AssertCalled(t, "Input", mock.Anything, "/commit", mock.Anything)
			},
		},
		{
			name:    "map_data_context_usage",
			command: "",
			rawData: map[string]any{"command": "context_usage"},
			setup: func(tc *workerCommandTestCtx) {
				w := &mockControlWorker{
					controlResp: map[string]any{
						"totalTokens": float64(50000),
						"maxTokens":   float64(200000),
						"categories":  []any{},
					},
				}
				tc.w = w
				tc.controlWkr = w
			},
			verify: func(t *testing.T, tc *workerCommandTestCtx) {
				t.Helper()
				require.True(t, tc.controlWkr.controlCalled)
			},
		},
		{
			name:       "terminated_session",
			command:    events.StdioCompact,
			state:      events.StateTerminated,
			wantErr:    true,
			errContain: "SESSION_BUSY",
			setup: func(tc *workerCommandTestCtx) {
				w := new(mockWorkerForHandler)
				tc.w = w
			},
		},
		{
			name:       "no_worker_attached",
			command:    events.StdioCompact,
			wantErr:    true,
			errContain: "no worker attached",
			setup: func(tc *workerCommandTestCtx) {
				tc.w = nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.run(t)
		})
	}
}

func TestHandleWorkerCommandSessionNotFound(t *testing.T) {
	t.Parallel()

	handler, _, _, _ := newHandlerWithRealStore(t)

	env := &events.Envelope{
		SessionID: "sess-missing",
		OwnerID:   "user1",
		Event: events.Event{
			Type: events.WorkerCmd,
			Data: events.WorkerCommandData{
				Command: events.StdioCompact,
			},
		},
	}

	err := handler.handleWorkerCommand(context.Background(), env)
	require.Error(t, err)
	require.Contains(t, err.Error(), "SESSION_NOT_FOUND")
}

// ─── handleReset tests ──────────────────────────────────────────────────────

func TestHandler_HandleReset_Unauthorized(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sm := new(mockHandlerSM)
	hub := newMockHub()

	h := &testableHandler{
		log: slog.Default(),
		cfg: config.Default(),
		sm:  sm,
		hub: hub,
	}

	sm.On("ValidateOwnership", ctx, "sess1", "wrong_user", "").Return(session.ErrOwnershipMismatch)

	err := h.handleReset(ctx, "sess1", "wrong_user")
	require.Error(t, err)
	require.Contains(t, err.Error(), "UNAUTHORIZED")
	sm.AssertExpectations(t)
}

func TestHandler_HandleReset_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sm := new(mockHandlerSM)
	hub := newMockHub()

	h := &testableHandler{
		log: slog.Default(),
		cfg: config.Default(),
		sm:  sm,
		hub: hub,
	}

	sm.On("ValidateOwnership", ctx, "sess1", "user1", "").Return(session.ErrSessionNotFound)

	err := h.handleReset(ctx, "sess1", "user1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "SESSION_NOT_FOUND")
	sm.AssertExpectations(t)
}

func TestHandler_HandleReset_Success(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sm := new(mockHandlerSM)
	hub := newMockHub()

	h := &testableHandler{
		log: slog.Default(),
		cfg: config.Default(),
		sm:  sm,
		hub: hub,
	}

	w := new(mockWorkerForHandler)

	sm.On("ValidateOwnership", ctx, "sess1", "user1", "").Return(nil)
	sm.On("Get", "sess1").Return(&session.SessionInfo{ID: "sess1", State: events.StateRunning}, nil)
	sm.On("ClearContext", ctx, "sess1").Return(nil)
	sm.On("GetWorker", "sess1").Return(w)
	w.On("ResetContext", ctx).Return(nil)
	sm.On("TransitionWithReason", ctx, "sess1", events.StateRunning, "reset").Return(nil)

	err := h.handleReset(ctx, "sess1", "user1")
	require.NoError(t, err)

	// Verify state notification was sent
	sent := hub.Sent()
	require.Len(t, sent, 1)
	require.Equal(t, events.State, sent[0].Event.Type)
	stateData, ok := sent[0].Event.Data.(events.StateData)
	require.True(t, ok)
	require.Equal(t, events.StateRunning, stateData.State)
	require.Equal(t, "context_reset", stateData.Message)

	sm.AssertExpectations(t)
	w.AssertExpectations(t)
}

func TestHandler_HandleReset_NoWorker(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sm := new(mockHandlerSM)
	hub := newMockHub()

	h := &testableHandler{
		log: slog.Default(),
		cfg: config.Default(),
		sm:  sm,
		hub: hub,
	}

	sm.On("ValidateOwnership", ctx, "sess1", "user1", "").Return(nil)
	sm.On("Get", "sess1").Return(&session.SessionInfo{ID: "sess1", State: events.StateRunning}, nil)
	sm.On("ClearContext", ctx, "sess1").Return(nil)
	sm.On("GetWorker", "sess1").Return(nil) // no worker attached
	sm.On("TransitionWithReason", ctx, "sess1", events.StateRunning, "reset").Return(nil)

	err := h.handleReset(ctx, "sess1", "user1")
	require.NoError(t, err)

	sm.AssertExpectations(t)
}

func TestHandler_HandleReset_WorkerResetFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sm := new(mockHandlerSM)
	hub := newMockHub()

	h := &testableHandler{
		log: slog.Default(),
		cfg: config.Default(),
		sm:  sm,
		hub: hub,
	}

	w := new(mockWorkerForHandler)

	sm.On("ValidateOwnership", ctx, "sess1", "user1", "").Return(nil)
	sm.On("Get", "sess1").Return(&session.SessionInfo{ID: "sess1", State: events.StateRunning}, nil)
	sm.On("ClearContext", ctx, "sess1").Return(nil)
	sm.On("GetWorker", "sess1").Return(w)
	w.On("ResetContext", ctx).Return(errors.New("worker reset failed"))

	err := h.handleReset(ctx, "sess1", "user1")
	require.Error(t, err)

	sm.AssertExpectations(t)
	w.AssertExpectations(t)
}

func TestHandler_HandleReset_TerminatedState(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sm := new(mockHandlerSM)
	hub := newMockHub()

	h := &testableHandler{
		log: slog.Default(),
		cfg: config.Default(),
		sm:  sm,
		hub: hub,
	}

	sm.On("ValidateOwnership", ctx, "sess1", "user1", "").Return(nil)
	sm.On("Get", "sess1").Return(&session.SessionInfo{ID: "sess1", State: events.StateTerminated}, nil)

	err := h.handleReset(ctx, "sess1", "user1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "PROTOCOL_VIOLATION")

	sm.AssertExpectations(t)
}

// ─── handleGC tests ────────────────────────────────────────────────────────

func TestHandler_HandleGC_Unauthorized(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sm := new(mockHandlerSM)
	hub := newMockHub()

	h := &testableHandler{
		log: slog.Default(),
		cfg: config.Default(),
		sm:  sm,
		hub: hub,
	}

	sm.On("ValidateOwnership", ctx, "sess1", "wrong_user", "").Return(session.ErrOwnershipMismatch)

	err := h.handleGC(ctx, "sess1", "wrong_user")
	require.Error(t, err)
	require.Contains(t, err.Error(), "UNAUTHORIZED")
	sm.AssertExpectations(t)
}

func TestHandler_HandleGC_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sm := new(mockHandlerSM)
	hub := newMockHub()

	h := &testableHandler{
		log: slog.Default(),
		cfg: config.Default(),
		sm:  sm,
		hub: hub,
	}

	sm.On("ValidateOwnership", ctx, "sess1", "user1", "").Return(session.ErrSessionNotFound)

	err := h.handleGC(ctx, "sess1", "user1")
	require.Error(t, err)
	require.Contains(t, err.Error(), "SESSION_NOT_FOUND")
	sm.AssertExpectations(t)
}

func TestHandler_HandleGC_Success(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sm := new(mockHandlerSM)
	hub := newMockHub()

	h := &testableHandler{
		log: slog.Default(),
		cfg: config.Default(),
		sm:  sm,
		hub: hub,
	}

	w := new(mockWorkerForHandler)

	sm.On("ValidateOwnership", ctx, "sess1", "user1", "").Return(nil)
	sm.On("Get", "sess1").Return(&session.SessionInfo{ID: "sess1", State: events.StateRunning}, nil)
	sm.On("GetWorker", "sess1").Return(w)
	w.On("Terminate", ctx).Return(nil)
	sm.On("DetachWorker", "sess1")
	sm.On("TransitionWithReason", ctx, "sess1", events.StateTerminated, "gc").Return(nil)

	err := h.handleGC(ctx, "sess1", "user1")
	require.NoError(t, err)

	// Verify state notification
	sent := hub.Sent()
	require.Len(t, sent, 1)
	require.Equal(t, events.State, sent[0].Event.Type)
	stateData, ok := sent[0].Event.Data.(events.StateData)
	require.True(t, ok)
	require.Equal(t, events.StateTerminated, stateData.State)
	require.Equal(t, "session_archived", stateData.Message)

	sm.AssertExpectations(t)
	w.AssertExpectations(t)
}

func TestHandler_HandleGC_NoWorker(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sm := new(mockHandlerSM)
	hub := newMockHub()

	h := &testableHandler{
		log: slog.Default(),
		cfg: config.Default(),
		sm:  sm,
		hub: hub,
	}

	sm.On("ValidateOwnership", ctx, "sess1", "user1", "").Return(nil)
	sm.On("Get", "sess1").Return(&session.SessionInfo{ID: "sess1", State: events.StateIdle}, nil)
	sm.On("GetWorker", "sess1").Return(nil) // no worker attached
	sm.On("TransitionWithReason", ctx, "sess1", events.StateTerminated, "gc").Return(nil)

	err := h.handleGC(ctx, "sess1", "user1")
	require.NoError(t, err)

	sm.AssertExpectations(t)
}

func TestHandler_HandleGC_Idempotent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sm := new(mockHandlerSM)
	hub := newMockHub()

	h := &testableHandler{
		log: slog.Default(),
		cfg: config.Default(),
		sm:  sm,
		hub: hub,
	}

	// Session is already TERMINATED — gc should succeed without transitioning.
	sm.On("ValidateOwnership", ctx, "sess1", "user1", "").Return(nil)
	sm.On("Get", "sess1").Return(&session.SessionInfo{ID: "sess1", State: events.StateTerminated}, nil)
	// No GetWorker, no Terminate, no DetachWorker, no Transition — idempotent.

	err := h.handleGC(ctx, "sess1", "user1")
	require.NoError(t, err)

	// No state notifications sent (idempotent — no changes).
	require.Empty(t, hub.Sent())
	sm.AssertExpectations(t)
}

// ─── Worker.ResetContext implementation tests ─────────────────────────────────

func TestWorker_ResetContext_Noop(t *testing.T) {
	t.Parallel()
	w := noopworker.NewWorker()
	err := w.ResetContext(context.Background())
	require.NoError(t, err)
}
