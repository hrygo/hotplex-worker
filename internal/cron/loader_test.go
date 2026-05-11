package cron

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseYAMLSchedule_At(t *testing.T) {
	t.Parallel()

	def := YAMLJobDef{
		Name:       "test-at",
		ScheduleAt: "2026-06-01T09:00:00Z",
	}
	sched, err := parseYAMLSchedule(def)
	require.NoError(t, err)
	require.Equal(t, ScheduleAt, sched.Kind)
	require.Equal(t, "2026-06-01T09:00:00Z", sched.At)
}

func TestParseYAMLSchedule_Every(t *testing.T) {
	t.Parallel()

	def := YAMLJobDef{
		Name:          "test-every",
		ScheduleEvery: 300_000,
	}
	sched, err := parseYAMLSchedule(def)
	require.NoError(t, err)
	require.Equal(t, ScheduleEvery, sched.Kind)
	require.Equal(t, int64(300_000), sched.EveryMs)
}

func TestParseYAMLSchedule_Cron(t *testing.T) {
	t.Parallel()

	def := YAMLJobDef{
		Name:     "test-cron",
		Schedule: "*/10 * * * *",
	}
	sched, err := parseYAMLSchedule(def)
	require.NoError(t, err)
	require.Equal(t, ScheduleCron, sched.Kind)
	require.Equal(t, "*/10 * * * *", sched.Expr)
}

func TestParseYAMLSchedule_Priority(t *testing.T) {
	t.Parallel()

	// ScheduleAt takes priority over ScheduleEvery and Schedule.
	def := YAMLJobDef{
		Name:          "test-priority",
		ScheduleAt:    "2026-06-01T09:00:00Z",
		ScheduleEvery: 60_000,
		Schedule:      "*/5 * * * *",
	}
	sched, err := parseYAMLSchedule(def)
	require.NoError(t, err)
	require.Equal(t, ScheduleAt, sched.Kind)

	// ScheduleEvery takes priority over Schedule.
	def2 := YAMLJobDef{
		Name:          "test-priority-2",
		ScheduleEvery: 60_000,
		Schedule:      "*/5 * * * *",
	}
	sched2, err := parseYAMLSchedule(def2)
	require.NoError(t, err)
	require.Equal(t, ScheduleEvery, sched2.Kind)
}

func TestParseYAMLSchedule_NoSchedule(t *testing.T) {
	t.Parallel()

	_, err := parseYAMLSchedule(YAMLJobDef{Name: "no-sched"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "no schedule specified")
}

func TestYAMLDefToJob(t *testing.T) {
	def := YAMLJobDef{
		Name:          "test-yaml-job",
		Description:   "A test job",
		ScheduleEvery: 60_000,
		Prompt:        "Check system health",
		WorkDir:       "/tmp",
		BotID:         "bot1",
		OwnerID:       "user1",
		Platform:      "feishu",
		TimeoutSec:    120,
		MaxRetries:    3,
		Silent:        true,
		AllowedTools:  []string{"Read", "Bash"},
		MaxRuns:       50,
		ExpiresAt:     "2099-01-01T00:00:00Z",
	}

	job, err := yamlDefToJob(def)
	require.NoError(t, err)
	require.NotEmpty(t, job.ID)
	require.Contains(t, job.ID, "cron_")
	require.Equal(t, "test-yaml-job", job.Name)
	require.Equal(t, "A test job", job.Description)
	require.True(t, job.Enabled)
	require.Equal(t, ScheduleEvery, job.Schedule.Kind)
	require.Equal(t, PayloadIsolatedSession, job.Payload.Kind)
	require.Equal(t, "Check system health", job.Payload.Message)
	require.ElementsMatch(t, []string{"Read", "Bash"}, job.Payload.AllowedTools)
	require.Equal(t, "/tmp", job.WorkDir)
	require.Equal(t, "bot1", job.BotID)
	require.Equal(t, "user1", job.OwnerID)
	require.Equal(t, "feishu", job.Platform)
	require.Equal(t, 120, job.TimeoutSec)
	require.Equal(t, 3, job.MaxRetries)
	require.True(t, job.Silent)
	require.NotZero(t, job.State.NextRunAtMs)
	require.NotZero(t, job.CreatedAtMs)
}

func TestYAMLDefToJob_InvalidSchedule(t *testing.T) {
	def := YAMLJobDef{
		Name:     "bad-sched",
		Schedule: "invalid cron expr!!!",
		BotID:    "bot1",
		OwnerID:  "user1",
	}
	_, err := yamlDefToJob(def)
	require.Error(t, err)
}

func TestYAMLDefToJob_MissingLifecycle(t *testing.T) {
	def := YAMLJobDef{
		Name:          "no-lifecycle",
		ScheduleEvery: 60_000,
		Prompt:        "test",
		BotID:         "bot1",
		OwnerID:       "user1",
	}
	_, err := yamlDefToJob(def)
	require.Error(t, err)
	require.Contains(t, err.Error(), "max_runs is required")
}

func TestLoadFromYAML_Integration(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	s := &Scheduler{
		log:      slog.Default(),
		store:    store,
		jobs:     map[string]*CronJob{},
		tickLoop: newTimerLoop(&Scheduler{}),
	}

	defs := []YAMLJobDef{
		{
			Name:          "job-a",
			ScheduleEvery: 60_000,
			Prompt:        "Job A",
			OwnerID:       "user1",
			BotID:         "bot1",
			MaxRuns:       10,
			ExpiresAt:     "2099-01-01T00:00:00Z",
		},
		{
			Name:          "job-b",
			ScheduleEvery: 120_000,
			Prompt:        "Job B",
			OwnerID:       "user1",
			BotID:         "bot1",
			MaxRuns:       10,
			ExpiresAt:     "2099-01-01T00:00:00Z",
		},
	}

	require.NoError(t, s.LoadFromYAML(ctx, defs))

	jobs, err := store.List(ctx, false)
	require.NoError(t, err)
	require.Len(t, jobs, 2)

	// Idempotent: load same definitions again.
	require.NoError(t, s.LoadFromYAML(ctx, defs))

	jobs2, err := store.List(ctx, false)
	require.NoError(t, err)
	require.Len(t, jobs2, 2) // no duplicates
}

func TestLoadFromYAML_SkipNoName(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	s := &Scheduler{
		log:      slog.Default(),
		store:    store,
		jobs:     map[string]*CronJob{},
		tickLoop: newTimerLoop(&Scheduler{}),
	}

	defs := []YAMLJobDef{
		{Name: "", ScheduleEvery: 60_000, Prompt: "no name"},
		{Name: "valid", ScheduleEvery: 60_000, Prompt: "valid job", BotID: "bot1", OwnerID: "user1", MaxRuns: 10, ExpiresAt: "2099-01-01T00:00:00Z"},
	}

	require.NoError(t, s.LoadFromYAML(ctx, defs))

	jobs, err := store.List(ctx, false)
	require.NoError(t, err)
	require.Len(t, jobs, 1)
	require.Equal(t, "valid", jobs[0].Name)
}

func TestLoadFromYAML_UpdatePreservesState(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	s := &Scheduler{
		log:      slog.Default(),
		store:    store,
		jobs:     map[string]*CronJob{},
		tickLoop: newTimerLoop(&Scheduler{}),
	}

	// First load.
	defs := []YAMLJobDef{
		{Name: "update-test", ScheduleEvery: 60_000, Prompt: "original", BotID: "bot1", OwnerID: "user1", MaxRuns: 10, ExpiresAt: "2099-01-01T00:00:00Z"},
	}
	require.NoError(t, s.LoadFromYAML(ctx, defs))

	// Simulate runtime state.
	jobs, _ := store.List(ctx, false)
	job := jobs[0]
	require.NoError(t, store.UpdateState(ctx, job.ID, CronJobState{
		NextRunAtMs:     12345,
		LastRunAtMs:     67890,
		LastStatus:      StatusSuccess,
		ConsecutiveErrs: 0,
	}))

	// Reload with updated prompt — runtime state should be preserved.
	defs[0].Prompt = "updated prompt"
	require.NoError(t, s.LoadFromYAML(ctx, defs))

	updated, _ := store.Get(ctx, job.ID)
	require.Equal(t, "updated prompt", updated.Payload.Message)
	require.Equal(t, int64(67890), updated.State.LastRunAtMs) // preserved
	require.Equal(t, StatusSuccess, updated.State.LastStatus) // preserved
}
