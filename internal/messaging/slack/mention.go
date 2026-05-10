package slack

import (
	"context"
	"regexp"
	"time"

	"golang.org/x/sync/errgroup"
)

const (
	userCacheTTL      = 30 * time.Minute
	userCacheMax      = 500
	userCacheSweepInt = 10 * time.Minute
)

var mentionPattern = regexp.MustCompile(`<@([A-Z0-9]+)(?:\|([^>]*))?>`)

// UserCache resolves Slack user IDs to display names.
// Evicts entries older than 30 minutes and caps at 500 entries.
type UserCache struct {
	client SlackAPI
	cache  *TTLCache[string, string]
}

// NewUserCache creates a new user mention resolver with bounded TTL and size.
func NewUserCache(client SlackAPI) *UserCache {
	return &UserCache{
		client: client,
		cache:  NewTTLCache[string, string](userCacheTTL, userCacheSweepInt),
	}
}

// Close stops the periodic cache cleanup goroutine. Safe to call multiple times.
func (uc *UserCache) Close() {
	if uc == nil {
		return
	}
	uc.cache.Stop()
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
	if name, ok := uc.cache.Get(userID); ok {
		return name
	}

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

	uc.cache.Do(func(items map[string]ttlEntry[string]) {
		if len(items) >= userCacheMax {
			evictOne(items)
		}
		items[userID] = ttlEntry[string]{
			Value:  name,
			Expiry: time.Now().Add(userCacheTTL),
		}
	})

	return name
}

// evictOne removes a single expired entry. If none expired, removes the oldest.
func evictOne(items map[string]ttlEntry[string]) {
	now := time.Now()
	oldestID := ""
	oldestExpiry := now.Add(userCacheTTL * 2) // far future sentinel

	for id, entry := range items {
		if now.After(entry.Expiry) {
			delete(items, id)
			return
		}
		if entry.Expiry.Before(oldestExpiry) {
			oldestExpiry = entry.Expiry
			oldestID = id
		}
	}
	if oldestID != "" {
		delete(items, oldestID)
	}
}
