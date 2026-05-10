package cron

import (
	"context"
	"fmt"
	"time"
)

// YAMLJobDef represents a job definition from the YAML config file.
type YAMLJobDef struct {
	Name           string   `mapstructure:"name" yaml:"name"`
	Description    string   `mapstructure:"description" yaml:"description"`
	Schedule       string   `mapstructure:"schedule" yaml:"schedule"`                   // cron expression or "at:..."
	ScheduleAt     string   `mapstructure:"schedule_at" yaml:"schedule_at"`             // ISO-8601 for one-shot
	ScheduleEvery  int64    `mapstructure:"schedule_every_ms" yaml:"schedule_every_ms"` // interval in ms
	Prompt         string   `mapstructure:"prompt" yaml:"prompt"`
	WorkDir        string   `mapstructure:"work_dir" yaml:"work_dir"`
	BotID          string   `mapstructure:"bot_id" yaml:"bot_id"`
	OwnerID        string   `mapstructure:"owner_id" yaml:"owner_id"`
	Platform       string   `mapstructure:"platform" yaml:"platform"`
	TimeoutSec     int      `mapstructure:"timeout_sec" yaml:"timeout_sec"`
	DeleteAfterRun bool     `mapstructure:"delete_after_run" yaml:"delete_after_run"`
	MaxRetries     int      `mapstructure:"max_retries" yaml:"max_retries"`
	MaxRuns        int      `mapstructure:"max_runs" yaml:"max_runs"`
	ExpiresAt      string   `mapstructure:"expires_at" yaml:"expires_at"`
	AllowedTools   []string `mapstructure:"allowed_tools" yaml:"allowed_tools"`
}

// LoadFromYAML imports job definitions from YAML config into the store.
// Uses name as idempotency key: existing jobs are updated, new ones are created.
func (s *Scheduler) LoadFromYAML(ctx context.Context, defs []YAMLJobDef) error {
	s.log.Info("cron: loading YAML job definitions", "count", len(defs))

	var created, updated int
	for _, def := range defs {
		if def.Name == "" {
			s.log.Warn("cron: skipping YAML job without name")
			continue
		}

		job, err := yamlDefToJob(def)
		if err != nil {
			s.log.Error("cron: invalid YAML job definition", "name", def.Name, "err", err)
			continue
		}

		existing, _ := s.store.GetByName(ctx, def.Name)
		if existing != nil {
			copyJobDefinition(existing, job)

			// Recompute next run after schedule change.
			next, err := NextRun(existing.Schedule, time.Now())
			if err != nil {
				s.log.Error("cron: recompute next run for YAML job", "name", def.Name, "err", err)
				continue
			}
			existing.State.NextRunAtMs = next.UnixMilli()

			if err := s.store.Update(ctx, existing); err != nil {
				s.log.Error("cron: update YAML job", "name", def.Name, "err", err)
				continue
			}
			updated++
		} else {
			if err := s.store.Create(ctx, job); err != nil {
				s.log.Error("cron: create YAML job", "name", def.Name, "err", err)
				continue
			}
			created++
		}
	}

	s.log.Info("cron: YAML import complete", "created", created, "updated", updated)

	// Rebuild in-memory index after batch import.
	s.rebuildIndex()
	s.tickLoop.arm(s.nextTickDuration(time.Now()))
	return nil
}

func yamlDefToJob(def YAMLJobDef) (*CronJob, error) {
	sched, err := parseYAMLSchedule(def)
	if err != nil {
		return nil, err
	}

	now := time.Now().UnixMilli()
	job := &CronJob{
		ID:          GenerateJobID(),
		Name:        def.Name,
		Description: def.Description,
		Enabled:     true,
		Schedule:    sched,
		Payload: CronPayload{
			Kind:         PayloadAgentTurn,
			Message:      def.Prompt,
			AllowedTools: def.AllowedTools,
		},
		WorkDir:        def.WorkDir,
		BotID:          def.BotID,
		OwnerID:        def.OwnerID,
		Platform:       def.Platform,
		PlatformKey:    map[string]string{},
		TimeoutSec:     def.TimeoutSec,
		DeleteAfterRun: def.DeleteAfterRun,
		MaxRetries:     def.MaxRetries,
		MaxRuns:        def.MaxRuns,
		ExpiresAt:      def.ExpiresAt,
		CreatedAtMs:    now,
		UpdatedAtMs:    now,
	}

	next, err := NextRun(job.Schedule, time.Now())
	if err != nil {
		return nil, fmt.Errorf("compute initial next run: %w", err)
	}
	job.State.NextRunAtMs = next.UnixMilli()

	return job, nil
}

func parseYAMLSchedule(def YAMLJobDef) (CronSchedule, error) {
	switch {
	case def.ScheduleAt != "":
		return CronSchedule{Kind: ScheduleAt, At: def.ScheduleAt}, nil
	case def.ScheduleEvery > 0:
		return CronSchedule{Kind: ScheduleEvery, EveryMs: def.ScheduleEvery}, nil
	case def.Schedule != "":
		return CronSchedule{Kind: ScheduleCron, Expr: def.Schedule}, nil
	default:
		return CronSchedule{}, fmt.Errorf("cron: no schedule specified for job %q", def.Name)
	}
}
