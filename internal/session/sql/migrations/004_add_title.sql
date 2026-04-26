-- +goose Up
ALTER TABLE sessions ADD COLUMN title TEXT NOT NULL DEFAULT '';

-- +goose Down
-- SQLite does not support DROP COLUMN before 3.35.0; no-op down migration.
