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
