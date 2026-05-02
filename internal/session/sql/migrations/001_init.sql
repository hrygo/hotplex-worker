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

CREATE TABLE IF NOT EXISTS conversation (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    seq INTEGER NOT NULL,
    role TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
    content TEXT NOT NULL,
    platform TEXT NOT NULL DEFAULT '',
    user_id TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    success INTEGER,
    source TEXT NOT NULL DEFAULT 'normal' CHECK(source IN ('normal', 'crash', 'timeout', 'fresh_start')),
    tools_json TEXT,
    tool_call_count INTEGER DEFAULT 0,
    tokens_in INTEGER DEFAULT 0,
    tokens_out INTEGER DEFAULT 0,
    duration_ms INTEGER DEFAULT 0,
    cost_usd REAL DEFAULT 0,
    metadata_json TEXT,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_conv_session ON conversation(session_id, seq);
CREATE INDEX IF NOT EXISTS idx_conv_user ON conversation(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_conv_platform ON conversation(platform, created_at);

-- +goose Down
DROP TABLE IF EXISTS conversation;
DROP TABLE IF EXISTS sessions;
