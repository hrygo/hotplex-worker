-- Returns done events for a session ordered by sequence.
SELECT seq, payload_json, created_at
  FROM events
 WHERE session_id = ? AND event_type = 'done'
 ORDER BY seq ASC;
