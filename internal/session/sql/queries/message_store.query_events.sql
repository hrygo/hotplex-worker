-- query_events returns events as Envelopes for a session from a given sequence.
SELECT id, session_id, seq, event_type, payload_json
 FROM events WHERE session_id = ? AND seq > ? ORDER BY seq ASC;
