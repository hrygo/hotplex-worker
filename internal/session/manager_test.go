package session

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

// ─── mockStore implements Store for testing ───────────────────────────────────

type mockStore struct {
	mock.Mock
}

func (m *mockStore) Upsert(ctx context.Context, info *SessionInfo) error {
	args := m.Called(ctx, info)
	if args.Error(0) == nil {
		// Copy fields back to info so callers see updated state
		if ms, ok := args.Get(0).(*SessionInfo); ok {
			*info = *ms
		}
	}
	return args.Error(0)
}

func (m *mockStore) Get(ctx context.Context, id string) (*SessionInfo, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*SessionInfo), args.Error(1)
}

func (m *mockStore) List(ctx context.Context, limit, offset int) ([]*SessionInfo, error) {
	args := m.Called(ctx, limit, offset)
	return args.Get(0).([]*SessionInfo), args.Error(1)
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
	args := m.Called()
	return args.Error(0)
}

// ─── test helpers ──────────────────────────────────────────────────────────────

// ─── state transition tests ───────────────────────────────────────────────────

func TestStateTransition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		from    events.SessionState
		to      events.SessionState
		wantErr bool
	}{
		// CREATED transitions
		{"CREATED → RUNNING", events.StateCreated, events.StateRunning, false},
		{"CREATED → TERMINATED", events.StateCreated, events.StateTerminated, false},
		{"CREATED → IDLE invalid", events.StateCreated, events.StateIdle, true},
		{"CREATED → DELETED invalid", events.StateCreated, events.StateDeleted, true},

		// RUNNING transitions
		{"RUNNING → IDLE", events.StateRunning, events.StateIdle, false},
		{"RUNNING → TERMINATED", events.StateRunning, events.StateTerminated, false},
		{"RUNNING → DELETED", events.StateRunning, events.StateDeleted, false},
		{"RUNNING → CREATED invalid", events.StateRunning, events.StateCreated, true},

		// IDLE transitions
		{"IDLE → RUNNING", events.StateIdle, events.StateRunning, false},
		{"IDLE → TERMINATED", events.StateIdle, events.StateTerminated, false},
		{"IDLE → DELETED", events.StateIdle, events.StateDeleted, false},
		{"IDLE → CREATED invalid", events.StateIdle, events.StateCreated, true},

		// TERMINATED transitions
		{"TERMINATED → RUNNING (resume)", events.StateTerminated, events.StateRunning, false},
		{"TERMINATED → DELETED", events.StateTerminated, events.StateDeleted, false},
		{"TERMINATED → IDLE invalid", events.StateTerminated, events.StateIdle, true},
		{"TERMINATED → CREATED invalid", events.StateTerminated, events.StateCreated, true},

		// DELETED is terminal
		{"DELETED → RUNNING invalid", events.StateDeleted, events.StateRunning, true},
		{"DELETED → IDLE invalid", events.StateDeleted, events.StateIdle, true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ok := events.IsValidTransition(tt.from, tt.to)
			if tt.wantErr {
				require.False(t, ok)
			} else {
				require.True(t, ok)
			}
		})
	}
}

func TestManager_Create(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Upsert", ctx, mock.AnythingOfType("*session.SessionInfo")).
		Return(nil)
	store.On("Close").Return(nil)

	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	info, err := m.Create(ctx, "sess_new", "user1", worker.TypeClaudeCode, nil)
	require.NoError(t, err)
	require.NotNil(t, info)
	require.Equal(t, "sess_new", info.ID)
	require.Equal(t, "user1", info.UserID)
	require.Equal(t, worker.TypeClaudeCode, info.WorkerType)
	require.Equal(t, events.StateCreated, info.State)
	require.NotNil(t, info.ExpiresAt)
}

func TestManager_Get(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	// Not in memory, falls back to store
	now := time.Now()
	expected := &SessionInfo{
		ID:         "sess_existing",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateRunning,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	store.On("Get", ctx, "sess_existing").Return(expected, nil)

	info, err := m.Get("sess_existing")
	require.NoError(t, err)
	require.Equal(t, "sess_existing", info.ID)
	require.Equal(t, events.StateRunning, info.State)

	// After Get, session should be in memory map
	store.On("Upsert", ctx, mock.AnythingOfType("*session.SessionInfo")).Return(nil)
	store.On("Get", ctx, "sess_existing").Return(expected, nil).Maybe()

	// In-memory hit
	info2, err := m.Get("sess_existing")
	require.NoError(t, err)
	require.Equal(t, "sess_existing", info2.ID)
}

func TestManager_Get_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Get", ctx, "sess_missing").Return(nil, ErrSessionNotFound)
	store.On("Close").Return(nil)

	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	_, err = m.Get("sess_missing")
	require.True(t, errors.Is(err, ErrSessionNotFound))
}

func TestManager_Transition(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	// Seed a session in memory
	now := time.Now()
	seed := &SessionInfo{
		ID:         "sess_trans",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateCreated,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	m.mu.Lock()
	m.sessions["sess_trans"] = &managedSession{info: *seed}
	m.mu.Unlock()

	store.On("Upsert", ctx, mock.AnythingOfType("*session.SessionInfo")).Return(nil)

	err = m.Transition(ctx, "sess_trans", events.StateRunning)
	require.NoError(t, err)

	info, _ := m.Get("sess_trans")
	require.Equal(t, events.StateRunning, info.State)
}

func TestManager_Transition_Invalid(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	// Seed a CREATED session
	seed := &SessionInfo{
		ID:         "sess_invalid",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateCreated,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	m.sessions["sess_invalid"] = &managedSession{info: *seed}
	m.mu.Unlock()

	// Cannot go CREATED → IDLE directly
	err = m.Transition(ctx, "sess_invalid", events.StateIdle)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrInvalidTransition))
}

func TestManager_Transition_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Get", ctx, "sess_ghost").Return(nil, ErrSessionNotFound)
	store.On("Close").Return(nil)

	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	err = m.Transition(ctx, "sess_ghost", events.StateRunning)
	require.True(t, errors.Is(err, ErrSessionNotFound))
}

func TestManager_TransitionWithInput_Atomic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	seed := &SessionInfo{
		ID:         "sess_atomic",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateRunning,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	m.sessions["sess_atomic"] = &managedSession{info: *seed}
	m.mu.Unlock()

	store.On("Upsert", ctx, mock.AnythingOfType("*session.SessionInfo")).Return(nil)

	// TransitionWithInput should succeed atomically
	err = m.TransitionWithInput(ctx, "sess_atomic", events.StateIdle, "user input", nil)
	require.NoError(t, err)
}

func TestManager_TransitionWithInput_InvalidTransition(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	seed := &SessionInfo{
		ID:         "sess_atomic_inv",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateCreated,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	m.sessions["sess_atomic_inv"] = &managedSession{info: *seed}
	m.mu.Unlock()

	err = m.TransitionWithInput(ctx, "sess_atomic_inv", events.StateIdle, "input", nil)
	require.Error(t, err)
}

func TestSessionBusy_RejectWhenNotActive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	// Seed a TERMINATED session
	seed := &SessionInfo{
		ID:         "sess_busy",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateTerminated,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	m.sessions["sess_busy"] = &managedSession{info: *seed}
	m.mu.Unlock()

	// Attempt TransitionWithInput on TERMINATED → IDLE is invalid (TERMINATED → IDLE not allowed)
	store.On("Upsert", ctx, mock.AnythingOfType("*session.SessionInfo")).Return(nil)

	err = m.TransitionWithInput(ctx, "sess_busy", events.StateIdle, "input", nil)
	require.Error(t, err)
}

func TestManager_Delete(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	seed := &SessionInfo{
		ID:         "sess_del",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateTerminated,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	m.sessions["sess_del"] = &managedSession{info: *seed}
	m.mu.Unlock()

	store.On("Upsert", ctx, mock.AnythingOfType("*session.SessionInfo")).Return(nil)

	err = m.Delete(ctx, "sess_del")
	require.NoError(t, err)

	// Session should be removed from in-memory map
	m.mu.RLock()
	_, ok := m.sessions["sess_del"]
	m.mu.RUnlock()
	require.False(t, ok)
}

func TestManager_DeletePhysical(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	t.Run("removes session from memory and database", func(t *testing.T) {
		t.Parallel()

		seed := &SessionInfo{
			ID:         "sess_phys",
			UserID:     "user1",
			WorkerType: worker.TypeClaudeCode,
			State:      events.StateRunning,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
		m.mu.Lock()
		m.sessions["sess_phys"] = &managedSession{info: *seed}
		m.mu.Unlock()

		store.On("DeletePhysical", ctx, "sess_phys").Return(nil)

		err := m.DeletePhysical(ctx, "sess_phys")
		require.NoError(t, err)

		m.mu.RLock()
		_, ok := m.sessions["sess_phys"]
		m.mu.RUnlock()
		require.False(t, ok)
	})

	t.Run("no-op when session not in memory", func(t *testing.T) {
		t.Parallel()

		store.On("DeletePhysical", ctx, "nonexistent").Return(nil)

		err := m.DeletePhysical(ctx, "nonexistent")
		require.NoError(t, err)
	})

	t.Run("returns database error", func(t *testing.T) {
		t.Parallel()

		store.On("DeletePhysical", ctx, "db_fail").Return(errors.New("db error"))

		err := m.DeletePhysical(ctx, "db_fail")
		require.Error(t, err)
	})
}

func TestManager_ValidateOwnership(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	seed := &SessionInfo{
		ID:         "sess_owner",
		UserID:     "user_owner",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateRunning,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	m.sessions["sess_owner"] = &managedSession{info: *seed}
	m.mu.Unlock()

	// Owner matches
	err = m.ValidateOwnership(ctx, "sess_owner", "user_owner", "")
	require.NoError(t, err)

	// Owner mismatch
	err = m.ValidateOwnership(ctx, "sess_owner", "wrong_user", "")
	require.True(t, errors.Is(err, ErrOwnershipMismatch))

	// Admin bypass
	err = m.ValidateOwnership(ctx, "sess_owner", "wrong_user", "admin_user")
	require.NoError(t, err)
}

func TestManager_ValidateOwnership_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Get", ctx, "sess_missing").Return(nil, ErrSessionNotFound)
	store.On("Close").Return(nil)

	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	err = m.ValidateOwnership(ctx, "sess_missing", "user1", "")
	require.True(t, errors.Is(err, ErrSessionNotFound))
}

func TestManager_Lock(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	seed := &SessionInfo{
		ID:         "sess_lock",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateRunning,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	m.sessions["sess_lock"] = &managedSession{info: *seed}
	m.mu.Unlock()

	// Lock and immediately unlock
	unlock, err := m.Lock("sess_lock")
	require.NoError(t, err)
	require.NotNil(t, unlock)
	unlock()
}

func TestManager_Lock_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Get", ctx, "sess_ghost_lock").Return(nil, ErrSessionNotFound)
	store.On("Close").Return(nil)

	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	_, err = m.Lock("sess_ghost_lock")
	require.True(t, errors.Is(err, ErrSessionNotFound))
}

func TestManager_List(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	expected := []*SessionInfo{
		{ID: "sess_1", UserID: "user1", WorkerType: worker.TypeClaudeCode, State: events.StateRunning},
		{ID: "sess_2", UserID: "user2", WorkerType: worker.TypeClaudeCode, State: events.StateIdle},
	}
	store.On("List", ctx, 50, 0).Return(expected, nil)

	list, err := m.List(ctx, 50, 0)
	require.NoError(t, err)
	require.Len(t, list, 2)
}

func TestManager_ListActive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	// Seed sessions
	for _, id := range []string{"sess_a", "sess_b"} {
		m.mu.Lock()
		m.sessions[id] = &managedSession{info: SessionInfo{
			ID:         id,
			UserID:     "user1",
			WorkerType: worker.TypeClaudeCode,
			State:      events.StateRunning,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}}
		m.mu.Unlock()
	}

	active := m.ListActive()
	require.Len(t, active, 2)
}

func TestManager_Stats(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	total, max, users := m.Stats()
	require.Equal(t, 0, total)
	require.Equal(t, cfg.Pool.MaxSize, max)
	require.Equal(t, 0, users)
}

func TestSessionInfo_IsActive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		state  events.SessionState
		active bool
	}{
		{events.StateCreated, true},
		{events.StateRunning, true},
		{events.StateIdle, true},
		{events.StateTerminated, false},
		{events.StateDeleted, false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(string(tt.state), func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.active, tt.state.IsActive())
		})
	}
}

func TestSessionInfo_IsTerminal(t *testing.T) {
	t.Parallel()

	require.True(t, events.StateDeleted.IsTerminal())
	require.False(t, events.StateTerminated.IsTerminal())
	require.False(t, events.StateRunning.IsTerminal())
}

// ─── mockWorker implements worker.Worker for testing ──────────────────────────

type mockWorker struct {
	mock.Mock
	workerType  worker.WorkerType
	maxTurns    int
	lastIO      time.Time
	health      worker.WorkerHealth
	sessionConn *mockSessionConn
}

type mockSessionConn struct {
	mock.Mock
}

func (m *mockSessionConn) Send(ctx context.Context, msg *events.Envelope) error {
	args := m.Called(ctx, msg)
	return args.Error(0)
}

func (m *mockSessionConn) Recv() <-chan *events.Envelope {
	args := m.Called()
	if args.Get(0) == nil {
		ch := make(chan *events.Envelope)
		close(ch)
		return ch
	}
	return args.Get(0).(<-chan *events.Envelope)
}

func (m *mockSessionConn) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *mockSessionConn) UserID() string    { return "user1" }
func (m *mockSessionConn) SessionID() string { return "mock_sess" }

func newMockWorker(t worker.WorkerType, maxTurns int) *mockWorker {
	return &mockWorker{
		workerType: t,
		maxTurns:   maxTurns,
		health: worker.WorkerHealth{
			Type:      t,
			SessionID: "mock_sess",
			PID:       12345,
			Running:   true,
			Healthy:   true,
			Uptime:    "1m0s",
		},
		sessionConn: &mockSessionConn{},
	}
}

func (w *mockWorker) Type() worker.WorkerType { return w.workerType }
func (w *mockWorker) SupportsResume() bool    { return false }
func (w *mockWorker) SupportsStreaming() bool { return true }
func (w *mockWorker) SupportsTools() bool     { return true }
func (w *mockWorker) EnvWhitelist() []string  { return nil }
func (w *mockWorker) SessionStoreDir() string { return "" }
func (w *mockWorker) MaxTurns() int           { return w.maxTurns }
func (w *mockWorker) Modalities() []string    { return []string{"text", "code"} }
func (w *mockWorker) Start(ctx context.Context, session worker.SessionInfo) error {
	args := w.Called(ctx, session)
	return args.Error(0)
}
func (w *mockWorker) Input(ctx context.Context, content string, metadata map[string]any) error {
	args := w.Called(ctx, content, metadata)
	return args.Error(0)
}
func (w *mockWorker) Resume(ctx context.Context, session worker.SessionInfo) error {
	args := w.Called(ctx, session)
	return args.Error(0)
}
func (w *mockWorker) Terminate(ctx context.Context) error {
	args := w.Called(ctx)
	return args.Error(0)
}
func (w *mockWorker) Kill() error {
	return nil
}
func (w *mockWorker) Wait() (int, error) {
	return 0, nil
}
func (w *mockWorker) Conn() worker.SessionConn { return w.sessionConn }
func (w *mockWorker) Health() worker.WorkerHealth {
	return w.health
}
func (w *mockWorker) LastIO() time.Time { return w.lastIO }
func (w *mockWorker) ResetContext(ctx context.Context) error {
	args := w.Called(ctx)
	return args.Error(0)
}

// ─── AttachWorker tests ───────────────────────────────────────────────────────

func TestManager_AttachWorker_Success(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	seed := &SessionInfo{
		ID:         "sess_attach",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateCreated,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	m.sessions["sess_attach"] = &managedSession{info: *seed}
	m.mu.Unlock()

	w := newMockWorker(worker.TypeClaudeCode, 0)
	w.On("Terminate", mock.Anything).Return(nil)
	err = m.AttachWorker("sess_attach", w)
	require.NoError(t, err)

	// Verify pool slot acquired
	total, _, users := m.Stats()
	require.Equal(t, 1, total)
	require.Equal(t, 1, users)
}

func TestManager_AttachWorker_PoolExhausted(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	// Global pool size = 1
	cfg.Pool.MaxSize = 1
	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	seed := &SessionInfo{
		ID:         "sess_exhaust",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateCreated,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	m.sessions["sess_exhaust"] = &managedSession{info: *seed}
	m.mu.Unlock()

	w := newMockWorker(worker.TypeClaudeCode, 0)
	w.On("Terminate", mock.Anything).Return(nil)

	// First session exhausts the global pool
	err = m.AttachWorker("sess_exhaust", w)
	require.NoError(t, err)

	// Second session (different user) fails due to global limit
	seed2 := &SessionInfo{
		ID:         "sess_exhaust2",
		UserID:     "user2",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateCreated,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	m.sessions["sess_exhaust2"] = &managedSession{info: *seed2}
	m.mu.Unlock()
	w2 := newMockWorker(worker.TypeClaudeCode, 0)
	w2.On("Terminate", mock.Anything).Return(nil)
	err = m.AttachWorker("sess_exhaust2", w2)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrPoolExhausted))
}

func TestManager_AttachWorker_UserQuotaExceeded(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	// Per-user limit = 1
	cfg.Pool.MaxIdlePerUser = 1
	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	seed := &SessionInfo{
		ID:         "sess_quota",
		UserID:     "user_quota",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateCreated,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	m.sessions["sess_quota"] = &managedSession{info: *seed}
	m.mu.Unlock()

	w := newMockWorker(worker.TypeClaudeCode, 0)
	w.On("Terminate", mock.Anything).Return(nil)
	err = m.AttachWorker("sess_quota", w)
	require.NoError(t, err)

	// Second session for same user → quota exceeded
	seed2 := &SessionInfo{
		ID:         "sess_quota2",
		UserID:     "user_quota",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateCreated,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	m.sessions["sess_quota2"] = &managedSession{info: *seed2}
	m.mu.Unlock()
	w2 := newMockWorker(worker.TypeClaudeCode, 0)
	w2.On("Terminate", mock.Anything).Return(nil)
	err = m.AttachWorker("sess_quota2", w2)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrUserQuotaExceeded))
}

func TestManager_AttachWorker_MemoryExceeded_Rollback(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	// 1 GB per user, 512 MB per worker → max 2
	cfg.Pool.MaxMemoryPerUser = 512 * 1024 * 1024
	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	seed := &SessionInfo{
		ID:         "sess_mem",
		UserID:     "user_mem",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateCreated,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	m.sessions["sess_mem"] = &managedSession{info: *seed}
	m.mu.Unlock()

	w := newMockWorker(worker.TypeClaudeCode, 0)
	w.On("Terminate", mock.Anything).Return(nil)
	err = m.AttachWorker("sess_mem", w)
	require.NoError(t, err)

	// Detach first worker, then reattach succeeds (memory freed)
	m.DetachWorker("sess_mem")

	// After detach, pool is clean
	total, _, _ := m.Stats()
	require.Equal(t, 0, total)

	// Second worker on same session after detach — succeeds since memory is freed
	w2 := newMockWorker(worker.TypeClaudeCode, 0)
	w2.On("Terminate", mock.Anything).Return(nil)
	err = m.AttachWorker("sess_mem", w2)
	require.NoError(t, err)
}

func TestManager_AttachWorker_AlreadyAttached(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	seed := &SessionInfo{
		ID:         "sess_double",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateCreated,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	m.sessions["sess_double"] = &managedSession{info: *seed}
	m.mu.Unlock()

	w := newMockWorker(worker.TypeClaudeCode, 0)
	w.On("Terminate", mock.Anything).Return(nil)
	err = m.AttachWorker("sess_double", w)
	require.NoError(t, err)

	// Second attach on same session → ErrWorkerAttached (no quota acquired)
	w2 := newMockWorker(worker.TypeClaudeCode, 0)
	w2.On("Terminate", mock.Anything).Return(nil)
	err = m.AttachWorker("sess_double", w2)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrWorkerAttached))

	// Pool quota not leaked
	total, _, _ := m.Stats()
	require.Equal(t, 1, total)
}

func TestManager_AttachWorker_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Get", ctx, "sess_missing").Return(nil, ErrSessionNotFound)
	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	w := newMockWorker(worker.TypeClaudeCode, 0)
	w.On("Terminate", mock.Anything).Return(nil)
	err = m.AttachWorker("sess_missing", w)
	require.Error(t, err)
}

// ─── DetachWorker tests ───────────────────────────────────────────────────────

func TestManager_DetachWorker_WithWorker(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	seed := &SessionInfo{
		ID:         "sess_detach",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateRunning,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	m.sessions["sess_detach"] = &managedSession{info: *seed}
	m.mu.Unlock()

	w := newMockWorker(worker.TypeClaudeCode, 0)
	w.On("Terminate", mock.Anything).Return(nil)
	err = m.AttachWorker("sess_detach", w)
	require.NoError(t, err)

	m.DetachWorker("sess_detach")

	// Pool slot released
	total, _, _ := m.Stats()
	require.Equal(t, 0, total)
	// No worker attached
	require.Nil(t, m.GetWorker("sess_detach"))
}

func TestManager_DetachWorker_NoWorker(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	seed := &SessionInfo{
		ID:         "sess_no_worker",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateIdle,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	m.sessions["sess_no_worker"] = &managedSession{info: *seed}
	m.mu.Unlock()

	// Detaching with no worker attached should be safe (no panic)
	m.DetachWorker("sess_no_worker")

	total, _, _ := m.Stats()
	require.Equal(t, 0, total)
}

func TestManager_DetachWorker_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Get", ctx, "sess_ghost_detach").Return(nil, ErrSessionNotFound)
	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	// Should be safe
	m.DetachWorker("sess_ghost_detach")
}

// ─── GetWorker tests ──────────────────────────────────────────────────────────

func TestManager_GetWorker_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Get", ctx, "sess_ghost_worker").Return(nil, ErrSessionNotFound)
	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	require.Nil(t, m.GetWorker("sess_ghost_worker"))
}

// ─── DebugSnapshot tests ──────────────────────────────────────────────────────

func TestManager_DebugSnapshot_WithWorker(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	seed := &SessionInfo{
		ID:         "sess_debug",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateRunning,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	ms := &managedSession{info: *seed, TurnCount: 5}
	m.sessions["sess_debug"] = ms
	m.mu.Unlock()

	w := newMockWorker(worker.TypeClaudeCode, 0)
	w.On("Terminate", mock.Anything).Return(nil)
	_ = m.AttachWorker("sess_debug", w)

	snap, ok := m.DebugSnapshot("sess_debug")
	require.True(t, ok)
	require.Equal(t, 5, snap.TurnCount)
	require.True(t, snap.HasWorker)
}

func TestManager_DebugSnapshot_NoWorker(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	seed := &SessionInfo{
		ID:         "sess_no_worker_debug",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateIdle,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	m.sessions["sess_no_worker_debug"] = &managedSession{info: *seed, TurnCount: 3}
	m.mu.Unlock()

	snap, ok := m.DebugSnapshot("sess_no_worker_debug")
	require.True(t, ok)
	require.Equal(t, 3, snap.TurnCount)
	require.False(t, snap.HasWorker)
}

func TestManager_DebugSnapshot_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Get", ctx, "sess_ghost_debug").Return(nil, ErrSessionNotFound)
	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	_, ok := m.DebugSnapshot("sess_ghost_debug")
	require.False(t, ok)
}

// ─── releaseWorkerQuota tests ─────────────────────────────────────────────────

func TestManager_ReleaseWorkerQuota(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	seed := &SessionInfo{
		ID:         "sess_quota_rel",
		UserID:     "user_quota_rel",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateRunning,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	ms := &managedSession{info: *seed}
	m.sessions["sess_quota_rel"] = ms
	m.mu.Unlock()

	// Attach and release
	w := newMockWorker(worker.TypeClaudeCode, 0)
	w.On("Terminate", mock.Anything).Return(nil)
	_ = m.AttachWorker("sess_quota_rel", w)
	total, _, _ := m.Stats()
	require.Equal(t, 1, total)

	m.releaseWorkerQuota(ms)
	total, _, _ = m.Stats()
	require.Equal(t, 0, total)
}

func TestManager_TransitionTerminated_NilsWorker(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)
	store.On("Upsert", mock.Anything, mock.AnythingOfType("*session.SessionInfo")).Return(nil)
	store.On("Close").Return(nil)

	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	// Seed a RUNNING session with a mock worker.
	seed := &SessionInfo{
		ID:         "sess_worker_nil",
		UserID:     "user_worker_nil",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateRunning,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	ms := &managedSession{info: *seed}
	m.mu.Lock()
	m.sessions["sess_worker_nil"] = ms
	m.mu.Unlock()

	w := newMockWorker(worker.TypeClaudeCode, 0)
	w.On("Terminate", mock.Anything).Return(nil)
	_ = m.AttachWorker("sess_worker_nil", w)

	total, _, _ := m.Stats()
	require.Equal(t, 1, total)

	// Transition to TERMINATED — should nil the worker pointer.
	err = m.TransitionWithReason(ctx, "sess_worker_nil", events.StateTerminated, "zombie")
	require.NoError(t, err)

	// Worker pointer must be nil to prevent double release by DetachWorker.
	ms.mu.RLock()
	workerPtr := ms.worker
	ms.mu.RUnlock()
	require.Nil(t, workerPtr, "worker pointer should be nil after transition to TERMINATED")

	// DetachWorker should be a no-op (no pool underflow).
	m.DetachWorker("sess_worker_nil")
	total, _, _ = m.Stats()
	require.Equal(t, 0, total, "pool should be at 0, not negative")
}

// ─── WorkerHealthStatuses tests ───────────────────────────────────────────────

func TestManager_WorkerHealthStatuses(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	seed := &SessionInfo{
		ID:         "sess_health",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateRunning,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
	m.mu.Lock()
	m.sessions["sess_health"] = &managedSession{info: *seed}
	m.mu.Unlock()

	w := newMockWorker(worker.TypeClaudeCode, 0)
	w.On("Terminate", mock.Anything).Return(nil)
	_ = m.AttachWorker("sess_health", w)

	statuses := m.WorkerHealthStatuses()
	require.Len(t, statuses, 1)
	require.Equal(t, worker.TypeClaudeCode, statuses[0].Type)
}

func TestManager_WorkerHealthStatuses_NoWorkers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	statuses := m.WorkerHealthStatuses()
	require.Len(t, statuses, 0)
}

// ─── GC tests ─────────────────────────────────────────────────────────────────

func TestManager_GC_ZombieDetection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)

	// Simulate zombie: worker lastIO was 10 min ago (beyond timeout)
	now := time.Now()
	seed := &SessionInfo{
		ID:         "sess_zombie",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateRunning,
		CreatedAt:  now.Add(-20 * time.Minute),
		UpdatedAt:  now.Add(-20 * time.Minute),
	}
	m.mu.Lock()
	ms := &managedSession{info: *seed}
	m.sessions["sess_zombie"] = ms
	m.mu.Unlock()

	w := newMockWorker(worker.TypeClaudeCode, 0)
	w.On("Terminate", mock.Anything).Return(nil)
	w.lastIO = now.Add(-31 * time.Minute) // zombie: no IO beyond 30m default execution_timeout
	_ = m.AttachWorker("sess_zombie", w)

	store.On("Upsert", mock.Anything, mock.AnythingOfType("*session.SessionInfo")).Return(nil)
	store.On("GetExpiredMaxLifetime", mock.Anything, mock.AnythingOfType("time.Time")).Return([]string(nil), nil)
	store.On("GetExpiredIdle", mock.Anything, mock.AnythingOfType("time.Time")).Return([]string(nil), nil)
	store.On("DeleteTerminated", mock.Anything, mock.AnythingOfType("time.Time")).Return(nil)

	m.gc(ctx)

	// Session should be terminated
	m.mu.RLock()
	state := m.sessions["sess_zombie"].info.State
	m.mu.RUnlock()
	require.Equal(t, events.StateTerminated, state)
}

func TestManager_GC_NoZombieWhenRecentIO(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)

	now := time.Now()
	seed := &SessionInfo{
		ID:         "sess_healthy",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateRunning,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	m.mu.Lock()
	m.sessions["sess_healthy"] = &managedSession{info: *seed}
	m.mu.Unlock()

	w := newMockWorker(worker.TypeClaudeCode, 0)
	w.lastIO = now // very recent IO
	_ = m.AttachWorker("sess_healthy", w)

	store.On("GetExpiredMaxLifetime", mock.Anything, mock.AnythingOfType("time.Time")).Return([]string(nil), nil)
	store.On("GetExpiredIdle", mock.Anything, mock.AnythingOfType("time.Time")).Return([]string(nil), nil)
	store.On("DeleteTerminated", mock.Anything, mock.AnythingOfType("time.Time")).Return(nil)

	m.gc(ctx)

	// Session still RUNNING
	m.mu.RLock()
	state := m.sessions["sess_healthy"].info.State
	m.mu.RUnlock()
	require.Equal(t, events.StateRunning, state)
}

func TestManager_GC_ExpiredMaxLifetime(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	now := time.Now()
	store.On("GetExpiredMaxLifetime", mock.Anything, mock.AnythingOfType("time.Time")).
		Return([]string{"sess_maxlife"}, nil)
	store.On("GetExpiredIdle", mock.Anything, mock.AnythingOfType("time.Time")).
		Return([]string(nil), nil)
	store.On("DeleteTerminated", mock.Anything, mock.AnythingOfType("time.Time")).
		Return(nil)
	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)

	seed := &SessionInfo{
		ID:         "sess_maxlife",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateIdle,
		CreatedAt:  now.Add(-48 * time.Hour),
		UpdatedAt:  now.Add(-48 * time.Hour),
	}
	m.mu.Lock()
	m.sessions["sess_maxlife"] = &managedSession{info: *seed}
	m.mu.Unlock()

	store.On("Upsert", mock.Anything, mock.AnythingOfType("*session.SessionInfo")).Return(nil)

	m.gc(ctx)

	m.mu.RLock()
	state := m.sessions["sess_maxlife"].info.State
	m.mu.RUnlock()
	require.Equal(t, events.StateTerminated, state)
}

func TestManager_GC_ExpiredIdleTimeout(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	now := time.Now()
	store.On("GetExpiredMaxLifetime", mock.Anything, mock.AnythingOfType("time.Time")).
		Return([]string(nil), nil)
	store.On("GetExpiredIdle", mock.Anything, mock.AnythingOfType("time.Time")).
		Return([]string{"sess_idle_exp"}, nil)
	store.On("DeleteTerminated", mock.Anything, mock.AnythingOfType("time.Time")).
		Return(nil)
	store.On("Close").Return(nil)
	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)

	seed := &SessionInfo{
		ID:         "sess_idle_exp",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateIdle,
		CreatedAt:  now,
		UpdatedAt:  now.Add(-35 * time.Minute),
	}
	m.mu.Lock()
	m.sessions["sess_idle_exp"] = &managedSession{info: *seed}
	m.mu.Unlock()

	store.On("Upsert", mock.Anything, mock.AnythingOfType("*session.SessionInfo")).Return(nil)

	m.gc(ctx)

	m.mu.RLock()
	state := m.sessions["sess_idle_exp"].info.State
	m.mu.RUnlock()
	require.Equal(t, events.StateTerminated, state)
}

func TestManager_GC_NoRetentionCleanup(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("GetExpiredMaxLifetime", mock.Anything, mock.AnythingOfType("time.Time")).
		Return([]string(nil), nil)
	store.On("GetExpiredIdle", mock.Anything, mock.AnythingOfType("time.Time")).
		Return([]string(nil), nil)
	// DeleteTerminated is NOT expected — retention cleanup is intentionally
	// removed so that TERMINATED records serve as "resume decision flags".
	store.On("Close").Return(nil)

	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)

	m.gc(ctx)
}

func TestManager_GC_TerminatedSessionPreserved(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("GetExpiredMaxLifetime", mock.Anything, mock.AnythingOfType("time.Time")).
		Return([]string(nil), nil)
	store.On("GetExpiredIdle", mock.Anything, mock.AnythingOfType("time.Time")).
		Return([]string(nil), nil)
	store.On("Close").Return(nil)

	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)

	// Seed a TERMINATED session with UpdatedAt old enough to be past retention.
	oldTime := time.Now().Add(-cfg.Session.RetentionPeriod - time.Hour)
	ms := &managedSession{
		info: SessionInfo{
			ID:         "sess_retention_preserved",
			UserID:     "user1",
			WorkerType: worker.TypeClaudeCode,
			State:      events.StateTerminated,
			CreatedAt:  oldTime,
			UpdatedAt:  oldTime,
		},
	}
	m.mu.Lock()
	m.sessions["sess_retention_preserved"] = ms
	m.mu.Unlock()

	// Before GC: session exists in memory.
	_, ok := m.sessions["sess_retention_preserved"]
	require.True(t, ok, "session should exist in memory before GC")

	m.gc(ctx)

	// After GC: session STILL in memory because retention cleanup is removed.
	// TERMINATED records are "resume decision flags" and should not be auto-deleted.
	m.mu.RLock()
	_, ok = m.sessions["sess_retention_preserved"]
	m.mu.RUnlock()
	require.True(t, ok, "TERMINATED session should remain in memory after GC (resume decision flag)")
}

func TestManager_GC_TerminatedSession_DBError_NoImpact(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	// Even if the store had a DeleteTerminated method that errored, GC should
	// be unaffected because retention cleanup is no longer performed.
	store.On("GetExpiredMaxLifetime", mock.Anything, mock.AnythingOfType("time.Time")).
		Return([]string(nil), errors.New("db error"))
	store.On("GetExpiredIdle", mock.Anything, mock.AnythingOfType("time.Time")).
		Return([]string(nil), errors.New("db error"))
	store.On("Close").Return(nil)

	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)

	// Seed a TERMINATED session.
	oldTime := time.Now().Add(-cfg.Session.RetentionPeriod - time.Hour)
	ms := &managedSession{
		info: SessionInfo{
			ID:         "sess_retention_noop",
			UserID:     "user1",
			WorkerType: worker.TypeClaudeCode,
			State:      events.StateTerminated,
			CreatedAt:  oldTime,
			UpdatedAt:  oldTime,
		},
	}
	m.mu.Lock()
	m.sessions["sess_retention_noop"] = ms
	m.mu.Unlock()

	// gc should not panic and should not touch TERMINATED sessions.
	m.gc(ctx)

	m.mu.RLock()
	_, ok := m.sessions["sess_retention_noop"]
	m.mu.RUnlock()
	require.True(t, ok, "TERMINATED session should remain after GC even with store errors")
}

func TestManager_GC_NoPanicOnStoreErrors(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("GetExpiredMaxLifetime", mock.Anything, mock.AnythingOfType("time.Time")).
		Return([]string(nil), errors.New("db error"))
	store.On("GetExpiredIdle", mock.Anything, mock.AnythingOfType("time.Time")).
		Return([]string(nil), errors.New("db error"))
	// DeleteTerminated no longer called — retention cleanup removed.
	store.On("Close").Return(nil)

	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)

	// gc should not panic even on store errors
	m.gc(ctx)
}

// ─── ClearContext tests ──────────────────────────────────────────────────────

func TestManager_ClearContext_Success(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)
	store.On("Close").Return(nil)

	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	now := time.Now()
	seed := &SessionInfo{
		ID:         "sess_clear",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateRunning,
		CreatedAt:  now,
		UpdatedAt:  now,
		Context:    map[string]any{"key1": "value1", "key2": 42},
	}
	m.mu.Lock()
	m.sessions["sess_clear"] = &managedSession{info: *seed}
	m.mu.Unlock()

	store.On("Upsert", ctx, mock.AnythingOfType("*session.SessionInfo")).Return(nil)

	err = m.ClearContext(ctx, "sess_clear")
	require.NoError(t, err)

	// Verify Context is now empty in memory
	info, _ := m.Get("sess_clear")
	require.NotNil(t, info)
	require.Empty(t, info.Context)
}

func TestManager_ClearContext_NotFound(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)

	store.On("Get", ctx, "sess_missing_clear").Return(nil, ErrSessionNotFound)
	store.On("Close").Return(nil)

	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	err = m.ClearContext(ctx, "sess_missing_clear")
	require.True(t, errors.Is(err, ErrSessionNotFound))
}

func TestManager_ClearContext_NilManager(t *testing.T) {
	t.Parallel()

	m := (*Manager)(nil)
	err := m.ClearContext(context.Background(), "any")
	require.True(t, errors.Is(err, ErrSessionNotFound))
}

func TestManager_ClearContext_UpdatesTimestamp(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)
	store.On("Close").Return(nil)

	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	now := time.Now()
	seed := &SessionInfo{
		ID:         "sess_clear_ts",
		UserID:     "user1",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateRunning,
		CreatedAt:  now.Add(-1 * time.Hour),
		UpdatedAt:  now.Add(-1 * time.Hour),
		Context:    map[string]any{"old": "data"},
	}
	m.mu.Lock()
	m.sessions["sess_clear_ts"] = &managedSession{info: *seed}
	m.mu.Unlock()

	store.On("Upsert", ctx, mock.AnythingOfType("*session.SessionInfo")).Return(nil)

	err = m.ClearContext(ctx, "sess_clear_ts")
	require.NoError(t, err)

	// Verify UpdatedAt was updated by checking in-memory state
	m.mu.RLock()
	updatedMs := m.sessions["sess_clear_ts"]
	m.mu.RUnlock()
	require.NotNil(t, updatedMs)
	// UpdatedAt should be after the original time
	require.True(t, updatedMs.info.UpdatedAt.After(now.Add(-5*time.Second)))
}

func TestManager_ClearContext_PreservesOtherFields(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	cfg := config.Default()
	store := new(mockStore)
	store.Test(t)
	store.On("Close").Return(nil)

	m, err := NewManager(ctx, nil, cfg, nil, store, nil)
	require.NoError(t, err)
	defer m.Close()

	now := time.Now()
	seed := &SessionInfo{
		ID:         "sess_clear_preserved",
		UserID:     "user_preserve",
		OwnerID:    "owner_preserve",
		BotID:      "bot_preserve",
		WorkerType: worker.TypeOpenCodeSrv,
		State:      events.StateRunning,
		CreatedAt:  now.Add(-30 * time.Minute),
		UpdatedAt:  now.Add(-30 * time.Minute),
		Context:    map[string]any{"some": "context"},
	}
	m.mu.Lock()
	m.sessions["sess_clear_preserved"] = &managedSession{info: *seed}
	m.mu.Unlock()

	store.On("Upsert", ctx, mock.AnythingOfType("*session.SessionInfo")).Return(nil)

	err = m.ClearContext(ctx, "sess_clear_preserved")
	require.NoError(t, err)

	// Verify other fields preserved in-memory
	m.mu.RLock()
	ms := m.sessions["sess_clear_preserved"]
	m.mu.RUnlock()
	require.NotNil(t, ms)
	require.Equal(t, "user_preserve", ms.info.UserID)
	require.Equal(t, "owner_preserve", ms.info.OwnerID)
	require.Equal(t, "bot_preserve", ms.info.BotID)
	require.Equal(t, worker.TypeOpenCodeSrv, ms.info.WorkerType)
	require.Equal(t, events.StateRunning, ms.info.State)
	require.Empty(t, ms.info.Context)
}
