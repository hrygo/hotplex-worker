package gateway

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHandshakeThrottle_Cleanup(t *testing.T) {
	t.Parallel()

	ht := newHandshakeThrottle()

	// Record some failures with timestamps.
	ht.states["old-session"] = &handshakeState{
		lastFail:  time.Now().Add(-15 * time.Minute),
		failCount: 3,
	}
	ht.states["recent-session"] = &handshakeState{
		lastFail:  time.Now().Add(-3 * time.Minute),
		failCount: 1,
	}
	ht.states["fresh-session"] = &handshakeState{
		lastFail:  time.Now(),
		failCount: 1,
	}

	ht.Cleanup()

	// old-session (>10min ago) should be removed.
	_, ok := ht.states["old-session"]
	require.False(t, ok, "old session should have been cleaned up")

	// recent-session and fresh-session should remain.
	_, ok = ht.states["recent-session"]
	require.True(t, ok, "recent session should remain")
	_, ok = ht.states["fresh-session"]
	require.True(t, ok, "fresh session should remain")
}

func TestHandshakeThrottle_Cleanup_EmptyMap(t *testing.T) {
	t.Parallel()

	ht := newHandshakeThrottle()
	// Cleanup on empty map should not panic.
	ht.Cleanup()
}
