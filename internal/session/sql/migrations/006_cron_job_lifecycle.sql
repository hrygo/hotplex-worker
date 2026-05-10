-- +goose Up

ALTER TABLE cron_jobs ADD COLUMN max_runs    INTEGER NOT NULL DEFAULT 0;
ALTER TABLE cron_jobs ADD COLUMN expires_at  TEXT    NOT NULL DEFAULT '';

-- +goose Down

-- SQLite does not support DROP COLUMN before 3.35.0.
-- For safety, the down migration is a no-op.
-- If running SQLite >= 3.35.0, uncomment:
-- ALTER TABLE cron_jobs DROP COLUMN max_runs;
-- ALTER TABLE cron_jobs DROP COLUMN expires_at;
