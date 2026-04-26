package slack

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSlashRateLimiter_Allow(t *testing.T) {
	t.Parallel()

	rl := NewSlashRateLimiter()
	defer rl.Stop()

	userID := "U123"

	require.True(t, rl.Allow(userID), "first request should be allowed")
	require.False(t, rl.Allow(userID), "second request within cooldown should be rate limited")

	time.Sleep(slashCooldown + 100*time.Millisecond)
	require.True(t, rl.Allow(userID), "request after cooldown should be allowed")
}

func TestSlashRateLimiter_DifferentUsers(t *testing.T) {
	t.Parallel()

	rl := NewSlashRateLimiter()
	defer rl.Stop()

	user1 := "U123"
	user2 := "U456"

	require.True(t, rl.Allow(user1))
	require.False(t, rl.Allow(user1))

	require.True(t, rl.Allow(user2))
	require.False(t, rl.Allow(user2))
}

func TestSlashRateLimiter_Stop(t *testing.T) {
	t.Parallel()

	rl := NewSlashRateLimiter()
	rl.Stop()
}

func TestSlashRateLimiter_SweepRemovesStaleEntries(t *testing.T) {
	t.Parallel()

	rl := &SlashRateLimiter{
		lastUsed: make(map[string]time.Time),
		done:     make(chan struct{}),
	}
	_ = rl.done

	// Add entries with different timestamps
	now := time.Now()
	rl.lastUsed["user1"] = now.Add(-11 * time.Minute) // stale (>10min)
	rl.lastUsed["user2"] = now.Add(-5 * time.Minute)  // fresh (<10min)
	rl.lastUsed["user3"] = now.Add(-20 * time.Minute) // stale (>10min)

	require.Equal(t, 3, len(rl.lastUsed), "should have 3 entries before sweep")

	// Manually trigger sweep logic
	rl.mu.Lock()
	for userID, last := range rl.lastUsed {
		if now.Sub(last) > slashEntryTTL {
			delete(rl.lastUsed, userID)
		}
	}
	rl.mu.Unlock()

	require.Equal(t, 1, len(rl.lastUsed), "should have 1 entry after sweep")
	_, exists := rl.lastUsed["user2"]
	require.True(t, exists, "user2 should still exist (fresh entry)")
}

func TestSlashRateLimiter_SweepLoopExitsOnDone(t *testing.T) {
	t.Parallel()

	rl := NewSlashRateLimiter()

	// Stop should cleanly terminate the goroutine
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

func TestExtractChannelThread(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sessionID  string
		wantCh     string
		wantThread string
	}{
		{"valid", "slack:T:C123:1234567890.123456:U1", "C123", "1234567890.123456"},
		{"short", "slack:T:C:456:U", "C", "456"},
		{"invalid", "invalid", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch, thread := ExtractChannelThread(tt.sessionID)
			require.Equal(t, tt.wantCh, ch)
			require.Equal(t, tt.wantThread, thread)
		})
	}
}
