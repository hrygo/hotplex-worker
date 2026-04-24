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
	"strings"
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
	// Execute schema DDL from embedded file (SQLite supports multi-statement exec).
	schema, err := sqlFS.ReadFile("sql/sessions.schema.sql")
	if err != nil {
		return fmt.Errorf("session store: read schema: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, string(schema)); err != nil {
		return fmt.Errorf("session store: apply schema: %w", err)
	}

	// Execute migration ALTER TABLE statements from embedded file.
	// Errors are ignored since ALTER COLUMN is idempotent on fresh installs
	// and silently no-op when columns already exist.
	migrationSQL, err := sqlFS.ReadFile("sql/sessions.migrations.sql")
	if err != nil {
		return fmt.Errorf("session store: read migrations: %w", err)
	}
	_, _ = s.db.ExecContext(ctx, strings.TrimSpace(stripSQLComments(string(migrationSQL))))

	return nil
}

// stripSQLComments removes single-line (--) SQL comments from text.
func stripSQLComments(sql string) string {
	var result strings.Builder
	for _, line := range strings.SplitAfter(sql, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "--") {
			result.WriteString(line)
		}
	}
	return result.String()
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
		info.Platform, string(platformKeyJSON),
		info.CreatedAt, info.UpdatedAt, info.ExpiresAt, info.IdleExpiresAt,
		isActive, string(ctxJSON),
	)
	return err
}

func (s *SQLiteStore) Get(ctx context.Context, id string) (*SessionInfo, error) {
	var info SessionInfo
	var ctxJSON, platformKeyStr sql.NullString
	var expiresAt, idleExpiresAt sql.NullTime
	var createdAt, updatedAt time.Time

	err := s.db.QueryRowContext(ctx, queries["store.get_session"], id).Scan(
		&info.ID, &info.UserID, &info.OwnerID, &info.WorkerSessionID, &info.WorkerType, &info.State, &info.BotID,
		&info.Platform, &platformKeyStr,
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
	if platformKeyStr.Valid && platformKeyStr.String != "" {
		_ = json.Unmarshal([]byte(platformKeyStr.String), &info.PlatformKey)
	}

	return &info, nil
}

func (s *SQLiteStore) List(ctx context.Context, limit, offset int) ([]*SessionInfo, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, queries["store.list_sessions"], limit, offset)
	if err != nil {
		return nil, fmt.Errorf("session store: list: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var sessions []*SessionInfo
	for rows.Next() {
		var si SessionInfo
		var ctxJSON, platformKeyStr sql.NullString
		var expiresAt, idleExpiresAt sql.NullTime
		var createdAt, updatedAt time.Time

		err := rows.Scan(&si.ID, &si.UserID, &si.OwnerID, &si.WorkerSessionID, &si.WorkerType, &si.State,
			&si.BotID, &si.Platform, &platformKeyStr, &createdAt, &updatedAt, &expiresAt, &idleExpiresAt, &ctxJSON)
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
		if platformKeyStr.Valid && platformKeyStr.String != "" {
			_ = json.Unmarshal([]byte(platformKeyStr.String), &si.PlatformKey)
		}
		sessions = append(sessions, &si)
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
	_, err := s.db.ExecContext(ctx, queries["store.delete_terminated"], events.StateTerminated, cutoff)
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
