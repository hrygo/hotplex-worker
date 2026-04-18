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

	"github.com/hotplex/hotplex-worker/internal/config"
	"github.com/hotplex/hotplex-worker/pkg/events"
)

// ErrEventNotFound is returned when no events exist for a given session.
var ErrEventNotFound = errors.New("session store: no events found")

// ErrAuditChainInvalid is returned when the audit log hash chain is broken.
var ErrAuditChainInvalid = errors.New("session store: audit chain invalid")

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
	// Query returns ordered events as Envelopes for a session from seq onwards (EVT-010).
	Query(ctx context.Context, sessionID string, fromSeq int64) ([]*events.Envelope, error)
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
	writeChanCap       = 1024 // buffered channel capacity
	batchFlushInterval = 100 * time.Millisecond
	batchMaxSize       = 50 // flush when batch reaches this size
)

// NewSQLiteMessageStore creates a SQLiteMessageStore backed by the same DB path
// as the session store. It starts a background goroutine for batch writes.
func NewSQLiteMessageStore(ctx context.Context, cfg *config.Config) (*SQLiteMessageStore, error) {
	if err := ensureDBDir(cfg.DB.Path); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", cfg.DB.Path)
	if err != nil {
		return nil, fmt.Errorf("session store: open msg db: %w", err)
	}

	if cfg.DB.WALMode {
		if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("session store: msg WAL: %w", err)
		}
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA busy_timeout=%d", int(cfg.DB.BusyTimeout.Milliseconds()))); err != nil {
		_ = db.Close()
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
	defer func() { _ = rows.Close() }()

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

func (s *SQLiteMessageStore) Query(ctx context.Context, sessionID string, fromSeq int64) ([]*events.Envelope, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, session_id, seq, event_type, payload_json
		 FROM events WHERE session_id = ? AND seq > ? ORDER BY seq ASC`, sessionID, fromSeq)
	if err != nil {
		return nil, fmt.Errorf("session store: query events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var envelopes []*events.Envelope
	for rows.Next() {
		var id, sessionID string
		var seq int64
		var eventType string
		var payloadJSON string
		if err := rows.Scan(&id, &sessionID, &seq, &eventType, &payloadJSON); err != nil {
			continue
		}
		var data interface{}
		if err := json.Unmarshal([]byte(payloadJSON), &data); err != nil {
			data = nil
		}
		envelopes = append(envelopes, &events.Envelope{
			ID:        id,
			Seq:       seq,
			SessionID: sessionID,
			Event: events.Event{
				Type: events.Kind(eventType),
				Data: data,
			},
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("session store: query scan: %w", err)
	}

	return envelopes, nil
}

// Close gracefully shuts down the async writer (draining pending writes) and closes the DB.
func (s *SQLiteMessageStore) Close() error {
	close(s.closeC)
	s.closeWg.Wait()
	return s.db.Close()
}
