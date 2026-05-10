package slack

import (
	"context"
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
	cache *TTLCache[string, *rate.Limiter]
}

// NewChannelRateLimiter creates a new rate limiter and starts the cleanup goroutine.
func NewChannelRateLimiter(_ context.Context) *ChannelRateLimiter {
	return &ChannelRateLimiter{
		cache: NewTTLCache[string, *rate.Limiter](rlTTL, rlCleanup),
	}
}

// Allow reports whether a request for the given channel is allowed.
func (r *ChannelRateLimiter) Allow(channelID string) bool {
	var allowed bool
	r.cache.Do(func(items map[string]ttlEntry[*rate.Limiter]) {
		e, ok := items[channelID]
		limiter := e.Value
		if !ok {
			limiter = rate.NewLimiter(rate.Limit(rlRate), rlBurst)
		}
		items[channelID] = ttlEntry[*rate.Limiter]{
			Value:  limiter,
			Expiry: time.Now().Add(rlTTL),
		}
		allowed = limiter.Allow()
	})
	return allowed
}

// Stop shuts down the cleanup goroutine.
func (r *ChannelRateLimiter) Stop() {
	r.cache.Stop()
}
