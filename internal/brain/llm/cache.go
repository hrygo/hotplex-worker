package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"sync/atomic"

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
	hits   atomic.Int64
	misses atomic.Int64
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
		c.hits.Add(1)
		return entry.Response, nil
	}

	c.misses.Add(1)
	response, err := c.client.Chat(ctx, prompt)
	if err != nil {
		return "", err
	}

	c.cache.Add(key, CacheEntry{Response: response})
	return response, nil
}

func (c *CachedClient) ChatWithOptions(ctx context.Context, prompt string, opts ChatOptions) (string, error) {
	key := c.makeOptsKey(prompt, opts)
	if entry, found := c.cache.Get(key); found {
		c.hits.Add(1)
		return entry.Response, nil
	}

	c.misses.Add(1)
	response, err := c.client.ChatWithOptions(ctx, prompt, opts)
	if err != nil {
		return "", err
	}

	c.cache.Add(key, CacheEntry{Response: response})
	return response, nil
}

func (c *CachedClient) Analyze(ctx context.Context, prompt string, target any) error {
	key := c.makeKey(prompt, true)
	if entry, found := c.cache.Get(key); found {
		c.hits.Add(1)
		return json.Unmarshal(entry.JSONData, target)
	}

	c.misses.Add(1)
	err := c.client.Analyze(ctx, prompt, target)
	if err != nil {
		return err
	}

	jsonData, err := json.Marshal(target)
	if err != nil {
		return fmt.Errorf("cachedclient: marshal analyze result for cache: %w", err)
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

func (c *CachedClient) makeOptsKey(prompt string, opts ChatOptions) string {
	h := sha256.Sum256([]byte(prompt))
	tempStr := "def"
	if opts.Temperature != nil {
		tempStr = strconv.FormatFloat(*opts.Temperature, 'f', -1, 64)
	}
	return fmt.Sprintf("chatopt:%d:%s:%s", opts.MaxTokens, tempStr, hex.EncodeToString(h[:]))
}

func (c *CachedClient) ClearCache() {
	c.cache.Purge()
}

func (c *CachedClient) CacheStats() (keys, hits, misses int) {
	return c.cache.Len(), int(c.hits.Load()), int(c.misses.Load())
}

func (c *CachedClient) UnderlyingClient() LLMClient {
	return c.client
}
