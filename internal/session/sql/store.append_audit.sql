-- append_audit inserts a new audit log entry (hash updated after insert).
INSERT INTO audit_log (timestamp, action, actor_id, session_id, details, previous_hash, current_hash)
 VALUES (?, ?, ?, ?, ?, ?, '');
