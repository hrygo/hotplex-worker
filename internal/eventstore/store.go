package eventstore

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed sql/queries/*.sql sql/schema.sql
var sqlFS embed.FS

var queries = loadQueries()

func loadQueries() map[string]string {
	entries, err := fs.ReadDir(sqlFS, "sql/queries")
	if err != nil {
		panic("eventstore: read sql fs: " + err.Error())
	}
	m := make(map[string]string)
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		data, err := sqlFS.ReadFile("sql/queries/" + name)
		if err != nil {
			panic("eventstore: read sql file " + name + ": " + err.Error())
		}
		key := strings.TrimSuffix(name, ".sql")
		// Strip "events." prefix from key: "events.insert.sql" → "insert"
		key = strings.TrimPrefix(key, "events.")
		text := strings.TrimSpace(stripComments(string(data)))
		if text != "" {
			m[key] = text
		}
	}
	return m
}

func stripComments(s string) string {
	var b strings.Builder
	for _, line := range strings.SplitAfter(s, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			b.WriteString(line)
		}
	}
	return b.String()
}

// CursorDirection controls pagination direction relative to a cursor seq value.
type CursorDirection int

const (
	// CursorLatest fetches the most recent N events (no cursor needed).
	CursorLatest CursorDirection = iota
	// CursorAfter fetches events with seq > cursor (newer, for incremental catch-up).
	CursorAfter
	// CursorBefore fetches events with seq < cursor (older, for loading history).
	CursorBefore
)

var ErrNotFound = errors.New("eventstore: no events found")

// StoredEvent represents a single persisted AEP event.
type StoredEvent struct {
	SessionID string          `json:"session_id"`
	Seq       int64           `json:"seq"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	Direction string          `json:"direction"`
	CreatedAt int64           `json:"created_at"`
}

// EventPage is a page of events with pagination metadata.
type EventPage struct {
	Events    []*StoredEvent `json:"events"`
	OldestSeq int64          `json:"oldest_seq"`
	NewestSeq int64          `json:"newest_seq"`
	HasOlder  bool           `json:"has_older"`
}

// EventStore defines the interface for AEP event persistence.
type EventStore interface {
	// Append adds a single event (used internally by the collector's batch writer).
	Append(ctx context.Context, event *StoredEvent) error

	// BeginTx starts a transaction for batch writes.
	BeginTx(ctx context.Context) (EventTx, error)

	// QueryBySession fetches events with cursor-based bidirectional pagination.
	//   dir=CursorLatest, cursor=0  → latest N events (initial load)
	//   dir=CursorAfter,  cursor=X  → events with seq > X (catch-up)
	//   dir=CursorBefore, cursor=X  → events with seq < X (load older)
	// Returns events always in seq ASC order.
	QueryBySession(ctx context.Context, sessionID string, cursor int64, dir CursorDirection, limit int) (*EventPage, error)

	// DeleteBySession removes all events for a session.
	DeleteBySession(ctx context.Context, sessionID string) error

	// DeleteExpired removes events older than the cutoff.
	DeleteExpired(ctx context.Context, cutoff time.Time) (int64, error)

	// Close flushes pending writes and closes the database.
	Close() error
}

// EventTx is a transaction handle for batch event writes.
type EventTx interface {
	Append(ctx context.Context, event *StoredEvent) error
	Commit() error
}

// SQLiteStore implements EventStore using an independent SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

var _ EventStore = (*SQLiteStore)(nil)

// NewSQLiteStore creates a new event store backed by an independent SQLite file.
func NewSQLiteStore(ctx context.Context, dbPath string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("eventstore: create dir: %w", err)
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("eventstore: open db: %w", err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("eventstore: enable WAL: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("eventstore: set busy_timeout: %w", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("eventstore: enable foreign_keys: %w", err)
	}

	for _, p := range []string{
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=-2000",
		"PRAGMA temp_store=MEMORY",
		"PRAGMA wal_autocheckpoint=1000",
	} {
		if _, err := db.Exec(p); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("eventstore: pragma %s: %w", p, err)
		}
	}

	schema, err := sqlFS.ReadFile("sql/schema.sql")
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("eventstore: read schema: %w", err)
	}
	if _, err := db.Exec(string(schema)); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("eventstore: apply schema: %w", err)
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(5 * time.Minute)

	return &SQLiteStore{db: db}, nil
}

func (s *SQLiteStore) Append(ctx context.Context, event *StoredEvent) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}
	_, err := s.db.ExecContext(ctx, queries["insert"],
		event.SessionID, event.Seq, event.Type, event.Data, event.Direction, event.CreatedAt)
	if err != nil {
		return fmt.Errorf("eventstore: append: %w", err)
	}
	return nil
}

func (s *SQLiteStore) BeginTx(ctx context.Context) (EventTx, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("eventstore: begin tx: %w", err)
	}
	return &sqliteTx{tx: tx}, nil
}

type sqliteTx struct {
	tx *sql.Tx
}

func (t *sqliteTx) Append(ctx context.Context, event *StoredEvent) error {
	_, err := t.tx.ExecContext(ctx, queries["insert"],
		event.SessionID, event.Seq, event.Type, event.Data, event.Direction, event.CreatedAt)
	if err != nil {
		return fmt.Errorf("eventstore: tx append: %w", err)
	}
	return nil
}

func (t *sqliteTx) Commit() error {
	return t.tx.Commit()
}

func (s *SQLiteStore) QueryBySession(ctx context.Context, sessionID string, cursor int64, dir CursorDirection, limit int) (*EventPage, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}

	// Fetch one extra to detect has_more.
	fetchLimit := limit + 1

	var rows *sql.Rows
	var err error

	switch dir {
	case CursorAfter:
		rows, err = s.db.QueryContext(ctx, queries["query_after"], sessionID, cursor, fetchLimit)
	case CursorBefore:
		rows, err = s.db.QueryContext(ctx, queries["query_before"], sessionID, cursor, fetchLimit)
	default: // CursorLatest
		rows, err = s.db.QueryContext(ctx, queries["query_latest"], sessionID, fetchLimit)
	}
	if err != nil {
		return nil, fmt.Errorf("eventstore: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	events, err := scanEvents(rows)
	if err != nil {
		return nil, err
	}

	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}

	// For DESC queries (CursorLatest, CursorBefore), reverse to ASC order.
	if dir == CursorLatest || dir == CursorBefore {
		for i, j := 0, len(events)-1; i < j; i, j = i+1, j-1 {
			events[i], events[j] = events[j], events[i]
		}
	}

	page := &EventPage{
		Events: events,
	}

	if len(events) > 0 {
		page.OldestSeq = events[0].Seq
		page.NewestSeq = events[len(events)-1].Seq
	}

	if len(events) > 0 {
		switch dir {
		case CursorLatest, CursorBefore:
			page.HasOlder = hasMore
		default:
			var exists int
			err := s.db.QueryRowContext(ctx, queries["has_older"], sessionID, page.OldestSeq).Scan(&exists)
			page.HasOlder = err == nil && exists == 1
		}
	}

	return page, nil
}

func (s *SQLiteStore) DeleteBySession(ctx context.Context, sessionID string) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}
	_, err := s.db.ExecContext(ctx, queries["delete_by_session"], sessionID)
	if err != nil {
		return fmt.Errorf("eventstore: delete by session: %w", err)
	}
	return nil
}

func (s *SQLiteStore) DeleteExpired(ctx context.Context, cutoff time.Time) (int64, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}
	res, err := s.db.ExecContext(ctx, queries["delete_expired"], cutoff.UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("eventstore: delete expired: %w", err)
	}
	return res.RowsAffected()
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

func scanEvents(rows *sql.Rows) ([]*StoredEvent, error) {
	var events []*StoredEvent
	for rows.Next() {
		var e StoredEvent
		if err := rows.Scan(&e.SessionID, &e.Seq, &e.Type, &e.Data, &e.Direction, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("eventstore: scan: %w", err)
		}
		events = append(events, &e)
	}
	if len(events) == 0 {
		return nil, ErrNotFound
	}
	return events, rows.Err()
}
