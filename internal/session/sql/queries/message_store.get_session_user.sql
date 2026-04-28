-- get_session_user returns the user ID of a session (fallback for pre-owner_id sessions).
SELECT user_id FROM sessions WHERE id = ?;
