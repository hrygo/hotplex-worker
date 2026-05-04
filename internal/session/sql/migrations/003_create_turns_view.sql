-- +goose Up

-- User input view
CREATE VIEW v_turns_user AS
SELECT
  e.session_id,
  e.seq,
  'user' AS role,
  json_extract(e.data, '$.content') AS content,
  COALESCE(s.platform, '') AS platform,
  COALESCE(s.owner_id, '') AS user_id,
  '' AS model,
  NULL AS success,
  e.source,
  NULL AS tools_json,
  0 AS tool_call_count,
  0 AS tokens_in,
  0 AS tokens_out,
  0 AS duration_ms,
  0.0 AS cost_usd,
  e.created_at
FROM events e
LEFT JOIN sessions s ON s.id = e.session_id
WHERE e.type = 'input' AND e.direction = 'inbound';

-- AI response view (uses window function for O(n log n) turn boundary detection)
CREATE VIEW v_turns_assistant AS
SELECT
  d.session_id,
  d.seq,
  'assistant' AS role,
  COALESCE(m.content, '') AS content,
  COALESCE(s.platform, '') AS platform,
  COALESCE(s.owner_id, '') AS user_id,
  COALESCE(json_extract(d.data, '$.stats._session.model_name'), '') AS model,
  json_extract(d.data, '$.success') AS success,
  d.source,
  json_extract(d.data, '$.stats._session.tool_names') AS tools_json,
  COALESCE(json_extract(d.data, '$.stats._session.tool_call_count'), 0) AS tool_call_count,
  COALESCE(json_extract(d.data, '$.stats._session.turn_input_tok'), 0) AS tokens_in,
  COALESCE(json_extract(d.data, '$.stats._session.turn_output_tok'), 0) AS tokens_out,
  COALESCE(json_extract(d.data, '$.stats._session.turn_duration_ms'), 0) AS duration_ms,
  COALESCE(json_extract(d.data, '$.stats._session.turn_cost_usd'), 0.0) AS cost_usd,
  d.created_at
FROM events d
LEFT JOIN sessions s ON s.id = d.session_id
LEFT JOIN (
  SELECT m.session_id, MAX(m.id) AS id,
    group_concat(json_extract(m.data, '$.content'), char(10)) AS content
  FROM (
    SELECT id, session_id, data,
      MAX(CASE WHEN type = 'done' THEN id END) OVER (
        PARTITION BY session_id ORDER BY id ROWS UNBOUNDED PRECEDING
      ) AS turn_end_id
    FROM events
    WHERE type IN ('message', 'done')
  ) grouped
  JOIN (SELECT DISTINCT session_id, id AS done_id FROM events WHERE type = 'done') done
    ON done.session_id = grouped.session_id AND done.done_id = grouped.turn_end_id
  WHERE grouped.type = 'message'
  GROUP BY grouped.session_id, grouped.turn_end_id
) m ON m.session_id = d.session_id AND m.id < d.id
WHERE d.type = 'done' AND d.direction = 'outbound';

-- Merged view
CREATE VIEW v_turns AS
SELECT * FROM v_turns_user
UNION ALL
SELECT * FROM v_turns_assistant
ORDER BY session_id, created_at, CASE role WHEN 'user' THEN 0 ELSE 1 END;

-- +goose Down
DROP VIEW IF EXISTS v_turns;
DROP VIEW IF EXISTS v_turns_assistant;
DROP VIEW IF EXISTS v_turns_user;
