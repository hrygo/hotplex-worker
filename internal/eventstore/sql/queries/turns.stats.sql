SELECT session_id, seq, role, success, source, tools_json, tool_call_count, tokens_in, tokens_out, duration_ms, cost_usd, model, created_at
FROM v_turns_assistant
WHERE session_id = ?
ORDER BY created_at ASC
