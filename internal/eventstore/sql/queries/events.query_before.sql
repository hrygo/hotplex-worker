SELECT session_id, seq, type, data, direction, source, created_at
FROM events
WHERE session_id = ? AND seq < ?
ORDER BY seq DESC
LIMIT ?
