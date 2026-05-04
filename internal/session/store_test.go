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

func helperDB(t *testing.T) (*SQLiteStore, *config.Config) {
	t.Helper()
	cfg := config.Default()
	cfg.DB.Path = filepath.Join(t.TempDir(), "test.db")
	cfg.DB.WALMode = true

	store, err := NewSQLiteStore(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store, cfg
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
	store, _ := helperDB(t)
	ctx := context.Background()

	helperUpsert(t, store, "sess_del_phys", "user1", events.StateTerminated)

	err := store.DeletePhysical(ctx, "sess_del_phys")
	require.NoError(t, err)

	_, err = store.Get(ctx, "sess_del_phys")
	require.Error(t, err)
}

func TestSQLiteStore_DeletePhysical_NotFound(t *testing.T) {
	store, _ := helperDB(t)

	err := store.DeletePhysical(context.Background(), "nonexistent")
	require.NoError(t, err)
}

// ─── SQLiteStore: Compact ────────────────────────────────────────────────────

func TestSQLiteStore_Compact_BelowThreshold(t *testing.T) {
	store, _ := helperDB(t)
	ctx := context.Background()

	err := store.Compact(ctx, 0.99)
	require.NoError(t, err)
}

// ─── SQLiteStore: Upsert with Context and PlatformKey ────────────────────────

func TestSQLiteStore_Upsert_WithContext(t *testing.T) {
	store, _ := helperDB(t)
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
	store, _ := helperDB(t)
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
	store, _ := helperDB(t)
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
	store, _ := helperDB(t)
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
	store, _ := helperDB(t)
	ctx := context.Background()

	helperUpsert(t, store, "sess_term", "user1", events.StateTerminated)

	cutoff := time.Now().Add(time.Hour)
	err := store.DeleteTerminated(ctx, cutoff)
	require.NoError(t, err)
}

// ─── SQLiteStore: GetSessionsByState ─────────────────────────────────────────

func TestSQLiteStore_GetSessionsByState(t *testing.T) {
	store, _ := helperDB(t)
	ctx := context.Background()

	helperUpsert(t, store, "sess_state_r", "user1", events.StateRunning)
	helperUpsert(t, store, "sess_state_i", "user1", events.StateIdle)

	ids, err := store.GetSessionsByState(ctx, events.StateRunning)
	require.NoError(t, err)
	require.Contains(t, ids, "sess_state_r")
	require.NotContains(t, ids, "sess_state_i")
}
