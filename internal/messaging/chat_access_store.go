package messaging

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// ChatAccessType classifies why the chat-entered event fired.
type ChatAccessType int

const (
	ChatAccessNew       ChatAccessType = iota // No prior record — first contact.
	ChatAccessReturning                       // Last activity > 24 h ago.
	ChatAccessActive                          // Last activity ≤ 24 h — suppress welcome.
)

// ChatAccessRecord is one row in chat_access_events.
type ChatAccessRecord struct {
	EventID       string
	Platform      string
	ChatID        string
	UserID        string
	BotID         string
	LastMessageAt int64 // ms epoch, 0 = unknown
	WelcomeSent   bool
	CreatedAt     int64 // unix epoch
}

// ChatAccessStore provides dedup + cooldown + persistence for chat-entered events.
type ChatAccessStore struct {
	db  *sql.DB
	log *slog.Logger
}

// NewChatAccessStore creates a store backed by the shared SQLite connection.
func NewChatAccessStore(db *sql.DB, log *slog.Logger) *ChatAccessStore {
	return &ChatAccessStore{db: db, log: log}
}

// Record inserts the event. Returns false (with nil error) when event_id
// already exists (duplicate event from the platform).
func (s *ChatAccessStore) Record(ctx context.Context, r ChatAccessRecord) (inserted bool, err error) {
	now := time.Now().Unix()
	r.CreatedAt = now
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO chat_access_events (event_id, platform, chat_id, user_id, bot_id, last_message_at, welcome_sent, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.EventID, r.Platform, r.ChatID, r.UserID, r.BotID, r.LastMessageAt, boolToInt(r.WelcomeSent), r.CreatedAt,
	)
	if err != nil {
		// UNIQUE constraint violation → duplicate event.
		if isSQLiteUnique(err) {
			return false, nil
		}
		return false, fmt.Errorf("chat_access insert: %w", err)
	}
	return true, nil
}

// Classify determines whether a welcome should be sent.
//
// Feishu: pass lastMessageAtMs from the event payload.
// Slack:  pass 0; the function falls back to the DB.
func (s *ChatAccessStore) Classify(ctx context.Context, platform, chatID, botID, userID string, lastMessageAtMs int64) ChatAccessType {
	cooldown := int64(3600) // 1 h debounce window in seconds.

	// Check recent row for (platform, chat_id, bot_id).
	var lastCreatedAt int64
	err := s.db.QueryRowContext(ctx,
		`SELECT created_at FROM chat_access_events
		 WHERE platform = ? AND chat_id = ? AND bot_id = ?
		 ORDER BY created_at DESC LIMIT 1`,
		platform, chatID, botID,
	).Scan(&lastCreatedAt)

	if err == nil {
		now := time.Now().Unix()
		// Within 1 h cooldown — suppress even if we'd otherwise send.
		if now-lastCreatedAt < cooldown {
			return ChatAccessActive
		}
	}

	// Feishu fast-path: use event payload timestamp directly.
	if lastMessageAtMs > 0 {
		lastSec := lastMessageAtMs / 1000
		since := time.Now().Unix() - lastSec
		if since > 24*3600 {
			return ChatAccessNew // no prior messages in 24 h
		}
		return ChatAccessActive
	}

	// Slack path: check if we've ever seen this user.
	var cnt int
	err = s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM chat_access_events
		 WHERE platform = ? AND user_id = ? AND bot_id = ?`,
		platform, userID, botID,
	).Scan(&cnt)
	if err != nil || cnt == 0 {
		return ChatAccessNew
	}

	// Existing user — check last activity.
	var lastAct int64
	err = s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(created_at), 0) FROM chat_access_events
		 WHERE platform = ? AND user_id = ? AND bot_id = ?`,
		platform, userID, botID,
	).Scan(&lastAct)
	if err != nil {
		return ChatAccessNew
	}
	since := time.Now().Unix() - lastAct
	if since > 24*3600 {
		return ChatAccessReturning
	}
	return ChatAccessActive
}

// boolToInt converts bool to SQLite integer.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// isSQLiteUnique returns true for UNIQUE constraint violations.
func isSQLiteUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
