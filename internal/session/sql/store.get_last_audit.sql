-- get_last_audit returns the last audit entry for hash chaining.
SELECT id, current_hash FROM audit_log ORDER BY id DESC LIMIT 1;
