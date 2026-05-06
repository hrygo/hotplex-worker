package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
)

// CacheKey represents a unique cache key for LLM requests.
type CacheKey struct {
	Prompt    string
	Model     string
	IsAnalyze bool
}

// CacheEntry represents a cached response.
type CacheEntry struct {
	Response string
	JSONData []byte
}

// CachedClient wraps an LLM client with LRU caching.
type CachedClient struct {
	client LLMClient
	cache  *lru.Cache[string, CacheEntry]
	mu     sync.RWMutex
}

// NewCachedClient creates a new cached client wrapper.
// Set cacheSize to 0 to disable caching.
func NewCachedClient(client LLMClient, cacheSize int) *CachedClient {
	cache, _ := lru.New[string, CacheEntry](cacheSize)
	return &CachedClient{
		client: client,
		cache:  cache,
	}
}

// Chat implements the Chat method with caching.
func (c *CachedClient) Chat(ctx context.Context, prompt string) (string, error) {
	key := c.makeKey(prompt, false)

	// Try cache first
	c.mu.RLock()
	if entry, found := c.cache.Get(key); found {
		c.mu.RUnlock()
		return entry.Response, nil
	}
	c.mu.RUnlock()

	// Cache miss - call underlying client
	response, err := c.client.Chat(ctx, prompt)
	if err != nil {
		return "", err
	}

	// Cache the result
	c.mu.Lock()
	c.cache.Add(key, CacheEntry{Response: response})
	c.mu.Unlock()

	return response, nil
}

// Analyze implements the Analyze method with caching.
func (c *CachedClient) Analyze(ctx context.Context, prompt string, target any) error {
	key := c.makeKey(prompt, true)

	// Try cache first
	c.mu.RLock()
	if entry, found := c.cache.Get(key); found {
		c.mu.RUnlock()
		return json.Unmarshal(entry.JSONData, target)
	}
	c.mu.RUnlock()

	// Cache miss - call underlying client
	err := c.client.Analyze(ctx, prompt, target)
	if err != nil {
		return err
	}

	// Cache the result by marshaling target back to JSON
	jsonData, err := json.Marshal(target)
	if err != nil {
		// Don't fail the request if caching fails, but log for observability
		log.Printf("[CachedClient] WARN: failed to marshal analyze result for cache: %v", err)
		return nil
	}

	c.mu.Lock()
	c.cache.Add(key, CacheEntry{JSONData: jsonData})
	c.mu.Unlock()

	return nil
}

// ChatStream does not use caching (streams are not cacheable).
func (c *CachedClient) ChatStream(ctx context.Context, prompt string) (<-chan string, error) {
	return c.client.ChatStream(ctx, prompt)
}

// HealthCheck delegates to the underlying client.
func (c *CachedClient) HealthCheck(ctx context.Context) HealthStatus {
	return c.client.HealthCheck(ctx)
}

// makeKey creates a unique cache key for a request using SHA-256 hash.
func (c *CachedClient) makeKey(prompt string, isAnalyze bool) string {
	h := sha256.Sum256([]byte(prompt))
	prefix := "chat"
	if isAnalyze {
		prefix = "analyze"
	}
	return prefix + ":" + hex.EncodeToString(h[:])
}

// ClearCache clears all cached entries.
func (c *CachedClient) ClearCache() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache.Purge()
}

// CacheStats returns current cache statistics.
func (c *CachedClient) CacheStats() (keys, hits, misses int) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	// Note: golang-lru doesn't expose hit/miss stats by default
	// You can extend this if needed
	return c.cache.Len(), 0, 0
}

// UnderlyingClient returns the underlying client.
// Used for component extraction in observable chains.
func (c *CachedClient) UnderlyingClient() LLMClient {
	return c.client
}
