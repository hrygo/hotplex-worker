package slack

import (
	"context"
	"regexp"
	"sync"
	"time"

	"github.com/slack-go/slack"
	"golang.org/x/sync/errgroup"
)

const (
	userCacheTTL      = 30 * time.Minute
	userCacheMax      = 500
	userCacheSweepInt = 10 * time.Minute
)

var mentionPattern = regexp.MustCompile(`<@([A-Z0-9]+)(?:\|([^>]*))?>`)

type cacheEntry struct {
	name      string
	expiresAt time.Time
}

// UserCache resolves Slack user IDs to display names.
// Uses slack.Client.GetUserInfoContext for resolution.
// Evicts entries older than 30 minutes and caps at 500 entries.
type UserCache struct {
	client *slack.Client
	cache  map[string]cacheEntry
	mu     sync.RWMutex
	stopCh chan struct{} // closed on Close to stop the eviction goroutine
	once   sync.Once
}

// NewUserCache creates a new user mention resolver with bounded TTL and size.
func NewUserCache(client *slack.Client) *UserCache {
	uc := &UserCache{
		client: client,
		cache:  make(map[string]cacheEntry),
		stopCh: make(chan struct{}),
	}
	go uc.sweepLoop()
	return uc
}

// Close stops the periodic cache cleanup goroutine. Safe to call multiple times.
func (uc *UserCache) Close() {
	if uc == nil {
		return
	}
	uc.once.Do(func() {
		close(uc.stopCh)
	})
}

// ResolveMentions replaces <@UID> with @DisplayName.
// Bot self-mentions are removed. Non-resolvable mentions kept as-is.
// Resolves all mentions in parallel using errgroup for better throughput.
func (uc *UserCache) ResolveMentions(ctx context.Context, text, botID string) string {
	matches := mentionPattern.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text
	}

	type resolveResult struct {
		start int
		end   int
		repl  string
	}

	results := make([]resolveResult, len(matches))
	g, gctx := errgroup.WithContext(ctx)

	for i, loc := range matches {
		i, loc := i, loc
		g.Go(func() error {
			submatch := mentionPattern.FindStringSubmatch(text[loc[0]:loc[1]])
			if len(submatch) < 2 {
				results[i] = resolveResult{start: loc[0], end: loc[1], repl: text[loc[0]:loc[1]]}
				return nil
			}
			userID := submatch[1]
			inlineName := ""
			if len(submatch) >= 3 {
				inlineName = submatch[2]
			}

			if userID == botID {
				results[i] = resolveResult{start: loc[0], end: loc[1], repl: ""}
				return nil
			}

			name := uc.resolve(gctx, userID, inlineName)
			if name != "" {
				results[i] = resolveResult{start: loc[0], end: loc[1], repl: "@" + name}
			} else {
				results[i] = resolveResult{start: loc[0], end: loc[1], repl: text[loc[0]:loc[1]]}
			}
			return nil
		})
	}

	_ = g.Wait()

	// Build result string by replacing from end to start (preserving indices).
	result := []byte(text)
	for i := len(results) - 1; i >= 0; i-- {
		r := results[i]
		result = append(result[:r.start], append([]byte(r.repl), result[r.end:]...)...)
	}
	return string(result)
}

func (uc *UserCache) resolve(ctx context.Context, userID, fallback string) string {
	uc.mu.RLock()
	if entry, ok := uc.cache[userID]; ok && time.Now().Before(entry.expiresAt) {
		name := entry.name
		uc.mu.RUnlock()
		return name
	}
	uc.mu.RUnlock()

	if uc.client == nil {
		return fallback
	}

	// Add a bounded timeout to prevent hanging on the Slack API call.
	resolveCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	user, err := uc.client.GetUserInfoContext(resolveCtx, userID)
	if err != nil {
		return fallback
	}

	name := user.Profile.DisplayName
	if name == "" {
		name = user.RealName
	}

	uc.mu.Lock()
	defer uc.mu.Unlock()

	// Enforce max size: evict oldest expired entry before inserting.
	if len(uc.cache) >= userCacheMax {
		uc.evictOne()
	}
	uc.cache[userID] = cacheEntry{
		name:      name,
		expiresAt: time.Now().Add(userCacheTTL),
	}
	return name
}

// sweepLoop periodically removes expired entries from the cache.
func (uc *UserCache) sweepLoop() {
	ticker := time.NewTicker(userCacheSweepInt)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			uc.sweep()
		case <-uc.stopCh:
			return
		}
	}
}

func (uc *UserCache) sweep() {
	now := time.Now()
	uc.mu.Lock()
	defer uc.mu.Unlock()

	for id, entry := range uc.cache {
		if now.After(entry.expiresAt) {
			delete(uc.cache, id)
		}
	}
}

// evictOne removes a single expired entry. If none expired, removes the oldest.
// Caller must hold uc.mu.
func (uc *UserCache) evictOne() {
	now := time.Now()
	oldestID := ""
	oldestExpiry := time.Now().Add(userCacheTTL * 2) // far future sentinel

	for id, entry := range uc.cache {
		if now.After(entry.expiresAt) {
			delete(uc.cache, id)
			return
		}
		if entry.expiresAt.Before(oldestExpiry) {
			oldestExpiry = entry.expiresAt
			oldestID = id
		}
	}
	if oldestID != "" {
		delete(uc.cache, oldestID)
	}
}
