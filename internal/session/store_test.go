package session

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/pkg/events"
)

// helperDB creates a real SQLiteStore for integration tests.
func helperDB(t *testing.T) *SQLiteStore {
	t.Helper()
	cfg := config.Default()
	cfg.DB.Path = filepath.Join(t.TempDir(), "test.db")
	cfg.DB.WALMode = true

	store, err := NewSQLiteStore(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// helperStoreWithMsg creates a Store and MessageStore sharing the same DB.
// The Store runs migrations (CREATE TABLE), so the MessageStore has tables to use.
func helperStoreWithMsg(t *testing.T) (*SQLiteStore, *SQLiteMessageStore) {
	t.Helper()
	cfg := config.Default()
	cfg.DB.Path = filepath.Join(t.TempDir(), "shared_test.db")
	cfg.DB.WALMode = true

	store, err := NewSQLiteStore(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	ms, err := NewSQLiteMessageStore(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ms.Close() })
	return store, ms
}

func helperUpsert(t *testing.T, store *SQLiteStore, id, userID string, state events.SessionState) {
	t.Helper()
	now := time.Now()
	err := store.Upsert(context.Background(), &SessionInfo{
		ID:         id,
		UserID:     userID,
		WorkerType: "claude_code",
		State:      state,
		CreatedAt:  now,
		UpdatedAt:  now,
	})
	require.NoError(t, err)
}

// ─── SQLiteStore: DeletePhysical ─────────────────────────────────────────────

func TestSQLiteStore_DeletePhysical(t *testing.T) {
	store := helperDB(t)
	ctx := context.Background()

	helperUpsert(t, store, "sess_del_phys", "user1", events.StateTerminated)

	err := store.DeletePhysical(ctx, "sess_del_phys")
	require.NoError(t, err)

	_, err = store.Get(ctx, "sess_del_phys")
	require.Error(t, err)
}

func TestSQLiteStore_DeletePhysical_NotFound(t *testing.T) {
	store := helperDB(t)

	err := store.DeletePhysical(context.Background(), "nonexistent")
	require.NoError(t, err)
}

// ─── SQLiteStore: DeleteExpiredEvents ────────────────────────────────────────

func TestSQLiteStore_DeleteExpiredEvents(t *testing.T) {
	store, ms := helperStoreWithMsg(t)
	ctx := context.Background()

	// DeleteExpiredEvents only deletes events for terminated/deleted sessions
	helperUpsert(t, store, "sess_evt", "user1", events.StateTerminated)

	err := ms.Append(ctx, "sess_evt", 1, "message.delta", []byte(`{"text":"hi"}`))
	require.NoError(t, err)
	time.Sleep(200 * time.Millisecond)

	// Cutoff in the future — events should be deleted
	n, err := store.DeleteExpiredEvents(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.GreaterOrEqual(t, n, int64(1))

	// After deletion, querying again returns 0
	n, err = store.DeleteExpiredEvents(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.Equal(t, int64(0), n)
}

// ─── SQLiteStore: Compact ────────────────────────────────────────────────────

func TestSQLiteStore_Compact_BelowThreshold(t *testing.T) {
	store := helperDB(t)
	ctx := context.Background()

	err := store.Compact(ctx, 0.99)
	require.NoError(t, err)
}

// ─── SQLiteStore: AppendAudit / GetAuditTrail ────────────────────────────────

func TestSQLiteStore_AppendAudit_GetAuditTrail(t *testing.T) {
	store := helperDB(t)
	ctx := context.Background()
	helperUpsert(t, store, "sess_audit", "user1", events.StateRunning)

	err := store.AppendAudit(ctx, "session.create", "actor1", "sess_audit", map[string]any{"source": "test"})
	require.NoError(t, err)

	err = store.AppendAudit(ctx, "session.transition", "actor1", "sess_audit", map[string]any{"from": "created", "to": "running"})
	require.NoError(t, err)

	records, err := store.GetAuditTrail(ctx, "sess_audit")
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, "session.create", records[0].Action)
	require.Equal(t, "session.transition", records[1].Action)
	require.Equal(t, "actor1", records[0].ActorID)
	require.Equal(t, "sess_audit", records[0].SessionID)
}

func TestSQLiteStore_AppendAudit_NilDetails(t *testing.T) {
	store := helperDB(t)
	ctx := context.Background()
	helperUpsert(t, store, "sess_audit_nil", "user1", events.StateRunning)

	err := store.AppendAudit(ctx, "session.create", "actor1", "sess_audit_nil", nil)
	require.NoError(t, err)

	records, err := store.GetAuditTrail(ctx, "sess_audit_nil")
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Nil(t, records[0].Details)
}

func TestSQLiteStore_GetAuditTrail_Empty(t *testing.T) {
	store := helperDB(t)
	ctx := context.Background()

	records, err := store.GetAuditTrail(ctx, "nonexistent")
	require.NoError(t, err)
	require.Len(t, records, 0)
}

// ─── SQLiteStore: Upsert with Context and PlatformKey ────────────────────────

func TestSQLiteStore_Upsert_WithContext(t *testing.T) {
	store := helperDB(t)
	ctx := context.Background()

	info := &SessionInfo{
		ID:         "sess_ctx",
		UserID:     "user1",
		WorkerType: "claude_code",
		State:      events.StateCreated,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
		Context:    map[string]any{"thread_id": "1234.56", "channel": "C123"},
		PlatformKey: map[string]string{
			"team_id":    "T123",
			"channel_id": "C123",
			"thread_ts":  "1234.56",
			"user_id":    "U123",
		},
	}
	err := store.Upsert(ctx, info)
	require.NoError(t, err)

	got, err := store.Get(ctx, "sess_ctx")
	require.NoError(t, err)
	require.Equal(t, "user1", got.UserID)

	ctxJSON, _ := json.Marshal(got.Context)
	require.Contains(t, string(ctxJSON), "thread_id")

	require.NotNil(t, got.PlatformKey)
	require.Equal(t, "T123", got.PlatformKey["team_id"])
}

// ─── SQLiteStore: List with pagination ───────────────────────────────────────

func TestSQLiteStore_List_DefaultLimit(t *testing.T) {
	store := helperDB(t)
	ctx := context.Background()

	helperUpsert(t, store, "sess_list1", "user1", events.StateRunning)
	helperUpsert(t, store, "sess_list2", "user1", events.StateIdle)

	// limit=0 should default to 100
	sessions, err := store.List(ctx, "", "", 0, 0)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(sessions), 2)
}

// ─── SQLiteStore: GetExpiredMaxLifetime / GetExpiredIdle ──────────────────────

func TestSQLiteStore_GetExpiredMaxLifetime(t *testing.T) {
	store := helperDB(t)
	ctx := context.Background()

	now := time.Now()
	info := &SessionInfo{
		ID:         "sess_expired",
		UserID:     "user1",
		WorkerType: "claude_code",
		State:      events.StateRunning,
		CreatedAt:  now,
		UpdatedAt:  now,
		ExpiresAt:  &now,
	}
	err := store.Upsert(ctx, info)
	require.NoError(t, err)

	ids, err := store.GetExpiredMaxLifetime(ctx, now.Add(time.Second))
	require.NoError(t, err)
	require.Contains(t, ids, "sess_expired")
}

func TestSQLiteStore_GetExpiredIdle(t *testing.T) {
	store := helperDB(t)
	ctx := context.Background()

	past := time.Now().Add(-2 * time.Hour)
	info := &SessionInfo{
		ID:            "sess_idle_exp",
		UserID:        "user1",
		WorkerType:    "claude_code",
		State:         events.StateIdle,
		CreatedAt:     past,
		UpdatedAt:     past,
		IdleExpiresAt: &past,
	}
	err := store.Upsert(ctx, info)
	require.NoError(t, err)

	ids, err := store.GetExpiredIdle(ctx, time.Now())
	require.NoError(t, err)
	require.Contains(t, ids, "sess_idle_exp")
}

// ─── SQLiteStore: DeleteTerminated ───────────────────────────────────────────

func TestSQLiteStore_DeleteTerminated(t *testing.T) {
	store := helperDB(t)
	ctx := context.Background()

	helperUpsert(t, store, "sess_term", "user1", events.StateTerminated)

	cutoff := time.Now().Add(time.Hour)
	err := store.DeleteTerminated(ctx, cutoff)
	require.NoError(t, err)
}

// ─── SQLiteStore: GetSessionsByState ─────────────────────────────────────────

func TestSQLiteStore_GetSessionsByState(t *testing.T) {
	store := helperDB(t)
	ctx := context.Background()

	helperUpsert(t, store, "sess_state_r", "user1", events.StateRunning)
	helperUpsert(t, store, "sess_state_i", "user1", events.StateIdle)

	ids, err := store.GetSessionsByState(ctx, events.StateRunning)
	require.NoError(t, err)
	require.Contains(t, ids, "sess_state_r")
	require.NotContains(t, ids, "sess_state_i")
}

// ─── MessageStore: Append + GetBySession + Query ─────────────────────────────

func TestMessageStore_Append_GetBySession(t *testing.T) {
	_, ms := helperStoreWithMsg(t)
	ctx := context.Background()

	err := ms.Append(ctx, "sess_msg", 1, "message.delta", []byte(`{"text":"hello"}`))
	require.NoError(t, err)
	err = ms.Append(ctx, "sess_msg", 2, "message.delta", []byte(`{"text":"world"}`))
	require.NoError(t, err)
	time.Sleep(200 * time.Millisecond)

	records, err := ms.GetBySession(ctx, "sess_msg", 0)
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, int64(1), records[0].Seq)
	require.Equal(t, "message.delta", records[0].EventType)
	require.Equal(t, "sess_msg", records[0].SessionID)
}

func TestMessageStore_GetBySession_Empty(t *testing.T) {
	_, ms := helperStoreWithMsg(t)
	ctx := context.Background()

	records, err := ms.GetBySession(ctx, "nonexistent", 0)
	require.NoError(t, err)
	require.Len(t, records, 0)
}

func TestMessageStore_Query(t *testing.T) {
	_, ms := helperStoreWithMsg(t)
	ctx := context.Background()

	err := ms.Append(ctx, "sess_query", 1, "message.delta", []byte(`{"text":"hi"}`))
	require.NoError(t, err)
	time.Sleep(200 * time.Millisecond)

	envelopes, err := ms.Query(ctx, "sess_query", 0)
	require.NoError(t, err)
	require.Len(t, envelopes, 1)
	require.Equal(t, int64(1), envelopes[0].Seq)
	require.Equal(t, events.Kind("message.delta"), envelopes[0].Event.Type)
}

func TestMessageStore_Query_Empty(t *testing.T) {
	_, ms := helperStoreWithMsg(t)
	ctx := context.Background()

	envelopes, err := ms.Query(ctx, "nonexistent", 0)
	require.NoError(t, err)
	require.Len(t, envelopes, 0)
}

func TestMessageStore_Append_DuplicateSeq_Idempotent(t *testing.T) {
	_, ms := helperStoreWithMsg(t)
	ctx := context.Background()

	err := ms.Append(ctx, "sess_dup", 1, "message.delta", []byte(`{"text":"first"}`))
	require.NoError(t, err)
	// Same seq — silently ignored (INSERT OR IGNORE)
	err = ms.Append(ctx, "sess_dup", 1, "message.delta", []byte(`{"text":"second"}`))
	require.NoError(t, err)
	time.Sleep(200 * time.Millisecond)

	records, err := ms.GetBySession(ctx, "sess_dup", 0)
	require.NoError(t, err)
	require.Len(t, records, 1) // only one record despite two appends
	require.Equal(t, `{"text":"first"}`, string(records[0].Payload))
}

// ─── MessageStore: GetOwner ──────────────────────────────────────────────────

func TestMessageStore_GetOwner(t *testing.T) {
	store, ms := helperStoreWithMsg(t)
	ctx := context.Background()

	helperUpsert(t, store, "sess_owner", "user1", events.StateRunning)

	ownerID, err := ms.GetOwner(ctx, "sess_owner")
	require.NoError(t, err)
	require.Equal(t, "user1", ownerID)
}

func TestMessageStore_GetOwner_NotFound(t *testing.T) {
	_, ms := helperStoreWithMsg(t)
	ctx := context.Background()

	_, err := ms.GetOwner(ctx, "nonexistent")
	require.ErrorIs(t, err, ErrSessionNotFound)
}

// ─── Stores: NewMessageStore ─────────────────────────────────────────────────

func TestNewMessageStore_DefaultType(t *testing.T) {
	cfg := config.Default()
	cfg.DB.Path = filepath.Join(t.TempDir(), "new_msg.db")

	ms, err := NewMessageStore(context.Background(), cfg)
	require.NoError(t, err)
	require.NotNil(t, ms)
	_ = ms.Close()
}

func TestNewMessageStore_UnknownType(t *testing.T) {
	cfg := config.Default()
	cfg.Session.EventStoreType = "unknown"

	_, err := NewMessageStore(context.Background(), cfg)
	require.ErrorIs(t, err, ErrMessageStoreTypeUnknown)
}

func TestKnownStoreTypes(t *testing.T) {
	types := knownStoreTypes()
	require.Contains(t, types, StoreTypeSQLite)
}

// ─── PostgresMessageStore stubs ───────────────────────────────────────────────

func TestPostgresMessageStore_Stubs(t *testing.T) {
	ms := &PostgresMessageStore{}
	ctx := context.Background()

	_, err := NewPostgresMessageStore(ctx, "")
	require.ErrorIs(t, err, ErrNotImplemented)

	require.ErrorIs(t, ms.Append(ctx, "", 0, "", nil), ErrNotImplemented)
	_, err = ms.GetBySession(ctx, "", 0)
	require.ErrorIs(t, err, ErrNotImplemented)
	_, err = ms.GetOwner(ctx, "")
	require.ErrorIs(t, err, ErrNotImplemented)
	_, err = ms.Query(ctx, "", 0)
	require.ErrorIs(t, err, ErrNotImplemented)
	require.ErrorIs(t, ms.Close(), ErrNotImplemented)
	_, err = ms.SessionStats(ctx, "")
	require.ErrorIs(t, err, ErrNotImplemented)
}

func TestMessageStore_SessionStats(t *testing.T) {
	_, ms := helperStoreWithMsg(t)
	ctx := context.Background()
	sessID := "sess_stats_test"

	// Append two done events simulating two turns.
	done1 := `{"event":{"type":"done","data":{"success":true,"dropped":false,"stats":{"duration_ms":3200,"duration_api_ms":2100,"total_cost_usd":0.016,"usage":{"input_tokens":1500,"output_tokens":620},"model_usage":{"claude-sonnet-4-6":{"input_tokens":1500,"output_tokens":620}}}}}}`
	done2 := `{"event":{"type":"done","data":{"success":true,"dropped":true,"stats":{"duration_ms":5100,"duration_api_ms":3400,"total_cost_usd":0.028,"usage":{"input_tokens":2800,"output_tokens":940},"model_usage":{"claude-sonnet-4-6":{"input_tokens":2800,"output_tokens":940}}}}}}`

	require.NoError(t, ms.Append(ctx, sessID, 10, "done", []byte(done1)))
	require.NoError(t, ms.Append(ctx, sessID, 20, "done", []byte(done2)))
	require.NoError(t, ms.Append(ctx, sessID, 15, "message.delta", []byte(`{}`))) // non-done, ignored

	time.Sleep(200 * time.Millisecond)

	stats, err := ms.SessionStats(ctx, sessID)
	require.NoError(t, err)
	require.Equal(t, 2, stats.TotalTurns)
	require.Equal(t, 2, stats.SuccessTurns)
	require.Equal(t, 0, stats.FailedTurns)
	require.Equal(t, 1, stats.DroppedTurns)
	require.Equal(t, int64(8300), stats.TotalDurationMs)
	require.Equal(t, int64(5500), stats.TotalAPIDuration)
	require.InDelta(t, 0.044, stats.TotalCostUSD, 0.001)
	require.InDelta(t, 4300.0, stats.TotalUsage["input_tokens"], 0.01)
	require.InDelta(t, 1560.0, stats.TotalUsage["output_tokens"], 0.01)
	require.Equal(t, 2, len(stats.Turns))
	require.Equal(t, int64(10), stats.Turns[0].Seq)
	require.Equal(t, int64(20), stats.Turns[1].Seq)

	// Model usage aggregation
	require.InDelta(t, 4300.0, stats.TotalModelUsage["claude-sonnet-4-6"]["input_tokens"], 0.01)
}

func TestMessageStore_SessionStats_Empty(t *testing.T) {
	_, ms := helperStoreWithMsg(t)
	ctx := context.Background()

	_, err := ms.SessionStats(ctx, "nonexistent")
	require.ErrorIs(t, err, ErrEventNotFound)
}

func TestMessageStore_SessionStats_FailedTurn(t *testing.T) {
	_, ms := helperStoreWithMsg(t)
	ctx := context.Background()
	sessID := "sess_stats_fail"

	doneFail := `{"event":{"type":"done","data":{"success":false,"stats":{"duration_ms":1000,"usage":{"input_tokens":500}}}}}`
	require.NoError(t, ms.Append(ctx, sessID, 5, "done", []byte(doneFail)))
	time.Sleep(200 * time.Millisecond)

	stats, err := ms.SessionStats(ctx, sessID)
	require.NoError(t, err)
	require.Equal(t, 1, stats.TotalTurns)
	require.Equal(t, 0, stats.SuccessTurns)
	require.Equal(t, 1, stats.FailedTurns)
	require.False(t, stats.Turns[0].Success)
	require.InDelta(t, 500.0, stats.TotalUsage["input_tokens"], 0.01)
}

// ─── ConversationStore helpers ─────────────────────────────────────────────────

func helperStoreWithConv(t *testing.T) (*SQLiteStore, *SQLiteConversationStore) {
	t.Helper()
	cfg := config.Default()
	cfg.DB.Path = filepath.Join(t.TempDir(), "conv_test.db")
	cfg.DB.WALMode = true

	store, err := NewSQLiteStore(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	cs, err := NewSQLiteConversationStore(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })
	return store, cs
}

// ─── ConversationStore: Append + GetBySession ─────────────────────────────────

func TestConversationStore_Append_GetBySession(t *testing.T) {
	_, cs := helperStoreWithConv(t)
	ctx := context.Background()

	success := true
	require.NoError(t, cs.Append(ctx, &ConversationRecord{
		SessionID: "sess_conv", Seq: 1, Role: RoleUser, Content: "hello",
		Platform: "slack", UserID: "user1",
	}))
	require.NoError(t, cs.Append(ctx, &ConversationRecord{
		SessionID: "sess_conv", Seq: 2, Role: RoleAssistant, Content: "hi there",
		Model: "claude-sonnet-4-6", Success: &success, Source: SourceNormal,
		TokensIn: 100, TokensOut: 50, DurationMs: 3200, CostUSD: 0.01,
	}))
	time.Sleep(200 * time.Millisecond)

	records, err := cs.GetBySession(ctx, "sess_conv", 100, 0)
	require.NoError(t, err)
	require.Len(t, records, 2)
	require.Equal(t, int64(1), records[0].Seq)
	require.Equal(t, RoleUser, records[0].Role)
	require.Equal(t, "hello", records[0].Content)
	require.Equal(t, RoleAssistant, records[1].Role)
	require.Equal(t, "claude-sonnet-4-6", records[1].Model)
	require.True(t, *records[1].Success)
	require.Equal(t, int64(3200), records[1].DurationMs)
}

func TestConversationStore_Append_WithToolsAndMeta(t *testing.T) {
	_, cs := helperStoreWithConv(t)
	ctx := context.Background()

	require.NoError(t, cs.Append(ctx, &ConversationRecord{
		SessionID: "sess_tools", Seq: 1, Role: RoleAssistant, Content: "done",
		Tools: map[string]int{"Read": 3, "Edit": 1}, ToolCallCount: 4,
		Metadata: map[string]any{"key": "value"},
	}))
	time.Sleep(200 * time.Millisecond)

	records, err := cs.GetBySession(ctx, "sess_tools", 100, 0)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, 4, records[0].ToolCallCount)
	// Metadata round-trips as map[string]any
	require.NotNil(t, records[0].Metadata)
	require.Equal(t, "value", records[0].Metadata["key"])
}

func TestConversationStore_GetBySession_NotFound(t *testing.T) {
	_, cs := helperStoreWithConv(t)
	ctx := context.Background()

	_, err := cs.GetBySession(ctx, "nonexistent", 100, 0)
	require.ErrorIs(t, err, ErrConvNotFound)
}

func TestConversationStore_Append_AutoID(t *testing.T) {
	_, cs := helperStoreWithConv(t)
	ctx := context.Background()

	require.NoError(t, cs.Append(ctx, &ConversationRecord{
		SessionID: "sess_autoid", Seq: 5, Role: RoleUser, Content: "test",
	}))
	time.Sleep(200 * time.Millisecond)

	records, err := cs.GetBySession(ctx, "sess_autoid", 100, 0)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "conv_sess_autoid_5", records[0].ID)
}

// ─── ConversationStore: DeleteBySession ────────────────────────────────────────

func TestConversationStore_DeleteBySession(t *testing.T) {
	_, cs := helperStoreWithConv(t)
	ctx := context.Background()

	require.NoError(t, cs.Append(ctx, &ConversationRecord{
		SessionID: "sess_del", Seq: 1, Role: RoleUser, Content: "bye",
	}))
	time.Sleep(200 * time.Millisecond)

	require.NoError(t, cs.DeleteBySession(ctx, "sess_del"))

	_, err := cs.GetBySession(ctx, "sess_del", 100, 0)
	require.ErrorIs(t, err, ErrConvNotFound)
}

// ─── ConversationStore: DeleteExpired ──────────────────────────────────────────

func TestConversationStore_DeleteExpired(t *testing.T) {
	_, cs := helperStoreWithConv(t)
	ctx := context.Background()

	require.NoError(t, cs.Append(ctx, &ConversationRecord{
		SessionID: "sess_exp", Seq: 1, Role: RoleUser, Content: "old",
	}))
	time.Sleep(200 * time.Millisecond)

	n, err := cs.DeleteExpired(ctx, time.Now().Add(time.Hour))
	require.NoError(t, err)
	require.GreaterOrEqual(t, n, int64(1))

	_, err = cs.GetBySession(ctx, "sess_exp", 100, 0)
	require.ErrorIs(t, err, ErrConvNotFound)
}

func TestConversationStore_DeleteExpired_NoMatch(t *testing.T) {
	_, cs := helperStoreWithConv(t)
	ctx := context.Background()

	n, err := cs.DeleteExpired(ctx, time.Now().Add(-time.Hour))
	require.NoError(t, err)
	require.Equal(t, int64(0), n)
}

// ─── ConversationStore: cascade delete via Store ──────────────────────────────

func TestConversationStore_CascadeDelete_Physical(t *testing.T) {
	store, cs := helperStoreWithConv(t)
	ctx := context.Background()
	sessID := "sess_cascade"

	helperUpsert(t, store, sessID, "user1", events.StateRunning)

	require.NoError(t, cs.Append(ctx, &ConversationRecord{
		SessionID: sessID, Seq: 1, Role: RoleUser, Content: "test cascade",
	}))
	time.Sleep(200 * time.Millisecond)

	require.NoError(t, store.DeletePhysical(ctx, sessID))

	_, err := cs.GetBySession(ctx, sessID, 100, 0)
	require.ErrorIs(t, err, ErrConvNotFound)
}

// ─── Pool: UpdateLimits ──────────────────────────────────────────────────────

func TestPoolUpdateLimits(t *testing.T) {
	pool := NewPoolManager(nil, 10, 5, 0)

	require.NoError(t, pool.Acquire("user1"))

	pool.UpdateLimits(20, 10)

	total, max, _ := pool.Stats()
	require.Equal(t, 1, total)
	require.Equal(t, 20, max)
}
