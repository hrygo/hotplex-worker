-- +goose Up
CREATE TABLE IF NOT EXISTS conversation (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    seq INTEGER NOT NULL,
    role TEXT NOT NULL CHECK(role IN ('user', 'assistant')),
    content TEXT NOT NULL,
    platform TEXT NOT NULL DEFAULT '',
    user_id TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    success INTEGER,
    source TEXT NOT NULL DEFAULT 'normal',
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
