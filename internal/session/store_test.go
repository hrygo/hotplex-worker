package session

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/pkg/events"
)

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
