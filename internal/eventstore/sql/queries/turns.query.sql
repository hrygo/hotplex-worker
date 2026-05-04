SELECT session_id, seq, role, content, platform, user_id, model, success, source, tools_json, tool_call_count, tokens_in, tokens_out, duration_ms, cost_usd, created_at
FROM v_turns
WHERE session_id = ?
ORDER BY created_at ASC
LIMIT ? OFFSET ?
