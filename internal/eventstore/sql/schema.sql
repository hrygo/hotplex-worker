-- Event store schema (independent SQLite file)

CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    seq INTEGER NOT NULL,
    type TEXT NOT NULL,
    data TEXT NOT NULL,
    direction TEXT NOT NULL DEFAULT 'outbound',
    created_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_events_session_seq ON events(session_id, seq);
CREATE INDEX IF NOT EXISTS idx_events_created ON events(created_at);
