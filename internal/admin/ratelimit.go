package admin

import (
	"sync"
	"time"
)

type simpleRateLimiter struct {
	tokens     float64
	maxTokens  float64
	refillRate float64
	lastRefill time.Time
	mu         sync.Mutex
}

func NewRateLimiter(reqPerSec, burst int) *simpleRateLimiter {
	return &simpleRateLimiter{
		tokens:     float64(burst),
		maxTokens:  float64(burst),
		refillRate: float64(reqPerSec),
		lastRefill: time.Now(),
	}
}

func (r *simpleRateLimiter) Allow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	elapsed := time.Since(r.lastRefill).Seconds()
	r.tokens = min(r.maxTokens, r.tokens+elapsed*r.refillRate)
	r.lastRefill = time.Now()
	if r.tokens >= 1 {
		r.tokens--
		return true
	}
	return false
}

// UpdateRate dynamically adjusts the refill rate and burst capacity.
func (r *simpleRateLimiter) UpdateRate(reqPerSec, burst int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refillRate = float64(reqPerSec)
	r.maxTokens = float64(burst)
	if r.tokens > r.maxTokens {
		r.tokens = r.maxTokens
	}
}
