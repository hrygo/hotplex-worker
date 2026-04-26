package session

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/pkg/events"
)

// ErrAuditChainInvalid is returned when the audit log hash chain is broken.
var ErrAuditChainInvalid = errors.New("session store: audit chain invalid")

// Store defines the interface for session persistence.
type Store interface {
	Upsert(ctx context.Context, info *SessionInfo) error
	Get(ctx context.Context, id string) (*SessionInfo, error)
	List(ctx context.Context, userID, platform string, limit, offset int) ([]*SessionInfo, error)
	GetExpiredMaxLifetime(ctx context.Context, now time.Time) ([]string, error)
	GetExpiredIdle(ctx context.Context, now time.Time) ([]string, error)
	DeleteTerminated(ctx context.Context, cutoff time.Time) error
	DeletePhysical(ctx context.Context, id string) error
	DeleteExpiredEvents(ctx context.Context, cutoff time.Time) (int64, error)
	Compact(ctx context.Context, threshold float64) error
	GetSessionsByState(ctx context.Context, state events.SessionState) ([]string, error)
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

	if err := initSQLiteDB(db, cfg, "enable"); err != nil {
		_ = db.Close()
		return nil, err
	}

	// Session store is read-heavy: allow 2 concurrent connections.
	db.SetMaxOpenConns(cfg.DB.MaxOpenConns)
	db.SetMaxIdleConns(cfg.DB.MaxOpenConns)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(5 * time.Minute)

	if err := runMigrations(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &SQLiteStore{db: db}, nil
}

// initSQLiteDB configures a SQLite connection with standard PRAGMAs.
func initSQLiteDB(db *sql.DB, cfg *config.Config, label string) error {
	if cfg.DB.WALMode {
		if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
			return fmt.Errorf("session store: %s WAL: %w", label, err)
		}
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA busy_timeout=%d", int(cfg.DB.BusyTimeout.Milliseconds()))); err != nil {
		return fmt.Errorf("session store: %s busy_timeout: %w", label, err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return fmt.Errorf("session store: %s foreign_keys: %w", label, err)
	}
	if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		return fmt.Errorf("session store: %s synchronous: %w", label, err)
	}
	if _, err := db.Exec("PRAGMA cache_size=-32000"); err != nil {
		return fmt.Errorf("session store: %s cache_size: %w", label, err)
	}
	if _, err := db.Exec("PRAGMA temp_store=MEMORY"); err != nil {
		return fmt.Errorf("session store: %s temp_store: %w", label, err)
	}
	if _, err := db.Exec("PRAGMA mmap_size=268435456"); err != nil {
		return fmt.Errorf("session store: %s mmap_size: %w", label, err)
	}
	if _, err := db.Exec("PRAGMA wal_autocheckpoint=5000"); err != nil {
		return fmt.Errorf("session store: %s wal_autocheckpoint: %w", label, err)
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
		_, _ = tx.ExecContext(ctx, queries["events.delete_by_session"], id)
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
	// Cascade-delete child rows before the parent session.
	_, _ = s.db.ExecContext(ctx, queries["events.delete_by_session"], id)
	_, _ = s.db.ExecContext(ctx, queries["store.delete_audit_by_session"], id)
	_, _ = s.db.ExecContext(ctx, queries["conversation.delete_by_session"], id)
	_, err := s.db.ExecContext(ctx, queries["store.delete_physical"], id)
	return err
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func (s *SQLiteStore) DeleteExpiredEvents(ctx context.Context, cutoff time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, queries["events.delete_expired"], cutoff)
	if err != nil {
		return 0, fmt.Errorf("session store: delete expired events: %w", err)
	}
	n, _ := result.RowsAffected()
	// Also purge expired conversation records (best-effort).
	if q, ok := queries["conversation.delete_expired"]; ok {
		if cn, cerr := s.db.ExecContext(ctx, q, cutoff); cerr == nil {
			if rows, _ := cn.RowsAffected(); rows > 0 {
				n += rows
			}
		}
	}
	return n, nil
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
	slog.Info("session store: VACUUM starting",
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
	row := s.db.QueryRowContext(ctx, queries["store.get_last_audit"])
	if err := row.Scan(&lastID, &previousHash); err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("session store: get last audit entry: %w", err)
	}
	if lastID == 0 {
		previousHash = ""
	}

	timestamp := time.Now().UnixMilli()

	res, err := s.db.ExecContext(ctx, queries["store.append_audit"],
		timestamp, action, actorID, sessionID, detailsStr, previousHash)
	if err != nil {
		return fmt.Errorf("session store: insert audit: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("session store: get last insert id: %w", err)
	}

	data := fmt.Sprintf("%d%d%s%s%s%s%s", id, timestamp, action, actorID, sessionID, detailsStr, previousHash)
	hash := sha256.Sum256([]byte(data))
	currentHash := hex.EncodeToString(hash[:])

	_, err = s.db.ExecContext(ctx, queries["store.update_audit_hash"], currentHash, id)
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
	rows, err := s.db.QueryContext(ctx, queries["store.get_audit_trail"], sessionID)
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
