-- +goose Up
-- Rename payload_kind 'agent_turn' → 'isolated_session' and add 'attached_session'.
-- SQLite doesn't support ALTER CONSTRAINT, so we use the replacement table pattern.
-- Two-step: (1) copy into unconstrained table + rename values, (2) copy into constrained table.

CREATE TABLE cron_jobs_new (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL UNIQUE,
    description      TEXT NOT NULL DEFAULT '',
    enabled          INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0, 1)),
    schedule_kind    TEXT NOT NULL CHECK(schedule_kind IN ('at', 'every', 'cron')),
    schedule_data    TEXT NOT NULL,
    payload_kind     TEXT NOT NULL DEFAULT 'isolated_session',
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

INSERT INTO cron_jobs_new SELECT * FROM cron_jobs;

UPDATE cron_jobs_new SET payload_kind = 'isolated_session' WHERE payload_kind = 'agent_turn';

DROP TABLE cron_jobs;

ALTER TABLE cron_jobs_new RENAME TO cron_jobs;

-- Now enforce the CHECK constraint via second replacement.
CREATE TABLE cron_jobs_final (
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

INSERT INTO cron_jobs_final SELECT * FROM cron_jobs;

DROP TABLE cron_jobs;

ALTER TABLE cron_jobs_final RENAME TO cron_jobs;

CREATE INDEX idx_cron_jobs_enabled ON cron_jobs(enabled);
CREATE INDEX idx_cron_jobs_next_run ON cron_jobs(enabled, json_extract(state, '$.next_run_at_ms'));

-- +goose Down
-- Reverse: rename 'isolated_session' → 'agent_turn', remove 'attached_session' from CHECK.

CREATE TABLE cron_jobs_new (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL UNIQUE,
    description      TEXT NOT NULL DEFAULT '',
    enabled          INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0, 1)),
    schedule_kind    TEXT NOT NULL CHECK(schedule_kind IN ('at', 'every', 'cron')),
    schedule_data    TEXT NOT NULL,
    payload_kind     TEXT NOT NULL DEFAULT 'agent_turn',
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

INSERT INTO cron_jobs_new SELECT * FROM cron_jobs;

UPDATE cron_jobs_new SET payload_kind = 'agent_turn' WHERE payload_kind != 'system_event';

DROP TABLE cron_jobs;

ALTER TABLE cron_jobs_new RENAME TO cron_jobs;

-- Enforce CHECK via second replacement.
CREATE TABLE cron_jobs_final (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL UNIQUE,
    description      TEXT NOT NULL DEFAULT '',
    enabled          INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0, 1)),
    schedule_kind    TEXT NOT NULL CHECK(schedule_kind IN ('at', 'every', 'cron')),
    schedule_data    TEXT NOT NULL,
    payload_kind     TEXT NOT NULL DEFAULT 'agent_turn' CHECK(payload_kind IN ('agent_turn', 'system_event')),
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

INSERT INTO cron_jobs_final SELECT * FROM cron_jobs;

DROP TABLE cron_jobs;

ALTER TABLE cron_jobs_final RENAME TO cron_jobs;

CREATE INDEX idx_cron_jobs_enabled ON cron_jobs(enabled);
CREATE INDEX idx_cron_jobs_next_run ON cron_jobs(enabled, json_extract(state, '$.next_run_at_ms'));
