-- Sessions: core session metadata.
-- id is a UUIDv5 derived from (ownerID, workerType, platform, platform_key) via DerivePlatformSessionKey.
-- This deterministic mapping ensures the same platform conversation always maps to the same session.
CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    owner_id TEXT,
    bot_id TEXT,
    worker_session_id TEXT,
    worker_type TEXT NOT NULL,
    state TEXT NOT NULL,
    platform TEXT NOT NULL DEFAULT '',
    platform_key_json TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    expires_at DATETIME,
    idle_expires_at DATETIME,
    is_active INTEGER NOT NULL DEFAULT 0,
    context_json TEXT
);
CREATE INDEX IF NOT EXISTS idx_sessions_state ON sessions(state);
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_owner_id ON sessions(owner_id);
CREATE INDEX IF NOT EXISTS idx_sessions_bot_id ON sessions(bot_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_sessions_idle_expires_at ON sessions(idle_expires_at);
CREATE INDEX IF NOT EXISTS idx_sessions_platform ON sessions(platform);

-- EVT-001: events table for AEP message persistence.
CREATE TABLE IF NOT EXISTS events (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    seq INTEGER NOT NULL,
    event_type TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_events_session_id ON events(session_id);
CREATE INDEX IF NOT EXISTS idx_events_session_seq ON events(session_id, seq);
CREATE UNIQUE INDEX IF NOT EXISTS idx_events_session_seq_unique ON events(session_id, seq);

-- EVT-008: audit_log table with hash chain for tamper-evident audit trail.
CREATE TABLE IF NOT EXISTS audit_log (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp INTEGER NOT NULL,
    action TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    session_id TEXT NOT NULL,
    details TEXT,
    previous_hash TEXT NOT NULL,
    current_hash TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_session_id ON audit_log(session_id);
CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_log(timestamp);
