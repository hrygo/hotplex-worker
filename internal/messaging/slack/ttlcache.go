package slack

import (
	"sync"
	"time"
)

// ttlEntry pairs a cached value with its absolute expiration time.
type ttlEntry[V any] struct {
	Value  V
	Expiry time.Time
}

// TTLCache provides a generic time-to-live cache with background sweep.
type TTLCache[K comparable, V any] struct {
	mu       sync.RWMutex
	items    map[K]ttlEntry[V]
	ttl      time.Duration
	sweepInt time.Duration
	done     chan struct{}
	once     sync.Once
	wg       sync.WaitGroup
}

// NewTTLCache creates a cache and starts its background sweep goroutine.
func NewTTLCache[K comparable, V any](ttl, sweepInt time.Duration) *TTLCache[K, V] {
	c := &TTLCache[K, V]{
		items:    make(map[K]ttlEntry[V]),
		ttl:      ttl,
		sweepInt: sweepInt,
		done:     make(chan struct{}),
	}
	c.wg.Add(1)
	go c.sweepLoop()
	return c
}

// Get retrieves a cached value. Returns false if the key is missing or expired.
func (c *TTLCache[K, V]) Get(key K) (V, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.items[key]
	if !ok || time.Now().After(e.Expiry) {
		var zero V
		return zero, false
	}
	return e.Value, true
}

// Set stores a value with the default TTL (now + ttl).
func (c *TTLCache[K, V]) Set(key K, value V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[key] = ttlEntry[V]{Value: value, Expiry: time.Now().Add(c.ttl)}
}

// Delete removes a key from the cache.
func (c *TTLCache[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

// Len returns the number of entries in the cache.
func (c *TTLCache[K, V]) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

// Stop terminates the background sweep goroutine and waits for it to finish.
// Safe to call multiple times.
func (c *TTLCache[K, V]) Stop() {
	c.once.Do(func() { close(c.done) })
	c.wg.Wait()
}

// Do provides exclusive access to cache items for complex read-modify-write operations.
func (c *TTLCache[K, V]) Do(fn func(items map[K]ttlEntry[V])) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fn(c.items)
}

func (c *TTLCache[K, V]) sweepLoop() {
	defer c.wg.Done()
	ticker := time.NewTicker(c.sweepInt)
	defer ticker.Stop()

	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.mu.Lock()
			now := time.Now()
			for k, e := range c.items {
				if now.After(e.Expiry) {
					delete(c.items, k)
				}
			}
			c.mu.Unlock()
		}
	}
}
