package cron

import (
	"context"
	"strings"
	"time"
)

// backoffDurations defines exponential backoff intervals for consecutive failures.
var backoffDurations = []time.Duration{
	30 * time.Second,
	1 * time.Minute,
	5 * time.Minute,
	15 * time.Minute,
	1 * time.Hour,
}

// backoff returns the backoff duration for the given consecutive error count.
// After exhausting the list, it returns 1 hour.
func backoff(consecutiveErrs int) time.Duration {
	if consecutiveErrs <= 0 {
		return backoffDurations[0]
	}
	if consecutiveErrs >= len(backoffDurations) {
		return backoffDurations[len(backoffDurations)-1]
	}
	return backoffDurations[consecutiveErrs]
}

// isTemporaryError classifies an execution error as retriable or permanent.
// Temporary: timeout, rate-limit, 5xx.
// Permanent: everything else (invalid config, auth failure, etc.).
func isTemporaryError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return containsAny(msg,
		"timeout",
		"deadline exceeded",
		"rate limit",
		"429",
		"500",
		"502",
		"503",
		"504",
		"connection refused",
		"temporary",
	)
}

func containsAny(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// maxRetries returns the effective max retries for a job (default 3).
func maxRetries(job *CronJob) int {
	if job.MaxRetries > 0 {
		return job.MaxRetries
	}
	return 3
}

// scheduleRetry advances the job's next_run to a backoff time for retry.
func (s *Scheduler) scheduleRetry(ctx context.Context, job *CronJob) {
	delay := backoff(job.State.ConsecutiveErrs)
	nextRun := time.Now().Add(delay)
	job.State.NextRunAtMs = nextRun.UnixMilli()
	job.State.RetryCount++
	_ = s.store.UpdateState(ctx, job.ID, job.State)
	s.mergeJobState(job.ID, job.State, false)

	s.log.Info("cron: retry scheduled",
		"job_id", job.ID, "name", job.Name,
		"retry", job.State.RetryCount, "delay", delay, "next_run", nextRun.Format(time.RFC3339))
}

// resetRetry resets retry state after a successful execution.
func resetRetry(job *CronJob) {
	job.State.RetryCount = 0
}
