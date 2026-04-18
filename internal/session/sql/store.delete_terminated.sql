-- delete_terminated deletes terminated sessions older than cutoff.
DELETE FROM sessions WHERE state=? AND updated_at <= ?;
