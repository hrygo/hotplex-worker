-- +goose Up
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    owner_id TEXT,
    bot_id TEXT,
    worker_session_id TEXT,
    worker_type TEXT NOT NULL,
    state TEXT NOT NULL CHECK(state IN ('created', 'running', 'idle', 'terminated', 'deleted')),
    platform TEXT NOT NULL DEFAULT '',
    platform_key_json TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    expires_at DATETIME,
    idle_expires_at DATETIME,
    context_json TEXT,
    work_dir TEXT,
    title TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_sessions_state ON sessions(state);
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_owner_id ON sessions(owner_id);
CREATE INDEX IF NOT EXISTS idx_sessions_bot_id ON sessions(bot_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_sessions_idle_expires_at ON sessions(idle_expires_at);

-- +goose Down
DROP TABLE IF EXISTS sessions;
