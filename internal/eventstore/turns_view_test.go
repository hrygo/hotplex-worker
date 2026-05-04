package eventstore

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/sqlutil"
)

func init() {
	_ = sqlutil.DriverName
}

func newTestStoreWithViews(t *testing.T) *SQLiteStore {
	t.Helper()
	store := newTestStore(t)

	// Create sessions table (needed for VIEW JOIN).
	_, err := store.db.Exec(`CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		platform TEXT NOT NULL DEFAULT '',
		owner_id TEXT,
		state TEXT NOT NULL DEFAULT 'running'
	)`)
	require.NoError(t, err)

	// Insert a test session for the VIEW to JOIN against.
	_, err = store.db.Exec(`INSERT INTO sessions (id, platform, owner_id) VALUES (?, ?, ?)`,
		"sess-view-test", "feishu", "ou_test")
	require.NoError(t, err)

	// Create VIEWs (from migration 003).
	_, err = store.db.Exec(`CREATE VIEW v_turns_user AS
SELECT
  e.session_id, e.seq, 'user' AS role,
  json_extract(e.data, '$.content') AS content,
  COALESCE(s.platform, '') AS platform,
  COALESCE(s.owner_id, '') AS user_id,
  '' AS model, NULL AS success, e.source,
  NULL AS tools_json, 0 AS tool_call_count,
  0 AS tokens_in, 0 AS tokens_out,
  0 AS duration_ms, 0.0 AS cost_usd, e.created_at
FROM events e
LEFT JOIN sessions s ON s.id = e.session_id
WHERE e.type = 'input' AND e.direction = 'inbound'`)
	require.NoError(t, err)

	_, err = store.db.Exec(`CREATE VIEW v_turns_assistant AS
SELECT
  d.session_id, d.seq, 'assistant' AS role,
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
  SELECT grouped.session_id, grouped.next_done_id,
    group_concat(json_extract(grouped.data, '$.content'), char(10)) AS content
  FROM (
    SELECT id, session_id, type, data,
      MIN(CASE WHEN type = 'done' THEN id END) OVER (
        PARTITION BY session_id ORDER BY id ROWS BETWEEN CURRENT ROW AND UNBOUNDED FOLLOWING
      ) AS next_done_id
    FROM events
    WHERE type IN ('message', 'done')
  ) grouped
  WHERE grouped.type = 'message' AND grouped.next_done_id IS NOT NULL
  GROUP BY grouped.session_id, grouped.next_done_id
) m ON m.session_id = d.session_id AND m.next_done_id = d.id
WHERE d.type = 'done' AND d.direction = 'outbound'`)
	require.NoError(t, err)

	_, err = store.db.Exec(`CREATE VIEW v_turns AS
SELECT * FROM v_turns_user
UNION ALL
SELECT * FROM v_turns_assistant
ORDER BY session_id, created_at, role DESC`)
	require.NoError(t, err)

	return store
}

func TestTurnsView_ThreeTurns(t *testing.T) {
	store := newTestStoreWithViews(t)
	ctx := context.Background()
	sid := "sess-view-test"
	now := time.Now().UnixMilli()

	// Turn 1: user input → AI message → done
	require.NoError(t, store.Append(ctx, &StoredEvent{SessionID: sid, Seq: 1, Type: "input", Data: raw(`{"content":"hello"}`), Direction: "inbound", Source: SourceNormal, CreatedAt: now}))
	require.NoError(t, store.Append(ctx, &StoredEvent{SessionID: sid, Seq: 2, Type: "message", Data: raw(`{"content":"hi there"}`), Direction: "outbound", Source: SourceNormal, CreatedAt: now + 1}))
	require.NoError(t, store.Append(ctx, &StoredEvent{SessionID: sid, Seq: 3, Type: "done", Data: raw(`{"success":true,"stats":{"_session":{"model_name":"claude-3","turn_input_tok":10,"turn_output_tok":20,"turn_duration_ms":500,"turn_cost_usd":0.01,"tool_call_count":0}}}`), Direction: "outbound", Source: SourceNormal, CreatedAt: now + 2}))

	// Turn 2: user input → AI message → done
	require.NoError(t, store.Append(ctx, &StoredEvent{SessionID: sid, Seq: 4, Type: "input", Data: raw(`{"content":"how are you"}`), Direction: "inbound", Source: SourceNormal, CreatedAt: now + 100}))
	require.NoError(t, store.Append(ctx, &StoredEvent{SessionID: sid, Seq: 5, Type: "message", Data: raw(`{"content":"I'm fine"}`), Direction: "outbound", Source: SourceNormal, CreatedAt: now + 101}))
	require.NoError(t, store.Append(ctx, &StoredEvent{SessionID: sid, Seq: 6, Type: "done", Data: raw(`{"success":true,"stats":{"_session":{"model_name":"claude-3","turn_input_tok":15,"turn_output_tok":30,"turn_duration_ms":600,"turn_cost_usd":0.02,"tool_call_count":1,"tool_names":{"Read":1}}}}`), Direction: "outbound", Source: SourceNormal, CreatedAt: now + 102}))

	// Turn 3: user input → AI message → done
	require.NoError(t, store.Append(ctx, &StoredEvent{SessionID: sid, Seq: 7, Type: "input", Data: raw(`{"content":"bye"}`), Direction: "inbound", Source: SourceNormal, CreatedAt: now + 200}))
	require.NoError(t, store.Append(ctx, &StoredEvent{SessionID: sid, Seq: 8, Type: "message", Data: raw(`{"content":"see you"}`), Direction: "outbound", Source: SourceNormal, CreatedAt: now + 201}))
	require.NoError(t, store.Append(ctx, &StoredEvent{SessionID: sid, Seq: 9, Type: "done", Data: raw(`{"success":true,"stats":{"_session":{"model_name":"claude-3","turn_input_tok":5,"turn_output_tok":10,"turn_duration_ms":300,"turn_cost_usd":0.005,"tool_call_count":0}}}`), Direction: "outbound", Source: SourceNormal, CreatedAt: now + 202}))

	t.Run("QueryTurns returns all 6 turns (3 user + 3 assistant)", func(t *testing.T) {
		records, err := store.QueryTurns(ctx, sid, 100, 0)
		require.NoError(t, err)
		require.Len(t, records, 6)

		// Order: user/assistant interleaved by created_at with user first
		require.Equal(t, "user", records[0].Role)
		require.Equal(t, "hello", records[0].Content)
		require.Equal(t, "assistant", records[1].Role)
		require.Equal(t, "hi there", records[1].Content)
		require.Equal(t, "user", records[2].Role)
		require.Equal(t, "how are you", records[2].Content)
		require.Equal(t, "assistant", records[3].Role)
		require.Equal(t, "I'm fine", records[3].Content)
		require.Equal(t, "user", records[4].Role)
		require.Equal(t, "bye", records[4].Content)
		require.Equal(t, "assistant", records[5].Role)
		require.Equal(t, "see you", records[5].Content)
	})

	t.Run("assistant turns have correct content for each turn", func(t *testing.T) {
		records, err := store.QueryTurns(ctx, sid, 100, 0)
		require.NoError(t, err)
		// Verify all 3 assistant turns have non-empty content (no lost turns)
		assistants := filterByRole(records, "assistant")
		require.Len(t, assistants, 3)
		require.Equal(t, "hi there", assistants[0].Content)
		require.Equal(t, "I'm fine", assistants[1].Content)
		require.Equal(t, "see you", assistants[2].Content)
	})

	t.Run("QueryTurnStats aggregates correctly", func(t *testing.T) {
		stats, err := store.QueryTurnStats(ctx, sid)
		require.NoError(t, err)
		require.Equal(t, 3, stats.TotalTurns)
		require.Equal(t, 3, stats.SuccessTurns)
		require.Equal(t, int64(30), stats.TotalTokIn)        // 10+15+5
		require.Equal(t, int64(60), stats.TotalTokOut)       // 20+30+10
		require.Equal(t, int64(1400), stats.TotalDurMs)      // 500+600+300
		require.InDelta(t, 0.035, stats.TotalCostUSD, 0.001) // 0.01+0.02+0.005
	})

	t.Run("QueryTurnsBefore cursor pagination", func(t *testing.T) {
		records, err := store.QueryTurnsBefore(ctx, sid, 6, 10)
		require.NoError(t, err)
		// seq<6: user(1), assistant(3), user(4) = 3 records
		require.Len(t, records, 3)
		require.Equal(t, "user", records[0].Role)
		require.Equal(t, "assistant", records[1].Role)
		require.Equal(t, "user", records[2].Role)
	})

	t.Run("no duplicates in results", func(t *testing.T) {
		records, err := store.QueryTurns(ctx, sid, 100, 0)
		require.NoError(t, err)
		seqs := map[int64]int{}
		for _, r := range records {
			if r.Role == "assistant" {
				seqs[r.Seq]++
			}
		}
		for seq, count := range seqs {
			require.Equal(t, 1, count, "duplicate assistant turn at seq=%d", seq)
		}
	})
}

func TestTurnsView_IncompleteTurn(t *testing.T) {
	store := newTestStoreWithViews(t)
	ctx := context.Background()
	sid := "sess-view-test"
	now := time.Now().UnixMilli()

	// Only input, no message or done — incomplete turn
	require.NoError(t, store.Append(ctx, &StoredEvent{SessionID: sid, Seq: 1, Type: "input", Data: raw(`{"content":"hello"}`), Direction: "inbound", Source: SourceNormal, CreatedAt: now}))
	// Message without done — should not appear in assistant view
	require.NoError(t, store.Append(ctx, &StoredEvent{SessionID: sid, Seq: 2, Type: "message", Data: raw(`{"content":"orphan"}`), Direction: "outbound", Source: SourceNormal, CreatedAt: now + 1}))

	t.Run("orphan message not in turns", func(t *testing.T) {
		records, err := store.QueryTurns(ctx, sid, 100, 0)
		require.NoError(t, err)
		require.Len(t, records, 1) // only user input
		require.Equal(t, "user", records[0].Role)
	})
}

func raw(s string) json.RawMessage { return json.RawMessage(s) }

func TestTurnsView_UserInputJoinsSession(t *testing.T) {
	store := newTestStoreWithViews(t)
	ctx := context.Background()
	sid := "sess-view-test"
	now := time.Now().UnixMilli()

	require.NoError(t, store.Append(ctx, &StoredEvent{SessionID: sid, Seq: 1, Type: "input", Data: raw(`{"content":"hello"}`), Direction: "inbound", Source: SourceNormal, CreatedAt: now}))
	require.NoError(t, store.Append(ctx, &StoredEvent{SessionID: sid, Seq: 2, Type: "message", Data: raw(`{"content":"hi"}`), Direction: "outbound", Source: SourceNormal, CreatedAt: now + 1}))
	require.NoError(t, store.Append(ctx, &StoredEvent{SessionID: sid, Seq: 3, Type: "done", Data: raw(`{"success":true}`), Direction: "outbound", Source: SourceNormal, CreatedAt: now + 2}))

	records, err := store.QueryTurns(ctx, sid, 100, 0)
	require.NoError(t, err)
	require.Len(t, records, 2)

	user := records[0]
	require.Equal(t, "user", user.Role)
	require.Equal(t, "feishu", user.Platform)
	require.Equal(t, "ou_test", user.UserID)
}

func TestTurnsView_CrashSource(t *testing.T) {
	store := newTestStoreWithViews(t)
	ctx := context.Background()
	sid := "sess-view-test"
	now := time.Now().UnixMilli()

	require.NoError(t, store.Append(ctx, &StoredEvent{SessionID: sid, Seq: 1, Type: "input", Data: raw(`{"content":"do work"}`), Direction: "inbound", Source: SourceNormal, CreatedAt: now}))
	require.NoError(t, store.Append(ctx, &StoredEvent{SessionID: sid, Seq: 2, Type: "done", Data: raw(`{"success":false,"reason":"worker_crash","message":"Worker crashed with exit code 1","synthetic":true}`), Direction: "outbound", Source: SourceCrash, CreatedAt: now + 1}))

	records, err := store.QueryTurns(ctx, sid, 100, 0)
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, SourceCrash, records[1].Source)

	stats, err := store.QueryTurnStats(ctx, sid)
	require.NoError(t, err)
	require.Equal(t, 1, stats.FailedTurns)
	require.Equal(t, SourceCrash, stats.Turns[0].Source)
}

func TestTurnsView_TimeoutSource(t *testing.T) {
	store := newTestStoreWithViews(t)
	ctx := context.Background()
	sid := "sess-view-test"
	now := time.Now().UnixMilli()

	require.NoError(t, store.Append(ctx, &StoredEvent{SessionID: sid, Seq: 1, Type: "input", Data: raw(`{"content":"long task"}`), Direction: "inbound", Source: SourceNormal, CreatedAt: now}))
	require.NoError(t, store.Append(ctx, &StoredEvent{SessionID: sid, Seq: 2, Type: "done", Data: raw(`{"success":false,"reason":"turn_timeout","message":"Turn exceeded 5m time limit","synthetic":true}`), Direction: "outbound", Source: SourceTimeout, CreatedAt: now + 1}))

	records, err := store.QueryTurns(ctx, sid, 100, 0)
	require.NoError(t, err)
	require.Equal(t, SourceTimeout, records[1].Source)
}

func TestTurnsView_FreshStartSource(t *testing.T) {
	store := newTestStoreWithViews(t)
	ctx := context.Background()
	sid := "sess-view-test"
	now := time.Now().UnixMilli()

	require.NoError(t, store.Append(ctx, &StoredEvent{SessionID: sid, Seq: 1, Type: "input", Data: raw(`{"content":"retry"}`), Direction: "inbound", Source: SourceNormal, CreatedAt: now}))
	require.NoError(t, store.Append(ctx, &StoredEvent{SessionID: sid, Seq: 2, Type: "done", Data: raw(`{"success":false,"reason":"fresh_start","message":"Session restarted with context reset","synthetic":true}`), Direction: "outbound", Source: SourceFreshStart, CreatedAt: now + 1}))

	records, err := store.QueryTurns(ctx, sid, 100, 0)
	require.NoError(t, err)
	require.Equal(t, SourceFreshStart, records[1].Source)
}

func TestQueryTurns_Pagination(t *testing.T) {
	store := newTestStoreWithViews(t)
	ctx := context.Background()
	sid := "sess-view-test"
	now := time.Now().UnixMilli()

	// Insert 5 complete turns (10 events: 5 input + 5 done)
	for i := 0; i < 5; i++ {
		base := int64(i * 100)
		require.NoError(t, store.Append(ctx, &StoredEvent{SessionID: sid, Seq: base + 1, Type: "input", Data: raw(`{"content":"q"}`), Direction: "inbound", Source: SourceNormal, CreatedAt: now + base*10}))
		require.NoError(t, store.Append(ctx, &StoredEvent{SessionID: sid, Seq: base + 2, Type: "done", Data: raw(`{"success":true}`), Direction: "outbound", Source: SourceNormal, CreatedAt: now + base*10 + 1}))
	}

	t.Run("page 1", func(t *testing.T) {
		records, err := store.QueryTurns(ctx, sid, 4, 0)
		require.NoError(t, err)
		require.Len(t, records, 4)
	})

	t.Run("page 2", func(t *testing.T) {
		records, err := store.QueryTurns(ctx, sid, 4, 4)
		require.NoError(t, err)
		require.Len(t, records, 4)
	})

	t.Run("page 3 partial", func(t *testing.T) {
		records, err := store.QueryTurns(ctx, sid, 4, 8)
		require.NoError(t, err)
		require.Len(t, records, 2) // only 2 records left
	})

	t.Run("offset beyond data", func(t *testing.T) {
		_, err := store.QueryTurns(ctx, sid, 10, 100)
		require.ErrorIs(t, err, ErrNotFound)
	})
}

func TestHistoryAPI_BackwardCompatible(t *testing.T) {
	store := newTestStoreWithViews(t)
	ctx := context.Background()
	sid := "sess-view-test"
	now := time.Now().UnixMilli()

	require.NoError(t, store.Append(ctx, &StoredEvent{SessionID: sid, Seq: 1, Type: "input", Data: raw(`{"content":"hello"}`), Direction: "inbound", Source: SourceNormal, CreatedAt: now}))
	require.NoError(t, store.Append(ctx, &StoredEvent{SessionID: sid, Seq: 2, Type: "done", Data: raw(`{"success":true,"stats":{"_session":{"model_name":"claude-3"}}}`), Direction: "outbound", Source: SourceNormal, CreatedAt: now + 1}))

	records, err := store.QueryTurns(ctx, sid, 100, 0)
	require.NoError(t, err)
	require.Len(t, records, 2)

	// Verify JSON serialization matches API contract
	user := records[0]
	b, err := json.Marshal(user)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))

	// Required fields present
	require.Contains(t, m, "seq")
	require.Contains(t, m, "role")
	require.Contains(t, m, "content")
	require.Contains(t, m, "source")
	require.Contains(t, m, "created_at")
	require.Equal(t, "user", m["role"])
	require.Equal(t, "hello", m["content"])

	assistant := records[1]
	b, err = json.Marshal(assistant)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(b, &m))
	require.Equal(t, "assistant", m["role"])
	require.Contains(t, m, "success")
	require.Contains(t, m, "model")
}

func filterByRole(records []*TurnRecord, role string) []*TurnRecord {
	var filtered []*TurnRecord
	for _, r := range records {
		if r.Role == role {
			filtered = append(filtered, r)
		}
	}
	return filtered
}
