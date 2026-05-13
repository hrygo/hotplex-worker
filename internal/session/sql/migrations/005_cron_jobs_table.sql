-- +goose Up

CREATE TABLE IF NOT EXISTS cron_jobs (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL UNIQUE,
    description      TEXT NOT NULL DEFAULT '',
    enabled          INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0, 1)),
    schedule_kind    TEXT NOT NULL CHECK(schedule_kind IN ('at', 'every', 'cron')),
    schedule_data    TEXT NOT NULL,
    payload_kind     TEXT NOT NULL DEFAULT 'isolated_session' CHECK(payload_kind IN ('isolated_session', 'system_event', 'attached_session')),
    payload_data     TEXT NOT NULL,
    work_dir         TEXT NOT NULL DEFAULT '',
    bot_id           TEXT NOT NULL DEFAULT '',
    owner_id         TEXT NOT NULL DEFAULT '',
    platform         TEXT NOT NULL DEFAULT '',
    platform_key     TEXT NOT NULL DEFAULT '{}',
    timeout_sec      INTEGER NOT NULL DEFAULT 0,
    delete_after_run INTEGER NOT NULL DEFAULT 0 CHECK(delete_after_run IN (0, 1)),
    silent           INTEGER NOT NULL DEFAULT 0 CHECK(silent IN (0, 1)),
    max_retries      INTEGER NOT NULL DEFAULT 0,
    max_runs         INTEGER NOT NULL DEFAULT 0,
    expires_at       TEXT NOT NULL DEFAULT '',
    state            TEXT NOT NULL DEFAULT '{}',
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_cron_jobs_enabled ON cron_jobs(enabled);
CREATE INDEX IF NOT EXISTS idx_cron_jobs_next_run ON cron_jobs(enabled, json_extract(state, '$.next_run_at_ms'));

-- +goose Down
DROP TABLE IF EXISTS cron_jobs;
