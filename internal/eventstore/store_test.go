package eventstore

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/sqlutil"
	"github.com/hrygo/hotplex/pkg/events"
)

func init() {
	// Ensure SQLite driver is registered.
	_ = sqlutil.DriverName
}

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := NewIndependentStore(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	// Create the events table (normally done by goose migration 002).
	_, err = store.db.Exec(`CREATE TABLE IF NOT EXISTS events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		seq INTEGER NOT NULL,
		type TEXT NOT NULL,
		data TEXT NOT NULL,
		direction TEXT NOT NULL DEFAULT 'outbound',
		source TEXT NOT NULL DEFAULT 'normal'
			CHECK(source IN ('normal', 'crash', 'timeout', 'fresh_start')),
		created_at INTEGER NOT NULL
	)`)
	require.NoError(t, err)
	return store
}

func TestSQLiteStore_AppendAndQuery(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Append events
	for i := int64(1); i <= 5; i++ {
		err := store.Append(ctx, &StoredEvent{
			SessionID: "sess1",
			Seq:       i,
			Type:      "message",
			Data:      json.RawMessage(`{"content":"hello"}`),
			Direction: "outbound",
			Source:    SourceNormal,
			CreatedAt: time.Now().UnixMilli(),
		})
		require.NoError(t, err)
	}

	t.Run("cursor latest", func(t *testing.T) {
		page, err := store.QueryBySession(ctx, "sess1", 0, CursorLatest, 3)
		require.NoError(t, err)
		require.Len(t, page.Events, 3)
		require.Equal(t, int64(3), page.OldestSeq)
		require.Equal(t, int64(5), page.NewestSeq)
		require.True(t, page.HasOlder)
	})

	t.Run("cursor after", func(t *testing.T) {
		page, err := store.QueryBySession(ctx, "sess1", 3, CursorAfter, 10)
		require.NoError(t, err)
		require.Len(t, page.Events, 2) // seq 4, 5
		require.Equal(t, int64(4), page.OldestSeq)
		// HasOlder checks if events older than oldest exist — they do (seq 1-3)
		require.True(t, page.HasOlder)
	})

	t.Run("cursor before", func(t *testing.T) {
		page, err := store.QueryBySession(ctx, "sess1", 3, CursorBefore, 10)
		require.NoError(t, err)
		require.Len(t, page.Events, 2) // seq 1, 2
		require.Equal(t, int64(1), page.OldestSeq)
		require.False(t, page.HasOlder)
	})

	t.Run("nonexistent session", func(t *testing.T) {
		_, err := store.QueryBySession(ctx, "no-such-session", 0, CursorLatest, 10)
		require.ErrorIs(t, err, ErrNotFound)
	})
}

func TestSQLiteStore_DeleteBySession(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	err := store.Append(ctx, &StoredEvent{
		SessionID: "sess-del", Seq: 1, Type: "message",
		Data: json.RawMessage(`{}`), Direction: "outbound", Source: SourceNormal, CreatedAt: time.Now().UnixMilli(),
	})
	require.NoError(t, err)

	err = store.DeleteBySession(ctx, "sess-del")
	require.NoError(t, err)

	_, err = store.QueryBySession(ctx, "sess-del", 0, CursorLatest, 10)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestSQLiteStore_DeleteExpired(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now().UnixMilli()
	err := store.Append(ctx, &StoredEvent{
		SessionID: "sess-exp", Seq: 1, Type: "message",
		Data: json.RawMessage(`{}`), Direction: "outbound", Source: SourceNormal, CreatedAt: now - 86400000, // 1 day ago
	})
	require.NoError(t, err)

	err = store.Append(ctx, &StoredEvent{
		SessionID: "sess-exp", Seq: 2, Type: "message",
		Data: json.RawMessage(`{}`), Direction: "outbound", Source: SourceNormal, CreatedAt: now,
	})
	require.NoError(t, err)

	deleted, err := store.DeleteExpired(ctx, time.Now().Add(-12*time.Hour))
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)

	// Recent event should remain
	page, err := store.QueryBySession(ctx, "sess-exp", 0, CursorLatest, 10)
	require.NoError(t, err)
	require.Len(t, page.Events, 1)
	require.Equal(t, int64(2), page.Events[0].Seq)
}

func TestSQLiteStore_Transaction(t *testing.T) {
	store := newTestStore(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tx, err := store.BeginTx(ctx)
	require.NoError(t, err)

	for i := int64(1); i <= 3; i++ {
		err := tx.Append(ctx, &StoredEvent{
			SessionID: "sess-tx", Seq: i, Type: "message",
			Data: json.RawMessage(`{}`), Direction: "outbound", Source: SourceNormal, CreatedAt: time.Now().UnixMilli(),
		})
		require.NoError(t, err, "append event seq=%d", i)
	}

	require.NoError(t, tx.Commit())

	page, err := store.QueryBySession(ctx, "sess-tx", 0, CursorLatest, 10)
	require.NoError(t, err)
	require.Len(t, page.Events, 3)
}

func TestSQLiteStore_QueryLimitBounds(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	// Insert 5 events
	for i := int64(1); i <= 5; i++ {
		err := store.Append(ctx, &StoredEvent{
			SessionID: "sess-lim", Seq: i, Type: "message",
			Data: json.RawMessage(`{}`), Direction: "outbound", Source: SourceNormal, CreatedAt: time.Now().UnixMilli(),
		})
		require.NoError(t, err)
	}

	t.Run("limit 0 uses default", func(t *testing.T) {
		page, err := store.QueryBySession(ctx, "sess-lim", 0, CursorLatest, 0)
		require.NoError(t, err)
		require.Len(t, page.Events, 5) // default 200, we only have 5
	})

	t.Run("limit over 1000 clamped", func(t *testing.T) {
		page, err := store.QueryBySession(ctx, "sess-lim", 0, CursorLatest, 5000)
		require.NoError(t, err)
		require.Len(t, page.Events, 5)
	})
}

func TestIsStorable(t *testing.T) {
	t.Parallel()
	require.True(t, IsStorable(events.Message))
	require.True(t, IsStorable(events.Done))
	require.True(t, IsStorable(events.ToolCall))
	require.False(t, IsStorable(events.MessageDelta))
	require.False(t, IsStorable(events.Kind("unknown")))
}

func TestEventsTable_SourceCheck(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	tests := []struct {
		source string
		ok     bool
	}{
		{SourceNormal, true},
		{SourceCrash, true},
		{SourceTimeout, true},
		{SourceFreshStart, true},
		{"invalid_source", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			err := store.Append(ctx, &StoredEvent{
				SessionID: "sess-check", Seq: 1, Type: "done",
				Data: raw(`{}`), Direction: "outbound", Source: tt.source, CreatedAt: time.Now().UnixMilli(),
			})
			if tt.ok {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
}

func TestCollector_CaptureAndFlush(t *testing.T) {
	store := newTestStore(t)
	collector := NewCollector(store, slog.Default())

	// Capture storable events
	collector.Capture("sess1", 1, events.Message, json.RawMessage(`{"content":"hello"}`), "outbound", SourceNormal)
	collector.Capture("sess1", 2, events.Done, json.RawMessage(`{}`), "outbound", SourceNormal)

	// Close drains and flushes remaining events synchronously
	require.NoError(t, collector.Close())

	ctx := context.Background()
	page, err := store.QueryBySession(ctx, "sess1", 0, CursorLatest, 100)
	require.NoError(t, err)
	require.Len(t, page.Events, 2)
	require.Equal(t, int64(1), page.Events[0].Seq)
	require.Equal(t, int64(2), page.Events[1].Seq)
}

func TestCollector_DropNonStorable(t *testing.T) {
	store := newTestStore(t)
	collector := NewCollector(store, slog.Default())
	defer func() { _ = collector.Close() }()

	// Non-storable event should be silently dropped
	collector.Capture("sess1", 1, events.Kind("non_storable_type"), json.RawMessage(`{}`), "outbound", SourceNormal)

	// Wait briefly to ensure it's processed (or not)
	time.Sleep(200 * time.Millisecond)

	_, err := store.QueryBySession(context.Background(), "sess1", 0, CursorLatest, 10)
	require.ErrorIs(t, err, ErrNotFound)
}
