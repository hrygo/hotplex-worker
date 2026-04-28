-- +goose Up
-- Drop events table: conversation table is now the single source of truth.
-- SessionStats migrated to ConversationStore.SessionStats.
DROP TABLE IF EXISTS events;

-- +goose Down
-- Recreate events table for rollback.
CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    seq INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_events_session_id ON events(session_id);
CREATE INDEX IF NOT EXISTS idx_events_session_seq ON events(session_id, seq);
CREATE UNIQUE INDEX IF NOT EXISTS idx_events_session_seq_unique ON events(session_id, seq);
