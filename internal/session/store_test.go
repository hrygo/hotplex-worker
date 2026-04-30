package session

import (
	"context"
	"encoding/json"
	"fmt"
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

// ─── ConversationStore: GetBySessionBefore ─────────────────────────────────────

func TestConversationStore_GetBySessionBefore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, convStore := helperStoreWithConv(t)

	// Seed 5 records with seq 1-5 for session "sess-1"
	for i := 1; i <= 5; i++ {
		rec := &ConversationRecord{
			ID:        fmt.Sprintf("conv_%d", i),
			SessionID: "sess-1",
			Seq:       int64(i),
			Role:      RoleUser,
			Content:   fmt.Sprintf("msg %d", i),
			Platform:  "webchat",
			UserID:    "user-1",
			Source:    SourceNormal,
		}
		require.NoError(t, convStore.Append(ctx, rec))
	}
	// Wait for async writer to flush
	time.Sleep(200 * time.Millisecond)

	tests := []struct {
		name      string
		beforeSeq int64
		limit     int
		wantCount int
		wantSeqs  []int64
	}{
		{"returns records before cursor", 4, 10, 3, []int64{3, 2, 1}},
		{"respects limit", 6, 2, 2, []int64{5, 4}},
		{"no records before cursor when seq=1", 1, 10, 0, nil},
		{"all records when beforeSeq > max", 100, 10, 5, []int64{5, 4, 3, 2, 1}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			records, err := convStore.GetBySessionBefore(ctx, "sess-1", tt.beforeSeq, tt.limit)
			if tt.wantCount == 0 {
				require.ErrorIs(t, err, ErrConvNotFound)
				return
			}
			require.NoError(t, err)
			require.Len(t, records, tt.wantCount)
			for i, wantSeq := range tt.wantSeqs {
				require.Equal(t, wantSeq, records[i].Seq)
			}
		})
	}
}

func TestConversationStore_GetBySessionBefore_SessionIsolation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, convStore := helperStoreWithConv(t)

	// Seed records for two sessions
	for _, sid := range []string{"sess-a", "sess-b"} {
		rec := &ConversationRecord{
			ID:        fmt.Sprintf("conv_%s_1", sid),
			SessionID: sid,
			Seq:       1,
			Role:      RoleUser,
			Content:   "hello",
			Platform:  "webchat",
			UserID:    "user-1",
			Source:    SourceNormal,
		}
		require.NoError(t, convStore.Append(ctx, rec))
	}
	time.Sleep(200 * time.Millisecond)

	records, err := convStore.GetBySessionBefore(ctx, "sess-a", 100, 10)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "sess-a", records[0].SessionID)
}

func TestConversationStore_Append_UnmarshallableMetadata(t *testing.T) {
	_, cs := helperStoreWithConv(t)
	ctx := context.Background()

	// json.Marshal cannot encode a channel → triggers the error fallback path.
	ch := make(chan int)
	require.NoError(t, cs.Append(ctx, &ConversationRecord{
		SessionID: "sess-bad-meta", Seq: 1, Role: RoleUser, Content: "hello",
		Metadata: map[string]any{"bad": ch},
	}))
	time.Sleep(200 * time.Millisecond)

	// The record is still persisted (with "{}" as metadata fallback).
	records, err := cs.GetBySession(ctx, "sess-bad-meta", 100, 0)
	require.NoError(t, err)
	require.Len(t, records, 1)
}
