package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"

	lru "github.com/hashicorp/golang-lru/v2"
)

// CacheEntry represents a cached response.
type CacheEntry struct {
	Response string
	JSONData []byte
}

// CachedClient wraps an LLM client with LRU caching.
// golang-lru/v2 is goroutine-safe, so no additional mutex is needed.
type CachedClient struct {
	client LLMClient
	cache  *lru.Cache[string, CacheEntry]
}

// NewCachedClient creates a new cached client wrapper.
func NewCachedClient(client LLMClient, cacheSize int) *CachedClient {
	cache, _ := lru.New[string, CacheEntry](cacheSize)
	return &CachedClient{
		client: client,
		cache:  cache,
	}
}

func (c *CachedClient) Chat(ctx context.Context, prompt string) (string, error) {
	key := c.makeKey(prompt, false)
	if entry, found := c.cache.Get(key); found {
		return entry.Response, nil
	}

	response, err := c.client.Chat(ctx, prompt)
	if err != nil {
		return "", err
	}

	c.cache.Add(key, CacheEntry{Response: response})
	return response, nil
}

func (c *CachedClient) Analyze(ctx context.Context, prompt string, target any) error {
	key := c.makeKey(prompt, true)
	if entry, found := c.cache.Get(key); found {
		return json.Unmarshal(entry.JSONData, target)
	}

	err := c.client.Analyze(ctx, prompt, target)
	if err != nil {
		return err
	}

	jsonData, err := json.Marshal(target)
	if err != nil {
		slog.Default().Warn("cachedclient: failed to marshal analyze result for cache", "err", err)
		return nil
	}

	c.cache.Add(key, CacheEntry{JSONData: jsonData})
	return nil
}

func (c *CachedClient) ChatStream(ctx context.Context, prompt string) (<-chan string, error) {
	return c.client.ChatStream(ctx, prompt)
}

func (c *CachedClient) HealthCheck(ctx context.Context) HealthStatus {
	return c.client.HealthCheck(ctx)
}

func (c *CachedClient) makeKey(prompt string, isAnalyze bool) string {
	h := sha256.Sum256([]byte(prompt))
	prefix := "chat"
	if isAnalyze {
		prefix = "analyze"
	}
	return prefix + ":" + hex.EncodeToString(h[:])
}

func (c *CachedClient) ClearCache() {
	c.cache.Purge()
}

func (c *CachedClient) CacheStats() (keys, hits, misses int) {
	return c.cache.Len(), 0, 0
}

func (c *CachedClient) UnderlyingClient() LLMClient {
	return c.client
}
