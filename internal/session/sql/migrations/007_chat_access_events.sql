-- +goose Up

CREATE TABLE IF NOT EXISTS chat_access_events (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id          TEXT NOT NULL UNIQUE,
    platform          TEXT NOT NULL CHECK(platform IN ('feishu', 'slack')),
    chat_id           TEXT NOT NULL,
    user_id           TEXT NOT NULL,
    bot_id            TEXT NOT NULL DEFAULT '',
    last_message_at   INTEGER NOT NULL DEFAULT 0,
    welcome_sent      INTEGER NOT NULL DEFAULT 0 CHECK(welcome_sent IN (0, 1)),
    created_at        INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_ca_event ON chat_access_events(event_id);
CREATE INDEX IF NOT EXISTS idx_ca_chat_bot ON chat_access_events(platform, chat_id, bot_id);
CREATE INDEX IF NOT EXISTS idx_ca_user_bot ON chat_access_events(platform, user_id, bot_id);

-- +goose Down

DROP TABLE IF EXISTS chat_access_events;
