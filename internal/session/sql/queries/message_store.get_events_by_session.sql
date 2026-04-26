-- get_events_by_session returns all event records for a session from a given sequence.
SELECT id, session_id, seq, event_type, payload_json, created_at
 FROM events WHERE session_id = ? AND seq >= ? ORDER BY seq ASC;
