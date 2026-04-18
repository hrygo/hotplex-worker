package session

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/hotplex/hotplex-worker/internal/config"
	"github.com/hotplex/hotplex-worker/pkg/events"
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
	if err := ensureDBDir(cfg.DB.Path); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", cfg.DB.Path)
	if err != nil {
		return nil, fmt.Errorf("session store: open db: %w", err)
	}

	if cfg.DB.WALMode {
		if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("session store: enable WAL: %w", err)
		}
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA busy_timeout=%d", int(cfg.DB.BusyTimeout.Milliseconds()))); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("session store: set busy_timeout: %w", err)
	}

	store := &SQLiteStore{db: db}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
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

-- EVT-008: audit_log table with hash chain for tamper-evident audit trail.
CREATE TABLE IF NOT EXISTS audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp INTEGER NOT NULL,
    action TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    details TEXT,
    previous_hash TEXT NOT NULL,
    current_hash TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_session_id ON audit_log(session_id);
CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_log(timestamp);
`
	_, err := s.db.ExecContext(ctx, schema)
	if err != nil {
		return fmt.Errorf("session store: migrate: %w", err)
	}

	// Migrate: add owner_id column if it doesn't exist (no-op on fresh installs).
	// The column is nullable so existing rows remain valid; application code
	// falls back to user_id when owner_id IS NULL.
	_, _ = s.db.ExecContext(ctx, "ALTER TABLE sessions ADD COLUMN owner_id TEXT")
	// Migrate: add bot_id column for SEC-007 multi-bot isolation.
	_, _ = s.db.ExecContext(ctx, "ALTER TABLE sessions ADD COLUMN bot_id TEXT")

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
		`SELECT id, user_id, COALESCE(owner_id, user_id), worker_session_id, worker_type, state, bot_id, created_at, updated_at, expires_at, idle_expires_at, context_json
		 FROM sessions WHERE id = ?`, id,
	).Scan(&info.ID, &info.UserID, &info.OwnerID, &info.WorkerSessionID, &info.WorkerType, &info.State, &info.BotID,
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
		`SELECT id, user_id, COALESCE(owner_id, user_id), worker_session_id, worker_type, state, bot_id, created_at, updated_at, expires_at, idle_expires_at, context_json
		 FROM sessions ORDER BY created_at DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("session store: list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var sessions []*SessionInfo
	for rows.Next() {
		var si SessionInfo
		var ctxJSON sql.NullString
		var expiresAt, idleExpiresAt sql.NullTime
		var createdAt, updatedAt time.Time

		err := rows.Scan(&si.ID, &si.UserID, &si.OwnerID, &si.WorkerSessionID, &si.WorkerType, &si.State,
			&si.BotID, &createdAt, &updatedAt, &expiresAt, &idleExpiresAt, &ctxJSON)
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
	defer func() { _ = rows.Close() }()

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
	defer func() { _ = rows.Close() }()

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

func (s *SQLiteStore) AppendAudit(ctx context.Context, action, actorID, sessionID string, details map[string]any) error {
	detailsStr := ""
	if details != nil {
		detailsJSON, err := json.Marshal(details)
		if err != nil {
			return fmt.Errorf("session store: marshal audit details: %w", err)
		}
		detailsStr = string(detailsJSON)
	}

	var previousHash string
	var lastID int64
	row := s.db.QueryRowContext(ctx, "SELECT id, current_hash FROM audit_log ORDER BY id DESC LIMIT 1")
	if err := row.Scan(&lastID, &previousHash); err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("session store: get last audit entry: %w", err)
	}
	if lastID == 0 {
		previousHash = ""
	}

	timestamp := time.Now().UnixMilli()

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log (timestamp, action, actor_id, session_id, details, previous_hash, current_hash)
		 VALUES (?, ?, ?, ?, ?, ?, '')`,
		timestamp, action, actorID, sessionID, detailsStr, previousHash,
	)
	if err != nil {
		return fmt.Errorf("session store: insert audit: %w", err)
	}

	var id int64
	_ = s.db.QueryRowContext(ctx, "SELECT last_insert_rowid()").Scan(&id)

	data := fmt.Sprintf("%d%d%s%s%s%s%s", id, timestamp, action, actorID, sessionID, detailsStr, previousHash)
	hash := sha256.Sum256([]byte(data))
	currentHash := hex.EncodeToString(hash[:])

	_, err = s.db.ExecContext(ctx, "UPDATE audit_log SET current_hash=? WHERE id=?", currentHash, id)
	if err != nil {
		return fmt.Errorf("session store: update audit hash: %w", err)
	}

	return nil
}

type AuditRecord struct {
	ID           int64
	Timestamp    int64
	Action       string
	ActorID      string
	SessionID    string
	Details      map[string]any
	DetailsStr   string
	PreviousHash string
	CurrentHash  string
}

func (s *SQLiteStore) GetAuditTrail(ctx context.Context, sessionID string) ([]*AuditRecord, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, timestamp, action, actor_id, session_id, details, previous_hash, current_hash
		 FROM audit_log WHERE session_id=? ORDER BY id ASC`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("session store: get audit trail: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var records []*AuditRecord
	for rows.Next() {
		var r AuditRecord
		var detailsJSON sql.NullString
		var previousHash, currentHash string
		if err := rows.Scan(&r.ID, &r.Timestamp, &r.Action, &r.ActorID, &r.SessionID, &detailsJSON, &previousHash, &currentHash); err != nil {
			continue
		}
		r.PreviousHash = previousHash
		r.CurrentHash = currentHash
		if detailsJSON.Valid && detailsJSON.String != "" {
			r.DetailsStr = detailsJSON.String
			if err := json.Unmarshal([]byte(detailsJSON.String), &r.Details); err != nil {
				r.Details = nil
			}
		}
		records = append(records, &r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("session store: audit trail scan: %w", err)
	}

	for i, r := range records {
		data := fmt.Sprintf("%d%d%s%s%s%s%s", r.ID, r.Timestamp, r.Action, r.ActorID, r.SessionID, r.DetailsStr, r.PreviousHash)
		hash := sha256.Sum256([]byte(data))
		expectedHash := hex.EncodeToString(hash[:])
		if r.CurrentHash != expectedHash {
			slog.Error("session store: audit chain broken", "id", r.ID, "session_id", sessionID)
			return records[:i+1], ErrAuditChainInvalid
		}
	}

	return records, nil
}
