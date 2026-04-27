package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/pkg/events"
)

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

// TurnStats holds statistics for a single turn.
type TurnStats struct {
	Seq           int64          `json:"seq"`
	Success       bool           `json:"success"`
	Dropped       bool           `json:"dropped"`
	DurationMs    int64          `json:"duration_ms"`
	DurationAPIMs int64          `json:"duration_api_ms"`
	CostUSD       float64        `json:"cost_usd"`
	Usage         map[string]any `json:"usage"`
	ModelUsage    map[string]any `json:"model_usage"`
	CreatedAt     string         `json:"created_at"`
}

// SessionStats holds aggregated statistics across all turns of a session.
type SessionStats struct {
	SessionID        string                        `json:"session_id"`
	TotalTurns       int                           `json:"total_turns"`
	SuccessTurns     int                           `json:"success_turns"`
	FailedTurns      int                           `json:"failed_turns"`
	DroppedTurns     int                           `json:"dropped_turns"`
	TotalDurationMs  int64                         `json:"total_duration_ms"`
	TotalAPIDuration int64                         `json:"total_api_duration_ms"`
	TotalCostUSD     float64                       `json:"total_cost_usd"`
	TotalUsage       map[string]float64            `json:"total_usage"`
	TotalModelUsage  map[string]map[string]float64 `json:"total_model_usage"`
	Turns            []TurnStats                   `json:"turns"`
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
	// SessionStats returns aggregated turn statistics for a session.
	SessionStats(ctx context.Context, sessionID string) (*SessionStats, error)
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

	if err := initSQLiteDB(db, cfg, "msg"); err != nil {
		_ = db.Close()
		return nil, err
	}

	// Limit to one connection since writes are serialized by the single writer goroutine.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(5 * time.Minute)

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
		s.log.Warn("session store: write channel full, dropping event",
			"session_id", sessionID, "seq", seq)
		return fmt.Errorf("session store: write channel full for session %s", sessionID)
	}
}

// runWriter is the background goroutine that batches writes and flushes them.
func (s *SQLiteMessageStore) runWriter() {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("session store: runWriter panic", "panic", r, "stack", string(debug.Stack()))
		}
		s.closeWg.Done()
	}()

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

	stmt, err := tx.Prepare(queries["events.insert_batch"])
	if err != nil {
		_ = tx.Rollback()
		s.log.Error("session store: batch stmt prepare", "err", err)
		return
	}

	var hasInsertErr bool
	for _, req := range batch {
		id := fmt.Sprintf("evt_%s_%d", req.sessionID, req.seq)
		if _, execErr := stmt.Exec(id, req.sessionID, req.seq, req.eventType, string(req.payload)); execErr != nil {
			s.log.Warn("session store: batch insert", "err", execErr, "session_id", req.sessionID)
			hasInsertErr = true
		}
	}
	_ = stmt.Close()

	if hasInsertErr {
		_ = tx.Rollback()
		return
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		s.log.Error("session store: batch tx commit", "err", err)
	}
}

// GetBySession returns all event records for a session from seq onwards (EVT-003).
func (s *SQLiteMessageStore) GetBySession(ctx context.Context, sessionID string, fromSeq int64) ([]*EventRecord, error) {
	rows, err := s.db.QueryContext(ctx, queries["message_store.get_events_by_session"], sessionID, fromSeq)
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
	err := s.db.QueryRowContext(ctx, queries["message_store.get_session_owner"], sessionID).Scan(&ownerID)

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
	err = s.db.QueryRowContext(ctx, queries["message_store.get_session_user"], sessionID).Scan(&userID)
	if err == sql.ErrNoRows {
		return "", ErrSessionNotFound
	}
	return userID, err
}

func (s *SQLiteMessageStore) Query(ctx context.Context, sessionID string, fromSeq int64) ([]*events.Envelope, error) {
	rows, err := s.db.QueryContext(ctx, queries["message_store.query_events"], sessionID, fromSeq)
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

// SessionStats returns aggregated turn statistics for a session by querying
// all done events and extracting usage/cost/duration from their payloads.
func (s *SQLiteMessageStore) SessionStats(ctx context.Context, sessionID string) (*SessionStats, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}

	rows, err := s.db.QueryContext(ctx, queries["message_store.get_session_done_events"], sessionID)
	if err != nil {
		return nil, fmt.Errorf("session store: session stats: %w", err)
	}
	defer func() { _ = rows.Close() }()

	stats := &SessionStats{
		SessionID:       sessionID,
		TotalUsage:      make(map[string]float64),
		TotalModelUsage: make(map[string]map[string]float64),
	}

	for rows.Next() {
		var seq int64
		var payloadStr, createdAt string
		if err := rows.Scan(&seq, &payloadStr, &createdAt); err != nil {
			continue
		}

		var raw map[string]any
		if err := json.Unmarshal([]byte(payloadStr), &raw); err != nil {
			continue
		}

		event, _ := raw["event"].(map[string]any)
		if event == nil {
			continue
		}
		data, _ := event["data"].(map[string]any)
		if data == nil {
			continue
		}
		st, _ := data["stats"].(map[string]any)

		ts := TurnStats{
			Seq:       seq,
			Success:   toBool(data["success"]),
			Dropped:   toBool(data["dropped"]),
			CreatedAt: createdAt,
		}

		if st != nil {
			ts.DurationMs = toInt64(st["duration_ms"])
			ts.DurationAPIMs = toInt64(st["duration_api_ms"])
			ts.CostUSD = toFloat64(st["total_cost_usd"])

			if u, ok := st["usage"].(map[string]any); ok {
				ts.Usage = u
				accumulateMap(stats.TotalUsage, u)
			}
			if mu, ok := st["model_usage"].(map[string]any); ok {
				ts.ModelUsage = mu
				accumulateModelUsage(stats.TotalModelUsage, mu)
			}
		}

		stats.TotalTurns++
		if ts.Success {
			stats.SuccessTurns++
		} else {
			stats.FailedTurns++
		}
		if ts.Dropped {
			stats.DroppedTurns++
		}
		stats.TotalDurationMs += ts.DurationMs
		stats.TotalAPIDuration += ts.DurationAPIMs
		stats.TotalCostUSD += ts.CostUSD
		stats.Turns = append(stats.Turns, ts)
	}

	if stats.TotalTurns == 0 {
		return nil, ErrEventNotFound
	}
	return stats, rows.Err()
}

func toBool(v any) bool {
	b, _ := v.(bool)
	return b
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	case json.Number:
		i, _ := n.Int64()
		return i
	default:
		return 0
	}
}

func toFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

func accumulateMap(dst map[string]float64, src map[string]any) {
	for k, v := range src {
		dst[k] += toFloat64(v)
	}
}

func accumulateModelUsage(dst map[string]map[string]float64, src map[string]any) {
	for model, v := range src {
		fields, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if dst[model] == nil {
			dst[model] = make(map[string]float64)
		}
		accumulateMap(dst[model], fields)
	}
}

// Close gracefully shuts down the async writer (draining pending writes) and closes the DB.
func (s *SQLiteMessageStore) Close() error {
	close(s.closeC)
	s.closeWg.Wait()
	return s.db.Close()
}
