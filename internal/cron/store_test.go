package cron

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/sqlutil"
)

func init() {
	_ = sqlutil.DriverName
}

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open(sqlutil.DriverName, dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA journal_mode=WAL`)
	require.NoError(t, err)
	_, err = db.Exec(`PRAGMA busy_timeout=5000`)
	require.NoError(t, err)

	// Create cron_jobs table (normally done by goose migration 005).
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS cron_jobs (
			id               TEXT PRIMARY KEY,
			name             TEXT NOT NULL,
			description      TEXT NOT NULL DEFAULT '',
			enabled          INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0, 1)),
			schedule_kind    TEXT NOT NULL CHECK(schedule_kind IN ('at', 'every', 'cron')),
			schedule_data    TEXT NOT NULL,
			payload_kind     TEXT NOT NULL DEFAULT 'agent_turn' CHECK(payload_kind IN ('agent_turn', 'system_event')),
			payload_data     TEXT NOT NULL,
			work_dir         TEXT NOT NULL DEFAULT '',
			bot_id           TEXT NOT NULL DEFAULT '',
			owner_id         TEXT NOT NULL DEFAULT '',
			platform         TEXT NOT NULL DEFAULT '',
			platform_key     TEXT NOT NULL DEFAULT '{}',
			timeout_sec      INTEGER NOT NULL DEFAULT 0,
			delete_after_run INTEGER NOT NULL DEFAULT 0 CHECK(delete_after_run IN (0, 1)),
			max_retries      INTEGER NOT NULL DEFAULT 0,
			state            TEXT NOT NULL DEFAULT '{}',
			created_at       INTEGER NOT NULL,
			updated_at       INTEGER NOT NULL
		)`)
	require.NoError(t, err)

	return NewSQLiteStore(db, slog.Default())
}

func helperJob(name string) *CronJob {
	now := time.Now().UnixMilli()
	return &CronJob{
		ID:          generateJobID(),
		Name:        name,
		OwnerID:     "user1",
		BotID:       "bot1",
		Enabled:     true,
		Schedule:    CronSchedule{Kind: ScheduleEvery, EveryMs: 60_000},
		Payload:     CronPayload{Kind: PayloadAgentTurn, Message: "test prompt"},
		PlatformKey: map[string]string{},
		State:       CronJobState{NextRunAtMs: now + 60_000},
		CreatedAtMs: now,
		UpdatedAtMs: now,
	}
}

func TestSQLiteStore_CreateAndGet(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	job := helperJob("test-create")
	require.NoError(t, store.Create(ctx, job))

	got, err := store.Get(ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, job.ID, got.ID)
	require.Equal(t, job.Name, got.Name)
	require.True(t, got.Enabled)
	require.Equal(t, ScheduleEvery, got.Schedule.Kind)
	require.Equal(t, int64(60_000), got.Schedule.EveryMs)
	require.Equal(t, "test prompt", got.Payload.Message)
}

func TestSQLiteStore_GetNotFound(t *testing.T) {
	store := newTestStore(t)
	_, err := store.Get(context.Background(), "nonexistent")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not found")
}

func TestSQLiteStore_GetByName(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	job := helperJob("test-byname")
	require.NoError(t, store.Create(ctx, job))

	got, err := store.GetByName(ctx, "test-byname")
	require.NoError(t, err)
	require.Equal(t, job.ID, got.ID)

	_, err = store.GetByName(ctx, "nonexistent")
	require.Error(t, err)
}

func TestSQLiteStore_Update(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	job := helperJob("test-update")
	require.NoError(t, store.Create(ctx, job))

	job.Name = "test-update-v2"
	job.Description = "updated description"
	job.UpdatedAtMs = time.Now().UnixMilli()
	require.NoError(t, store.Update(ctx, job))

	got, err := store.Get(ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, "test-update-v2", got.Name)
	require.Equal(t, "updated description", got.Description)
}

func TestSQLiteStore_UpdateNotFound(t *testing.T) {
	store := newTestStore(t)
	job := helperJob("ghost")
	job.ID = "nonexistent"
	require.Error(t, store.Update(context.Background(), job))
}

func TestSQLiteStore_Delete(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	job := helperJob("test-delete")
	require.NoError(t, store.Create(ctx, job))
	require.NoError(t, store.Delete(ctx, job.ID))

	_, err := store.Get(ctx, job.ID)
	require.Error(t, err)
}

func TestSQLiteStore_DeleteNotFound(t *testing.T) {
	store := newTestStore(t)
	require.Error(t, store.Delete(context.Background(), "nonexistent"))
}

func TestSQLiteStore_List(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		require.NoError(t, store.Create(ctx, helperJob("list-job")))
	}

	all, err := store.List(ctx, false)
	require.NoError(t, err)
	require.Len(t, all, 3)

	enabled, err := store.List(ctx, true)
	require.NoError(t, err)
	require.Len(t, enabled, 3)
}

func TestSQLiteStore_ListEnabledOnly(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	job1 := helperJob("enabled-job")
	require.NoError(t, store.Create(ctx, job1))

	job2 := helperJob("disabled-job")
	job2.Enabled = false
	require.NoError(t, store.Create(ctx, job2))

	enabled, err := store.List(ctx, true)
	require.NoError(t, err)
	require.Len(t, enabled, 1)
	require.Equal(t, "enabled-job", enabled[0].Name)
}

func TestSQLiteStore_UpdateState(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	job := helperJob("test-state")
	require.NoError(t, store.Create(ctx, job))

	newState := CronJobState{
		NextRunAtMs:     time.Now().Add(2 * time.Hour).UnixMilli(),
		LastRunAtMs:     time.Now().UnixMilli(),
		LastStatus:      StatusSuccess,
		ConsecutiveErrs: 0,
		RetryCount:      2,
	}
	require.NoError(t, store.UpdateState(ctx, job.ID, newState))

	got, err := store.Get(ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, StatusSuccess, got.State.LastStatus)
	require.Equal(t, 2, got.State.RetryCount)
	require.False(t, got.State.LastRunAtMs == 0)
}

func TestSQLiteStore_UpsertByName_Create(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	job := helperJob("upsert-new")
	require.NoError(t, store.UpsertByName(ctx, job))

	got, err := store.GetByName(ctx, "upsert-new")
	require.NoError(t, err)
	require.Equal(t, job.ID, got.ID)
}

func TestSQLiteStore_UpsertByName_Update(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	job := helperJob("upsert-existing")
	require.NoError(t, store.Create(ctx, job))

	// Simulate some runtime state.
	require.NoError(t, store.UpdateState(ctx, job.ID, CronJobState{
		NextRunAtMs: time.Now().Add(1 * time.Hour).UnixMilli(),
		LastStatus:  StatusSuccess,
	}))

	// Upsert with updated definition.
	updated := helperJob("upsert-existing")
	updated.ID = job.ID // same ID
	updated.Description = "updated via upsert"
	updated.Payload.Message = "new prompt"
	require.NoError(t, store.UpsertByName(ctx, updated))

	got, err := store.Get(ctx, job.ID)
	require.NoError(t, err)
	require.Equal(t, "updated via upsert", got.Description)
	require.Equal(t, "new prompt", got.Payload.Message)
	// Runtime state should be preserved from the existing job update.
	require.Equal(t, StatusSuccess, got.State.LastStatus)
}

func TestSQLiteStore_FieldsRoundtrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	now := time.Now().UnixMilli()
	job := &CronJob{
		ID:          generateJobID(),
		Name:        "full-fields",
		Description: "a detailed description",
		Enabled:     true,
		Schedule:    CronSchedule{Kind: ScheduleCron, Expr: "*/5 * * * *", TZ: "UTC"},
		Payload: CronPayload{
			Kind:         PayloadAgentTurn,
			Message:      "complex job",
			AllowedTools: []string{"Read", "Write"},
		},
		WorkDir:        "/home/user/project",
		BotID:          "bot_123",
		OwnerID:        "user_456",
		Platform:       "feishu",
		PlatformKey:    map[string]string{"chat_id": "oc_abc", "user_id": "ou_xyz"},
		TimeoutSec:     300,
		DeleteAfterRun: true,
		MaxRetries:     5,
		State: CronJobState{
			NextRunAtMs:     now + 60_000,
			LastRunAtMs:     now - 60_000,
			RunningAtMs:     0,
			LastStatus:      StatusFailed,
			ConsecutiveErrs: 2,
			RetryCount:      1,
			LastRunID:       "run_abc",
		},
		CreatedAtMs: now - 300_000,
		UpdatedAtMs: now,
	}

	require.NoError(t, store.Create(ctx, job))

	got, err := store.Get(ctx, job.ID)
	require.NoError(t, err)

	require.Equal(t, job.ID, got.ID)
	require.Equal(t, job.Name, got.Name)
	require.Equal(t, job.Description, got.Description)
	require.Equal(t, job.Enabled, got.Enabled)
	require.Equal(t, job.Schedule, got.Schedule)
	require.Equal(t, job.Payload.Kind, got.Payload.Kind)
	require.Equal(t, job.Payload.Message, got.Payload.Message)
	require.ElementsMatch(t, job.Payload.AllowedTools, got.Payload.AllowedTools)
	require.Equal(t, job.WorkDir, got.WorkDir)
	require.Equal(t, job.BotID, got.BotID)
	require.Equal(t, job.OwnerID, got.OwnerID)
	require.Equal(t, job.Platform, got.Platform)
	require.Equal(t, job.PlatformKey, got.PlatformKey)
	require.Equal(t, job.TimeoutSec, got.TimeoutSec)
	require.Equal(t, job.DeleteAfterRun, got.DeleteAfterRun)
	require.Equal(t, job.MaxRetries, got.MaxRetries)
	require.Equal(t, job.State, got.State)
	require.Equal(t, job.CreatedAtMs, got.CreatedAtMs)
}
