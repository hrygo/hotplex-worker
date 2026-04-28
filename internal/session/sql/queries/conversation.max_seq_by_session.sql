SELECT session_id, MAX(seq) FROM conversation GROUP BY session_id;
