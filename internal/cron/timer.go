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
		// Skip for "at" schedules — NextRun returns zero for past timestamps,
		// which silently loses the job on crash. executeJob handles post-execution
		// disable/delete.
		if job.Schedule.Kind != ScheduleAt {
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
		}

		// Persist state before execution.
		if err := s.store.UpdateState(s.ctx, job.ID, job.State); err != nil {
			s.log.Error("cron: persist state before execution", "job_id", job.ID, "err", err)
		}
		if !job.Enabled {
			if err := s.store.Update(s.ctx, job); err != nil {
				s.log.Error("cron: persist disabled job", "job_id", job.ID, "err", err)
			}
			s.rebuildIndex()
			continue
		}

		// Execute with concurrency cap.
		if int(tl.running.Load()) >= s.maxConcurrent {
			s.log.Warn("cron: concurrency cap reached, skipping job", "job_id", job.ID)
			continue
		}

		tl.running.Add(1)
		s.wg.Add(1)
		go func(j *CronJob) {
			defer func() {
				tl.running.Add(-1)
				s.wg.Done()
				if r := recover(); r != nil {
					s.log.Error("cron: panic in executeJob",
						"job_id", j.ID, "name", j.Name, "panic", r)
				}
			}()
			metrics.CronFiresTotal.WithLabelValues(j.Name).Inc()
			s.executeJob(j)
		}(job)
	}

	tl.arm(s.nextTickDuration(now))
}

// executeJob runs a single job and updates its state.
func (s *Scheduler) executeJob(job *CronJob) {
	now := time.Now().UnixMilli()
	job.State.RunningAtMs = now
	if err := s.store.UpdateState(s.ctx, job.ID, job.State); err != nil {
		s.log.Error("cron: persist running state", "job_id", job.ID, "err", err)
	}

	timeout := s.jobTimeout(job)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	start := time.Now()
	sessionKey, err := s.executor.Execute(ctx, job, s.jobTimeout(job))
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
			s.scheduleRetry(context.Background(), job)
			s.rebuildIndex()
			return
		}
	} else {
		job.State.LastStatus = StatusSuccess
		job.State.ConsecutiveErrs = 0
		resetRetry(job)
		if s.delivery != nil {
			s.delivery.Deliver(context.Background(), job, sessionKey)
		}
	}

	// One-shot: disable or delete after run (success or permanent error).
	if job.Schedule.Kind == ScheduleAt {
		if job.DeleteAfterRun {
			if err := s.store.Delete(s.ctx, job.ID); err != nil {
				s.log.Error("cron: delete one-shot job", "job_id", job.ID, "err", err)
			}
			s.mu.Lock()
			delete(s.jobs, job.ID)
			s.mu.Unlock()
			return
		}
		job.Enabled = false
	}

	if err := s.store.UpdateState(s.ctx, job.ID, job.State); err != nil {
		s.log.Error("cron: persist final state", "job_id", job.ID, "err", err)
	}
	s.rebuildIndex()
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
