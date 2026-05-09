-- +goose Up
-- Index for GC delete_terminated query: DELETE FROM sessions WHERE state=? AND updated_at <= ?
CREATE INDEX IF NOT EXISTS idx_sessions_updated_at ON sessions(updated_at);

-- +goose Down
DROP INDEX IF EXISTS idx_sessions_updated_at;
