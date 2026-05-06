package llm

import (
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"time"
)

// Static errors for validation.
var (
	ErrAPIKeyRequired  = errors.New("API key is required")
	ErrModelRequired   = errors.New("model is required")
	ErrInvalidEndpoint = errors.New("invalid endpoint URL")
)

// ClientBuilder provides a fluent API for constructing LLM clients
// with various middleware layers (rate limiting, caching, circuit breaker, etc.).
type ClientBuilder struct {
	// Base configuration
	apiKey   string
	endpoint string
	model    string
	logger   *slog.Logger

	// Capability switches
	withMetrics   bool
	withCache     bool
	withRetry     bool
	withCircuit   bool
	withRateLimit bool

	// Capability configurations
	metricsConfig  *MetricsConfig
	cacheSize      int
	maxRetries     int
	retryMinWaitMs int
	retryMaxWaitMs int
	circuitConfig  *CircuitBreakerConfig
	rateLimitRPS   float64
	rateLimitBurst int
}

// NewClientBuilder creates a new client builder with default logger.
func NewClientBuilder() *ClientBuilder {
	return &ClientBuilder{
		logger: slog.Default(),
	}
}

// WithAPIKey sets the API key for the LLM provider.
func (b *ClientBuilder) WithAPIKey(key string) *ClientBuilder {
	b.apiKey = key
	return b
}

// WithEndpoint sets the API endpoint (optional, for non-OpenAI providers).
func (b *ClientBuilder) WithEndpoint(endpoint string) *ClientBuilder {
	b.endpoint = endpoint
	return b
}

// WithModel sets the model to use.
func (b *ClientBuilder) WithModel(model string) *ClientBuilder {
	b.model = model
	return b
}

// WithLogger sets a custom logger.
func (b *ClientBuilder) WithLogger(logger *slog.Logger) *ClientBuilder {
	b.logger = logger
	return b
}

// WithMetrics enables metrics collection with optional custom configuration.
func (b *ClientBuilder) WithMetrics(config ...MetricsConfig) *ClientBuilder {
	b.withMetrics = true
	if len(config) > 0 {
		b.metricsConfig = &config[0]
	}
	return b
}

// WithCache enables response caching with optional custom cache size.
func (b *ClientBuilder) WithCache(size ...int) *ClientBuilder {
	b.withCache = true
	if len(size) > 0 && size[0] > 0 {
		b.cacheSize = size[0]
	} else {
		b.cacheSize = DefaultCacheSize
	}
	return b
}

// WithRetry enables retry logic with specified max retries.
func (b *ClientBuilder) WithRetry(maxRetries int) *ClientBuilder {
	b.withRetry = true
	b.maxRetries = maxRetries
	b.retryMinWaitMs = DefaultRetryMinWaitMs
	b.retryMaxWaitMs = DefaultRetryMaxWaitMs
	return b
}

// WithRetryConfig enables retry logic with custom wait times.
func (b *ClientBuilder) WithRetryConfig(maxRetries, minWaitMs, maxWaitMs int) *ClientBuilder {
	b.withRetry = true
	b.maxRetries = maxRetries
	b.retryMinWaitMs = minWaitMs
	b.retryMaxWaitMs = maxWaitMs
	return b
}

// WithCircuitBreaker enables circuit breaker protection with optional custom configuration.
func (b *ClientBuilder) WithCircuitBreaker(config ...CircuitBreakerConfig) *ClientBuilder {
	b.withCircuit = true
	if len(config) > 0 {
		b.circuitConfig = &config[0]
	}
	return b
}

// WithRateLimit enables rate limiting with specified requests per second.
func (b *ClientBuilder) WithRateLimit(rps float64) *ClientBuilder {
	b.withRateLimit = true
	b.rateLimitRPS = rps
	b.rateLimitBurst = int(rps) // Default burst = RPS
	return b
}

// WithRateLimitConfig enables rate limiting with custom burst size.
func (b *ClientBuilder) WithRateLimitConfig(rps float64, burst int) *ClientBuilder {
	b.withRateLimit = true
	b.rateLimitRPS = rps
	b.rateLimitBurst = burst
	return b
}

// Build constructs the LLM client with all configured middleware layers.
// The wrapping order (from innermost to outermost):
//  1. Base client (OpenAI)
//  2. Cache (innermost - cache raw responses before retries)
//  3. Retry (retry on transient failures)
//  4. Rate Limiter (control request rate)
//  5. Circuit Breaker (fail fast on repeated errors)
//  6. Metrics (outermost - observe all operations)
func (b *ClientBuilder) Build() (LLMClient, error) {
	if err := b.validate(); err != nil {
		return nil, err
	}

	// 1. Create base client
	var client LLMClient = NewOpenAIClient(b.apiKey, b.endpoint, b.model, b.logger)

	// 2. Apply cache layer (innermost - cache raw responses)
	if b.withCache {
		size := b.cacheSize
		if size <= 0 {
			size = DefaultCacheSize
		}
		client = NewCachedClient(client, size)
	}

	// 3. Apply retry layer
	if b.withRetry {
		maxRetries := b.maxRetries
		minWait := b.retryMinWaitMs
		maxWait := b.retryMaxWaitMs

		if maxRetries <= 0 {
			maxRetries = DefaultMaxRetries
		}

		client = NewRetryClient(client, maxRetries, minWait, maxWait)
	}

	// 4. Apply rate limit layer
	if b.withRateLimit {
		rps := b.rateLimitRPS
		burst := b.rateLimitBurst

		if rps <= 0 {
			rps = DefaultRPS
		}
		if burst <= 0 {
			burst = int(rps)
		}

		cfg := RateLimitConfig{
			RequestsPerSecond: rps,
			BurstSize:         burst,
			MaxQueueSize:      DefaultMaxQueueSize,
			QueueTimeout:      DefaultQueueTimeout,
		}

		limiter := NewRateLimiter(cfg)
		client = NewRateLimitedClient(client, limiter)
	}

	// 5. Apply circuit breaker layer
	if b.withCircuit {
		cfg := b.circuitConfig
		if cfg == nil {
			defaultCfg := DefaultCircuitBreakerConfig()
			cfg = &defaultCfg
		}
		if cfg.Logger == nil {
			cfg.Logger = b.logger
		}

		cb := NewCircuitBreaker(*cfg)
		client = NewCircuitClient(client, cb)
	}

	// 6. Apply metrics layer (outermost for visibility)
	if b.withMetrics {
		cfg := b.metricsConfig
		if cfg == nil {
			cfg = &MetricsConfig{
				Enabled:           true,
				ServiceName:       "hotplex-brain",
				MaxLatencySamples: DefaultMaxLatencySamples,
			}
		}
		if cfg.MaxLatencySamples <= 0 {
			cfg.MaxLatencySamples = DefaultMaxLatencySamples
		}

		collector := NewMetricsCollector(*cfg)
		client = NewMetricsClient(client, collector, b.model)
	}

	return client, nil
}

// validate validates the builder configuration.
func (b *ClientBuilder) validate() error {
	if b.apiKey == "" {
		return ErrAPIKeyRequired
	}
	if b.model == "" {
		return ErrModelRequired
	}
	if b.endpoint != "" {
		if _, err := url.Parse(b.endpoint); err != nil {
			return fmt.Errorf("%w: %w", ErrInvalidEndpoint, err)
		}
	}
	if b.logger == nil {
		b.logger = slog.Default()
	}
	return nil
}

// Default configuration values.
const (
	DefaultCacheSize         = 1000
	DefaultMaxRetries        = 3
	DefaultRetryMinWaitMs    = 100
	DefaultRetryMaxWaitMs    = 5000
	DefaultRPS               = 10.0
	DefaultMaxQueueSize      = 100
	DefaultQueueTimeout      = 30 * time.Second
	DefaultMaxLatencySamples = 1000
)

// DefaultMetricsConfig returns default metrics configuration.
func DefaultMetricsConfig() MetricsConfig {
	return MetricsConfig{
		Enabled:           true,
		ServiceName:       "hotplex-brain",
		MaxLatencySamples: DefaultMaxLatencySamples,
	}
}
