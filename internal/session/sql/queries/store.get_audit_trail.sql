-- get_audit_trail returns the full audit trail for a session.
SELECT id, timestamp, action, actor_id, session_id, details, previous_hash, current_hash
 FROM audit_log WHERE session_id=? ORDER BY id ASC;
