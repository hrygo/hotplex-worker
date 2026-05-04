package eventstore

import (
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	"github.com/hrygo/hotplex/internal/sqlutil"
)

//go:embed sql/queries/*.sql
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
		data, err := fs.ReadFile(sqlFS, "sql/queries/"+name)
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

// Source constants for event provenance tracking.
const (
	SourceNormal     = "normal"
	SourceCrash      = "crash"
	SourceTimeout    = "timeout"
	SourceFreshStart = "fresh_start"
)

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

const defaultTimeout = 5 * time.Second

// withDefaultTimeout wraps ctx with a 5s timeout if it has no deadline.
func withDefaultTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, defaultTimeout)
}

// TurnRecord represents a single conversational turn derived from events VIEW.
type TurnRecord struct {
	SessionID  string  `json:"session_id"`
	Seq        int64   `json:"seq"`
	Role       string  `json:"role"`
	Content    string  `json:"content"`
	Platform   string  `json:"platform"`
	UserID     string  `json:"user_id"`
	Model      string  `json:"model"`
	Success    *bool   `json:"success"`
	Source     string  `json:"source"`
	ToolCount  int     `json:"tool_call_count"`
	TokensIn   int     `json:"tokens_in"`
	TokensOut  int     `json:"tokens_out"`
	DurationMs int64   `json:"duration_ms"`
	CostUSD    float64 `json:"cost_usd"`
	CreatedAt  int64   `json:"created_at"`
}

// TurnStats holds aggregated statistics across all assistant turns of a session.
type TurnStats struct {
	SessionID    string         `json:"session_id"`
	TotalTurns   int            `json:"total_turns"`
	SuccessTurns int            `json:"success_turns"`
	FailedTurns  int            `json:"failed_turns"`
	TotalDurMs   int64          `json:"total_duration_ms"`
	TotalCostUSD float64        `json:"total_cost_usd"`
	TotalTokIn   int64          `json:"total_tokens_in"`
	TotalTokOut  int64          `json:"total_tokens_out"`
	Turns        []TurnStatItem `json:"turns"`
}

// TurnStatItem holds per-turn statistics.
type TurnStatItem struct {
	Seq        int64   `json:"seq"`
	Success    bool    `json:"success"`
	DurationMs int64   `json:"duration_ms"`
	CostUSD    float64 `json:"cost_usd"`
	TokensIn   int64   `json:"tokens_in"`
	TokensOut  int64   `json:"tokens_out"`
	Model      string  `json:"model"`
	Source     string  `json:"source"`
	CreatedAt  int64   `json:"created_at"`
}

// StoredEvent represents a single persisted AEP event.
type StoredEvent struct {
	SessionID string          `json:"session_id"`
	Seq       int64           `json:"seq"`
	Type      string          `json:"type"`
	Data      json.RawMessage `json:"data"`
	Direction string          `json:"direction"`
	Source    string          `json:"source"`
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

	// Close flushes pending writes and closes the database (if owned).
	Close() error
}

// EventTx is a transaction handle for batch event writes.
type EventTx interface {
	Append(ctx context.Context, event *StoredEvent) error
	Commit() error
}

// SQLiteStore implements EventStore using a shared SQLite database connection.
type SQLiteStore struct {
	db     *sql.DB
	ownsDB bool // true only when opened independently (tests); false when sharing session store DB.
}

var _ EventStore = (*SQLiteStore)(nil)

// NewSQLiteStore creates an event store using a shared *sql.DB.
// The schema is managed by the session store goose migrations (002_events_table.sql).
func NewSQLiteStore(db *sql.DB) *SQLiteStore {
	return &SQLiteStore{db: db, ownsDB: false}
}

// NewIndependentStore opens its own DB for testing.
func NewIndependentStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open(sqlutil.DriverName, dbPath)
	if err != nil {
		return nil, fmt.Errorf("eventstore: open: %w", err)
	}
	// Apply same pragmas as production for test fidelity.
	_, _ = db.Exec("PRAGMA journal_mode=WAL")
	_, _ = db.Exec("PRAGMA busy_timeout=5000")
	return &SQLiteStore{db: db, ownsDB: true}, nil
}

func (s *SQLiteStore) Append(ctx context.Context, event *StoredEvent) error {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	_, err := s.db.ExecContext(ctx, queries["insert"],
		event.SessionID, event.Seq, event.Type, event.Data, event.Direction, event.Source, event.CreatedAt)
	if err != nil {
		return fmt.Errorf("eventstore: append: %w", err)
	}
	return nil
}

func (s *SQLiteStore) BeginTx(ctx context.Context) (EventTx, error) {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
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
		event.SessionID, event.Seq, event.Type, event.Data, event.Direction, event.Source, event.CreatedAt)
	if err != nil {
		return fmt.Errorf("eventstore: tx append: %w", err)
	}
	return nil
}

func (t *sqliteTx) Commit() error {
	return t.tx.Commit()
}

func (s *SQLiteStore) QueryBySession(ctx context.Context, sessionID string, cursor int64, dir CursorDirection, limit int) (*EventPage, error) {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
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
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	_, err := s.db.ExecContext(ctx, queries["delete_by_session"], sessionID)
	if err != nil {
		return fmt.Errorf("eventstore: delete by session: %w", err)
	}
	return nil
}

func (s *SQLiteStore) DeleteExpired(ctx context.Context, cutoff time.Time) (int64, error) {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	res, err := s.db.ExecContext(ctx, queries["delete_expired"], cutoff.UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("eventstore: delete expired: %w", err)
	}
	return res.RowsAffected()
}

func (s *SQLiteStore) Close() error {
	if s.ownsDB {
		return s.db.Close()
	}
	return nil
}

// QueryTurns fetches conversation turns via v_turns view with limit/offset pagination.
func (s *SQLiteStore) QueryTurns(ctx context.Context, sessionID string, limit, offset int) ([]*TurnRecord, error) {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, queries["turns.query"], sessionID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("eventstore: query turns: %w", err)
	}
	defer func() { _ = rows.Close() }()
	return scanTurns(rows)
}

// QueryTurnsBefore fetches turns with seq < beforeSeq (cursor-based, DESC in SQL, reversed to ASC).
func (s *SQLiteStore) QueryTurnsBefore(ctx context.Context, sessionID string, beforeSeq int64, limit int) ([]*TurnRecord, error) {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, queries["turns.query_before"], sessionID, beforeSeq, limit)
	if err != nil {
		return nil, fmt.Errorf("eventstore: query turns before: %w", err)
	}
	defer func() { _ = rows.Close() }()
	records, err := scanTurns(rows)
	if err != nil {
		return nil, err
	}
	// Reverse to ASC order (SQL returns DESC).
	for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
		records[i], records[j] = records[j], records[i]
	}
	return records, nil
}

// QueryTurnStats returns aggregated turn statistics for a session via v_turns_assistant.
func (s *SQLiteStore) QueryTurnStats(ctx context.Context, sessionID string) (*TurnStats, error) {
	ctx, cancel := withDefaultTimeout(ctx)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, queries["turns.stats"], sessionID)
	if err != nil {
		return nil, fmt.Errorf("eventstore: query turn stats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	stats := &TurnStats{SessionID: sessionID}
	for rows.Next() {
		var ts TurnStatItem
		var success sql.NullInt64
		var role, toolsJSON sql.NullString
		var toolCount sql.NullInt64
		if err := rows.Scan(new(string), &ts.Seq, &role, &success, &ts.Source,
			&toolsJSON, &toolCount, &ts.TokensIn, &ts.TokensOut,
			&ts.DurationMs, &ts.CostUSD, &ts.Model, &ts.CreatedAt); err != nil {
			continue
		}
		ts.Success = success.Valid && success.Int64 == 1
		stats.TotalTurns++
		if ts.Success {
			stats.SuccessTurns++
		} else {
			stats.FailedTurns++
		}
		stats.TotalDurMs += ts.DurationMs
		stats.TotalCostUSD += ts.CostUSD
		stats.TotalTokIn += ts.TokensIn
		stats.TotalTokOut += ts.TokensOut
		stats.Turns = append(stats.Turns, ts)
	}
	if stats.TotalTurns == 0 {
		return nil, ErrNotFound
	}
	return stats, rows.Err()
}

func scanEvents(rows *sql.Rows) ([]*StoredEvent, error) {
	var events []*StoredEvent
	for rows.Next() {
		var e StoredEvent
		if err := rows.Scan(&e.SessionID, &e.Seq, &e.Type, &e.Data, &e.Direction, &e.Source, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("eventstore: scan: %w", err)
		}
		events = append(events, &e)
	}
	if len(events) == 0 {
		return nil, ErrNotFound
	}
	return events, rows.Err()
}

func scanTurns(rows *sql.Rows) ([]*TurnRecord, error) {
	var records []*TurnRecord
	for rows.Next() {
		var r TurnRecord
		var success sql.NullInt64
		var toolsJSON sql.NullString // consumed from VIEW but not exposed
		if err := rows.Scan(&r.SessionID, &r.Seq, &r.Role, &r.Content,
			&r.Platform, &r.UserID, &r.Model, &success, &r.Source,
			&toolsJSON, &r.ToolCount, &r.TokensIn, &r.TokensOut,
			&r.DurationMs, &r.CostUSD, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("eventstore: scan turn: %w", err)
		}
		if success.Valid {
			s := success.Int64 == 1
			r.Success = &s
		}
		records = append(records, &r)
	}
	if len(records) == 0 {
		return nil, ErrNotFound
	}
	return records, rows.Err()
}
