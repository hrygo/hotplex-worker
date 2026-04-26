DELETE FROM events WHERE session_id IN (
	SELECT id FROM sessions
	WHERE state IN ('terminated', 'deleted')
	  AND updated_at <= ?
);
