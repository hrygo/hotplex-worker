package slack

import (
	"context"
	"regexp"
	"sync"

	"github.com/slack-go/slack"
)

var mentionPattern = regexp.MustCompile(`<@([A-Z0-9]+)(?:\|([^>]*))?>`)

// UserCache resolves Slack user IDs to display names.
// Uses slack.Client.GetUserInfoContext for resolution.
type UserCache struct {
	client *slack.Client
	cache  map[string]string
	mu     sync.RWMutex
}

// NewUserCache creates a new user mention resolver.
func NewUserCache(client *slack.Client) *UserCache {
	return &UserCache{client: client, cache: make(map[string]string)}
}

// ResolveMentions replaces <@UID> with @DisplayName.
// Bot self-mentions are removed. Non-resolvable mentions kept as-is.
func (uc *UserCache) ResolveMentions(ctx context.Context, text, botID string) string {
	return mentionPattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := mentionPattern.FindStringSubmatch(match)
		if len(parts) < 2 {
			return match
		}
		userID := parts[1]
		inlineName := "" // from <@UID|Name> format
		if len(parts) >= 3 {
			inlineName = parts[2]
		}

		if userID == botID {
			return "" // remove bot self-mention
		}

		name := uc.resolve(ctx, userID, inlineName)
		if name != "" {
			return "@" + name
		}
		return match // keep <@UID> if unresolvable
	})
}

func (uc *UserCache) resolve(ctx context.Context, userID, fallback string) string {
	uc.mu.RLock()
	if name, ok := uc.cache[userID]; ok {
		uc.mu.RUnlock()
		return name
	}
	uc.mu.RUnlock()

	if uc.client == nil {
		return fallback
	}

	// SDK API: slack.Client.GetUserInfoContext
	user, err := uc.client.GetUserInfoContext(ctx, userID)
	if err != nil {
		return fallback
	}

	name := user.Profile.DisplayName
	if name == "" {
		name = user.RealName
	}

	uc.mu.Lock()
	uc.cache[userID] = name
	uc.mu.Unlock()
	return name
}
