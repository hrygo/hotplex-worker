package gateway

import (
	"sync"
	"time"
)

// handshakeThrottle prevents brute-force or buggy client reconnection loops
// by tracking failed handshakes per session ID.
type handshakeThrottle struct {
	mu     sync.Mutex
	states map[string]*handshakeState
}

type handshakeState struct {
	lastFail  time.Time
	failCount int
}

func newHandshakeThrottle() *handshakeThrottle {
	return &handshakeThrottle{
		states: make(map[string]*handshakeState),
	}
}

// Check returns true if the handshake for sessionID is allowed,
// false if it should be throttled.
func (t *handshakeThrottle) Check(sessionID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	state, ok := t.states[sessionID]
	if !ok {
		return true
	}

	// Reset if last failure was long ago
	if time.Since(state.lastFail) > 5*time.Minute {
		delete(t.states, sessionID)
		return true
	}

	// Throttle if failed too many times recently
	if state.failCount >= 5 {
		// Allow one attempt every 10 seconds per failure over the limit
		backoff := time.Duration(state.failCount-4) * 5 * time.Second
		if time.Since(state.lastFail) < backoff {
			return false
		}
	}

	return true
}

// RecordFailure records a failed handshake attempt.
func (t *handshakeThrottle) RecordFailure(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	state, ok := t.states[sessionID]
	if !ok {
		state = &handshakeState{}
		t.states[sessionID] = state
	}
	state.lastFail = time.Now()
	state.failCount++
}

// RecordSuccess clears the failure state for a session.
func (t *handshakeThrottle) RecordSuccess(sessionID string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.states, sessionID)
}

// Cleanup removes old throttle states.
func (t *handshakeThrottle) Cleanup() {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	for sid, state := range t.states {
		if now.Sub(state.lastFail) > 10*time.Minute {
			delete(t.states, sid)
		}
	}
}
