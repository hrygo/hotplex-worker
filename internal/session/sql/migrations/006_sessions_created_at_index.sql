-- +goose Up
-- Index for list_sessions ORDER BY created_at DESC to avoid full-table sort.
CREATE INDEX IF NOT EXISTS idx_sessions_created_at ON sessions(created_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_sessions_created_at;
