package gateway

import (
	"context"
	"sync"
	"time"
)

// cachedSkills holds the cached skills result with expiry metadata.
type cachedSkills struct {
	skills    []Skill
	expiresAt time.Time
}

// SkillsCache wraps a SkillsLocator with a TTL cache.
// Thread-safe and suitable for concurrent access across sessions.
type SkillsCache struct {
	inner SkillsLocator
	ttl   time.Duration
	mu    sync.RWMutex
	cache cachedSkills
}

// NewSkillsCache wraps a SkillsLocator with the given TTL.
func NewSkillsCache(inner SkillsLocator, ttl time.Duration) *SkillsCache {
	return &SkillsCache{inner: inner, ttl: ttl}
}

// List returns cached skills if available and not expired,
// otherwise fetches from the inner locator and caches the result.
func (c *SkillsCache) List(ctx context.Context, homeDir, workDir string) ([]Skill, error) {
	if c.ttl <= 0 {
		return c.inner.List(ctx, homeDir, workDir)
	}

	c.mu.RLock()
	cached := c.cache
	c.mu.RUnlock()

	if !cached.expiresAt.IsZero() && time.Now().Before(cached.expiresAt) {
		return cached.skills, nil
	}

	skills, err := c.inner.List(ctx, homeDir, workDir)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.cache = cachedSkills{skills: skills, expiresAt: time.Now().Add(c.ttl)}
	c.mu.Unlock()

	return skills, nil
}
