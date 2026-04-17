// Package feishu provides rate limiting for feishu API calls.
package feishu

import (
	"sync"
	"time"
)

// FeishuRateLimiter provides per-resource rate limiting for Feishu API calls.
// CardKit streaming is limited to 1 update per 100ms per card.
// IM patch is limited to 1 update per 1500ms per message.
type FeishuRateLimiter struct {
	mu sync.Mutex

	cardKitLimit time.Duration // minimum interval between CardKit updates (100ms)
	patchLimit   time.Duration // minimum interval between IM patch updates (1500ms)

	lastCardKit map[string]time.Time // cardID → last update time
	lastPatch   map[string]time.Time // msgID → last update time
}

// NewFeishuRateLimiter creates a rate limiter with standard intervals.
func NewFeishuRateLimiter() *FeishuRateLimiter {
	return &FeishuRateLimiter{
		cardKitLimit: 100 * time.Millisecond,
		patchLimit:   1500 * time.Millisecond,
		lastCardKit:  make(map[string]time.Time),
		lastPatch:    make(map[string]time.Time),
	}
}

// AllowCardKit checks if a CardKit update is allowed for the given card ID.
// Returns true if the minimum interval has elapsed since the last update.
func (r *FeishuRateLimiter) AllowCardKit(cardID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if last, ok := r.lastCardKit[cardID]; ok && now.Sub(last) < r.cardKitLimit {
		return false
	}
	r.lastCardKit[cardID] = now
	return true
}

// AllowPatch checks if an IM patch update is allowed for the given message ID.
// Returns true if the minimum interval has elapsed since the last update.
func (r *FeishuRateLimiter) AllowPatch(msgID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if last, ok := r.lastPatch[msgID]; ok && now.Sub(last) < r.patchLimit {
		return false
	}
	r.lastPatch[msgID] = now
	return true
}

// Sweep removes stale entries older than 10x the rate limit.
// Call periodically to prevent unbounded memory growth.
func (r *FeishuRateLimiter) Sweep() {
	r.mu.Lock()
	defer r.mu.Unlock()

	cardCutoff := time.Now().Add(-10 * r.cardKitLimit)
	for id, t := range r.lastCardKit {
		if t.Before(cardCutoff) {
			delete(r.lastCardKit, id)
		}
	}

	patchCutoff := time.Now().Add(-10 * r.patchLimit)
	for id, t := range r.lastPatch {
		if t.Before(patchCutoff) {
			delete(r.lastPatch, id)
		}
	}
}
