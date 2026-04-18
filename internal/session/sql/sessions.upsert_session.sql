INSERT INTO sessions (id, user_id, owner_id, bot_id, worker_session_id, worker_type, state, platform, platform_key_json, created_at, updated_at, expires_at, idle_expires_at, is_active, context_json)
 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
 ON CONFLICT(id) DO UPDATE SET
   state=excluded.state,
   updated_at=excluded.updated_at,
   expires_at=excluded.expires_at,
   idle_expires_at=excluded.idle_expires_at,
   is_active=excluded.is_active,
   context_json=excluded.context_json;
