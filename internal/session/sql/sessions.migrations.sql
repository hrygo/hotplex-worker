-- Migrations: add columns to sessions table if they don't exist.
-- No-op on fresh installs where columns already exist.
-- These ALTER TABLE statements are executed with errors ignored (via _ = exec)
-- because older SQLite versions don't support ADD COLUMN IF NOT EXISTS.

-- Migrate: add owner_id column (nullable so existing rows remain valid).
ALTER TABLE sessions ADD COLUMN owner_id TEXT;

-- Migrate: add bot_id column for SEC-007 multi-bot isolation.
ALTER TABLE sessions ADD COLUMN bot_id TEXT;

-- Migrate: add platform + platform_key_json for consistency mapping persistence.
ALTER TABLE sessions ADD COLUMN platform TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN platform_key_json TEXT NOT NULL DEFAULT '';
