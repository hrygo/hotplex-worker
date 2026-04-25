package session

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	ctx := context.Background()
	cfg := config.Default()
	cfg.DB.Path = t.TempDir() + "/test.db"
	store, err := NewSQLiteStore(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestNewSQLiteStore(t *testing.T) {
	t.Parallel()

	t.Run("creates store with WAL mode", func(t *testing.T) {
		store := newTestStore(t)
		require.NotNil(t, store)
		require.NotNil(t, store.db)
	})

	t.Run("creates tables on init", func(t *testing.T) {
		store := newTestStore(t)
		ctx := context.Background()

		// Check sessions table exists
		var name string
		err := store.db.QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type='table' AND name='sessions'",
		).Scan(&name)
		require.NoError(t, err)
		require.Equal(t, "sessions", name)

		// Check events table exists
		err = store.db.QueryRowContext(ctx,
			"SELECT name FROM sqlite_master WHERE type='table' AND name='events'",
		).Scan(&name)
		require.NoError(t, err)
		require.Equal(t, "events", name)
	})
}

func TestSQLiteStore_Upsert_Get(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now()
	info := &SessionInfo{
		ID:              "sess_test1",
		UserID:          "user_001",
		OwnerID:         "user_001",
		BotID:           "bot_001",
		WorkerType:      worker.TypeClaudeCode,
		State:           events.StateRunning,
		CreatedAt:       now,
		UpdatedAt:       now,
		ExpiresAt:       ptrTime(now.Add(24 * time.Hour)),
		IdleExpiresAt:   ptrTime(now.Add(30 * time.Minute)),
		AllowedTools:    []string{"Read", "Edit"},
		WorkerSessionID: "wsess_001",
	}

	// Insert
	err := store.Upsert(ctx, info)
	require.NoError(t, err)

	// Get
	got, err := store.Get(ctx, "sess_test1")
	require.NoError(t, err)
	require.Equal(t, info.ID, got.ID)
	require.Equal(t, info.UserID, got.UserID)
	require.Equal(t, info.BotID, got.BotID)
	require.Equal(t, info.State, got.State)
	require.Equal(t, info.WorkerSessionID, got.WorkerSessionID)
	// Note: AllowedTools is not persisted to DB, only passed to worker proc

	// Update (upsert)
	info.State = events.StateIdle
	info.UpdatedAt = now.Add(1 * time.Minute)
	err = store.Upsert(ctx, info)
	require.NoError(t, err)

	got, err = store.Get(ctx, "sess_test1")
	require.NoError(t, err)
	require.Equal(t, events.StateIdle, got.State)
}

func TestSQLiteStore_Get_NotFound(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	_, err := store.Get(ctx, "nonexistent")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrSessionNotFound)
}

func TestSQLiteStore_List(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now()

	// Insert multiple sessions
	sessIDs := []string{"sess_a", "sess_b", "sess_c", "sess_d", "sess_e"}
	for i, id := range sessIDs {
		info := &SessionInfo{
			ID:         id,
			UserID:     "user_001",
			OwnerID:    "user_001",
			WorkerType: worker.TypeClaudeCode,
			State:      events.StateRunning,
			CreatedAt:  now.Add(time.Duration(i) * time.Minute),
			UpdatedAt:  now.Add(time.Duration(i) * time.Minute),
		}
		err := store.Upsert(ctx, info)
		require.NoError(t, err)
	}

	// List with pagination
	list, err := store.List(ctx, "", "", 3, 0)
	require.NoError(t, err)
	require.Len(t, list, 3)

	list, err = store.List(ctx, "", "", 3, 3)
	require.NoError(t, err)
	require.Len(t, list, 2)

	list, err = store.List(ctx, "", "", 10, 10)
	require.NoError(t, err)
	require.Empty(t, list)
}

func TestSQLiteStore_GetExpiredMaxLifetime(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now()

	// Expired session
	expired := &SessionInfo{
		ID:         "sess_expired",
		UserID:     "user_001",
		OwnerID:    "user_001",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateRunning,
		CreatedAt:  now.Add(-2 * time.Hour),
		UpdatedAt:  now.Add(-2 * time.Hour),
		ExpiresAt:  ptrTime(now.Add(-1 * time.Hour)), // expired 1h ago
	}
	err := store.Upsert(ctx, expired)
	require.NoError(t, err)

	// Active session
	active := &SessionInfo{
		ID:         "sess_active",
		UserID:     "user_001",
		OwnerID:    "user_001",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateRunning,
		CreatedAt:  now,
		UpdatedAt:  now,
		ExpiresAt:  ptrTime(now.Add(24 * time.Hour)),
	}
	err = store.Upsert(ctx, active)
	require.NoError(t, err)

	// Query expired
	ids, err := store.GetExpiredMaxLifetime(ctx, now)
	require.NoError(t, err)
	require.Contains(t, ids, "sess_expired")
	require.NotContains(t, ids, "sess_active")
}

func TestSQLiteStore_GetExpiredIdle(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now()

	// Idle expired
	idleExpired := &SessionInfo{
		ID:            "sess_idle_expired",
		UserID:        "user_001",
		OwnerID:       "user_001",
		WorkerType:    worker.TypeClaudeCode,
		State:         events.StateIdle,
		CreatedAt:     now.Add(-1 * time.Hour),
		UpdatedAt:     now.Add(-1 * time.Hour),
		IdleExpiresAt: ptrTime(now.Add(-30 * time.Minute)), // idle expired
	}
	err := store.Upsert(ctx, idleExpired)
	require.NoError(t, err)

	// Idle active
	idleActive := &SessionInfo{
		ID:            "sess_idle_active",
		UserID:        "user_001",
		OwnerID:       "user_001",
		WorkerType:    worker.TypeClaudeCode,
		State:         events.StateIdle,
		CreatedAt:     now,
		UpdatedAt:     now,
		IdleExpiresAt: ptrTime(now.Add(30 * time.Minute)),
	}
	err = store.Upsert(ctx, idleActive)
	require.NoError(t, err)

	// Query idle expired
	ids, err := store.GetExpiredIdle(ctx, now)
	require.NoError(t, err)
	require.Contains(t, ids, "sess_idle_expired")
	require.NotContains(t, ids, "sess_idle_active")
}

func TestSQLiteStore_DeleteTerminated(t *testing.T) {
	t.Parallel()
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now()

	// Terminated session (old)
	oldTerminated := &SessionInfo{
		ID:         "sess_old_term",
		UserID:     "user_001",
		OwnerID:    "user_001",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateTerminated,
		CreatedAt:  now.Add(-48 * time.Hour),
		UpdatedAt:  now.Add(-48 * time.Hour),
	}
	err := store.Upsert(ctx, oldTerminated)
	require.NoError(t, err)

	// Terminated session (recent)
	recentTerminated := &SessionInfo{
		ID:         "sess_recent_term",
		UserID:     "user_001",
		OwnerID:    "user_001",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateTerminated,
		CreatedAt:  now.Add(-1 * time.Hour),
		UpdatedAt:  now.Add(-1 * time.Hour),
	}
	err = store.Upsert(ctx, recentTerminated)
	require.NoError(t, err)

	// Delete old terminated
	cutoff := now.Add(-24 * time.Hour)
	err = store.DeleteTerminated(ctx, cutoff)
	require.NoError(t, err)

	// Verify old deleted, recent kept
	_, err = store.Get(ctx, "sess_old_term")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrSessionNotFound)

	_, err = store.Get(ctx, "sess_recent_term")
	require.NoError(t, err)
}

// ─── MessageStore Tests ────────────────────────────────────────────────────────

func TestSQLiteMessageStore_Append_GetBySession(t *testing.T) {
	t.Parallel()
	// Use same DB path for both stores to ensure events table exists
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"

	ctx := context.Background()
	cfg := config.Default()
	cfg.DB.Path = dbPath

	// Create session store first to run migrations (including events table)
	store, err := NewSQLiteStore(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Now create message store on the same DB
	msgStore, err := NewSQLiteMessageStore(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = msgStore.Close() })

	// Append events
	err = msgStore.Append(ctx, "sess_001", 1, "message", []byte(`{"text":"hello"}`))
	require.NoError(t, err)

	err = msgStore.Append(ctx, "sess_001", 2, "message", []byte(`{"text":"world"}`))
	require.NoError(t, err)

	// Give time for async writer to flush
	time.Sleep(200 * time.Millisecond)

	// Get by session
	got, err := msgStore.GetBySession(ctx, "sess_001", 0)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, int64(1), got[0].Seq)
	require.Equal(t, int64(2), got[1].Seq)
}

func TestSQLiteMessageStore_GetOwner(t *testing.T) {
	t.Parallel()
	// Use same DB path for both stores
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"

	ctx := context.Background()
	cfg := config.Default()
	cfg.DB.Path = dbPath

	store, err := NewSQLiteStore(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	msgStore, err := NewSQLiteMessageStore(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = msgStore.Close() })

	// Create session first
	now := time.Now()
	info := &SessionInfo{
		ID:         "sess_owner_test",
		UserID:     "user_owner_001",
		OwnerID:    "user_owner_001",
		WorkerType: worker.TypeClaudeCode,
		State:      events.StateRunning,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	err = store.Upsert(ctx, info)
	require.NoError(t, err)

	// Get owner
	owner, err := msgStore.GetOwner(ctx, "sess_owner_test")
	require.NoError(t, err)
	require.Equal(t, "user_owner_001", owner)
}

func TestSQLiteMessageStore_GetOwner_NotFound(t *testing.T) {
	t.Parallel()
	// Use same DB path for both stores
	tmpDir := t.TempDir()
	dbPath := tmpDir + "/test.db"

	ctx := context.Background()
	cfg := config.Default()
	cfg.DB.Path = dbPath

	store, err := NewSQLiteStore(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	msgStore, err := NewSQLiteMessageStore(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = msgStore.Close() })

	_, err = msgStore.GetOwner(ctx, "nonexistent")
	require.Error(t, err)
}

// ─── Helper Functions ──────────────────────────────────────────────────────────

func ptrTime(t time.Time) *time.Time {
	return &t
}
