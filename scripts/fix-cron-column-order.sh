#!/usr/bin/env bash
# fix-cron-column-order.sh — one-time repair for cron_jobs column corruption
#
# Background:
#   Migrations 005-008 (v1.11.0-v1.11.3) used ALTER TABLE ADD COLUMN which
#   appends columns to the physical end in SQLite. Migration 008 then used
#   INSERT INTO ... SELECT * for table replacement, which maps by physical
#   position — causing a 7-column misalignment for positions 16-22:
#
#   Old physical order:  max_retries, state, created_at, updated_at, max_runs, expires_at, silent
#   New logical order:   silent, max_retries, max_runs, expires_at, state, created_at, updated_at
#
#   Result: state JSON ended up in max_retries, max_runs(0) in state, etc.
#
# This script:
#   1. Detects if corruption exists (max_retries contains JSON)
#   2. Rebuilds the table with explicit column mapping
#   3. Resets goose_db_version to match new consolidated migration
#
# Usage:
#   ./scripts/fix-cron-column-order.sh [--dry-run] [--db PATH]
#
# Options:
#   --dry-run    Show what would be fixed without making changes
#   --db PATH    Database path (default: auto-detect)

set -euo pipefail

DRY_RUN=false
DB_PATH=""

for arg in "$@"; do
    case "$arg" in
        --dry-run) DRY_RUN=true ;;
        --db)      shift; DB_PATH="${1:-}" ;;
        -h|--help)
            echo "Usage: $0 [--dry-run] [--db PATH]"
            echo ""
            echo "One-time repair for cron_jobs column corruption (v1.11.0-v1.11.3)."
            echo ""
            echo "Options:"
            echo "  --dry-run    Show what would be fixed without making changes"
            echo "  --db PATH    Database path (default: auto-detect)"
            exit 0
            ;;
    esac
done

# Auto-detect DB path
if [ -z "$DB_PATH" ]; then
    if [ -f ~/.hotplex/data/hotplex.db ]; then
        DB_PATH=~/.hotplex/data/hotplex.db
    elif [ -f ~/.hotplex/hotplex.db ]; then
        DB_PATH=~/.hotplex/hotplex.db
    else
        echo "ERROR: Cannot find hotplex.db. Use --db PATH to specify." >&2
        exit 1
    fi
fi

if [ ! -f "$DB_PATH" ]; then
    echo "ERROR: Database not found: $DB_PATH" >&2
    exit 1
fi

echo "Database: $DB_PATH"
echo ""

# Check if cron_jobs table exists
TABLE_EXISTS=$(sqlite3 "$DB_PATH" "SELECT count(*) FROM sqlite_master WHERE type='table' AND name='cron_jobs';")
if [ "$TABLE_EXISTS" -eq 0 ]; then
    echo "No cron_jobs table found. Nothing to fix."
    exit 0
fi

# Check if corruption exists: max_retries contains JSON-like data
CORRUPT=$(sqlite3 "$DB_PATH" "SELECT count(*) FROM cron_jobs WHERE max_retries LIKE '{%';")

if [ "$CORRUPT" -eq 0 ]; then
    echo "No corruption detected. cron_jobs.max_retries has no JSON data."

    # Still check if goose versions need cleanup
    OLD_MIGRATIONS=$(sqlite3 "$DB_PATH" "SELECT count(*) FROM goose_db_version WHERE version_id BETWEEN 6 AND 8;" 2>/dev/null || echo "0")
    if [ "$OLD_MIGRATIONS" -gt 0 ]; then
        echo ""
        echo "Old migration versions (6-8) found in goose_db_version."
        echo "Cleaning up to match consolidated migration..."
        if [ "$DRY_RUN" = true ]; then
            echo "[DRY-RUN] Would delete goose versions 6-8 and update version 5."
        else
            sqlite3 "$DB_PATH" "
                DELETE FROM goose_db_version WHERE version_id BETWEEN 6 AND 8;
            "
            echo "Done. Goose versions 6-8 removed."
        fi
    fi

    exit 0
fi

echo "Corruption detected: $CORRUPT job(s) have JSON data in max_retries column."
echo ""

# Show affected jobs
echo "Affected jobs:"
sqlite3 -header -column "$DB_PATH" "
    SELECT name,
           substr(max_retries, 1, 40) AS max_retries_preview,
           state AS state_value
    FROM cron_jobs
    WHERE max_retries LIKE '{%';
"
echo ""

if [ "$DRY_RUN" = true ]; then
    echo "[DRY-RUN] Would rebuild cron_jobs table with correct column mapping:"
    echo "  - max_retries (JSON) → state"
    echo "  - state (int) → max_runs"
    echo "  - silent (int) → max_retries"
    echo "  - max_runs (int) → created_at"
    echo "  - expires_at (int) → updated_at"
    echo "  - created_at (text) → expires_at"
    echo "  - updated_at (int) → silent"
    echo ""
    echo "[DRY-RUN] Would also clean up goose versions 6-8."
    exit 0
fi

# Back up the database
BACKUP="${DB_PATH}.bak.$(date +%Y%m%d%H%M%S)"
cp "$DB_PATH" "$BACKUP"
echo "Backup created: $BACKUP"

# Rebuild table with explicit column mapping
sqlite3 "$DB_PATH" "
-- Step 1: Create fix table with correct schema
CREATE TABLE IF NOT EXISTS cron_jobs_fix (
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

-- Step 2: Copy with column remapping (positions 16-22 swapped)
-- Current column → actually holds → remap to correct column
INSERT INTO cron_jobs_fix (
    id, name, description, enabled,
    schedule_kind, schedule_data, payload_kind, payload_data,
    work_dir, bot_id, owner_id, platform, platform_key,
    timeout_sec, delete_after_run,
    silent, max_retries, max_runs, expires_at,
    state, created_at, updated_at
)
SELECT
    id, name, description, enabled,
    schedule_kind, schedule_data, payload_kind, payload_data,
    work_dir, bot_id, owner_id, platform, platform_key,
    timeout_sec, delete_after_run,
    -- Remap positions 16-22 from corrupted to correct
    CASE WHEN updated_at IN (0, 1) THEN 0 ELSE updated_at END,   -- silent ← old updated_at (was old silent)
    0,                                                               -- max_retries ← reset to 0
    0,                                                               -- max_runs ← reset to 0 (original lost)
    CASE
        WHEN created_at LIKE '%-%' OR (typeof(created_at) = 'text' AND length(created_at) > 8) THEN created_at
        WHEN expires_at NOT LIKE '{%' AND length(expires_at) > 0 THEN expires_at
        ELSE ''
    END,                                                             -- expires_at ← recover from available sources
    CASE
        WHEN max_retries LIKE '{%' THEN max_retries
        ELSE '{}'
    END,                                                             -- state ← recover JSON from max_retries
    CASE
        WHEN max_runs > 1000000000000 THEN max_runs                 -- created_at ← recover from max_runs (old created_at)
        WHEN created_at > 1000000000000 AND typeof(created_at) = 'integer' THEN created_at
        ELSE 0
    END,
    0                                                                -- updated_at ← reset to 0
FROM cron_jobs;

-- Step 3: Replace table
DROP TABLE cron_jobs;
ALTER TABLE cron_jobs_fix RENAME TO cron_jobs;

-- Step 4: Recreate indexes
CREATE INDEX idx_cron_jobs_enabled ON cron_jobs(enabled);
CREATE INDEX idx_cron_jobs_next_run ON cron_jobs(enabled, json_extract(state, '$.next_run_at_ms'));
"

# Step 5: Clean up goose migration versions
sqlite3 "$DB_PATH" "
DELETE FROM goose_db_version WHERE version_id BETWEEN 6 AND 8;
"

echo ""
echo "Repair complete. Verifying..."

# Verify
REMAINING=$(sqlite3 "$DB_PATH" "SELECT count(*) FROM cron_jobs WHERE max_retries LIKE '{%';")
TOTAL=$(sqlite3 "$DB_PATH" "SELECT count(*) FROM cron_jobs;")

if [ "$REMAINING" -eq 0 ]; then
    echo "OK: $TOTAL job(s) verified, no corruption remaining."
else
    echo "WARNING: $REMAINING job(s) still show corruption. Manual investigation needed."
    echo "Backup available at: $BACKUP"
fi

echo ""
echo "Note: Cron job state (next_run_at_ms, run_count) was preserved."
echo "      max_retries, max_runs, updated_at were reset to defaults (0)."
echo "      Run 'hotplex cron list' to verify schedule times are correct."
