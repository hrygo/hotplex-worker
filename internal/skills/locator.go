package skills

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	defaultTTL      = 5 * time.Minute
	maxCacheEntries = 100
	sweepInterval   = 5 * time.Minute
)

type cacheEntry struct {
	skills    []Skill
	expiresAt time.Time
}

// Locator discovers skills from the filesystem with TTL-based caching.
type Locator struct {
	log    *slog.Logger
	cache  map[string]*cacheEntry
	mu     sync.RWMutex
	ttl    time.Duration
	stopCh chan struct{}
}

// NewLocator creates a skill locator with TTL cache.
func NewLocator(log *slog.Logger, ttl time.Duration) *Locator {
	if ttl <= 0 {
		ttl = defaultTTL
	}
	l := &Locator{
		log:    log,
		cache:  make(map[string]*cacheEntry),
		ttl:    ttl,
		stopCh: make(chan struct{}),
	}
	go l.sweep()
	return l
}

// List returns deduplicated skills for the given homeDir and workDir.
// Returns cached results if fresh; otherwise scans filesystem.
func (l *Locator) List(_ context.Context, homeDir, workDir string) ([]Skill, error) {
	key := workDir

	l.mu.RLock()
	if e, ok := l.cache[key]; ok && time.Now().Before(e.expiresAt) {
		skills := e.skills
		l.mu.RUnlock()
		return skills, nil
	}
	l.mu.RUnlock()

	skills := scanDirs(homeDir, workDir)

	l.mu.Lock()
	// Evict oldest if at capacity
	if len(l.cache) >= maxCacheEntries {
		l.evictOldest()
	}
	l.cache[key] = &cacheEntry{
		skills:    skills,
		expiresAt: time.Now().Add(l.ttl),
	}
	l.mu.Unlock()

	return skills, nil
}

// Close stops the background sweep goroutine.
func (l *Locator) Close() {
	close(l.stopCh)
}

func (l *Locator) sweep() {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			l.mu.Lock()
			now := time.Now()
			for k, e := range l.cache {
				if now.After(e.expiresAt) {
					delete(l.cache, k)
				}
			}
			l.mu.Unlock()
		case <-l.stopCh:
			return
		}
	}
}

func (l *Locator) evictOldest() {
	var oldestKey string
	var oldestTime time.Time
	for k, e := range l.cache {
		if oldestKey == "" || e.expiresAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = e.expiresAt
		}
	}
	if oldestKey != "" {
		delete(l.cache, oldestKey)
	}
}
