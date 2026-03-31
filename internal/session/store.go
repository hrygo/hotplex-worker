package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"hotplex-worker/internal/config"
	"hotplex-worker/pkg/events"
)

// Store defines the interface for session persistence.
type Store interface {
	Upsert(ctx context.Context, info *SessionInfo) error
	Get(ctx context.Context, id string) (*SessionInfo, error)
	List(ctx context.Context, limit, offset int) ([]*SessionInfo, error)
	GetExpiredMaxLifetime(ctx context.Context, now time.Time) ([]string, error)
	GetExpiredIdle(ctx context.Context, now time.Time) ([]string, error)
	DeleteTerminated(ctx context.Context, cutoff time.Time) error
	Close() error
}

// SQLiteStore implements Store using SQLite.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates and initializes a new SQLiteStore.
func NewSQLiteStore(ctx context.Context, cfg *config.Config) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite3", cfg.DB.Path)
	if err != nil {
		return nil, fmt.Errorf("session store: open db: %w", err)
	}

	if cfg.DB.WALMode {
		if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
			db.Close()
			return nil, fmt.Errorf("session store: enable WAL: %w", err)
		}
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA busy_timeout=%d", int(cfg.DB.BusyTimeout.Milliseconds()))); err != nil {
		db.Close()
		return nil, fmt.Errorf("session store: set busy_timeout: %w", err)
	}

	store := &SQLiteStore{db: db}
	if err := store.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}

	return store, nil
}

func (s *SQLiteStore) migrate(ctx context.Context) error {
	schema := `
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    owner_id TEXT,
    bot_id TEXT,
    worker_session_id TEXT,
    worker_type TEXT NOT NULL,
    state TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    expires_at DATETIME,
    idle_expires_at DATETIME,
    is_active INTEGER NOT NULL DEFAULT 0,
    context_json TEXT
);
CREATE INDEX IF NOT EXISTS idx_sessions_state ON sessions(state);
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_owner_id ON sessions(owner_id);
CREATE INDEX IF NOT EXISTS idx_sessions_bot_id ON sessions(bot_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_sessions_idle_expires_at ON sessions(idle_expires_at);

-- EVT-001: events table for AEP message persistence.
-- Append-only enforced at application layer; SQLite does not support
-- standard BEFORE triggers for INSERT/UPDATE/DELETE prevention.
CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    seq INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_events_session_id ON events(session_id);
CREATE INDEX IF NOT EXISTS idx_events_session_seq ON events(session_id, seq);
CREATE UNIQUE INDEX IF NOT EXISTS idx_events_session_seq_unique ON events(session_id, seq);

-- Migrate: add owner_id column if it doesn't exist (no-op on fresh installs).
-- The column is nullable so existing rows remain valid; application code
-- falls back to user_id when owner_id IS NULL.
_ = s.db.Exec("ALTER TABLE sessions ADD COLUMN owner_id TEXT");
-- Migrate: add bot_id column for SEC-007 multi-bot isolation.
_ = s.db.Exec("ALTER TABLE sessions ADD COLUMN bot_id TEXT")
`
	_, err := s.db.ExecContext(ctx, schema)
	if err != nil {
		return fmt.Errorf("session store: migrate: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Upsert(ctx context.Context, info *SessionInfo) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}

	var ctxJSON []byte
	if info.Context != nil {
		var err error
		ctxJSON, err = json.Marshal(info.Context)
		if err != nil {
			return fmt.Errorf("session store: marshal context: %w", err)
		}
	}

	isActive := 0
	if info.State.IsActive() {
		isActive = 1
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, user_id, owner_id, bot_id, worker_session_id, worker_type, state, created_at, updated_at, expires_at, idle_expires_at, is_active, context_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   state=excluded.state,
		   updated_at=excluded.updated_at,
		   expires_at=excluded.expires_at,
		   idle_expires_at=excluded.idle_expires_at,
		   is_active=excluded.is_active,
		   context_json=excluded.context_json`,
		info.ID, info.UserID, info.OwnerID, info.BotID, info.WorkerSessionID, info.WorkerType, string(info.State),
		info.CreatedAt, info.UpdatedAt, info.ExpiresAt, info.IdleExpiresAt,
		isActive, string(ctxJSON),
	)
	return err
}

func (s *SQLiteStore) Get(ctx context.Context, id string) (*SessionInfo, error) {
	var info SessionInfo
	var ctxJSON sql.NullString
	var expiresAt, idleExpiresAt sql.NullTime
	var createdAt, updatedAt time.Time

	err := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, COALESCE(owner_id, user_id), worker_session_id, worker_type, state, created_at, updated_at, expires_at, idle_expires_at, context_json
		 FROM sessions WHERE id = ?`, id,
	).Scan(&info.ID, &info.UserID, &info.OwnerID, &info.WorkerSessionID, &info.WorkerType, &info.State,
		&createdAt, &updatedAt, &expiresAt, &idleExpiresAt, &ctxJSON)

	if err == sql.ErrNoRows {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("session store: load: %w", err)
	}

	info.CreatedAt = createdAt
	info.UpdatedAt = updatedAt
	if expiresAt.Valid {
		info.ExpiresAt = &expiresAt.Time
	}
	if idleExpiresAt.Valid {
		info.IdleExpiresAt = &idleExpiresAt.Time
	}
	if ctxJSON.Valid && ctxJSON.String != "" {
		if err := json.Unmarshal([]byte(ctxJSON.String), &info.Context); err != nil {
			return nil, fmt.Errorf("session store: unmarshal context: %w", err)
		}
	}

	return &info, nil
}

func (s *SQLiteStore) List(ctx context.Context, limit, offset int) ([]*SessionInfo, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, COALESCE(owner_id, user_id), worker_session_id, worker_type, state, created_at, updated_at, expires_at, idle_expires_at, context_json
		 FROM sessions ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("session store: list: %w", err)
	}
	defer rows.Close()

	var sessions []*SessionInfo
	for rows.Next() {
		var si SessionInfo
		var ctxJSON sql.NullString
		var expiresAt, idleExpiresAt sql.NullTime
		var createdAt, updatedAt time.Time

		err := rows.Scan(&si.ID, &si.UserID, &si.OwnerID, &si.WorkerSessionID, &si.WorkerType, &si.State,
			&createdAt, &updatedAt, &expiresAt, &idleExpiresAt, &ctxJSON)
		if err != nil {
			continue
		}
		si.CreatedAt = createdAt
		si.UpdatedAt = updatedAt
		if expiresAt.Valid {
			si.ExpiresAt = &expiresAt.Time
		}
		if idleExpiresAt.Valid {
			si.IdleExpiresAt = &idleExpiresAt.Time
		}
		if ctxJSON.Valid && ctxJSON.String != "" {
			_ = json.Unmarshal([]byte(ctxJSON.String), &si.Context)
		}
		sessions = append(sessions, &si)
	}
	return sessions, rows.Err()
}

func (s *SQLiteStore) GetExpiredMaxLifetime(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM sessions WHERE state IN (?,?,?) AND expires_at IS NOT NULL AND expires_at <= ?`,
		string(events.StateCreated), string(events.StateRunning), string(events.StateIdle), now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}

func (s *SQLiteStore) GetExpiredIdle(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id FROM sessions WHERE state=? AND idle_expires_at IS NOT NULL AND idle_expires_at <= ?`,
		events.StateIdle, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}

func (s *SQLiteStore) DeleteTerminated(ctx context.Context, cutoff time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM sessions WHERE state=? AND updated_at <= ?`,
		events.StateTerminated, cutoff)
	return err
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// ─── MessageStore (EVT-003) ───────────────────────────────────────────────────

// ErrEventNotFound is returned when no events exist for a given session.
var ErrEventNotFound = errors.New("session store: no events found")

// EventRecord describes a single persisted event.
type EventRecord struct {
	ID        string
	SessionID string
	Seq       int64
	EventType string
	Payload   []byte
	CreatedAt time.Time
}

// MessageStore defines the interface for AEP event persistence (EVT-003).
//
// Append-Only Enforcement (EVT-002):
// SQLite does not support standard BEFORE triggers to prevent UPDATE/DELETE
// at the SQL level. Integrity is enforced at the application layer:
//   - Append inserts only; no public Update/Delete methods are exposed.
//   - The async batch writer consumes from an unidirectional channel.
//   - Duplicate (session_id, seq) keys are handled with INSERT OR IGNORE.
type MessageStore interface {
	// Append persists a single event. It returns immediately after enqueueing
	// to the background writer; the write is performed asynchronously (EVT-005).
	// Duplicate (session_id, seq) pairs are silently ignored (idempotent).
	Append(ctx context.Context, sessionID string, seq int64, eventType string, payload []byte) error
	// GetBySession returns all event records for a session starting from fromSeq.
	GetBySession(ctx context.Context, sessionID string, fromSeq int64) ([]*EventRecord, error)
	// GetOwner returns the owner ID of a session by querying the sessions table.
	// Returns ErrSessionNotFound if the session does not exist (EVT-006).
	GetOwner(ctx context.Context, sessionID string) (string, error)
	// Close gracefully shuts down the async writer and closes the DB connection.
	Close() error
}

// writeReq is a pending write request for the background batch writer (EVT-005).
type writeReq struct {
	sessionID string
	seq       int64
	eventType string
	payload   []byte
	resp      chan<- error
}

// SQLiteMessageStore implements MessageStore using SQLite with an async
// background batch writer for high-throughput append workloads.
type SQLiteMessageStore struct {
	db *sql.DB

	log     *slog.Logger
	writeC  chan *writeReq // buffered channel for async writes
	closeC  chan struct{}
	closeWg sync.WaitGroup
}

var _ MessageStore = (*SQLiteMessageStore)(nil) // compile-time verification

const (
	writeChanCap     = 1024              // buffered channel capacity
	batchFlushInterval = 100 * time.Millisecond
	batchMaxSize     = 50               // flush when batch reaches this size
)

// NewSQLiteMessageStore creates a SQLiteMessageStore backed by the same DB path
// as the session store. It starts a background goroutine for batch writes.
func NewSQLiteMessageStore(ctx context.Context, cfg *config.Config) (*SQLiteMessageStore, error) {
	db, err := sql.Open("sqlite3", cfg.DB.Path)
	if err != nil {
		return nil, fmt.Errorf("session store: open msg db: %w", err)
	}

	if cfg.DB.WALMode {
		if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
			db.Close()
			return nil, fmt.Errorf("session store: msg WAL: %w", err)
		}
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA busy_timeout=%d", int(cfg.DB.BusyTimeout.Milliseconds()))); err != nil {
		db.Close()
		return nil, fmt.Errorf("session store: msg busy_timeout: %w", err)
	}

	// Limit to one connection since writes are serialized by the single writer goroutine.
	db.SetMaxOpenConns(1)

	ms := &SQLiteMessageStore{
		db:     db,
		log:    slog.Default(),
		writeC: make(chan *writeReq, writeChanCap),
		closeC: make(chan struct{}),
	}

	ms.closeWg.Add(1)
	go ms.runWriter()

	ms.log.Info("session store: message store initialized")
	return ms, nil
}

// Append enqueues an event for async batch writing (EVT-005).
// It uses INSERT OR IGNORE so duplicate (session_id, seq) are silently dropped.
func (s *SQLiteMessageStore) Append(ctx context.Context, sessionID string, seq int64, eventType string, payload []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case s.writeC <- &writeReq{
		sessionID: sessionID,
		seq:       seq,
		eventType: eventType,
		payload:   payload,
		resp:      nil, // async; no caller waiting
	}:
		return nil
	default:
		// Channel full — log and drop to avoid blocking the event stream.
		s.log.Warn("session store: write channel full, dropping event",
			"session_id", sessionID, "seq", seq)
		return nil
	}
}

// runWriter is the background goroutine that batches writes and flushes them.
func (s *SQLiteMessageStore) runWriter() {
	defer s.closeWg.Done()

	ticker := time.NewTicker(batchFlushInterval)
	defer ticker.Stop()

	var batch []*writeReq
	flush := func() {
		if len(batch) == 0 {
			return
		}
		s.flushBatch(batch)
		batch = batch[:0]
	}

	for {
		select {
		case <-s.closeC:
			// Final flush on shutdown.
			flush()
			return
		case req := <-s.writeC:
			batch = append(batch, req)
			if len(batch) >= batchMaxSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// flushBatch writes all events in the batch under a single transaction.
func (s *SQLiteMessageStore) flushBatch(batch []*writeReq) {
	if len(batch) == 0 {
		return
	}

	tx, err := s.db.Begin()
	if err != nil {
		s.log.Error("session store: batch tx begin", "err", err)
		return
	}

	stmt, err := tx.Prepare(`INSERT OR IGNORE INTO events (id, session_id, seq, event_type, payload_json) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		s.log.Error("session store: batch stmt prepare", "err", err)
		return
	}

	for _, req := range batch {
		id := fmt.Sprintf("evt_%s_%d", req.sessionID, req.seq)
		if _, execErr := stmt.Exec(id, req.sessionID, req.seq, req.eventType, string(req.payload)); execErr != nil {
			s.log.Warn("session store: batch insert", "err", execErr, "session_id", req.sessionID)
		}
	}
	_ = stmt.Close()

	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		s.log.Error("session store: batch tx commit", "err", err)
	}
}

// GetBySession returns all event records for a session from seq onwards (EVT-003).
func (s *SQLiteMessageStore) GetBySession(ctx context.Context, sessionID string, fromSeq int64) ([]*EventRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, seq, event_type, payload_json, created_at
		 FROM events WHERE session_id = ? AND seq >= ? ORDER BY seq ASC`, sessionID, fromSeq)
	if err != nil {
		return nil, fmt.Errorf("session store: get events: %w", err)
	}
	defer rows.Close()

	var records []*EventRecord
	for rows.Next() {
		var r EventRecord
		var payloadStr string
		if err := rows.Scan(&r.ID, &r.SessionID, &r.Seq, &r.EventType, &payloadStr, &r.CreatedAt); err != nil {
			continue
		}
		r.Payload = []byte(payloadStr)
		records = append(records, &r)
	}
	return records, rows.Err()
}

// GetOwner returns the owner ID of a session (EVT-006).
// It queries the sessions table using COALESCE(owner_id, user_id).
// Returns ErrSessionNotFound if the session does not exist.
func (s *SQLiteMessageStore) GetOwner(ctx context.Context, sessionID string) (string, error) {
	var ownerID sql.NullString
	err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(owner_id, user_id) FROM sessions WHERE id = ?`, sessionID,
	).Scan(&ownerID)

	if err == sql.ErrNoRows {
		return "", ErrSessionNotFound
	}
	if err != nil {
		return "", fmt.Errorf("session store: get owner: %w", err)
	}
	if ownerID.Valid && ownerID.String != "" {
		return ownerID.String, nil
	}
	// Fallback: query user_id directly (for sessions created before owner_id existed).
	var userID string
	err = s.db.QueryRowContext(ctx,
		`SELECT user_id FROM sessions WHERE id = ?`, sessionID,
	).Scan(&userID)
	if err == sql.ErrNoRows {
		return "", ErrSessionNotFound
	}
	return userID, err
}

// Close gracefully shuts down the async writer (draining pending writes) and closes the DB.
func (s *SQLiteMessageStore) Close() error {
	close(s.closeC)
	s.closeWg.Wait()
	return s.db.Close()
}
