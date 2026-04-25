-- list_sessions lists sessions with pagination, excluding soft-deleted.
-- Filters by user_id and platform if provided.
SELECT id, user_id, COALESCE(owner_id, user_id), worker_session_id, worker_type, state, bot_id, platform, platform_key_json, created_at, updated_at, expires_at, idle_expires_at, context_json
 FROM sessions 
 WHERE state != 'deleted'
   AND (? = '' OR user_id = ?)
   AND (? = '' OR platform = ?)
 ORDER BY created_at DESC LIMIT ? OFFSET ?;
