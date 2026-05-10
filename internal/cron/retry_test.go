package cron

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBackoff(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		errs      int
		wantDelay time.Duration
	}{
		{"first retry", 0, 30 * time.Second},
		{"second retry", 1, 1 * time.Minute},
		{"third retry", 2, 5 * time.Minute},
		{"fourth retry", 3, 15 * time.Minute},
		{"fifth retry", 4, 1 * time.Hour},
		{"exhausted stays at 1h", 5, 1 * time.Hour},
		{"large count stays at 1h", 100, 1 * time.Hour},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := backoff(tt.errs)
			require.Equal(t, tt.wantDelay, got)
		})
	}
}

func TestIsTemporaryError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		temp bool
	}{
		{"nil error", nil, false},
		{"timeout", errors.New("context deadline exceeded"), true},
		{"Timeout uppercase", errors.New("Timeout waiting for response"), true},
		{"rate limit 429", errors.New("HTTP 429 rate limit exceeded"), true},
		{"500 error", errors.New("server returned 500 Internal Server Error"), true},
		{"502 error", errors.New("bad gateway 502"), true},
		{"503 error", errors.New("service unavailable 503"), true},
		{"504 error", errors.New("gateway timeout 504"), true},
		{"connection refused", errors.New("dial tcp: connection refused"), true},
		{"temporary", errors.New("temporary failure in name resolution"), true},
		{"deadline exceeded", errors.New("context deadline exceeded"), true},
		{"auth failure", errors.New("authentication failed"), false},
		{"invalid config", errors.New("invalid schedule expression"), false},
		{"not found", errors.New("job not found: cron_xxx"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isTemporaryError(tt.err)
			require.Equal(t, tt.temp, got)
		})
	}
}

func TestMaxRetries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		job  *CronJob
		want int
	}{
		{"default 3", &CronJob{MaxRetries: 0}, 3},
		{"explicit 5", &CronJob{MaxRetries: 5}, 5},
		{"explicit 1", &CronJob{MaxRetries: 1}, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := maxRetries(tt.job)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestResetRetry(t *testing.T) {
	t.Parallel()

	job := &CronJob{
		State: CronJobState{
			RetryCount:      5,
			ConsecutiveErrs: 3,
		},
	}
	resetRetry(job)
	require.Equal(t, 0, job.State.RetryCount)
	// resetRetry does not reset ConsecutiveErrs — that's done in executeJob on success.
	require.Equal(t, 3, job.State.ConsecutiveErrs)
}
