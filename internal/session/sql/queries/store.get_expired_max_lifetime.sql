-- get_expired_max_lifetime returns sessions that exceeded max lifetime.
SELECT id FROM sessions WHERE state IN (?,?,?) AND expires_at IS NOT NULL AND expires_at <= ?;
