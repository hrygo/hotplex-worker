package llm

import (
	"log/slog"
	"time"
)

// Preset configurations for common use cases.

// ProductionClient creates a production-ready LLM client with all capabilities enabled.
// Suitable for production workloads requiring reliability, observability, and protection.
func ProductionClient(apiKey, model string) (LLMClient, error) {
	return NewClientBuilder().
		WithAPIKey(apiKey).
		WithModel(model).
		WithMetrics().
		WithCache(DefaultCacheSize).
		WithCircuitBreaker().
		WithRateLimit(100).
		WithRetry(3).
		Build()
}

// ProductionClientWithEndpoint creates a production-ready client with custom endpoint.
func ProductionClientWithEndpoint(apiKey, endpoint, model string) (LLMClient, error) {
	return NewClientBuilder().
		WithAPIKey(apiKey).
		WithEndpoint(endpoint).
		WithModel(model).
		WithMetrics().
		WithCache(DefaultCacheSize).
		WithCircuitBreaker().
		WithRateLimit(100).
		WithRetry(3).
		Build()
}

// DevelopmentClient creates a development client with minimal overhead.
// Only metrics are enabled for debugging purposes.
func DevelopmentClient(apiKey, model string) (LLMClient, error) {
	return NewClientBuilder().
		WithAPIKey(apiKey).
		WithModel(model).
		WithMetrics(DefaultMetricsConfig()).
		Build()
}

// DevelopmentClientWithEndpoint creates a development client with custom endpoint.
func DevelopmentClientWithEndpoint(apiKey, endpoint, model string) (LLMClient, error) {
	return NewClientBuilder().
		WithAPIKey(apiKey).
		WithEndpoint(endpoint).
		WithModel(model).
		WithMetrics(DefaultMetricsConfig()).
		Build()
}

// TestingClient creates a client optimized for testing scenarios.
// Includes cache for deterministic responses and minimal rate limiting.
func TestingClient(apiKey, model string) (LLMClient, error) {
	return NewClientBuilder().
		WithAPIKey(apiKey).
		WithModel(model).
		WithCache(100).
		WithRateLimit(1000). // High limit for tests
		Build()
}

// HighThroughputClient creates a client optimized for high-throughput scenarios.
// Aggressive caching, high rate limits, and no retries for speed.
func HighThroughputClient(apiKey, model string) (LLMClient, error) {
	return NewClientBuilder().
		WithAPIKey(apiKey).
		WithModel(model).
		WithCache(10000).
		WithRateLimit(500).
		WithMetrics().
		Build()
}

// ReliableClient creates a client optimized for reliability.
// Aggressive retries, circuit breaker, and conservative rate limiting.
func ReliableClient(apiKey, model string) (LLMClient, error) {
	return NewClientBuilder().
		WithAPIKey(apiKey).
		WithModel(model).
		WithRetryConfig(5, 200, 10000).
		WithCircuitBreaker(CircuitBreakerConfig{
			Name:                "reliable",
			MaxFailures:         3,
			Interval:            30 * time.Second,
			Timeout:             60 * time.Second,
			HalfOpenMaxRequests: 1,
			SuccessThreshold:    3,
		}).
		WithRateLimit(50).
		WithMetrics().
		Build()
}

// MinimalClient creates a bare-bones client with no middleware.
// Useful for simple use cases or custom configurations.
func MinimalClient(apiKey, model string) (LLMClient, error) {
	return NewClientBuilder().
		WithAPIKey(apiKey).
		WithModel(model).
		Build()
}

// === Independent Features (Non-Builder) ===

// BudgetClientWithTracker creates a client with budget tracking.
// The BudgetTracker must be created separately and managed by the caller.
//
// Usage:
//
//	tracker := llm.NewBudgetTracker(llm.BudgetConfig{
//	    Period:          llm.BudgetDaily,
//	    Limit:           10.0, // $10 daily
//	    EnableHardLimit: true,
//	}, "session-123")
//	client, _ := llm.BudgetClientWithTracker(baseClient, tracker, "gpt-4", nil)
func BudgetClientWithTracker(client LLMClient, tracker *BudgetTracker, model string, estimator CostEstimator) *BudgetClient {
	return NewBudgetClient(client, tracker, model, estimator)
}

// NewBudgetManagedClient creates a client with budget tracking using default configuration.
// This is a convenience function for simple budget control.
//
// Usage:
//
//	client, _ := llm.NewBudgetManagedClient(apiKey, "gpt-4", 10.0) // $10 budget
func NewBudgetManagedClient(apiKey, model string, dailyLimit float64) (*BudgetClient, error) {
	baseClient, err := NewClientBuilder().
		WithAPIKey(apiKey).
		WithModel(model).
		Build()
	if err != nil {
		return nil, err
	}

	tracker := NewBudgetTracker(BudgetConfig{
		Period:          BudgetDaily,
		Limit:           dailyLimit,
		EnableHardLimit: true,
		EnableSoftLimit: true,
		AlertThresholds: []BudgetAlertThreshold{
			{Percentage: 50.0, Message: "Budget 50% consumed"},
			{Percentage: 75.0, Message: "Budget 75% consumed"},
			{Percentage: 90.0, Message: "Budget 90% consumed"},
		},
	}, "default")

	return NewBudgetClient(baseClient, tracker, model, nil), nil
}

// PrioritySchedulerWithClient creates a priority scheduler and client for request prioritization.
// Returns both the scheduler (for workers) and client (for submitting requests).
//
// Usage:
//
//	scheduler, client := llm.PrioritySchedulerWithClient(5 * time.Minute, nil)
//	// Submit high priority request
//	_ = client.Submit(ctx, "req-1", llm.PriorityHigh, func() error {
//	    _, err := llmClient.Chat(ctx, prompt)
//	    return err
//	})
//	// Worker processes requests
//	_ = client.ProcessNext(ctx)
func PrioritySchedulerWithClient(timeout time.Duration, logger *slog.Logger) (*PriorityScheduler, *PriorityClient) {
	scheduler := NewPriorityScheduler(DefaultPriorityConfig())
	client := NewPriorityClient(scheduler, timeout, logger)
	return scheduler, client
}
