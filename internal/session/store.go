package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/pkg/events"
)

// Store defines the interface for session persistence.
type Store interface {
	Upsert(ctx context.Context, info *SessionInfo) error
	Get(ctx context.Context, id string) (*SessionInfo, error)
	List(ctx context.Context, userID, platform string, limit, offset int) ([]*SessionInfo, error)
	GetExpiredMaxLifetime(ctx context.Context, now time.Time) ([]string, error)
	GetExpiredIdle(ctx context.Context, now time.Time) ([]string, error)
	DeleteTerminated(ctx context.Context, cutoff time.Time) error
	DeletePhysical(ctx context.Context, id string) error
	Compact(ctx context.Context, threshold float64) error
	GetSessionsByState(ctx context.Context, state events.SessionState) ([]string, error)
	Close() error
}

// SQLiteStore implements Store using SQLite.
type SQLiteStore struct {
	db  *sql.DB
	log *slog.Logger
}

// NewSQLiteStore creates and initializes a new SQLiteStore.
func NewSQLiteStore(ctx context.Context, cfg *config.Config) (*SQLiteStore, error) {
	db, err := openSQLiteDB(cfg, dbOpenOpts{
		Label:       "session",
		MaxOpen:     cfg.DB.MaxOpenConns,
		MaxIdle:     cfg.DB.MaxOpenConns,
		MaxLifetime: 0,
		MaxIdleTime: 5 * time.Minute,
	})
	if err != nil {
		return nil, err
	}

	if err := runMigrations(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &SQLiteStore{db: db, log: slog.Default().With("component", "session_store")}, nil
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

	var platformKeyJSON []byte
	if info.PlatformKey != nil {
		var err2 error
		platformKeyJSON, err2 = json.Marshal(info.PlatformKey)
		if err2 != nil {
			return fmt.Errorf("session store: marshal platform key: %w", err2)
		}
	}

	_, err := s.db.ExecContext(ctx, queries["sessions.upsert_session"],
		info.ID, info.UserID, info.OwnerID, info.BotID, info.WorkerSessionID, info.WorkerType, string(info.State),
		info.Platform, string(platformKeyJSON), info.WorkDir, info.Title,
		info.CreatedAt, info.UpdatedAt, info.ExpiresAt, info.IdleExpiresAt,
		isActive, string(ctxJSON),
	)
	return err
}

type rowScanner interface{ Scan(dest ...any) error }

func scanSession(sc rowScanner) (*SessionInfo, error) {
	var info SessionInfo
	var ctxJSON, platformKeyStr sql.NullString
	var expiresAt, idleExpiresAt sql.NullTime
	var createdAt, updatedAt time.Time

	err := sc.Scan(
		&info.ID, &info.UserID, &info.OwnerID, &info.WorkerSessionID, &info.WorkerType, &info.State, &info.BotID,
		&info.Platform, &platformKeyStr, &info.WorkDir, &info.Title,
		&createdAt, &updatedAt, &expiresAt, &idleExpiresAt, &ctxJSON,
	)
	if err != nil {
		return nil, err
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
	if platformKeyStr.Valid && platformKeyStr.String != "" {
		_ = json.Unmarshal([]byte(platformKeyStr.String), &info.PlatformKey)
	}
	return &info, nil
}

func (s *SQLiteStore) Get(ctx context.Context, id string) (*SessionInfo, error) {
	info, err := scanSession(s.db.QueryRowContext(ctx, queries["store.get_session"], id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("session store: load: %w", err)
	}
	return info, nil
}

func (s *SQLiteStore) List(ctx context.Context, userID, platform string, limit, offset int) ([]*SessionInfo, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, queries["store.list_sessions"], userID, userID, platform, platform, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("session store: list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var sessions []*SessionInfo
	for rows.Next() {
		si, err := scanSession(rows)
		if err != nil {
			continue
		}
		sessions = append(sessions, si)
	}
	return sessions, rows.Err()
}

func (s *SQLiteStore) GetExpiredMaxLifetime(ctx context.Context, now time.Time) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, queries["store.get_expired_max_lifetime"],
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
	rows, err := s.db.QueryContext(ctx, queries["store.get_expired_idle"], events.StateIdle, now)
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("session store: delete terminated begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Collect session IDs to cascade-delete their events and audit entries.
	rows, err := tx.QueryContext(ctx, "SELECT id FROM sessions WHERE state = ? AND updated_at <= ?",
		events.StateTerminated, cutoff)
	if err != nil {
		return fmt.Errorf("session store: delete terminated query: %w", err)
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	_ = rows.Close()

	for _, id := range ids {
		_, _ = tx.ExecContext(ctx, queries["store.delete_audit_by_session"], id)
		_, _ = tx.ExecContext(ctx, queries["conversation.delete_by_session"], id)
	}

	_, err = tx.ExecContext(ctx, queries["store.delete_terminated"], events.StateTerminated, cutoff)
	if err != nil {
		return fmt.Errorf("session store: delete terminated exec: %w", err)
	}
	return tx.Commit()
}

func (s *SQLiteStore) DeletePhysical(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("session store: delete physical begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, queries["store.delete_audit_by_session"], id); err != nil {
		return fmt.Errorf("session store: delete physical audit: %w", err)
	}
	if _, err := tx.ExecContext(ctx, queries["conversation.delete_by_session"], id); err != nil {
		return fmt.Errorf("session store: delete physical conversations: %w", err)
	}
	if _, err := tx.ExecContext(ctx, queries["store.delete_physical"], id); err != nil {
		return fmt.Errorf("session store: delete physical session: %w", err)
	}
	return tx.Commit()
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) Compact(ctx context.Context, threshold float64) error {
	var pageCount, freeCount int
	if err := s.db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount); err != nil {
		return fmt.Errorf("session store: compact page_count: %w", err)
	}
	if err := s.db.QueryRowContext(ctx, "PRAGMA freelist_count").Scan(&freeCount); err != nil {
		return fmt.Errorf("session store: compact freelist_count: %w", err)
	}
	if pageCount == 0 || float64(freeCount)/float64(pageCount) < threshold {
		return nil
	}
	s.log.Info("session store: VACUUM starting",
		"page_count", pageCount, "free_count", freeCount,
		"ratio", fmt.Sprintf("%.1f%%", float64(freeCount)/float64(pageCount)*100))
	if _, err := s.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return fmt.Errorf("session store: compact checkpoint: %w", err)
	}
	_, err := s.db.ExecContext(ctx, "VACUUM")
	return err
}

func (s *SQLiteStore) GetSessionsByState(ctx context.Context, state events.SessionState) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, queries["store.get_sessions_by_state"], string(state))
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

