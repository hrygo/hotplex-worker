package cron

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// Scheduler manages cron job lifecycle: loading, scheduling, execution, and shutdown.
type Scheduler struct {
	log            *slog.Logger
	store          Store
	executor       *Executor
	delivery       *Delivery
	maxConcurrent  int
	maxJobs        int
	defaultTimeout time.Duration

	mu     sync.Mutex
	jobs   map[string]*CronJob // in-memory index
	wg     sync.WaitGroup
	closed atomic.Bool

	ctx      context.Context
	cancelFn context.CancelFunc

	tickLoop *timerLoop
	yamlDefs []YAMLJobDef
}

// Config holds scheduler configuration from the main config file.
type Config struct {
	Enabled           bool   `mapstructure:"enabled"`
	MaxConcurrentRuns int    `mapstructure:"max_concurrent_runs"`
	MaxJobs           int    `mapstructure:"max_jobs"`
	DefaultTimeoutSec int    `mapstructure:"default_timeout_sec"`
	TickIntervalSec   int    `mapstructure:"tick_interval_sec"`
	YAMLConfigPath    string `mapstructure:"yaml_config_path"`
}

// Dependencies for creating a Scheduler.
type Deps struct {
	Log        *slog.Logger
	Store      Store
	Bridge     BridgeStarter
	SessionMgr SessionStateChecker
	Delivery   *Delivery
	YAMLDefs   []YAMLJobDef
	Cfg        Config
}

// New creates a new cron Scheduler.
func New(deps Deps) *Scheduler {
	maxConcurrent := deps.Cfg.MaxConcurrentRuns
	if maxConcurrent <= 0 {
		maxConcurrent = 3
	}
	maxJobs := deps.Cfg.MaxJobs
	if maxJobs <= 0 {
		maxJobs = 50
	}

	s := &Scheduler{
		log:           deps.Log.With("component", "cron"),
		store:         deps.Store,
		maxConcurrent: maxConcurrent,
		maxJobs:       maxJobs,
		delivery:      deps.Delivery,
		jobs:          make(map[string]*CronJob),
	}
	defaultTimeout := 5 * time.Minute
	if deps.Cfg.DefaultTimeoutSec > 0 {
		defaultTimeout = time.Duration(deps.Cfg.DefaultTimeoutSec) * time.Second
	}
	s.defaultTimeout = defaultTimeout
	s.executor = NewExecutor(deps.Log, deps.Bridge, deps.SessionMgr)
	s.yamlDefs = deps.YAMLDefs
	s.tickLoop = newTimerLoop(s)
	return s
}

// Start loads jobs from the database (and optional YAML), computes initial next-run times,
// and arms the timer loop.
func (s *Scheduler) Start(ctx context.Context) error {
	s.log.Info("cron: starting scheduler")

	ctx, cancel := context.WithCancel(ctx)
	s.ctx = ctx
	s.cancelFn = cancel

	if err := s.loadFromDB(ctx); err != nil {
		return fmt.Errorf("cron: load from db: %w", err)
	}

	if len(s.yamlDefs) > 0 {
		if err := s.LoadFromYAML(ctx, s.yamlDefs); err != nil {
			s.log.Warn("cron: YAML import failed", "err", err)
		}
	}

	s.log.Info("cron: scheduler started", "jobs", len(s.jobs))
	s.releaseSkillManual()

	s.tickLoop.arm(s.nextTickDuration(time.Now()))
	return nil
}

// Shutdown stops the timer and waits for running jobs to drain.
func (s *Scheduler) Shutdown(ctx context.Context) {
	s.log.Info("cron: shutting down scheduler")
	s.closed.Store(true)
	if s.cancelFn != nil {
		s.cancelFn()
	}
	s.tickLoop.stop()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		s.log.Info("cron: all running jobs completed")
	case <-ctx.Done():
		s.log.Warn("cron: shutdown timeout, some jobs may still be running")
	}
}

// CreateJob validates and persists a new cron job.
func (s *Scheduler) CreateJob(ctx context.Context, job *CronJob) error {
	if err := ValidateJob(job); err != nil {
		return err
	}
	if len(s.jobs) >= s.maxJobs {
		return fmt.Errorf("cron: max jobs limit reached (%d)", s.maxJobs)
	}

	now := time.Now().UnixMilli()
	job.CreatedAtMs = now
	job.UpdatedAtMs = now
	job.Enabled = true

	next, err := NextRun(job.Schedule, time.Now())
	if err != nil {
		return fmt.Errorf("cron: compute initial next run: %w", err)
	}
	job.State.NextRunAtMs = next.UnixMilli()

	if err := s.store.Create(ctx, job); err != nil {
		return err
	}

	s.mu.Lock()
	s.jobs[job.ID] = job
	s.mu.Unlock()

	s.log.Info("cron: job created", "job_id", job.ID, "name", job.Name, "next_run", time.UnixMilli(next.UnixMilli()).Format(time.RFC3339))
	s.tickLoop.arm(s.nextTickDuration(time.Now()))
	return nil
}

// UpdateJob updates an existing job definition.
func (s *Scheduler) UpdateJob(ctx context.Context, job *CronJob) error {
	if err := ValidateJob(job); err != nil {
		return err
	}
	job.UpdatedAtMs = time.Now().UnixMilli()

	next, err := NextRun(job.Schedule, time.Now())
	if err != nil {
		return fmt.Errorf("cron: compute next run: %w", err)
	}
	job.State.NextRunAtMs = next.UnixMilli()

	if err := s.store.Update(ctx, job); err != nil {
		return err
	}

	s.mu.Lock()
	s.jobs[job.ID] = job
	s.mu.Unlock()

	s.log.Info("cron: job updated", "job_id", job.ID, "name", job.Name)
	s.tickLoop.arm(s.nextTickDuration(time.Now()))
	return nil
}

// DeleteJob removes a job.
func (s *Scheduler) DeleteJob(ctx context.Context, id string) error {
	if err := s.store.Delete(ctx, id); err != nil {
		return err
	}

	s.mu.Lock()
	delete(s.jobs, id)
	s.mu.Unlock()

	s.log.Info("cron: job deleted", "job_id", id)
	return nil
}

// GetJob returns a clone of the job by ID.
func (s *Scheduler) GetJob(ctx context.Context, id string) (*CronJob, error) {
	s.mu.Lock()
	j, ok := s.jobs[id]
	s.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrJobNotFound, id)
	}
	return j.Clone(), nil
}

// ListJobs returns clones of all jobs.
func (s *Scheduler) ListJobs(ctx context.Context) ([]*CronJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	result := make([]*CronJob, 0, len(s.jobs))
	for _, j := range s.jobs {
		result = append(result, j.Clone())
	}
	return result, nil
}

// TriggerJob manually triggers a job execution outside of its schedule.
func (s *Scheduler) TriggerJob(ctx context.Context, job *CronJob) error {
	if !s.tickLoop.tryAcquireSlot(s.maxConcurrent) {
		return fmt.Errorf("cron: concurrency cap (%d) reached, cannot trigger job %s", s.maxConcurrent, job.ID)
	}
	j := job.Clone()
	s.wg.Add(1)
	go func() {
		defer func() {
			s.tickLoop.releaseSlot()
			s.wg.Done()
		}()
		s.executeJob(j)
	}()
	return nil
}

// loadFromDB loads all jobs from SQLite into the in-memory index.
// It clears stale running_at_ms, applies catch-up logic for missed jobs within grace period,
// and recomputes next_run for past-due recurring jobs.
func (s *Scheduler) loadFromDB(ctx context.Context) error {
	jobs, err := s.store.List(ctx, false)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	var catchUp []*CronJob

	for _, job := range jobs {
		// Clear stale running_at_ms from previous crash.
		if job.State.RunningAtMs > 0 {
			job.State.RunningAtMs = 0
			_ = s.store.UpdateState(ctx, job.ID, job.State)
		}

		if !job.Enabled || job.State.NextRunAtMs <= 0 {
			s.jobs[job.ID] = job
			continue
		}

		// Job is past due — check catch-up eligibility.
		if job.State.NextRunAtMs <= now.UnixMilli() {
			if s.withinGracePeriod(job, now) {
				catchUp = append(catchUp, job)
			} else {
				// Outside grace period — recompute next run for recurring jobs.
				next, err := NextRun(job.Schedule, now)
				if err != nil {
					s.log.Warn("cron: skip job with invalid schedule", "job_id", job.ID, "err", err)
					s.jobs[job.ID] = job
					continue
				}
				if next.IsZero() {
					// One-shot past due and outside grace → disable.
					job.Enabled = false
					_ = s.store.Update(ctx, job)
				} else {
					job.State.NextRunAtMs = next.UnixMilli()
					_ = s.store.UpdateState(ctx, job.ID, job.State)
				}
			}
		}
		s.jobs[job.ID] = job
	}

	// Execute catch-up jobs (max 5 immediate, rest staggered 5s).
	s.scheduleCatchUp(catchUp)
	return nil
}

// withinGracePeriod checks whether a missed job is within its grace period.
// Grace period = 50% of schedule interval, capped at 2 hours.
func (s *Scheduler) withinGracePeriod(job *CronJob, now time.Time) bool {
	missedAt := time.UnixMilli(job.State.NextRunAtMs)
	elapsed := now.Sub(missedAt)

	interval := s.scheduleInterval(job)
	grace := interval / 2
	if grace > 2*time.Hour {
		grace = 2 * time.Hour
	}

	return elapsed <= grace
}

// scheduleInterval returns the approximate interval between runs for a job.
func (s *Scheduler) scheduleInterval(job *CronJob) time.Duration {
	switch job.Schedule.Kind {
	case ScheduleEvery:
		return time.Duration(job.Schedule.EveryMs) * time.Millisecond
	case ScheduleCron:
		// Estimate by computing delta between two consecutive runs.
		now := time.Now()
		next, err := NextRun(job.Schedule, now)
		if err != nil {
			return time.Hour // fallback
		}
		next2, err := NextRun(job.Schedule, next.Add(time.Second))
		if err != nil {
			return time.Hour
		}
		return next2.Sub(next)
	case ScheduleAt:
		// One-shot: use the time until scheduled run from creation.
		if job.CreatedAtMs > 0 && job.State.NextRunAtMs > 0 {
			return time.Duration(job.State.NextRunAtMs-job.CreatedAtMs) * time.Millisecond
		}
		return time.Hour
	default:
		return time.Hour
	}
}

// scheduleCatchUp executes missed jobs with staggering.
// First 5 jobs fire immediately; remaining jobs are staggered 5s apart.
func (s *Scheduler) scheduleCatchUp(jobs []*CronJob) {
	if len(jobs) == 0 {
		return
	}
	s.log.Info("cron: catch-up", "missed_jobs", len(jobs))

	for i, job := range jobs {
		delay := 0
		if i >= 5 {
			delay = (i - 5 + 1) * 5 // 5s, 10s, 15s, ...
		}
		j := job.Clone()
		if !s.tickLoop.tryAcquireSlot(s.maxConcurrent) {
			s.log.Warn("cron: catch-up skipped, concurrency cap reached", "job_id", j.ID)
			continue
		}
		s.wg.Add(1)
		go func(d int) {
			defer func() {
				s.tickLoop.releaseSlot()
				s.wg.Done()
			}()
			if d > 0 {
				time.Sleep(time.Duration(d) * time.Second)
			}
			if s.closed.Load() {
				return
			}
			s.log.Info("cron: catch-up executing", "job_id", j.ID, "name", j.Name, "delay_sec", d)
			s.executeJob(j)
		}(delay)
	}
}

// collectDue returns clones of all enabled, non-running jobs whose next_run_at_ms <= now.
// Returns copies so callers can mutate without racing with the map.
func (s *Scheduler) collectDue(now time.Time) []*CronJob {
	s.mu.Lock()
	defer s.mu.Unlock()

	var due []*CronJob
	for _, job := range s.jobs {
		if !job.Enabled {
			continue
		}
		if job.State.RunningAtMs > 0 {
			continue
		}
		if job.State.NextRunAtMs > 0 && job.State.NextRunAtMs <= now.UnixMilli() {
			due = append(due, job.Clone())
		}
	}
	return due
}

// nextTickDuration returns the duration until the next job is due, capped at maxTimerInterval.
func (s *Scheduler) nextTickDuration(now time.Time) time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()

	var earliest int64
	for _, job := range s.jobs {
		if !job.Enabled || job.State.NextRunAtMs <= 0 {
			continue
		}
		if earliest == 0 || job.State.NextRunAtMs < earliest {
			earliest = job.State.NextRunAtMs
		}
	}

	if earliest == 0 {
		return maxTimerInterval
	}

	d := time.UnixMilli(earliest).Sub(now)
	if d <= 0 {
		return time.Second
	}
	return d
}

// mergeJobState updates only the State field of the in-memory job and optionally
// disables it. Unlike a full replace, this preserves concurrent changes
// to Enabled, Schedule, etc. — preventing a goroutine's stale clone from
// overwriting an external disable.
func (s *Scheduler) mergeJobState(jobID string, state CronJobState, disable bool) {
	s.mu.Lock()
	if j, ok := s.jobs[jobID]; ok {
		j.State = state
		if disable {
			j.Enabled = false
		}
	}
	s.mu.Unlock()
}

// ReloadIndex refreshes the in-memory index from the store and re-arms the timer.
// Called externally (e.g. via SIGHUP) after CLI mutations.
func (s *Scheduler) ReloadIndex() {
	s.rebuildIndex()
	s.tickLoop.arm(s.nextTickDuration(time.Now()))
	s.log.Info("cron: index reloaded")
}

// rebuildIndex refreshes the in-memory index from the store.
func (s *Scheduler) rebuildIndex() {
	jobs, err := s.store.List(context.Background(), false)
	if err != nil {
		s.log.Error("cron: rebuild index failed", "err", err)
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.jobs = make(map[string]*CronJob, len(jobs))
	now := time.Now()
	for _, j := range jobs {
		if j.Enabled && j.State.NextRunAtMs <= 0 {
			if next, err := NextRun(j.Schedule, now); err == nil && !next.IsZero() {
				j.State.NextRunAtMs = next.UnixMilli()
				if err := s.store.UpdateState(context.Background(), j.ID, j.State); err != nil {
					s.log.Error("cron: persist next_run on reload", "job_id", j.ID, "err", err)
				}
			}
		}
		s.jobs[j.ID] = j
	}
}

// GenerateJobID creates a unique cron job identifier.
func GenerateJobID() string {
	return "cron_" + uuid.New().String()
}

func (s *Scheduler) releaseSkillManual() {
	home, err := os.UserHomeDir()
	if err != nil {
		s.log.Warn("cron: cannot determine home dir for skill manual release", "err", err)
		return
	}
	dir := filepath.Join(home, ".hotplex", "skills")
	_ = os.MkdirAll(dir, 0o755)
	path := filepath.Join(dir, "cron.md")
	if err := os.WriteFile(path, []byte(SkillManual()), 0o644); err != nil {
		s.log.Warn("cron: failed to release skill manual", "path", path, "err", err)
		return
	}
	s.log.Debug("cron: skill manual released", "path", path)
}

// UpdateConfig applies live configuration changes without restart.
func (s *Scheduler) UpdateConfig(maxConcurrent, maxJobs int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if maxConcurrent > 0 {
		s.maxConcurrent = maxConcurrent
	}
	if maxJobs > 0 {
		s.maxJobs = maxJobs
	}
	s.log.Info("cron: config updated", "max_concurrent", s.maxConcurrent, "max_jobs", s.maxJobs)
}
