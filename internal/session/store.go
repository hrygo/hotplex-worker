package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_sessions_idle_expires_at ON sessions(idle_expires_at);
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
		`INSERT INTO sessions (id, user_id, worker_type, state, created_at, updated_at, expires_at, idle_expires_at, is_active, context_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   state=excluded.state,
		   updated_at=excluded.updated_at,
		   expires_at=excluded.expires_at,
		   idle_expires_at=excluded.idle_expires_at,
		   is_active=excluded.is_active,
		   context_json=excluded.context_json`,
		info.ID, info.UserID, info.WorkerType, string(info.State),
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
		`SELECT id, user_id, worker_type, state, created_at, updated_at, expires_at, idle_expires_at, context_json
		 FROM sessions WHERE id = ?`, id,
	).Scan(&info.ID, &info.UserID, &info.WorkerType, &info.State,
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
		`SELECT id, user_id, worker_type, state, created_at, updated_at, expires_at, idle_expires_at, context_json
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

		err := rows.Scan(&si.ID, &si.UserID, &si.WorkerType, &si.State,
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
