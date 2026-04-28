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

	"github.com/hrygo/hotplex/internal/config"
)

var ErrConvNotFound = errors.New("conversation store: no records found")

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"

	SourceNormal     = "normal"
	SourceCrash      = "crash"
	SourceTimeout    = "timeout"
	SourceFreshStart = "fresh_start"
)

// ConversationRecord represents a single row in the conversation table.
type ConversationRecord struct {
	ID            string
	SessionID     string
	Seq           int64
	Role          string
	Content       string
	Platform      string
	UserID        string
	Model         string
	Success       *bool
	Source        string
	Tools         map[string]int
	ToolCallCount int
	TokensIn      int64
	TokensOut     int64
	DurationMs    int64
	CostUSD       float64
	Metadata      map[string]any
	CreatedAt     time.Time
}

// ConversationStore defines the interface for conversation turn persistence.
type ConversationStore interface {
	Append(ctx context.Context, rec *ConversationRecord) error
	GetBySession(ctx context.Context, sessionID string, limit, offset int) ([]*ConversationRecord, error)
	DeleteBySession(ctx context.Context, sessionID string) error
	DeleteExpired(ctx context.Context, cutoff time.Time) (int64, error)
	Close() error
}

// convWriteReq is a pending write request for the background batch writer.
type convWriteReq struct {
	rec *ConversationRecord
}

// SQLiteConversationStore implements ConversationStore using SQLite with async batch writer.
type SQLiteConversationStore struct {
	db *sql.DB

	log     *slog.Logger
	writeC  chan *convWriteReq
	closeC  chan struct{}
	closeWg sync.WaitGroup
}

var _ ConversationStore = (*SQLiteConversationStore)(nil)

// NewSQLiteConversationStore creates a conversation store backed by the same DB path.
func NewSQLiteConversationStore(ctx context.Context, cfg *config.Config) (*SQLiteConversationStore, error) {
	if err := ensureDBDir(cfg.DB.Path); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite3", cfg.DB.Path+"?_txlock=immediate")
	if err != nil {
		return nil, fmt.Errorf("conversation store: open db: %w", err)
	}

	if err := initSQLiteDB(db, cfg, "conversation"); err != nil {
		_ = db.Close()
		return nil, err
	}

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(5 * time.Minute)

	s := &SQLiteConversationStore{
		db:     db,
		log:    slog.Default().With("component", "conversation-store"),
		writeC: make(chan *convWriteReq, writeChanCap),
		closeC: make(chan struct{}),
	}

	s.closeWg.Add(1)
	go s.runWriter()

	return s, nil
}

func (s *SQLiteConversationStore) Append(_ context.Context, rec *ConversationRecord) error {
	if rec.ID == "" {
		rec.ID = fmt.Sprintf("conv_%s_%d", rec.SessionID, rec.Seq)
	}
	select {
	case s.writeC <- &convWriteReq{rec: rec}:
		return nil
	default:
		s.log.Warn("conversation store: write channel full, dropping record",
			"session_id", rec.SessionID, "seq", rec.Seq, "role", rec.Role)
		return nil
	}
}

func (s *SQLiteConversationStore) GetBySession(ctx context.Context, sessionID string, limit, offset int) ([]*ConversationRecord, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}

	rows, err := s.db.QueryContext(ctx, queries["conversation.get_by_session"], sessionID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("conversation store: get by session: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var records []*ConversationRecord
	for rows.Next() {
		var r ConversationRecord
		var success sql.NullInt64
		var toolsJSON, metaJSON sql.NullString

		if err := rows.Scan(
			&r.ID, &r.SessionID, &r.Seq, &r.Role, &r.Content,
			&r.Platform, &r.UserID, &r.Model, &success, &r.Source,
			&toolsJSON, &r.ToolCallCount,
			&r.TokensIn, &r.TokensOut, &r.DurationMs, &r.CostUSD,
			&metaJSON, &r.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("conversation store: scan: %w", err)
		}

		if success.Valid {
			s := success.Int64 == 1
			r.Success = &s
		}
		if toolsJSON.Valid && toolsJSON.String != "" {
			_ = json.Unmarshal([]byte(toolsJSON.String), &r.Tools)
		}
		if metaJSON.Valid && metaJSON.String != "" {
			_ = json.Unmarshal([]byte(metaJSON.String), &r.Metadata)
		}
		records = append(records, &r)
	}
	if len(records) == 0 {
		return nil, ErrConvNotFound
	}
	return records, nil
}

func (s *SQLiteConversationStore) DeleteBySession(ctx context.Context, sessionID string) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}
	_, err := s.db.ExecContext(ctx, queries["conversation.delete_by_session"], sessionID)
	if err != nil {
		return fmt.Errorf("conversation store: delete by session: %w", err)
	}
	return nil
}

func (s *SQLiteConversationStore) DeleteExpired(ctx context.Context, cutoff time.Time) (int64, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
	}
	res, err := s.db.ExecContext(ctx, queries["conversation.delete_expired"], cutoff)
	if err != nil {
		return 0, fmt.Errorf("conversation store: delete expired: %w", err)
	}
	return res.RowsAffected()
}

func (s *SQLiteConversationStore) Close() error {
	close(s.closeC)
	s.closeWg.Wait()
	return s.db.Close()
}

func (s *SQLiteConversationStore) runWriter() {
	defer s.closeWg.Done()

	ticker := time.NewTicker(batchFlushInterval)
	defer ticker.Stop()

	var batch []*convWriteReq
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

func (s *SQLiteConversationStore) flushBatch(batch []*convWriteReq) {
	if len(batch) == 0 {
		return
	}

	tx, err := s.db.Begin()
	if err != nil {
		s.log.Error("conversation store: batch tx begin", "err", err)
		return
	}

	stmt, err := tx.Prepare(queries["conversation.insert_batch"])
	if err != nil {
		_ = tx.Rollback()
		s.log.Error("conversation store: batch stmt prepare", "err", err)
		return
	}

	for _, req := range batch {
		r := req.rec
		var success sql.NullInt64
		if r.Success != nil {
			if *r.Success {
				success.Int64 = 1
			}
			success.Valid = true
		}

		var toolsJSON string
		if len(r.Tools) > 0 {
			// Store as sorted unique tool names: ["Read","Edit","Bash"]
			names := make([]string, 0, len(r.Tools))
			for name := range r.Tools {
				names = append(names, name)
			}
			b, _ := json.Marshal(names)
			toolsJSON = string(b)
		}

		var metaJSON string
		if len(r.Metadata) > 0 {
			b, _ := json.Marshal(r.Metadata)
			metaJSON = string(b)
		}

		if _, execErr := stmt.Exec(
			r.ID, r.SessionID, r.Seq, r.Role, r.Content,
			r.Platform, r.UserID, r.Model, success, r.Source,
			toolsJSON, r.ToolCallCount,
			r.TokensIn, r.TokensOut, r.DurationMs, r.CostUSD,
			metaJSON,
		); execErr != nil {
			s.log.Warn("conversation store: batch insert", "err", execErr, "session_id", r.SessionID)
		}
	}
	_ = stmt.Close()

	if err := tx.Commit(); err != nil {
		s.log.Error("conversation store: batch commit", "err", err)
	}
}
