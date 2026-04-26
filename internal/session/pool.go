package session

import (
	"errors"
	"log/slog"

	"sync"

	"github.com/hrygo/hotplex/internal/metrics"
)

// PoolManager manages per-user and global concurrency quotas for worker sessions.
type PoolManager struct {
	log *slog.Logger

	mu         sync.Mutex
	totalCount int
	userCount  map[string]int   // userID → active session count
	userMemory map[string]int64 // userID → total estimated memory bytes (sum of RLIMIT_AS caps)

	maxSize          int   // 0 = unlimited
	maxIdlePerUser   int   // 0 = unlimited
	maxMemoryPerUser int64 // bytes; 0 = unlimited
}

// Default per-worker memory estimate (matches RLIMIT_AS in proc/manager.go).
const workerMemoryEstimate = 512 * 1024 * 1024 // 512 MB

const (
	poolErrKindExhausted         = "exhausted"
	poolErrKindUserQuotaExceeded = "user_quota_exceeded"
	poolErrKindMemoryExceeded    = "memory_exceeded"
)

// NewPoolManager creates a PoolManager with the given limits.
func NewPoolManager(log *slog.Logger, maxSize, maxIdlePerUser int, maxMemoryPerUser int64) *PoolManager {
	if log == nil {
		log = slog.Default()
	}
	return &PoolManager{
		log:              log,
		userCount:        make(map[string]int),
		userMemory:       make(map[string]int64),
		maxSize:          maxSize,
		maxIdlePerUser:   maxIdlePerUser,
		maxMemoryPerUser: maxMemoryPerUser,
	}
}

// PoolError records why a pool operation failed.
type PoolError struct {
	Kind    string
	UserID  string
	Current int
	Max     int
}

// ErrMemoryExceeded is returned when a user's estimated memory usage would exceed MaxMemoryPerUser.
var ErrMemoryExceeded = errors.New("pool: memory exceeded")

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
// Also releases memory quota under the same lock.
func (p *PoolManager) Release(userID string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.userCount[userID] <= 0 || p.totalCount <= 0 {
		p.log.Error("pool: release without acquire — possible double-release", "user_id", userID,
			"user_count", p.userCount[userID], "total", p.totalCount)
		metrics.PoolReleaseErrorsTotal.Inc()
		// Best-effort memory cleanup to prevent quota leak on accounting bug.
		p.releaseMemoryLocked(userID)
		return
	}
	p.userCount[userID]--
	if p.userCount[userID] <= 0 {
		delete(p.userCount, userID)
	}
	p.totalCount--
	total := p.totalCount
	if p.maxSize > 0 {
		metrics.PoolUtilization.Set(float64(total) / float64(p.maxSize))
	}
	p.releaseMemoryLocked(userID)
	p.log.Debug("pool: released", "user_id", userID, "total", total)
}

// Stats returns the current pool utilization.
func (p *PoolManager) Stats() (total, maxSize, uniqueUsers int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.totalCount, p.maxSize, len(p.userCount)
}

// UpdateLimits dynamically adjusts the pool limits.
// If maxSize is reduced below the current total, existing sessions are NOT evicted —
// new Acquire calls will be rejected until sessions are naturally released.
func (p *PoolManager) UpdateLimits(maxSize, maxIdlePerUser int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	old := p.maxSize
	p.maxSize = maxSize
	p.maxIdlePerUser = maxIdlePerUser
	if p.maxSize > 0 {
		metrics.PoolUtilization.Set(float64(p.totalCount) / float64(p.maxSize))
	}
	p.log.Info("pool: limits updated", "old_max", old, "new_max", maxSize, "max_per_user", maxIdlePerUser)
}

// AcquireMemory reserves memory quota for a user.
// It uses workerMemoryEstimate as the per-worker allocation.
// Returns nil on success, or ErrUserMemoryExceeded if the per-user limit is exceeded.
func (p *PoolManager) AcquireMemory(userID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.maxMemoryPerUser > 0 {
		used := p.userMemory[userID]
		if used+workerMemoryEstimate > p.maxMemoryPerUser {
			p.log.Warn("pool: memory quota exceeded", "user_id", userID,
				"used_mb", used/(1024*1024),
				"limit_mb", p.maxMemoryPerUser/(1024*1024),
				"worker_mb", workerMemoryEstimate/(1024*1024))
			return &PoolError{Kind: poolErrKindMemoryExceeded, UserID: userID}
		}
		p.userMemory[userID] = used + workerMemoryEstimate
	}
	return nil
}

// ReleaseMemory frees memory quota for a user.
func (p *PoolManager) ReleaseMemory(userID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.releaseMemoryLocked(userID)
}

// releaseMemoryLocked frees memory quota. Caller must hold p.mu.
func (p *PoolManager) releaseMemoryLocked(userID string) {
	if p.maxMemoryPerUser > 0 {
		used := p.userMemory[userID]
		if used >= workerMemoryEstimate {
			p.userMemory[userID] = used - workerMemoryEstimate
		} else if used > 0 {
			p.userMemory[userID] = 0
		}
		if p.userMemory[userID] <= 0 {
			delete(p.userMemory, userID)
		}
	}
}

// UserMemory returns the current estimated memory usage for a user in bytes.
func (p *PoolManager) UserMemory(userID string) int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.userMemory[userID]
}
