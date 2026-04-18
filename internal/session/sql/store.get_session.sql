-- upsert_session inserts or updates a session record.
SELECT id, user_id, COALESCE(owner_id, user_id), worker_session_id, worker_type, state, bot_id, platform, platform_key_json, created_at, updated_at, expires_at, idle_expires_at, context_json
 FROM sessions WHERE id = ?;
