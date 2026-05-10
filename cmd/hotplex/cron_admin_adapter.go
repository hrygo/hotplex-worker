package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hrygo/hotplex/internal/cron"
	"github.com/hrygo/hotplex/internal/eventstore"
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
)

// cronAdminAdapter bridges cron.Scheduler to admin.CronSchedulerProvider.
type cronAdminAdapter struct {
	scheduler  *cron.Scheduler
	turnsStore *eventstore.SQLiteStore
}

func (a *cronAdminAdapter) CreateJob(ctx context.Context, raw any) error {
	data, err := json.Marshal(raw)
	if err != nil {
		return fmt.Errorf("marshal job: %w", err)
	}
	var job cron.CronJob
	if err := json.Unmarshal(data, &job); err != nil {
		return fmt.Errorf("unmarshal job: %w", err)
	}
	return a.scheduler.CreateJob(ctx, &job)
}

func (a *cronAdminAdapter) UpdateJob(ctx context.Context, id string, updates map[string]any) error {
	job, err := a.scheduler.GetJob(ctx, id)
	if err != nil {
		return err
	}

	// Apply selective updates.
	if v, ok := updates["name"].(string); ok {
		job.Name = v
	}
	if v, ok := updates["description"].(string); ok {
		job.Description = v
	}
	if v, ok := updates["enabled"].(bool); ok {
		job.Enabled = v
	}
	if v, ok := updates["timeout_sec"].(float64); ok {
		job.TimeoutSec = int(v)
	}
	if v, ok := updates["delete_after_run"].(bool); ok {
		job.DeleteAfterRun = v
	}
	if v, ok := updates["silent"].(bool); ok {
		job.Silent = v
	}
	if v, ok := updates["max_retries"].(float64); ok {
		job.MaxRetries = int(v)
	}
	if v, ok := updates["max_runs"].(float64); ok {
		job.MaxRuns = int(v)
	}
	if v, ok := updates["expires_at"].(string); ok {
		job.ExpiresAt = v
	}

	if sched, ok := updates["schedule"].(map[string]any); ok {
		if kind, ok := sched["kind"].(string); ok {
			job.Schedule.Kind = cron.ScheduleKind(kind)
		}
		if at, ok := sched["at"].(string); ok {
			job.Schedule.At = at
		}
		if every, ok := sched["every_ms"].(float64); ok {
			job.Schedule.EveryMs = int64(every)
		}
		if expr, ok := sched["expr"].(string); ok {
			job.Schedule.Expr = expr
		}
		if tz, ok := sched["tz"].(string); ok {
			job.Schedule.TZ = tz
		}
	}

	if payload, ok := updates["payload"].(map[string]any); ok {
		if msg, ok := payload["message"].(string); ok {
			job.Payload.Message = msg
		}
		if tools, ok := payload["allowed_tools"].([]any); ok {
			var t []string
			for _, tool := range tools {
				if s, ok := tool.(string); ok {
					t = append(t, s)
				}
			}
			job.Payload.AllowedTools = t
		}
	}

	return a.scheduler.UpdateJob(ctx, job)
}

func (a *cronAdminAdapter) DeleteJob(ctx context.Context, id string) error {
	return a.scheduler.DeleteJob(ctx, id)
}

func (a *cronAdminAdapter) GetJob(ctx context.Context, id string) (any, error) {
	return a.scheduler.GetJob(ctx, id)
}

func (a *cronAdminAdapter) ListJobs(ctx context.Context) (any, error) {
	return a.scheduler.ListJobs(ctx)
}

func (a *cronAdminAdapter) TriggerJob(ctx context.Context, id string) error {
	job, err := a.scheduler.GetJob(ctx, id)
	if err != nil {
		return err
	}
	return a.scheduler.TriggerJob(ctx, job)
}

func (a *cronAdminAdapter) RunHistory(ctx context.Context, id string) (any, error) {
	job, err := a.scheduler.GetJob(ctx, id)
	if err != nil {
		return nil, err
	}

	sessionKey := session.DerivePlatformSessionKey(
		job.OwnerID, worker.TypeClaudeCode,
		session.PlatformContext{
			Platform: "cron",
			BotID:    job.BotID,
			UserID:   job.OwnerID,
			WorkDir:  job.WorkDir,
			ChatID:   job.ID,
		},
	)

	if a.turnsStore == nil {
		return nil, fmt.Errorf("eventstore not available")
	}
	return a.turnsStore.QueryTurnStats(ctx, sessionKey)
}
