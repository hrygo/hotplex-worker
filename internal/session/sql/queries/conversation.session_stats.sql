-- session_stats returns per-turn metrics for a session.
SELECT seq, role, success, duration_ms, cost_usd, tokens_in, tokens_out, model, tools_json, source, created_at
 FROM conversation WHERE session_id = ? ORDER BY seq ASC;
