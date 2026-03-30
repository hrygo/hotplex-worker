// Package session implements the session manager with SQLite persistence,
// state machine, and background GC.
package session

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"hotplex-worker/internal/config"
	"hotplex-worker/internal/worker"
	"hotplex-worker/pkg/events"
)

// Errors returned by the session manager.
var (
	ErrSessionNotFound   = errors.New("session: not found")
	ErrSessionBusy       = errors.New("session: busy")
	ErrInvalidTransition = errors.New("session: invalid state transition")
	ErrPoolExhausted     = errors.New("session: pool exhausted")
	ErrUserQuotaExceeded = errors.New("session: user quota exceeded")
)

// Manager orchestrates session lifecycle, persistence, and GC.
type Manager struct {
	log   *slog.Logger
	store Store
	cfg   *config.Config

	mu         sync.RWMutex
	sessions   map[string]*managedSession
	userCount  map[string]int // userID → count of active sessions
	totalCount int

	gcStop context.CancelFunc
	gcDone chan struct{}

	OnTerminate   func(sessionID string)
	StateNotifier func(ctx context.Context, sessionID string, state events.SessionState, message string)
}

// managedSession holds a session's in-memory state and its mutex.
type managedSession struct {
	info   SessionInfo
	worker worker.Worker
	mu     sync.Mutex // protects state transitions and input handling
}

// SessionInfo is the in-memory session metadata.
type SessionInfo struct {
	ID            string              `json:"id"`
	UserID        string              `json:"user_id"`
	WorkerType    worker.WorkerType   `json:"worker_type"`
	State         events.SessionState `json:"state"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
	ExpiresAt     *time.Time          `json:"expires_at,omitempty"`
	IdleExpiresAt *time.Time          `json:"idle_expires_at,omitempty"`
	Context       map[string]any      `json:"context,omitempty"`
}

// NewManager creates a new session manager using the provided Store.
func NewManager(ctx context.Context, log *slog.Logger, cfg *config.Config, store Store) (*Manager, error) {
	if log == nil {
		log = slog.Default()
	}

	m := &Manager{
		log:       log,
		store:     store,
		cfg:       cfg,
		sessions:  make(map[string]*managedSession),
		userCount: make(map[string]int),
	}

	// Start background GC.
	gcCtx, stop := context.WithCancel(context.Background())
	m.gcStop = stop
	m.gcDone = make(chan struct{})
	go m.runGC(gcCtx)

	m.log.Info("session: manager initialized")
	return m, nil
}

// Create creates a new session and persists it to SQLite.
func (m *Manager) Create(ctx context.Context, id, userID string, workerType worker.WorkerType) (*SessionInfo, error) {
	now := time.Now()
	info := &SessionInfo{
		ID:         id,
		UserID:     userID,
		WorkerType: workerType,
		State:      events.StateCreated,
		CreatedAt:  now,
		UpdatedAt:  now,
		ExpiresAt:  ptr(now.Add(m.cfg.Session.RetentionPeriod)),
	}

	if err := m.store.Upsert(ctx, info); err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.sessions[id] = &managedSession{info: *info}
	m.mu.Unlock()

	m.log.Info("session: created", "id", id, "user_id", userID, "worker_type", workerType)
	return info, nil
}

// Get returns a session by ID. Returns ErrSessionNotFound if not found.
func (m *Manager) Get(id string) (*SessionInfo, error) {
	m.mu.RLock()
	ms, ok := m.sessions[id]
	m.mu.RUnlock()
	if ok {
		return &ms.info, nil
	}

	// Fall back to Store.
	info, err := m.store.Get(context.Background(), id)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.sessions[id] = &managedSession{info: *info}
	m.mu.Unlock()

	return info, nil
}

// Transition atomically transitions a session to a new state.
// Both the in-memory state and the DB are updated.
// When transitioning to IDLE, sets idle_expires_at = now + IdleTimeout.
func (m *Manager) Transition(ctx context.Context, id string, to events.SessionState) error {
	ms := m.getManagedSession(id)
	if ms == nil {
		return ErrSessionNotFound
	}

	ms.mu.Lock()
	defer ms.mu.Unlock()

	from := ms.info.State
	if !events.IsValidTransition(from, to) {
		return fmt.Errorf("%w: %s → %s", ErrInvalidTransition, from, to)
	}

	ms.info.State = to
	ms.info.UpdatedAt = time.Now()

	// Set idle expiry when entering IDLE; clear when leaving IDLE.
	if to == events.StateIdle {
		ms.info.IdleExpiresAt = ptr(time.Now().Add(m.cfg.Worker.IdleTimeout))
	} else {
		ms.info.IdleExpiresAt = nil
	}

	if err := m.store.Upsert(ctx, &ms.info); err != nil {
		return err
	}

	if to == events.StateTerminated || to == events.StateDeleted {
		m.DetachWorker(id)
	}

	m.log.Info("session: transitioned", "id", id, "from", from, "to", to)

	if m.StateNotifier != nil {
		go m.StateNotifier(context.Background(), id, to, "")
	}
	if (to == events.StateTerminated || to == events.StateDeleted) && m.OnTerminate != nil {
		go m.OnTerminate(id)
	}

	return nil
}

// TransitionWithInput performs a state transition and processes user input
// atomically (both under the same mutex).
func (m *Manager) TransitionWithInput(ctx context.Context, id string, to events.SessionState, content string, metadata map[string]any) error {
	ms := m.getManagedSession(id)
	if ms == nil {
		return ErrSessionNotFound
	}

	ms.mu.Lock()
	defer ms.mu.Unlock()

	from := ms.info.State
	if !events.IsValidTransition(from, to) {
		return fmt.Errorf("%w: %s → %s", ErrInvalidTransition, from, to)
	}

	ms.info.State = to
	ms.info.UpdatedAt = time.Now()

	if err := m.store.Upsert(ctx, &ms.info); err != nil {
		return err
	}

	if to == events.StateTerminated || to == events.StateDeleted {
		m.DetachWorker(id)
	}

	m.log.Info("session: transition_with_input", "id", id, "from", from, "to", to)

	if m.StateNotifier != nil {
		go m.StateNotifier(context.Background(), id, to, "")
	}
	if (to == events.StateTerminated || to == events.StateDeleted) && m.OnTerminate != nil {
		go m.OnTerminate(id)
	}

	return nil
}

// AttachWorker attempts to allocate concurrency quota and pair the worker runtime to the session.
// It returns ErrPoolExhausted or ErrUserQuotaExceeded if limits are reached.
func (m *Manager) AttachWorker(id string, w worker.Worker) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ms, ok := m.sessions[id]
	if !ok {
		return ErrSessionNotFound
	}
	userID := ms.info.UserID

	if m.cfg.Pool.MaxSize > 0 && m.totalCount >= m.cfg.Pool.MaxSize {
		m.log.Warn("session: pool exhausted", "total", m.totalCount, "max", m.cfg.Pool.MaxSize)
		return ErrPoolExhausted
	}

	if m.cfg.Pool.MaxIdlePerUser > 0 {
		if m.userCount[userID] >= m.cfg.Pool.MaxIdlePerUser {
			m.log.Warn("session: user quota exceeded", "user_id", userID, "count", m.userCount[userID])
			return ErrUserQuotaExceeded
		}
	}

	ms.mu.Lock()
	ms.worker = w
	ms.mu.Unlock()

	m.userCount[userID]++
	m.totalCount++

	m.log.Debug("session: worker attached (quota acquired)", "session_id", id, "user_id", userID, "total", m.totalCount)
	return nil
}

// GetWorker returns the worker for a session.
func (m *Manager) GetWorker(id string) worker.Worker {
	ms := m.getManagedSession(id)
	if ms == nil {
		return nil
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()
	return ms.worker
}

// DetachWorker removes the worker from the session and releases the concurrency quota.
// This is automatically called when a session enters TERMINATED or DELETED state,
// but can also be explicitly called to immediately free resources.
func (m *Manager) DetachWorker(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ms, ok := m.sessions[id]
	if !ok {
		return
	}

	ms.mu.Lock()
	hasWorker := ms.worker != nil
	ms.worker = nil
	ms.mu.Unlock()

	if hasWorker {
		m.userCount[ms.info.UserID]--
		if m.userCount[ms.info.UserID] <= 0 {
			delete(m.userCount, ms.info.UserID)
		}
		m.totalCount--
		m.log.Debug("session: worker detached (quota released)", "session_id", id, "total", m.totalCount)
	}
}

// Delete marks a session as DELETED and removes it from the in-memory cache.
// The DB record is updated to reflect the deleted state.
func (m *Manager) Delete(ctx context.Context, id string) error {
	ms := m.getManagedSession(id)
	if ms != nil {
		ms.mu.Lock()
		ms.info.State = events.StateDeleted
		ms.info.UpdatedAt = time.Now()
		if err := m.store.Upsert(ctx, &ms.info); err != nil {
			ms.mu.Unlock()
			return err
		}
		ms.mu.Unlock()
		
		// Detach worker to release connection pool quota
		m.DetachWorker(id)

		if m.StateNotifier != nil {
			go m.StateNotifier(context.Background(), id, events.StateDeleted, "session deleted")
		}
		if m.OnTerminate != nil {
			go m.OnTerminate(id)
		}
	}

	m.mu.Lock()
	delete(m.sessions, id)
	m.mu.Unlock()

	m.log.Info("session: deleted", "id", id)
	return nil
}

// Lock acquires the per-session mutex for exclusive access.
// The caller MUST call Unlock when done.
func (m *Manager) Lock(id string) (release func(), err error) {
	ms := m.getManagedSession(id)
	if ms == nil {
		return nil, ErrSessionNotFound
	}
	ms.mu.Lock()
	return ms.mu.Unlock, nil
}

// List returns all sessions from Store. Use ListActive for in-memory active sessions only.
func (m *Manager) List(ctx context.Context, limit, offset int) ([]*SessionInfo, error) {
	return m.store.List(ctx, limit, offset)
}

// ListActive returns in-memory active sessions (no DB round-trip).
func (m *Manager) ListActive() []*SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := make([]*SessionInfo, 0, len(m.sessions))
	for _, ms := range m.sessions {
		sessions = append(sessions, &ms.info)
	}
	return sessions
}

// Stats returns the active worker quota statistics.
func (m *Manager) Stats() (totalWorkers, maxWorkers, uniqueUsers int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.totalCount, m.cfg.Pool.MaxSize, len(m.userCount)
}

// Close shuts down the manager and cancels the GC goroutine.
// It also terminates all actively tracked worker processes.
func (m *Manager) Close() error {
	m.gcStop()
	<-m.gcDone

	m.mu.Lock()
	var workers []worker.Worker
	for _, ms := range m.sessions {
		ms.mu.Lock()
		if ms.worker != nil {
			workers = append(workers, ms.worker)
		}
		ms.mu.Unlock()
	}
	m.mu.Unlock()

	for _, w := range workers {
		if w != nil {
			_ = w.Kill()
		}
	}

	return m.store.Close()
}

// ─── GC ─────────────────────────────────────────────────────────────────────

func (m *Manager) runGC(ctx context.Context) {
	defer close(m.gcDone)
	ticker := time.NewTicker(m.cfg.Session.GCScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.gc(ctx)
		}
	}
}

func (m *Manager) gc(ctx context.Context) {
	now := time.Now()

	// 0. Zombie IO Polling for RUNNING sessions
	m.mu.RLock()
	var runningSessions []string
	var runningWorkers []worker.Worker
	for _, ms := range m.sessions {
		if ms.info.State == events.StateRunning {
			runningSessions = append(runningSessions, ms.info.ID)
			runningWorkers = append(runningWorkers, ms.worker)
		} // We intentionally copy IDs/Workers to avoid holding RLock during DB Transition
	}
	m.mu.RUnlock()

	for i, id := range runningSessions {
		w := runningWorkers[i]
		if w != nil {
			if hc, ok := w.(interface{ LastIO() time.Time }); ok {
				lastIO := hc.LastIO()
				// Default 5 minutes zombie timeout if config missing
				timeout := 5 * time.Minute
				if m.cfg.Worker.ExecutionTimeout > 0 {
					timeout = m.cfg.Worker.ExecutionTimeout
				}
				if !lastIO.IsZero() && now.Sub(lastIO) > timeout {
					m.log.Warn("session: zombie IO polling triggered, terminating ghost process", "id", id, "last_io", lastIO)
					if err := m.Transition(ctx, id, events.StateTerminated); err != nil {
						m.log.Warn("session: zombie GC transition error", "err", err)
					}
				}
			}
		}
	}

	// 1. Terminate sessions (any active state) that have exceeded max_lifetime.
	maxIds, err := m.store.GetExpiredMaxLifetime(ctx, now)
	if err != nil {
		m.log.Error("session: gc (max_lifetime) query", "err", err)
	} else {
		for _, id := range maxIds {
			if err := m.Transition(ctx, id, events.StateTerminated); err != nil {
				m.log.Warn("session: gc (max_lifetime) transition", "id", id, "err", err)
			}
		}
	}

	// 2. Terminate IDLE sessions that have exceeded idle_timeout.
	idleIds, err := m.store.GetExpiredIdle(ctx, now)
	if err != nil {
		m.log.Error("session: gc (idle) query", "err", err)
	} else {
		for _, id := range idleIds {
			if err := m.Transition(ctx, id, events.StateTerminated); err != nil {
				m.log.Warn("session: gc (idle) transition", "id", id, "err", err)
			}
		}
	}

	// 3. Delete TERMINATED sessions past retention_period.
	cutoff := now.Add(-m.cfg.Session.RetentionPeriod)
	if err := m.store.DeleteTerminated(ctx, cutoff); err != nil {
		m.log.Error("session: gc (retention) delete", "err", err)
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func (m *Manager) getManagedSession(id string) *managedSession {
	m.mu.RLock()
	ms, ok := m.sessions[id]
	m.mu.RUnlock()
	if ok {
		return ms
	}
	// Load from Store.
	info, err := m.store.Get(context.Background(), id)
	if err != nil {
		return nil
	}
	m.mu.Lock()
	if ms, ok := m.sessions[id]; ok {
		m.mu.Unlock()
		return ms
	}
	ms = &managedSession{info: *info}
	m.sessions[id] = ms
	m.mu.Unlock()
	return ms
}

// ptr returns a pointer to v.
func ptr[T any](v T) *T { return &v }
