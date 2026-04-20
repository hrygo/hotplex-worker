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

	"golang.org/x/sync/errgroup"

	"github.com/hotplex/hotplex-worker/internal/config"
	"github.com/hotplex/hotplex-worker/internal/metrics"
	"github.com/hotplex/hotplex-worker/internal/worker"
	"github.com/hotplex/hotplex-worker/pkg/events"
)

// Errors returned by the session manager.
var (
	ErrSessionNotFound   = errors.New("session: not found")
	ErrSessionBusy       = errors.New("session: busy")
	ErrInvalidTransition = errors.New("session: invalid state transition")
	ErrPoolExhausted     = errors.New("session: pool exhausted")
	ErrUserQuotaExceeded = errors.New("session: user quota exceeded")
	ErrOwnershipMismatch = errors.New("session: ownership mismatch")
	ErrMaxTurnsReached   = errors.New("session: max turns reached")
	ErrWorkerAttached    = errors.New("session: worker already attached")
)

// Manager orchestrates session lifecycle, persistence, and GC.
type Manager struct {
	log      *slog.Logger
	store    Store
	msgStore MessageStore // EVT-004: optional event persistence; nil is safe
	cfg      *config.Config
	pool     *PoolManager

	mu       sync.RWMutex
	sessions map[string]*managedSession

	gcStop context.CancelFunc
	gcDone chan struct{}

	OnTerminate   func(sessionID string)
	StateNotifier func(ctx context.Context, sessionID string, state events.SessionState, message string)
}

// managedSession holds a session's in-memory state and its mutex.
type managedSession struct {
	info      SessionInfo
	worker    worker.Worker
	TurnCount int
	startedAt time.Time
	log       *slog.Logger
	mu        sync.RWMutex // protects state transitions and input handling; reads use RLock
}

// SessionInfo is the in-memory session metadata.
type SessionInfo struct {
	ID            string              `json:"id"`
	UserID        string              `json:"user_id"`
	OwnerID       string              `json:"owner_id,omitempty"` // authenticated owner; falls back to UserID when nil
	BotID         string              `json:"bot_id,omitempty"`   // SEC-007: bot isolation
	WorkerType    worker.WorkerType   `json:"worker_type"`
	State         events.SessionState `json:"state"`
	CreatedAt     time.Time           `json:"created_at"`
	UpdatedAt     time.Time           `json:"updated_at"`
	ExpiresAt     *time.Time          `json:"expires_at,omitempty"`
	IdleExpiresAt *time.Time          `json:"idle_expires_at,omitempty"`
	Context       map[string]any      `json:"context,omitempty"`
	// WorkerSessionID is the session ID used by the worker runtime itself.
	// Only populated for workers that auto-generate their own session IDs (OpenCode Server).
	// For Claude Code this is always empty — the gateway's ID IS the worker's session ID
	// (passed via --session-id / --resume).
	WorkerSessionID string `json:"worker_session_id,omitempty"`
	// AllowedTools is the list of tools this session is allowed to use.
	AllowedTools []string `json:"allowed_tools,omitempty"`
	// Platform identifies the messaging platform ("slack", "feishu", "" for direct WS).
	Platform string `json:"platform,omitempty"`
	// PlatformKey stores the consistency-mapping inputs as JSON.
	// This is the same data fed to DerivePlatformSessionKey, persisted so that
	// the mapping can be reconstructed from DB after a gateway restart.
	// Example (Feishu): {"chat_id":"oc_xxx","thread_ts":"","user_id":"ou_xxx"}
	// Example (Slack):  {"team_id":"Txxx","channel_id":"Cxxx","thread_ts":"1234.56","user_id":"Uxxx"}
	PlatformKey map[string]string `json:"platform_key,omitempty"`
}

// NewManager creates a new session manager using the provided Store and optional MessageStore.
func NewManager(ctx context.Context, log *slog.Logger, cfg *config.Config, store Store, msgStore MessageStore) (*Manager, error) {
	if log == nil {
		log = slog.Default()
	}

	m := &Manager{
		log:      log.With("component", "session"),
		store:    store,
		msgStore: msgStore,
		cfg:      cfg,
		pool:     NewPoolManager(log, cfg.Pool.MaxSize, cfg.Pool.MaxIdlePerUser, cfg.Pool.MaxMemoryPerUser),
		sessions: make(map[string]*managedSession),
	}

	// Start background GC.
	gcCtx, stop := context.WithCancel(context.Background())
	m.gcStop = stop
	m.gcDone = make(chan struct{})
	go m.runGC(gcCtx)

	m.log.Info("session: manager initialized", "msg_store", msgStore != nil)
	return m, nil
}

// Create creates a new session and persists it to SQLite.
func (m *Manager) Create(ctx context.Context, id, userID string, workerType worker.WorkerType, allowedTools []string) (*SessionInfo, error) {
	return m.CreateWithBot(ctx, id, userID, "", workerType, allowedTools, "", nil)
}

// CreateWithBot creates a new session with explicit bot_id and persists it to SQLite.
func (m *Manager) CreateWithBot(ctx context.Context, id, userID, botID string, workerType worker.WorkerType, allowedTools []string, platform string, platformKey map[string]string) (*SessionInfo, error) {
	now := time.Now()
	info := &SessionInfo{
		ID:           id,
		UserID:       userID,
		BotID:        botID,
		WorkerType:   workerType,
		State:        events.StateCreated,
		CreatedAt:    now,
		UpdatedAt:    now,
		ExpiresAt:    ptr(now.Add(m.cfg.Session.RetentionPeriod)),
		AllowedTools: allowedTools,
		Platform:     platform,
		PlatformKey:  platformKey,
	}

	if err := m.store.Upsert(ctx, info); err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.sessions[id] = &managedSession{info: *info, log: m.log.With("worker_type", workerType)}
	m.mu.Unlock()

	m.log.Info("session: created", "session_id", id, "user_id", userID, "worker_type", workerType, "bot_id", botID)
	metrics.SessionsTotal.WithLabelValues(string(workerType)).Inc()
	metrics.SessionsActive.WithLabelValues(string(events.StateCreated)).Inc()
	return info, nil
}

// Get returns a snapshot of a session by ID. Returns ErrSessionNotFound if not found.
// The returned *SessionInfo is a copy safe to read without holding locks.
func (m *Manager) Get(id string) (*SessionInfo, error) {
	m.mu.RLock()
	ms, ok := m.sessions[id]
	m.mu.RUnlock()
	if ok {
		ms.mu.RLock()
		info := ms.info
		ms.mu.RUnlock()
		return &info, nil
	}

	// Fall back to Store.
	info, err := m.store.Get(context.Background(), id)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	m.sessions[id] = &managedSession{info: *info, log: m.log.With("worker_type", info.WorkerType)}
	m.mu.Unlock()

	return info, nil
}

// ─── State transitions ───────────────────────────────────────────────────────

// Default graceful shutdown timeout for worker termination.
const gracefulShutdownTimeout = 5 * time.Second

// transitionState performs the common state-transition work: validation,
// in-memory update, persistence, and notifications.
// Caller must hold ms.mu for write.
func (m *Manager) transitionState(ctx context.Context, ms *managedSession, from, to events.SessionState, termReason string) error {
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
		// Record worker execution duration and decrement running gauge before killing.
		if !ms.startedAt.IsZero() && ms.worker != nil {
			metrics.WorkerExecDuration.WithLabelValues(string(ms.info.WorkerType)).Observe(time.Since(ms.startedAt).Seconds())
		}
		if ms.worker != nil {
			metrics.WorkersRunning.WithLabelValues(string(ms.info.WorkerType)).Dec()
			// Release quota only when worker is still attached (DetachWorker may
			// have already released it on the bridge cleanup path).
			m.releaseWorkerQuota(ms)
		}
		// Gracefully terminate the worker process with 5s grace period.
		// Safe: ms.mu is held by the caller, and worker.Terminate() does not
		// acquire any session manager locks (it uses syscall.Kill only).
		if ms.worker != nil {
			terminateCtx, cancel := context.WithTimeout(ctx, gracefulShutdownTimeout)
			defer cancel()
			if err := ms.worker.Terminate(terminateCtx); err != nil {
				m.log.Warn("session: worker terminate failed", "session_id", ms.info.ID, "err", err)
			}
			// Nil the pointer to prevent DetachWorker from releasing quota a
			// second time (e.g. when forwardEvents goroutine exits after the
			// worker process dies). Without this, pool.totalCount underflows.
			ms.worker = nil
		}
	}

	m.log.Info("session: transitioned", "session_id", ms.info.ID, "from", from, "to", to)

	// Update active sessions gauge.
	metrics.SessionsActive.WithLabelValues(string(from)).Dec()
	metrics.SessionsActive.WithLabelValues(string(to)).Inc()

	// Record termination reason.
	if to == events.StateTerminated {
		if termReason == "" {
			termReason = "terminated"
		}
		metrics.SessionsTerminated.WithLabelValues(termReason).Inc()
	}
	if to == events.StateDeleted {
		metrics.SessionsDeleted.Inc()
	}

	if m.StateNotifier != nil {
		go m.StateNotifier(context.Background(), ms.info.ID, to, "")
	}
	if (to == events.StateTerminated || to == events.StateDeleted) && m.OnTerminate != nil {
		go m.OnTerminate(ms.info.ID)
	}

	return nil
}

// Transition atomically transitions a session to a new state.
// Both the in-memory state and the DB are updated.
// When transitioning to IDLE, sets idle_expires_at = now + IdleTimeout.
func (m *Manager) Transition(ctx context.Context, id string, to events.SessionState) error {
	return m.TransitionWithReason(ctx, id, to, "client_kill")
}

// TransitionWithReason transitions a session with an explicit termination reason.
// termReason is used as the label value for SessionsTerminated when transitioning
// to StateTerminated (e.g., "idle_timeout", "max_lifetime", "zombie", "admin_kill").
func (m *Manager) TransitionWithReason(ctx context.Context, id string, to events.SessionState, termReason string) error {
	if m == nil {
		return ErrSessionNotFound
	}
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

	return m.transitionState(ctx, ms, from, to, termReason)
}

// TransitionWithInput performs a state transition and processes user input
// atomically (both under the same mutex).
func (m *Manager) TransitionWithInput(ctx context.Context, id string, to events.SessionState, content string, metadata map[string]any) error {
	if m == nil {
		return ErrSessionNotFound
	}
	ms := m.getManagedSession(id)
	if ms == nil {
		return ErrSessionNotFound
	}

	ms.mu.Lock()
	defer ms.mu.Unlock()

	// Anti-pollution: enforce max turns limit.
	ms.TurnCount++
	if ms.worker != nil {
		maxTurns := ms.worker.MaxTurns()
		if maxTurns > 0 && ms.TurnCount > maxTurns {
			m.log.Warn("session: max turns exceeded, initiating anti-pollution restart",
				"session_id", id, "turn_count", ms.TurnCount, "max_turns", maxTurns)
			_ = ms.worker.Kill()
			from := ms.info.State
			if events.IsValidTransition(from, events.StateTerminated) {
				_ = m.transitionState(ctx, ms, from, events.StateTerminated, "max_turns")
			}
			return ErrMaxTurnsReached
		}
	}

	from := ms.info.State
	if !events.IsValidTransition(from, to) {
		return fmt.Errorf("%w: %s → %s", ErrInvalidTransition, from, to)
	}

	return m.transitionState(ctx, ms, from, to, "client_input")
}

// AttachWorker attempts to allocate concurrency quota and pair the worker runtime to the session.
func (m *Manager) AttachWorker(id string, w worker.Worker) error {
	if m == nil {
		return ErrSessionNotFound
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	ms, ok := m.sessions[id]
	if !ok {
		return ErrSessionNotFound
	}
	userID := ms.info.UserID

	if ms.worker != nil {
		return ErrWorkerAttached
	}
	if poolErr := m.pool.Acquire(userID); poolErr != nil {
		var pe *PoolError
		if !errors.As(poolErr, &pe) {
			m.log.Warn("session: attach rejected", "err", poolErr, "session_id", id)
			metrics.PoolAcquireTotal.WithLabelValues("pool_exhausted").Inc()
			return ErrPoolExhausted
		}
		m.log.Warn("session: attach rejected", "kind", pe.Kind, "session_id", id)
		if pe.Kind == poolErrKindUserQuotaExceeded {
			metrics.PoolAcquireTotal.WithLabelValues("user_quota_exceeded").Inc()
			return ErrUserQuotaExceeded
		}
		metrics.PoolAcquireTotal.WithLabelValues("pool_exhausted").Inc()
		return ErrPoolExhausted
	}

	// RES-008: track per-user estimated memory (RLIMIT_AS=512MB per worker).
	if err := m.pool.AcquireMemory(userID); err != nil {
		m.pool.Release(userID) // rollback slot quota
		metrics.PoolAcquireTotal.WithLabelValues("memory_exceeded").Inc()
		return ErrMemoryExceeded
	}
	ms.mu.Lock()
	ms.worker = w
	ms.startedAt = time.Now()
	metrics.WorkerStartsTotal.WithLabelValues(string(ms.info.WorkerType), "success").Inc()
	metrics.WorkersRunning.WithLabelValues(string(ms.info.WorkerType)).Inc()
	ms.mu.Unlock()

	m.log.Debug("session: worker attached", "session_id", id, "user_id", userID)
	return nil
}

// GetWorker returns the worker for a session.
func (m *Manager) GetWorker(id string) worker.Worker {
	if m == nil {
		return nil
	}
	ms := m.getManagedSession(id)
	if ms == nil {
		return nil
	}
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	return ms.worker
}

// releaseWorkerQuota delegates to PoolManager.Release.
func (m *Manager) releaseWorkerQuota(ms *managedSession) {
	m.pool.Release(ms.info.UserID)
}

// DetachWorker removes the worker from the session and releases the concurrency quota.
// It is safe to call even if no worker is attached.
// Acquires ms.mu then pool lock to avoid deadlock with Delete.
func (m *Manager) DetachWorker(id string) {
	if m == nil {
		return
	}
	ms := m.getManagedSession(id)
	if ms == nil {
		return
	}

	ms.mu.Lock()
	hasWorker := ms.worker != nil
	workerType := ms.info.WorkerType
	ms.worker = nil
	uid := ms.info.UserID
	ms.mu.Unlock()

	if hasWorker {
		metrics.WorkersRunning.WithLabelValues(string(workerType)).Dec()
		m.pool.Release(uid)
		m.pool.ReleaseMemory(uid)
		m.log.Debug("session: worker detached", "session_id", id)
	}
}

// Delete marks a session as DELETED and removes it from the in-memory cache.
// Lock ordering: m.mu → ms.mu (same as AttachWorker/DetachWorker to avoid deadlock).
func (m *Manager) Delete(ctx context.Context, id string) error {
	// Acquire m.mu first to maintain consistent lock order with AttachWorker.
	m.mu.Lock()
	ms, ok := m.sessions[id]
	if !ok {
		m.mu.Unlock()
		return nil
	}

	ms.mu.Lock()
	hasWorker := ms.worker != nil
	workerType := ms.info.WorkerType
	ms.info.State = events.StateDeleted
	ms.info.UpdatedAt = time.Now()
	if err := m.store.Upsert(ctx, &ms.info); err != nil {
		ms.mu.Unlock()
		m.mu.Unlock()
		return err
	}
	uid := ms.info.UserID
	ms.mu.Unlock()

	// Release quota and remove from map while still holding m.mu.
	if hasWorker {
		metrics.WorkersRunning.WithLabelValues(string(workerType)).Dec()
	}
	m.pool.Release(uid)
	delete(m.sessions, id)
	m.mu.Unlock()

	if m.StateNotifier != nil {
		go m.StateNotifier(context.Background(), id, events.StateDeleted, "session deleted")
	}
	if m.OnTerminate != nil {
		go m.OnTerminate(id)
	}

	m.log.Info("session: deleted", "session_id", id)
	return nil
}

// ValidateOwnership checks whether the given userID owns the session.
// Returns nil if the user is the owner, or ErrOwnershipMismatch otherwise.
// Admin bypass: if adminUserID is non-empty, it bypasses ownership check.
func (m *Manager) ValidateOwnership(ctx context.Context, sessionID, userID, adminUserID string) error {
	si, err := m.Get(sessionID)
	if err != nil {
		return err
	}
	if adminUserID != "" {
		return nil // admin bypass
	}
	if si.UserID != userID {
		m.log.Warn("session: ownership mismatch",
			"session_id", sessionID,
			"expected_owner", si.UserID,
			"actual_user", userID,
		)
		return ErrOwnershipMismatch
	}
	return nil
}

// ClearContext clears the session context map.
// Used by control.reset: Gateway layer clears SessionInfo.Context.
// Worker runtime context clearing is delegated to Worker.ResetContext (in-place or terminate+start).
func (m *Manager) ClearContext(ctx context.Context, sessionID string) error {
	if m == nil {
		return ErrSessionNotFound
	}
	ms := m.getManagedSession(sessionID)
	if ms == nil {
		return ErrSessionNotFound
	}

	ms.mu.Lock()
	defer ms.mu.Unlock()

	ms.info.Context = map[string]any{}
	ms.info.UpdatedAt = time.Now()

	return m.store.Upsert(ctx, &ms.info)
}

// UpdateWorkerSessionID persists the worker-internal session ID for resume support.
// Workers that manage their own session IDs (OpenCode Server) call this
// to store the ID so it can be restored on resume.
func (m *Manager) UpdateWorkerSessionID(ctx context.Context, id, workerSessionID string) error {
	if m == nil {
		return ErrSessionNotFound
	}
	ms := m.getManagedSession(id)
	if ms == nil {
		return ErrSessionNotFound
	}

	ms.mu.Lock()
	defer ms.mu.Unlock()

	if ms.info.WorkerSessionID == workerSessionID {
		return nil
	}
	ms.info.WorkerSessionID = workerSessionID
	ms.info.UpdatedAt = time.Now()

	return m.store.Upsert(ctx, &ms.info)
}

// DebugSessionSnapshot holds safe-to-expose debug info for a managed session.
// Exists to prevent callers from acquiring the per-session mutex directly,
// which would violate lock ordering invariants and risk deadlocks.
type DebugSessionSnapshot struct {
	TurnCount    int
	WorkerHealth worker.WorkerHealth
	HasWorker    bool
}

// DebugSnapshot safely captures debug fields from a managed session under the read lock.
func (m *Manager) DebugSnapshot(id string) (DebugSessionSnapshot, bool) {
	ms := m.getManagedSession(id)
	if ms == nil {
		return DebugSessionSnapshot{}, false
	}
	ms.mu.RLock()
	defer ms.mu.RUnlock()
	snap := DebugSessionSnapshot{
		TurnCount: ms.TurnCount,
	}
	if ms.worker != nil {
		snap.HasWorker = true
		snap.WorkerHealth = ms.worker.Health()
	}
	return snap, true
}

// MessageStore returns the configured MessageStore (may be nil).
func (m *Manager) MessageStore() MessageStore {
	return m.msgStore
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

// Stats returns the active worker pool utilization.
func (m *Manager) Stats() (totalWorkers, maxWorkers, uniqueUsers int) {
	total, max, users := m.pool.Stats()
	if max > 0 {
		metrics.PoolUtilization.Set(float64(total) / float64(max))
	}
	return total, max, users
}

// WorkerHealthStatuses returns a snapshot of health for all active worker processes.
func (m *Manager) WorkerHealthStatuses() []worker.WorkerHealth {
	m.mu.RLock()
	defer m.mu.RUnlock()

	statuses := make([]worker.WorkerHealth, 0, len(m.sessions))
	for _, ms := range m.sessions {
		ms.mu.RLock()
		if ms.worker != nil {
			statuses = append(statuses, ms.worker.Health())
		}
		ms.mu.RUnlock()
	}
	return statuses
}

// Close shuts down the manager and cancels the GC goroutine.
// It also terminates all actively tracked worker processes and closes the MessageStore.
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
			// Graceful shutdown with 5s grace period
			terminateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = w.Terminate(terminateCtx)
			cancel()
		}
	}

	if err := m.store.Close(); err != nil {
		return err
	}
	if m.msgStore != nil {
		if err := m.msgStore.Close(); err != nil {
			return err
		}
	}
	return nil
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

	// 0. Zombie IO Polling for RUNNING sessions.
	// Lock order: m.mu.RLock() → ms.mu.RLock() is consistent with all write paths.
	m.mu.RLock()
	var runningSessions []string
	var runningWorkers []worker.Worker
	for _, ms := range m.sessions {
		ms.mu.RLock()
		if ms.info.State == events.StateRunning {
			runningSessions = append(runningSessions, ms.info.ID)
			runningWorkers = append(runningWorkers, ms.worker)
		}
		ms.mu.RUnlock()
	}
	m.mu.RUnlock()

	for i, id := range runningSessions {
		w := runningWorkers[i]
		if w != nil {
			// LastIO() is now part of the Worker interface — direct call, no type assertion needed
			lastIO := w.LastIO()
			// Default 5 minutes zombie timeout if config missing
			timeout := 5 * time.Minute
			if m.cfg.Worker.ExecutionTimeout > 0 {
				timeout = m.cfg.Worker.ExecutionTimeout
			}
			if !lastIO.IsZero() && now.Sub(lastIO) > timeout {
				m.log.Warn("session: zombie IO polling triggered, terminating ghost process",
					"session_id", id, "worker_type", w.Type(), "last_io", lastIO, "timeout", timeout)
				if err := m.TransitionWithReason(ctx, id, events.StateTerminated, "zombie"); err != nil {
					m.log.Warn("session: zombie GC transition error", "err", err)
				}
			}
		}
	}

	// 1+2. Terminate sessions past max_lifetime and IDLE sessions past idle_timeout.
	// These two independent DB queries run in parallel.
	eg, egCtx := errgroup.WithContext(ctx)
	var maxIds, idleIds []string
	eg.Go(func() error {
		var err error
		maxIds, err = m.store.GetExpiredMaxLifetime(egCtx, now)
		if err != nil {
			m.log.Error("session: gc (max_lifetime) query", "err", err)
		}
		return nil // don't propagate — we log and continue
	})
	eg.Go(func() error {
		var err error
		idleIds, err = m.store.GetExpiredIdle(egCtx, now)
		if err != nil {
			m.log.Error("session: gc (idle) query", "err", err)
		}
		return nil
	})
	_ = eg.Wait() // errors already logged inside goroutines

	for _, id := range maxIds {
		if err := m.TransitionWithReason(ctx, id, events.StateTerminated, "max_lifetime"); err != nil {
			m.log.Warn("session: gc (max_lifetime) transition", "session_id", id, "err", err)
		}
	}
	for _, id := range idleIds {
		if err := m.TransitionWithReason(ctx, id, events.StateTerminated, "idle_timeout"); err != nil {
			m.log.Warn("session: gc (idle) transition", "session_id", id, "err", err)
		}
	}

	// 3. Retention cleanup is intentionally NOT performed here.
	// TERMINATED session records serve as "resume decision flags" — their
	// existence tells the gateway that a previous session existed and that
	// the worker's session files may still be on disk (e.g. Claude Code's
	// ~/.claude/projects/<hash>/sessions/), enabling --resume to restore
	// the conversation. Deleting DB records would force --session-id (new
	// session) instead of --resume, losing conversation history.
	// Physical deletion should be an explicit admin action, not automatic GC.
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
	ms = &managedSession{info: *info, log: m.log.With("worker_type", info.WorkerType)}
	m.sessions[id] = ms
	m.mu.Unlock()
	return ms
}

// ptr returns a pointer to v.
func ptr[T any](v T) *T { return &v }
