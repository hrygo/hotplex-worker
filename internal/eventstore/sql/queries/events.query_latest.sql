SELECT session_id, seq, type, data, direction, created_at
FROM events
WHERE session_id = ?
ORDER BY seq DESC
LIMIT ?
