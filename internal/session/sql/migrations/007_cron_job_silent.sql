-- +goose Up
ALTER TABLE cron_jobs ADD COLUMN silent INTEGER NOT NULL DEFAULT 0 CHECK(silent IN (0, 1));

-- +goose Down
-- SQLite DROP COLUMN requires 3.35.0+; no-op for safety.
