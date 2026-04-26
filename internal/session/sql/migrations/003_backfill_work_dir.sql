-- +goose Up
-- Backfill NULL work_dir values to empty string for Go string scan compatibility.
UPDATE sessions SET work_dir = '' WHERE work_dir IS NULL;

-- +goose Down
-- No-op: cannot determine original NULL values.
SELECT 1;
