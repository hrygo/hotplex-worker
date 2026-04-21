package slack

import (
	"math/rand/v2"
	"sync"
	"time"
)

// reconnectBackoff implements exponential backoff with jitter for reconnection attempts.
// It is safe for concurrent use.
type reconnectBackoff struct {
	attempt   int
	baseDelay time.Duration
	maxDelay  time.Duration
	mu        sync.Mutex
}

// newReconnectBackoff creates a new backoff with the specified base and max delays.
func newReconnectBackoff(baseDelay, maxDelay time.Duration) *reconnectBackoff {
	return &reconnectBackoff{
		attempt:   0,
		baseDelay: baseDelay,
		maxDelay:  maxDelay,
	}
}

// Next returns the next backoff duration with exponential backoff and jitter.
// Algorithm:
//   - delay = baseDelay * 2^attempt
//   - if delay > maxDelay → delay = maxDelay
//   - jitter = random duration in [0, delay/2)
//   - final = delay/2 + jitter (ensures at least half the computed delay)
//   - increment attempt
func (b *reconnectBackoff) Next() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	delay := b.baseDelay << b.attempt
	if delay <= 0 || delay > b.maxDelay {
		delay = b.maxDelay
	}

	halfDelay := delay / 2
	if halfDelay > 0 {
		jitter := time.Duration(rand.Int64N(int64(halfDelay)))
		delay = halfDelay + jitter
	}

	b.attempt++
	if b.attempt > 30 {
		b.attempt = 30
	}
	return delay
}

// Reset resets the backoff attempt counter to 0.
func (b *reconnectBackoff) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.attempt = 0
}
