package session

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/pkg/events"
)

// ─── Pool Manager tests ────────────────────────────────────────────────────────

func TestPoolAcquire_Release(t *testing.T) {
	t.Parallel()

	_ = config.Default()
	pool := NewPoolManager(nil, 10, 3, 0)

	// First acquire succeeds
	err := pool.Acquire("user1")
	require.Nil(t, err)

	total, max, users := pool.Stats()
	require.Equal(t, 1, total)
	require.Equal(t, 10, max)
	require.Equal(t, 1, users)

	pool.Release("user1")

	total, _, users = pool.Stats()
	require.Equal(t, 0, total)
	require.Equal(t, 0, users)
}

func TestPoolAcquire_GlobalLimit(t *testing.T) {
	t.Parallel()

	pool := NewPoolManager(nil, 2, 10, 0)

	require.Nil(t, pool.Acquire("user1"))
	require.Nil(t, pool.Acquire("user2"))

	// Third should fail due to global limit
	err := pool.Acquire("user3")
	require.NotNil(t, err)
	pe := new(PoolError)
	require.ErrorAs(t, err, &pe)
	require.Equal(t, poolErrKindExhausted, pe.Kind)
	require.Equal(t, 2, pe.Current)
	require.Equal(t, 2, pe.Max)
}

func TestPoolAcquire_UserQuotaLimit(t *testing.T) {
	t.Parallel()

	pool := NewPoolManager(nil, 10, 2, 0)

	require.Nil(t, pool.Acquire("user1"))
	require.Nil(t, pool.Acquire("user1"))

	// Third for same user fails
	err := pool.Acquire("user1")
	require.NotNil(t, err)
	pe := new(PoolError)
	require.ErrorAs(t, err, &pe)
	require.Equal(t, poolErrKindUserQuotaExceeded, pe.Kind)
	require.Equal(t, "user1", pe.UserID)
	require.Equal(t, 2, pe.Current)
	require.Equal(t, 2, pe.Max)

	// Different user succeeds
	require.Nil(t, pool.Acquire("user2"))
}

func TestPoolAcquire_Unlimited(t *testing.T) {
	t.Parallel()

	// maxSize=0 means unlimited
	pool := NewPoolManager(nil, 0, 0, 0)

	for i := 0; i < 100; i++ {
		err := pool.Acquire("user1")
		require.Nil(t, err, "acquire %d should succeed", i)
	}

	total, max, _ := pool.Stats()
	require.Equal(t, 100, total)
	require.Equal(t, 0, max) // max=0 means unlimited
}

func TestPoolRelease_UserCountGoesToZero(t *testing.T) {
	t.Parallel()

	pool := NewPoolManager(nil, 10, 3, 0)

	require.NoError(t, pool.Acquire("user1"))
	require.NoError(t, pool.Acquire("user1"))
	pool.Release("user1")
	pool.Release("user1")

	_, _, users := pool.Stats()
	require.Equal(t, 0, users)
}

func TestPoolRelease_Underflow(t *testing.T) {
	t.Parallel()

	pool := NewPoolManager(nil, 10, 3, 0)

	// Release without acquire is guarded — no underflow.
	pool.Release("user1")
	pool.Release("user1")

	total, _, users := pool.Stats()
	require.Equal(t, 0, total) // guard prevents negative total
	require.Equal(t, 0, users)
}

func TestPoolError_Error(t *testing.T) {
	t.Parallel()

	err := &PoolError{Kind: poolErrKindExhausted, Current: 10, Max: 10}
	require.Contains(t, err.Error(), "pool:")
	require.Contains(t, err.Error(), "exhausted")
}

func TestPoolStats_MultiUser(t *testing.T) {
	t.Parallel()

	pool := NewPoolManager(nil, 100, 5, 0)

	require.NoError(t, pool.Acquire("user1"))
	require.NoError(t, pool.Acquire("user1"))
	require.NoError(t, pool.Acquire("user2"))
	require.NoError(t, pool.Acquire("user3"))

	total, _, users := pool.Stats()
	require.Equal(t, 4, total)
	require.Equal(t, 3, users)
}

// ─── Pool GC integration ──────────────────────────────────────────────────────

func TestPoolRelease_AfterGCTransitions(t *testing.T) {
	t.Parallel()

	pool := NewPoolManager(nil, 100, 3, 0)

	// Simulate multiple sessions per user
	require.Nil(t, pool.Acquire("user1"))
	require.Nil(t, pool.Acquire("user1"))

	// GC transitions one session to terminated → release quota
	pool.Release("user1")

	// Now one slot is available for user1
	require.Nil(t, pool.Acquire("user1"))
}

// ─── ValidTransitions table ──────────────────────────────────────────────────

func TestValidTransitions_Completeness(t *testing.T) {
	t.Parallel()

	// Ensure every state has an entry in the table
	for _, state := range []events.SessionState{
		events.StateCreated,
		events.StateRunning,
		events.StateIdle,
		events.StateTerminated,
		events.StateDeleted,
	} {
		transitions, ok := events.ValidTransitions[state]
		require.True(t, ok, "state %s should be in ValidTransitions", state)
		require.NotNil(t, transitions, "transitions for %s should not be nil", state)
	}
}

func TestIsValidTransition_UnknownState(t *testing.T) {
	t.Parallel()

	// Unknown state should return false
	ok := events.IsValidTransition(events.SessionState("unknown"), events.StateRunning)
	require.False(t, ok)
}

// ─── Memory tracking tests ─────────────────────────────────────────────────────

func TestPoolAcquireMemory_Limit(t *testing.T) {
	t.Parallel()

	// 1 GB limit, 512 MB per worker → max 2 workers
	pool := NewPoolManager(nil, 100, 10, 1<<30)

	require.Nil(t, pool.AcquireMemory("user1"))
	require.Nil(t, pool.AcquireMemory("user1"))

	// Third worker would exceed 1 GB → rejected
	err := pool.AcquireMemory("user1")
	require.NotNil(t, err)
	pe := new(PoolError)
	require.ErrorAs(t, err, &pe)
	require.Equal(t, poolErrKindMemoryExceeded, pe.Kind)
}

func TestPoolAcquireMemory_Unlimited(t *testing.T) {
	t.Parallel()

	// maxMemoryPerUser=0 → unlimited
	pool := NewPoolManager(nil, 100, 10, 0)

	for i := 0; i < 10; i++ {
		require.Nil(t, pool.AcquireMemory("user1"))
	}
}

func TestPoolReleaseMemory(t *testing.T) {
	t.Parallel()

	pool := NewPoolManager(nil, 100, 10, 1<<30)

	require.Nil(t, pool.AcquireMemory("user1"))
	require.Nil(t, pool.AcquireMemory("user1"))

	// Release one → should allow a third acquire
	pool.ReleaseMemory("user1")
	require.Nil(t, pool.AcquireMemory("user1"))

	pool.ReleaseMemory("user1")
	pool.ReleaseMemory("user1")
}

func TestPoolMemory_AcrossUsers(t *testing.T) {
	t.Parallel()

	// 1 GB limit per user
	pool := NewPoolManager(nil, 100, 10, 1<<30)

	// user1 uses 1 GB
	require.Nil(t, pool.AcquireMemory("user1"))
	require.Nil(t, pool.AcquireMemory("user1"))
	err := pool.AcquireMemory("user1")
	require.NotNil(t, err)

	// user2 is independent
	require.Nil(t, pool.AcquireMemory("user2"))
	require.Nil(t, pool.AcquireMemory("user2"))
}

func TestPoolAttachMemory_Integrated(t *testing.T) {
	t.Parallel()

	// Test that memory is tracked alongside session count.
	// Simulates: Acquire + AcquireMemory → Release + ReleaseMemory.
	pool := NewPoolManager(nil, 10, 5, 1<<30)

	require.Nil(t, pool.Acquire("user1"))
	require.Nil(t, pool.AcquireMemory("user1"))

	require.Equal(t, int64(workerMemoryEstimate), pool.UserMemory("user1"))

	pool.Release("user1")
	pool.ReleaseMemory("user1")

	require.Equal(t, int64(0), pool.UserMemory("user1"))
}
