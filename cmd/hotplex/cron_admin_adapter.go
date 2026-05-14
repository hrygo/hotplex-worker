package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hrygo/hotplex/internal/cron"
	"github.com/hrygo/hotplex/internal/eventstore"
	"github.com/hrygo/hotplex/internal/gateway"
	"github.com/hrygo/hotplex/internal/session"
)

// cronAdminAdapter bridges cron.Scheduler to admin.CronSchedulerProvider.
type cronAdminAdapter struct {
	scheduler  *cron.Scheduler
	turnsStore *eventstore.SQLiteStore
}

// Fields that must not be overwritten via admin API UpdateJob.
var protectedFields = map[string]struct{}{
	"id":            {},
	"created_at_ms": {},
	"updated_at_ms": {},
	"state":         {},
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

	updated, err := mergeJobUpdates(job, updates)
	if err != nil {
		return err
	}
	return a.scheduler.UpdateJob(ctx, updated)
}

// mergeJobUpdates overlays user-supplied updates onto a CronJob via JSON merge.
// Nested objects (schedule, payload) are replaced entirely, not deep-merged.
// Protected fields (id, created_at_ms, updated_at_ms, state) are silently dropped.
func mergeJobUpdates(job *cron.CronJob, updates map[string]any) (*cron.CronJob, error) {
	base, err := json.Marshal(job)
	if err != nil {
		return nil, fmt.Errorf("marshal job: %w", err)
	}
	var merged map[string]any
	if err := json.Unmarshal(base, &merged); err != nil {
		return nil, fmt.Errorf("unmarshal job: %w", err)
	}
	for k, v := range updates {
		if _, ok := protectedFields[k]; ok {
			continue
		}
		merged[k] = v
	}
	data, err := json.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("re-marshal: %w", err)
	}
	var updated cron.CronJob
	if err := json.Unmarshal(data, &updated); err != nil {
		return nil, fmt.Errorf("unmarshal updated: %w", err)
	}
	return &updated, nil
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

	if a.turnsStore == nil {
		return nil, fmt.Errorf("eventstore not available")
	}
	return a.turnsStore.QueryTurnStats(ctx, job.SessionKey())
}

// cronAttachedRouter implements cron.AttachedSessionRouter using Bridge + SessionManager.
type cronAttachedRouter struct {
	bridge *gateway.Bridge
	sm     *session.Manager
}

func (r *cronAttachedRouter) GetSessionInfo(ctx context.Context, id string) (*session.SessionInfo, error) {
	return r.sm.Get(ctx, id)
}

func (r *cronAttachedRouter) InjectInput(ctx context.Context, sessionID, prompt string, metadata map[string]any) error {
	w := r.sm.GetWorker(sessionID)
	if w == nil {
		return fmt.Errorf("no worker for session %s", sessionID)
	}
	return w.Input(ctx, prompt, metadata)
}

func (r *cronAttachedRouter) ResumeAndInput(ctx context.Context, sessionID, workDir, prompt string, metadata map[string]any) error {
	if err := r.bridge.ResumeSession(ctx, sessionID, workDir); err != nil {
		return fmt.Errorf("resume session: %w", err)
	}
	w := r.sm.GetWorker(sessionID)
	if w == nil {
		return fmt.Errorf("no worker after resume for session %s", sessionID)
	}
	return w.Input(ctx, prompt, metadata)
}
