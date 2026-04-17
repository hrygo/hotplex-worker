package slack

import (
	"context"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	rlRate    = 1.0 // 1 request per second
	rlBurst   = 3   // allow short bursts
	rlTTL     = 10 * time.Minute
	rlCleanup = 5 * time.Minute
)

// ChannelRateLimiter provides per-channel rate limiting with TTL-based cleanup.
type ChannelRateLimiter struct {
	mu       sync.RWMutex
	limiters map[string]*rate.Limiter
	lastUsed map[string]time.Time
	done     chan struct{}
	wg       sync.WaitGroup
}

// NewChannelRateLimiter creates a new rate limiter and starts the cleanup goroutine.
func NewChannelRateLimiter(ctx context.Context) *ChannelRateLimiter {
	r := &ChannelRateLimiter{
		limiters: make(map[string]*rate.Limiter),
		lastUsed: make(map[string]time.Time),
		done:     make(chan struct{}),
	}
	r.wg.Add(1)
	go r.cleanupLoop()
	return r
}

// Allow reports whether a request for the given channel is allowed.
func (r *ChannelRateLimiter) Allow(channelID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	limiter, exists := r.limiters[channelID]
	if !exists {
		limiter = rate.NewLimiter(rate.Limit(rlRate), rlBurst)
		r.limiters[channelID] = limiter
	}
	r.lastUsed[channelID] = time.Now()
	return limiter.Allow()
}

// Stop shuts down the cleanup goroutine.
func (r *ChannelRateLimiter) Stop() {
	close(r.done)
	r.wg.Wait()
}

func (r *ChannelRateLimiter) cleanupLoop() {
	defer r.wg.Done()
	ticker := time.NewTicker(rlCleanup)
	defer ticker.Stop()

	for {
		select {
		case <-r.done:
			return
		case <-ticker.C:
			r.mu.Lock()
			now := time.Now()
			for chID, last := range r.lastUsed {
				if now.Sub(last) > rlTTL {
					delete(r.limiters, chID)
					delete(r.lastUsed, chID)
				}
			}
			r.mu.Unlock()
		}
	}
}
