-- get_expired_idle returns sessions that exceeded idle timeout.
SELECT id FROM sessions WHERE state=? AND idle_expires_at IS NOT NULL AND idle_expires_at <= ?;
