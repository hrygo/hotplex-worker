INSERT OR IGNORE INTO conversation (id, session_id, seq, role, content, platform, user_id, model, success, source, tools_json, tool_call_count, tokens_in, tokens_out, duration_ms, cost_usd, metadata_json)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
