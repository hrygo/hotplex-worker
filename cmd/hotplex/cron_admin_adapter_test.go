package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/cron"
)

func TestMergeJobUpdates_NormalFields(t *testing.T) {
	t.Parallel()
	job := &cron.CronJob{
		ID:          "job-123",
		Name:        "old-name",
		Description: "old-desc",
		Enabled:     true,
		TimeoutSec:  30,
	}

	updated, err := mergeJobUpdates(job, map[string]any{
		"name":        "new-name",
		"description": "new-desc",
		"enabled":     false,
		"timeout_sec": 60,
	})
	require.NoError(t, err)
	assert.Equal(t, "new-name", updated.Name)
	assert.Equal(t, "new-desc", updated.Description)
	assert.False(t, updated.Enabled)
	assert.Equal(t, 60, updated.TimeoutSec)
	assert.Equal(t, "job-123", updated.ID)
}

func TestMergeJobUpdates_ProtectedFieldsIgnored(t *testing.T) {
	t.Parallel()
	job := &cron.CronJob{
		ID:   "job-123",
		Name: "original",
		State: cron.CronJobState{
			RunCount:    5,
			NextRunAtMs: 999,
		},
	}

	updated, err := mergeJobUpdates(job, map[string]any{
		"id":            "hacked-id",
		"state":         map[string]any{"run_count": 0, "next_run_at_ms": 0},
		"created_at_ms": 0.0,
		"updated_at_ms": 0.0,
		"name":          "legit-update",
	})
	require.NoError(t, err)

	assert.Equal(t, "job-123", updated.ID, "id must not be injectable")
	assert.Equal(t, int64(999), updated.State.NextRunAtMs, "state must not be injectable")
	assert.Equal(t, 5, updated.State.RunCount, "state.run_count must not be reset")
	assert.Equal(t, "legit-update", updated.Name)
}

func TestMergeJobUpdates_UnknownFieldsIgnored(t *testing.T) {
	t.Parallel()
	job := &cron.CronJob{
		ID:   "job-123",
		Name: "original",
	}

	updated, err := mergeJobUpdates(job, map[string]any{
		"unknown_field": "value",
		"fake":          42,
		"name":          "updated",
	})
	require.NoError(t, err)
	assert.Equal(t, "updated", updated.Name)
	// Unknown fields are silently dropped by JSON unmarshal into typed struct.
}
