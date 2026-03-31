package session

import (
	"log/slog"

	"hotplex-worker/internal/metrics"
	"sync"
)

// PoolManager manages per-user and global concurrency quotas for worker sessions.
type PoolManager struct {
	log *slog.Logger

	mu         sync.Mutex
	totalCount int
	userCount  map[string]int // userID → active session count

	maxSize        int // 0 = unlimited
	maxIdlePerUser int // 0 = unlimited
}

const (
	poolErrKindExhausted        = "exhausted"
	poolErrKindUserQuotaExceeded = "user_quota_exceeded"
)

// NewPoolManager creates a PoolManager with the given limits.
func NewPoolManager(log *slog.Logger, maxSize, maxIdlePerUser int) *PoolManager {
	if log == nil {
		log = slog.Default()
	}
	return &PoolManager{
		log:            log,
		userCount:      make(map[string]int),
		maxSize:        maxSize,
		maxIdlePerUser: maxIdlePerUser,
	}
}

// PoolError records why a pool operation failed.
type PoolError struct {
	Kind    string
	UserID  string
	Current int
	Max     int
}

func (e *PoolError) Error() string {
	return "pool: " + e.Kind
}

// Acquire attempts to reserve a concurrency slot for userID.
// It returns nil on success, or a PoolError describing the failure.
func (p *PoolManager) Acquire(userID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.maxSize > 0 && p.totalCount >= p.maxSize {
		metrics.PoolAcquireTotal.WithLabelValues("pool_exhausted").Inc()
		return &PoolError{Kind: poolErrKindExhausted, Current: p.totalCount, Max: p.maxSize}
	}
	if p.maxIdlePerUser > 0 && p.userCount[userID] >= p.maxIdlePerUser {
		metrics.PoolAcquireTotal.WithLabelValues("user_quota_exceeded").Inc()
		return &PoolError{Kind: poolErrKindUserQuotaExceeded, UserID: userID, Current: p.userCount[userID], Max: p.maxIdlePerUser}
	}

	p.userCount[userID]++
	p.totalCount++
	if p.maxSize > 0 {
		metrics.PoolUtilization.Set(float64(p.totalCount) / float64(p.maxSize))
	}
	metrics.PoolAcquireTotal.WithLabelValues("success").Inc()
	p.log.Debug("pool: acquired", "user_id", userID, "total", p.totalCount)
	return nil
}

// Release frees a concurrency slot previously acquired for userID.
func (p *PoolManager) Release(userID string) {
	p.mu.Lock()
	p.userCount[userID]--
	if p.userCount[userID] <= 0 {
		delete(p.userCount, userID)
	}
	p.totalCount--
	total := p.totalCount
	if p.maxSize > 0 {
		metrics.PoolUtilization.Set(float64(total) / float64(p.maxSize))
	}
	p.mu.Unlock()
	p.log.Debug("pool: released", "user_id", userID, "total", total)
}

// Stats returns the current pool utilization.
func (p *PoolManager) Stats() (total, maxSize, uniqueUsers int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.totalCount, p.maxSize, len(p.userCount)
}
