package cron

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCronJob_SessionKey_Deterministic(t *testing.T) {
	t.Parallel()
	job := &CronJob{
		ID:      "job-123",
		OwnerID: "user-abc",
		BotID:   "bot-xyz",
		WorkDir: "/home/user/project",
	}

	key1 := job.SessionKey()
	key2 := job.SessionKey()
	assert.Equal(t, key1, key2, "SessionKey must be deterministic")
	assert.NotEmpty(t, key1)
}

func TestCronJob_SessionKey_DifferentForDifferentWorkDirs(t *testing.T) {
	t.Parallel()
	job1 := &CronJob{
		ID:      "job-123",
		OwnerID: "user-abc",
		BotID:   "bot-xyz",
		WorkDir: "/home/user/project-a",
	}
	job2 := &CronJob{
		ID:      "job-456",
		OwnerID: "user-abc",
		BotID:   "bot-xyz",
		WorkDir: "/home/user/project-b",
	}

	assert.NotEqual(t, job1.SessionKey(), job2.SessionKey(),
		"different WorkDirs must produce different session keys")
}

func TestCronJob_SessionKey_DifferentForDifferentOwners(t *testing.T) {
	t.Parallel()
	job1 := &CronJob{
		ID:      "job-123",
		OwnerID: "user-aaa",
		BotID:   "bot-xyz",
		WorkDir: "/home/user/project",
	}
	job2 := &CronJob{
		ID:      "job-123",
		OwnerID: "user-bbb",
		BotID:   "bot-xyz",
		WorkDir: "/home/user/project",
	}

	assert.NotEqual(t, job1.SessionKey(), job2.SessionKey(),
		"different owners must produce different session keys")
}

func TestCronJob_Clone(t *testing.T) {
	t.Parallel()
	original := &CronJob{
		ID:          "job-123",
		Name:        "test-job",
		OwnerID:     "user-abc",
		BotID:       "bot-xyz",
		Enabled:     true,
		PlatformKey: map[string]string{"chat_id": "oc_123"},
		Payload: CronPayload{
			Message:      "hello",
			AllowedTools: []string{"tool1", "tool2"},
		},
	}

	cloned := original.Clone()

	require.NotNil(t, cloned)
	assert.Equal(t, original.ID, cloned.ID)
	assert.Equal(t, original.Name, cloned.Name)
	assert.Equal(t, original.Enabled, cloned.Enabled)

	// Verify deep copy of maps and slices.
	cloned.PlatformKey["chat_id"] = "modified"
	cloned.Payload.AllowedTools[0] = "modified"
	assert.Equal(t, "oc_123", original.PlatformKey["chat_id"],
		"modifying clone must not affect original map")
	assert.Equal(t, "tool1", original.Payload.AllowedTools[0],
		"modifying clone must not affect original slice")
}
