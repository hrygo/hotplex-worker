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
