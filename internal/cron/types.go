package cron

import (
	"github.com/hrygo/hotplex/internal/session"
	"github.com/hrygo/hotplex/internal/worker"
)

// Schedule kinds.
type ScheduleKind string

const (
	ScheduleAt    ScheduleKind = "at"
	ScheduleEvery ScheduleKind = "every"
	ScheduleCron  ScheduleKind = "cron"
)

// CronSchedule defines when a job fires.
type CronSchedule struct {
	Kind    ScheduleKind `json:"kind"`
	At      string       `json:"at,omitempty"`       // kind=at: ISO-8601 timestamp
	EveryMs int64        `json:"every_ms,omitempty"` // kind=every: interval in ms
	Expr    string       `json:"expr,omitempty"`     // kind=cron: cron expression
	TZ      string       `json:"tz,omitempty"`       // timezone, default Local
}

// Payload kinds.
type PayloadKind string

const (
	PayloadIsolatedSession PayloadKind = "isolated_session" // renamed from agent_turn
	PayloadSystemEvent     PayloadKind = "system_event"     // reserved
	PayloadAttachedSession PayloadKind = "attached_session" // inject existing session
)

// CronPayload defines what a job executes.
type CronPayload struct {
	Kind            PayloadKind `json:"kind"`
	Message         string      `json:"message"`
	TargetSessionID string      `json:"target_session_id,omitempty"` // attached_session only
	AllowedTools    []string    `json:"allowed_tools,omitempty"`
	WorkerType      string      `json:"worker_type,omitempty"` // e.g. "claude_code"
}

// JobStatus records the outcome of the last run.
type JobStatus string

const (
	StatusSuccess JobStatus = "success"
	StatusFailed  JobStatus = "failed"
	StatusTimeout JobStatus = "timeout"
)

// CronJobState holds mutable runtime state.
type CronJobState struct {
	NextRunAtMs     int64     `json:"next_run_at_ms"`
	LastRunAtMs     int64     `json:"last_run_at_ms"`
	RunningAtMs     int64     `json:"running_at_ms"`
	LastStatus      JobStatus `json:"last_status,omitempty"`
	ConsecutiveErrs int       `json:"consecutive_errors"` // execution failures
	SchedErrs       int       `json:"sched_errors"`       // schedule computation failures
	RetryCount      int       `json:"retry_count,omitempty"`
	LastRunID       string    `json:"last_run_id,omitempty"`
	RunCount        int       `json:"run_count,omitempty"`
}

// SessionKey derives the deterministic session key for this cron job's execution history.
func (j *CronJob) SessionKey() string {
	return session.DerivePlatformSessionKey(
		j.OwnerID, worker.TypeClaudeCode,
		session.PlatformContext{
			Platform: "cron",
			BotID:    j.BotID,
			UserID:   j.OwnerID,
			WorkDir:  j.WorkDir,
			ChatID:   j.ID,
		},
	)
}

// Clone returns a deep copy of the job, including reference-type fields
// (PlatformKey map, AllowedTools slice), so the clone is safe for concurrent mutation.
func (j *CronJob) Clone() *CronJob {
	cp := *j
	if j.PlatformKey != nil {
		cp.PlatformKey = make(map[string]string, len(j.PlatformKey))
		for k, v := range j.PlatformKey {
			cp.PlatformKey[k] = v
		}
	}
	if j.Payload.AllowedTools != nil {
		cp.Payload.AllowedTools = make([]string, len(j.Payload.AllowedTools))
		copy(cp.Payload.AllowedTools, j.Payload.AllowedTools)
	}
	return &cp
}

// CronJob is the top-level job entity.
type CronJob struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Description    string            `json:"description,omitempty"`
	Enabled        bool              `json:"enabled"`
	Schedule       CronSchedule      `json:"schedule"`
	Payload        CronPayload       `json:"payload"`
	WorkDir        string            `json:"work_dir,omitempty"`
	BotID          string            `json:"bot_id,omitempty"`
	OwnerID        string            `json:"owner_id,omitempty"`
	Platform       string            `json:"platform,omitempty"`
	PlatformKey    map[string]string `json:"platform_key,omitempty"`
	TimeoutSec     int               `json:"timeout_sec,omitempty"`
	DeleteAfterRun bool              `json:"delete_after_run,omitempty"`
	Silent         bool              `json:"silent,omitempty"`
	MaxRetries     int               `json:"max_retries,omitempty"`
	MaxRuns        int               `json:"max_runs,omitempty"`
	ExpiresAt      string            `json:"expires_at,omitempty"`
	State          CronJobState      `json:"state"`
	CreatedAtMs    int64             `json:"created_at_ms"`
	UpdatedAtMs    int64             `json:"updated_at_ms"`
}
