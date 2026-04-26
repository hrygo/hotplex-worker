-- get_session_owner returns the owner ID of a session.
SELECT COALESCE(owner_id, user_id) FROM sessions WHERE id = ?;
