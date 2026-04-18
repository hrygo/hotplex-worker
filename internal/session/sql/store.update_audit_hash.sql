-- update_audit_hash updates the hash of a newly inserted audit entry (two-phase write).
UPDATE audit_log SET current_hash=? WHERE id=?;
