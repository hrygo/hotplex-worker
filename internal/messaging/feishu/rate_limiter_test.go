package feishu

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFeishuRateLimiter_StartAndStop(t *testing.T) {
	t.Parallel()

	rl := NewFeishuRateLimiter()
	rl.Start()

	// Stop should complete without panic
	done := make(chan struct{})
	go func() {
		rl.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() should complete quickly")
	}
}

func TestFeishuRateLimiter_StopWithSyncOnce(t *testing.T) {
	t.Parallel()

	rl := NewFeishuRateLimiter()
	rl.Start()

	// Multiple Stop() calls should not panic due to sync.Once protection
	rl.Stop()
	rl.Stop()
	rl.Stop()

	// Verify done channel is closed
	select {
	case <-rl.done:
		// Success - channel is closed
	case <-time.After(100 * time.Millisecond):
		t.Fatal("done channel should be closed after Stop()")
	}
}

func TestFeishuRateLimiter_SweepCalledByBackgroundGoroutine(t *testing.T) {
	t.Parallel()

	rl := NewFeishuRateLimiter()

	// Add stale entries
	now := time.Now()
	rl.lastCardKit["card1"] = now.Add(-2 * time.Second)        // stale (>1s, 10x 100ms)
	rl.lastCardKit["card2"] = now.Add(-500 * time.Millisecond) // fresh
	rl.lastPatch["msg1"] = now.Add(-20 * time.Second)          // stale (>15s, 10x 1500ms)
	rl.lastPatch["msg2"] = now.Add(-5 * time.Second)           // fresh

	require.Equal(t, 2, len(rl.lastCardKit), "should have 2 card entries before sweep")
	require.Equal(t, 2, len(rl.lastPatch), "should have 2 patch entries before sweep")

	// Manually call Sweep (simulating what background goroutine does)
	rl.Sweep()

	require.Equal(t, 1, len(rl.lastCardKit), "should have 1 card entry after sweep")
	require.Equal(t, 1, len(rl.lastPatch), "should have 1 patch entry after sweep")

	_, exists := rl.lastCardKit["card2"]
	require.True(t, exists, "card2 should still exist (fresh entry)")
	_, exists = rl.lastPatch["msg2"]
	require.True(t, exists, "msg2 should still exist (fresh entry)")
}

func TestFeishuRateLimiter_SweepRemovesStaleEntries(t *testing.T) {
	t.Parallel()

	rl := NewFeishuRateLimiter()

	now := time.Now()
	// CardKit entries (TTL = 10 * 100ms = 1s)
	rl.lastCardKit["staleCard"] = now.Add(-2 * time.Second)
	rl.lastCardKit["freshCard"] = now.Add(-500 * time.Millisecond)

	// Patch entries (TTL = 10 * 1500ms = 15s)
	rl.lastPatch["stalePatch"] = now.Add(-20 * time.Second)
	rl.lastPatch["freshPatch"] = now.Add(-5 * time.Second)

	rl.Sweep()

	require.Equal(t, 1, len(rl.lastCardKit), "should have 1 card entry after sweep")
	require.Equal(t, 1, len(rl.lastPatch), "should have 1 patch entry after sweep")

	_, exists := rl.lastCardKit["freshCard"]
	require.True(t, exists, "freshCard should exist")
	_, exists = rl.lastPatch["freshPatch"]
	require.True(t, exists, "freshPatch should exist")
}

func TestFeishuRateLimiter_AllowCardKit(t *testing.T) {
	t.Parallel()

	rl := NewFeishuRateLimiter()

	cardID := "card123"

	// First request should be allowed
	require.True(t, rl.AllowCardKit(cardID), "first request should be allowed")

	// Immediate second request should be rate limited
	require.False(t, rl.AllowCardKit(cardID), "second request within 100ms should be rate limited")

	// After waiting, request should be allowed
	time.Sleep(110 * time.Millisecond)
	require.True(t, rl.AllowCardKit(cardID), "request after 100ms should be allowed")
}

func TestFeishuRateLimiter_AllowPatch(t *testing.T) {
	t.Parallel()

	rl := NewFeishuRateLimiter()

	msgID := "msg123"

	// First request should be allowed
	require.True(t, rl.AllowPatch(msgID), "first request should be allowed")

	// Immediate second request should be rate limited
	require.False(t, rl.AllowPatch(msgID), "second request within 1500ms should be rate limited")

	// After waiting, request should be allowed
	time.Sleep(1550 * time.Millisecond)
	require.True(t, rl.AllowPatch(msgID), "request after 1500ms should be allowed")
}
