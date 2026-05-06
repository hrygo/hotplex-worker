package llm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/atomic"
	"golang.org/x/time/rate"
)

// RateLimitConfig holds configuration for rate limiting.
type RateLimitConfig struct {
	// RequestsPerSecond is the sustained request rate.
	RequestsPerSecond float64
	// BurstSize is the maximum burst size.
	BurstSize int
	// MaxQueueSize is the maximum number of waiting requests.
	MaxQueueSize int
	// QueueTimeout is the maximum time a request can wait in queue.
	QueueTimeout time.Duration
	// PerModel enables per-model rate limiting.
	PerModel bool
}

// RateLimiter provides rate limiting with token bucket algorithm.
type RateLimiter struct {
	mu       sync.RWMutex
	limiter  *rate.Limiter
	config   RateLimitConfig
	queue    chan *queuedRequest
	models   map[string]*rate.Limiter
	modelsMu sync.RWMutex
	stats    RateLimitStats
	atomics  AtomicRateLimitStats
}

// queuedRequest represents a request waiting in queue.
type queuedRequest struct {
	ctx      context.Context
	done     chan struct{}
	err      error
	enqueued time.Time
}

// RateLimitStats holds rate limiting statistics.
type RateLimitStats struct {
	TotalRequests    int64
	QueuedRequests   int64
	RejectedRequests int64
	AvgWaitTimeMs    float64
	LastReset        time.Time
}

// AtomicRateLimitStats holds atomic stats for concurrent access.
type AtomicRateLimitStats struct {
	TotalRequests    atomic.Int64
	QueuedRequests   atomic.Int64
	RejectedRequests atomic.Int64
	AvgWaitTimeMs    atomic.Float64
}

// NewRateLimiter creates a new rate limiter.
func NewRateLimiter(config RateLimitConfig) *RateLimiter {
	rl := &RateLimiter{
		config: config,
		queue:  make(chan *queuedRequest, config.MaxQueueSize),
		models: make(map[string]*rate.Limiter),
		stats: RateLimitStats{
			LastReset: time.Now(),
		},
	}

	// Create global limiter
	rl.limiter = rate.NewLimiter(rate.Limit(config.RequestsPerSecond), config.BurstSize)

	// Start queue processor
	go rl.processQueue()

	return rl
}

// Allow checks if a request can proceed immediately.
func (rl *RateLimiter) Allow() bool {
	rl.atomics.TotalRequests.Inc()
	return rl.limiter.Allow()
}

// Wait waits until a request can proceed or context is cancelled.
func (rl *RateLimiter) Wait(ctx context.Context) error {
	return rl.WaitModel(ctx, "")
}

// WaitModel waits for a specific model's rate limit.
func (rl *RateLimiter) WaitModel(ctx context.Context, model string) error {
	rl.atomics.TotalRequests.Inc()

	var limiter *rate.Limiter

	if rl.config.PerModel && model != "" {
		limiter = rl.getModelLimiter(model)
	} else {
		rl.mu.RLock()
		limiter = rl.limiter
		rl.mu.RUnlock()
	}

	// Check if we can proceed immediately
	if limiter.Allow() {
		return nil
	}

	// Check queue capacity
	rl.mu.RLock()
	queueSize := len(rl.queue)
	maxQueue := rl.config.MaxQueueSize
	rl.mu.RUnlock()

	if queueSize >= maxQueue {
		rl.atomics.RejectedRequests.Inc()
		return fmt.Errorf("rate limit queue full (max: %d)", maxQueue)
	}

	// Create queued request
	req := &queuedRequest{
		ctx:      ctx,
		done:     make(chan struct{}),
		enqueued: time.Now(),
	}

	// Try to enqueue with timeout
	select {
	case rl.queue <- req:
		rl.atomics.QueuedRequests.Inc()
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(rl.config.QueueTimeout):
		return fmt.Errorf("enqueue timeout exceeded (%v)", rl.config.QueueTimeout)
	}

	// Wait for turn or cancellation
	select {
	case <-req.done:
		return req.err
	case <-ctx.Done():
		// Do NOT close req.done — WaitModel owns cleanup on error paths
		return ctx.Err()
	case <-time.After(rl.config.QueueTimeout):
		// Do NOT close req.done — WaitModel owns cleanup on error paths
		return fmt.Errorf("queue timeout exceeded (%v)", rl.config.QueueTimeout)
	}
}

// processQueue processes queued requests in order.
func (rl *RateLimiter) processQueue() {
	for req := range rl.queue {
		// Check if request is already cancelled before waiting
		select {
		case <-req.ctx.Done():
			req.err = req.ctx.Err()
			continue // req.done already closed by WaitModel or sender
		default:
		}

		// Wait for rate limiter
		limiter := rl.limiter
		// Note: Per-model rate limiting requires tracking model in queuedRequest
		// Currently using global limiter for queue processing

		err := limiter.Wait(req.ctx)
		if err != nil {
			req.err = err
			// Do NOT close req.done — WaitModel owns cleanup on ctx/timeout
			continue
		}

		// Calculate wait time
		waitTime := time.Since(req.enqueued).Milliseconds()
		rl.atomics.AvgWaitTimeMs.Add(float64(waitTime))

		close(req.done)
	}
}

// getModelLimiter gets or creates a rate limiter for a specific model.
func (rl *RateLimiter) getModelLimiter(model string) *rate.Limiter {
	rl.modelsMu.RLock()
	limiter, ok := rl.models[model]
	rl.modelsMu.RUnlock()

	if ok {
		return limiter
	}

	// Create new limiter for this model
	rl.modelsMu.Lock()
	defer rl.modelsMu.Unlock()

	// Double-check after acquiring write lock
	if limiter, ok = rl.models[model]; ok {
		return limiter
	}

	limiter = rate.NewLimiter(rate.Limit(rl.config.RequestsPerSecond), rl.config.BurstSize)
	rl.models[model] = limiter
	return limiter
}

// SetRate updates the rate limit dynamically.
func (rl *RateLimiter) SetRate(requestsPerSecond float64, burstSize int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.limiter.SetLimit(rate.Limit(requestsPerSecond))
	rl.limiter.SetBurst(burstSize)
	rl.config.RequestsPerSecond = requestsPerSecond
	rl.config.BurstSize = burstSize

	// Update atomic stats for consistency
	rl.atomics.AvgWaitTimeMs.Store(0)
}

// SetModelRate sets rate limit for a specific model.
func (rl *RateLimiter) SetModelRate(model string, requestsPerSecond float64, burstSize int) {
	rl.modelsMu.Lock()
	defer rl.modelsMu.Unlock()

	limiter, ok := rl.models[model]
	if !ok {
		limiter = rate.NewLimiter(rate.Limit(requestsPerSecond), burstSize)
		rl.models[model] = limiter
		return
	}

	limiter.SetLimit(rate.Limit(requestsPerSecond))
	limiter.SetBurst(burstSize)
}

// GetStats returns current rate limiting statistics.
func (rl *RateLimiter) GetStats() RateLimitStats {
	return RateLimitStats{
		TotalRequests:    rl.atomics.TotalRequests.Load(),
		QueuedRequests:   rl.atomics.QueuedRequests.Load(),
		RejectedRequests: rl.atomics.RejectedRequests.Load(),
		AvgWaitTimeMs:    rl.atomics.AvgWaitTimeMs.Load(),
		LastReset:        rl.stats.LastReset,
	}
}

// ResetStats resets the statistics.
func (rl *RateLimiter) ResetStats() {
	rl.atomics.TotalRequests.Store(0)
	rl.atomics.QueuedRequests.Store(0)
	rl.atomics.RejectedRequests.Store(0)
	rl.atomics.AvgWaitTimeMs.Store(0)
	rl.stats.LastReset = time.Now()
}

// Remaining returns the number of requests remaining in the current burst.
func (rl *RateLimiter) Remaining() int {
	rl.mu.RLock()
	defer rl.mu.RUnlock()
	// rate.Limiter doesn't expose Available directly, estimate based on tokens
	return rl.limiter.Burst()
}

// Reset resets the rate limiter immediately.
func (rl *RateLimiter) Reset() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.limiter.AllowN(time.Now(), rl.limiter.Burst())
}

// Close closes the rate limiter and stops queue processing.
func (rl *RateLimiter) Close() {
	close(rl.queue)
}

// WithRateLimit wraps a function with rate limiting.
func (rl *RateLimiter) WithRateLimit(ctx context.Context, model string, fn func() error) error {
	if err := rl.WaitModel(ctx, model); err != nil {
		return err
	}
	return fn()
}

// TryWithRateLimit attempts to execute with rate limiting, returns immediately if rate limited.
func (rl *RateLimiter) TryWithRateLimit(ctx context.Context, model string, fn func() error) error {
	if !rl.Allow() {
		return fmt.Errorf("rate limit exceeded")
	}
	return fn()
}

// RateLimitedClient wraps a client with rate limiting.
type RateLimitedClient struct {
	client  LLMClient
	limiter *RateLimiter
	model   string
}

// NewRateLimitedClient creates a new rate-limited client wrapper.
func NewRateLimitedClient(client LLMClient, limiter *RateLimiter) *RateLimitedClient {
	return &RateLimitedClient{
		client:  client,
		limiter: limiter,
	}
}

// Chat implements rate-limited chat.
func (c *RateLimitedClient) Chat(ctx context.Context, prompt string) (string, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return "", err
	}
	return c.client.Chat(ctx, prompt)
}

// Analyze implements rate-limited analyze.
func (c *RateLimitedClient) Analyze(ctx context.Context, prompt string, target any) error {
	if err := c.limiter.Wait(ctx); err != nil {
		return err
	}
	return c.client.Analyze(ctx, prompt, target)
}

// ChatStream implements rate-limited streaming.
func (c *RateLimitedClient) ChatStream(ctx context.Context, prompt string) (<-chan string, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return nil, err
	}
	return c.client.ChatStream(ctx, prompt)
}

// HealthCheck implements rate-limited health check.
func (c *RateLimitedClient) HealthCheck(ctx context.Context) HealthStatus {
	return c.client.HealthCheck(ctx)
}

// Client returns the underlying client for component extraction.
func (c *RateLimitedClient) Client() LLMClient {
	return c.client
}

// SetModel sets the model for per-model rate limiting.
func (c *RateLimitedClient) SetModel(model string) {
	c.model = model
}
