package cron

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hrygo/hotplex/internal/metrics"
)

// timerLoop manages the scheduler's timer-driven tick cycle.
type timerLoop struct {
	scheduler *Scheduler
	timer     *time.Timer
	mu        sync.Mutex
	running   atomic.Int32
}

func newTimerLoop(s *Scheduler) *timerLoop {
	return &timerLoop{scheduler: s}
}

// tryAcquireSlot atomically checks the concurrency cap and reserves a slot.
// Returns false if the cap is reached.
func (tl *timerLoop) tryAcquireSlot(max int) bool {
	for {
		cur := tl.running.Load()
		if int(cur) >= max {
			return false
		}
		if tl.running.CompareAndSwap(cur, cur+1) {
			return true
		}
	}
}

func (tl *timerLoop) releaseSlot() {
	tl.running.Add(-1)
}

// arm sets the timer to fire at the given duration (capped at maxTimerInterval).
func (tl *timerLoop) arm(d time.Duration) {
	tl.mu.Lock()
	defer tl.mu.Unlock()

	if tl.timer != nil {
		tl.timer.Stop()
	}
	if d <= 0 {
		d = time.Second
	}
	if d > maxTimerInterval {
		d = maxTimerInterval
	}
	tl.timer = time.AfterFunc(d, tl.onTick)
}

func (tl *timerLoop) stop() {
	tl.mu.Lock()
	defer tl.mu.Unlock()
	if tl.timer != nil {
		tl.timer.Stop()
	}
}

const maxTimerInterval = 60 * time.Second

func (tl *timerLoop) onTick() {
	s := tl.scheduler
	if s.closed.Load() {
		return
	}

	now := time.Now()
	due := s.collectDue(now)
	if len(due) == 0 {
		tl.arm(s.nextTickDuration(now))
		return
	}

	s.log.Debug("cron: tick fired", "due_jobs", len(due), "now", now.Format(time.RFC3339))

	for _, job := range due {
		// At-most-once: advance next_run_at_ms BEFORE executing.
		// For all schedule types including "at": NextRun returns zero time
		// for past "at" schedules, which sets NextRunAtMs to a large negative
		// value. collectDue skips entries with NextRunAtMs <= 0, preventing
		// duplicate execution within the same tick cycle.
		next, err := NextRun(job.Schedule, now)
		if err != nil {
			s.log.Error("cron: compute next run", "job_id", job.ID, "err", err)
			job.State.ConsecutiveErrs++
			if job.State.ConsecutiveErrs >= 5 {
				s.log.Warn("cron: auto-disabling job after 5 schedule errors", "job_id", job.ID)
				job.Enabled = false
			}
		} else {
			job.State.NextRunAtMs = next.UnixMilli()
			job.State.ConsecutiveErrs = 0
		}

		// Persist state before execution.
		if err := s.store.UpdateState(s.ctx, job.ID, job.State); err != nil {
			s.log.Error("cron: persist state before execution", "job_id", job.ID, "err", err)
		}
		if !job.Enabled {
			if err := s.store.Update(s.ctx, job); err != nil {
				s.log.Error("cron: persist disabled job", "job_id", job.ID, "err", err)
			}
			s.putJob(job)
			continue
		}
		s.putJob(job)

		// Execute with concurrency cap.
		if !tl.tryAcquireSlot(s.maxConcurrent) {
			s.log.Warn("cron: concurrency cap reached, skipping job", "job_id", job.ID)
			continue
		}

		// Fresh clone for the goroutine — putJob stored the previous clone
		// into s.jobs, so we need an independent copy to avoid data races
		// when executeJob mutates state fields without holding s.mu.
		execJob := job.Clone()
		s.wg.Add(1)
		go func(j *CronJob) {
			defer func() {
				tl.releaseSlot()
				s.wg.Done()
				if r := recover(); r != nil {
					s.log.Error("cron: panic in executeJob",
						"job_id", j.ID, "name", j.Name, "panic", r)
				}
			}()
			metrics.CronFiresTotal.WithLabelValues(j.Name).Inc()
			s.executeJob(j)
		}(execJob)
	}

	tl.arm(s.nextTickDuration(now))
}

// executeJob runs a single job and updates its state.
// The job must be a clone (not a shared map pointer).
func (s *Scheduler) executeJob(job *CronJob) {
	now := time.Now().UnixMilli()
	job.State.RunningAtMs = now
	s.persistState(job.ID, job.State)
	s.putJob(job)

	timeout := s.jobTimeout(job)
	ctx, cancel := context.WithTimeout(s.ctx, timeout)
	defer cancel()

	start := time.Now()
	sessionKey, err := s.executor.Execute(ctx, job, timeout)
	duration := time.Since(start)

	metrics.CronDurationSeconds.WithLabelValues(job.Name).Observe(duration.Seconds())

	job.State.RunningAtMs = 0
	job.State.LastRunAtMs = now

	if err != nil {
		s.log.Error("cron: job execution failed",
			"job_id", job.ID, "name", job.Name, "duration", duration, "err", err)
		job.State.LastStatus = StatusFailed
		job.State.ConsecutiveErrs++
		metrics.CronErrorsTotal.WithLabelValues(job.Name, errorType(err)).Inc()

		// One-shot retry logic.
		if job.Schedule.Kind == ScheduleAt && isTemporaryError(err) && job.State.RetryCount < maxRetries(job) {
			s.scheduleRetry(s.ctx, job)
			return
		}
	} else {
		job.State.LastStatus = StatusSuccess
		job.State.ConsecutiveErrs = 0
		job.State.RunCount++
		resetRetry(job)
		if s.delivery != nil && !HasCLIDelivery(job) {
			s.delivery.Deliver(s.ctx, job, sessionKey)
		}
	}

	// One-shot: disable or delete after run (success or permanent error).
	if job.Schedule.Kind == ScheduleAt {
		if job.DeleteAfterRun {
			if err := s.store.Delete(s.persistCtx(), job.ID); err != nil {
				s.log.Error("cron: delete one-shot job", "job_id", job.ID, "err", err)
			}
			s.mu.Lock()
			delete(s.jobs, job.ID)
			s.mu.Unlock()
			return
		}
		job.Enabled = false
	}

	// Lifecycle: check max_runs and expires_at.
	if job.Enabled {
		if job.MaxRuns > 0 && job.State.RunCount >= job.MaxRuns {
			s.log.Info("cron: job reached max_runs, disabling",
				"job_id", job.ID, "name", job.Name, "run_count", job.State.RunCount, "max_runs", job.MaxRuns)
			job.Enabled = false
		} else if job.ExpiresAt != "" {
			if t, perr := time.Parse(time.RFC3339, job.ExpiresAt); perr == nil && time.Now().After(t) {
				s.log.Info("cron: job expired, disabling",
					"job_id", job.ID, "name", job.Name, "expires_at", job.ExpiresAt)
				job.Enabled = false
			}
		}
	}

	s.persistState(job.ID, job.State)
	s.putJob(job)
}

// persistState saves job state to the store, using a background context
// so final state is not lost during scheduler shutdown.
func (s *Scheduler) persistState(jobID string, state CronJobState) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.store.UpdateState(ctx, jobID, state); err != nil {
		s.log.Error("cron: persist state", "job_id", jobID, "err", err)
	}
}

// persistCtx returns a background context for store operations that must
// survive scheduler shutdown (e.g., deleting a completed one-shot job).
func (s *Scheduler) persistCtx() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	go cancel()
	return ctx
}

// jobTimeout returns the timeout for a job, falling back to the scheduler default.
func (s *Scheduler) jobTimeout(job *CronJob) time.Duration {
	if job.TimeoutSec > 0 {
		return time.Duration(job.TimeoutSec) * time.Second
	}
	return s.defaultTimeout
}

// errorType classifies an error for the error_type metric label.
func errorType(err error) string {
	if err == nil {
		return "unknown"
	}
	msg := strings.ToLower(err.Error())
	for _, tok := range []string{"timeout", "deadline exceeded"} {
		if strings.Contains(msg, tok) {
			return "timeout"
		}
	}
	for _, tok := range []string{"rate limit", "429"} {
		if strings.Contains(msg, tok) {
			return "rate_limit"
		}
	}
	for _, tok := range []string{"500", "502", "503", "504"} {
		if strings.Contains(msg, tok) {
			return "server_error"
		}
	}
	return "execution"
}
