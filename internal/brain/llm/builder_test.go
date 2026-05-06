package llm

import (
	"errors"
	"testing"
)

func TestClientBuilder_RequiresAPIKey(t *testing.T) {
	_, err := NewClientBuilder().
		WithModel("gpt-4").
		Build()

	if err == nil {
		t.Error("expected error for missing API key")
	}
	if !errors.Is(err, ErrAPIKeyRequired) {
		t.Errorf("expected ErrAPIKeyRequired, got %v", err)
	}
}

func TestClientBuilder_RequiresModel(t *testing.T) {
	_, err := NewClientBuilder().
		WithAPIKey("test-key").
		Build()

	if err == nil {
		t.Error("expected error for missing model")
	}
	if !errors.Is(err, ErrModelRequired) {
		t.Errorf("expected ErrModelRequired, got %v", err)
	}
}

func TestClientBuilder_MinimalClient(t *testing.T) {
	client, err := NewClientBuilder().
		WithAPIKey("test-key").
		WithModel("gpt-4").
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client == nil {
		t.Fatal("expected non-nil client")
	}

	// Verify it's an OpenAIClient (no wrappers)
	_, ok := client.(*OpenAIClient)
	if !ok {
		t.Error("expected OpenAIClient without wrappers")
	}
}

func TestClientBuilder_WithCache(t *testing.T) {
	client, err := NewClientBuilder().
		WithAPIKey("test-key").
		WithModel("gpt-4").
		WithCache().
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's a CachedClient
	_, ok := client.(*CachedClient)
	if !ok {
		t.Error("expected CachedClient wrapper")
	}
}

func TestClientBuilder_WithCacheSize(t *testing.T) {
	client, err := NewClientBuilder().
		WithAPIKey("test-key").
		WithModel("gpt-4").
		WithCache(500).
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's a CachedClient
	_, ok := client.(*CachedClient)
	if !ok {
		t.Error("expected CachedClient wrapper")
	}
}

func TestClientBuilder_WithRetry(t *testing.T) {
	client, err := NewClientBuilder().
		WithAPIKey("test-key").
		WithModel("gpt-4").
		WithRetry(3).
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's a RetryClient
	_, ok := client.(*RetryClient)
	if !ok {
		t.Error("expected RetryClient wrapper")
	}
}

func TestClientBuilder_WithRetryConfig(t *testing.T) {
	client, err := NewClientBuilder().
		WithAPIKey("test-key").
		WithModel("gpt-4").
		WithRetryConfig(5, 100, 1000).
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's a RetryClient
	_, ok := client.(*RetryClient)
	if !ok {
		t.Error("expected RetryClient wrapper")
	}
}

func TestClientBuilder_WithRateLimit(t *testing.T) {
	client, err := NewClientBuilder().
		WithAPIKey("test-key").
		WithModel("gpt-4").
		WithRateLimit(100).
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's a RateLimitedClient
	_, ok := client.(*RateLimitedClient)
	if !ok {
		t.Error("expected RateLimitedClient wrapper")
	}
}

func TestClientBuilder_WithRateLimitConfig(t *testing.T) {
	client, err := NewClientBuilder().
		WithAPIKey("test-key").
		WithModel("gpt-4").
		WithRateLimitConfig(50, 10).
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's a RateLimitedClient
	_, ok := client.(*RateLimitedClient)
	if !ok {
		t.Error("expected RateLimitedClient wrapper")
	}
}

func TestClientBuilder_WithCircuitBreaker(t *testing.T) {
	client, err := NewClientBuilder().
		WithAPIKey("test-key").
		WithModel("gpt-4").
		WithCircuitBreaker().
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's a CircuitClient
	_, ok := client.(*CircuitClient)
	if !ok {
		t.Error("expected CircuitClient wrapper")
	}
}

func TestClientBuilder_WithMetrics(t *testing.T) {
	client, err := NewClientBuilder().
		WithAPIKey("test-key").
		WithModel("gpt-4").
		WithMetrics().
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's a MetricsClient (outermost wrapper)
	_, ok := client.(*MetricsClient)
	if !ok {
		t.Error("expected MetricsClient wrapper")
	}
}

func TestClientBuilder_WithEndpoint(t *testing.T) {
	client, err := NewClientBuilder().
		WithAPIKey("test-key").
		WithEndpoint("https://api.deepseek.com/v1").
		WithModel("deepseek-chat").
		Build()

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client == nil {
		t.Error("expected non-nil client")
	}
}

func TestClientBuilder_InvalidEndpoint(t *testing.T) {
	_, err := NewClientBuilder().
		WithAPIKey("test-key").
		WithEndpoint("://invalid-url").
		WithModel("gpt-4").
		Build()

	if err == nil {
		t.Error("expected error for invalid endpoint URL")
	}
}

func TestClientBuilder_FluentChaining(t *testing.T) {
	// Verify that all With* methods return the builder for chaining
	builder := NewClientBuilder()

	if builder.WithAPIKey("key") != builder {
		t.Error("WithAPIKey should return builder")
	}
	if builder.WithEndpoint("endpoint") != builder {
		t.Error("WithEndpoint should return builder")
	}
	if builder.WithModel("model") != builder {
		t.Error("WithModel should return builder")
	}
	if builder.WithMetrics() != builder {
		t.Error("WithMetrics should return builder")
	}
	if builder.WithCache() != builder {
		t.Error("WithCache should return builder")
	}
	if builder.WithRetry(3) != builder {
		t.Error("WithRetry should return builder")
	}
	if builder.WithCircuitBreaker() != builder {
		t.Error("WithCircuitBreaker should return builder")
	}
	if builder.WithRateLimit(100) != builder {
		t.Error("WithRateLimit should return builder")
	}
}

func TestDefaultMetricsConfig(t *testing.T) {
	cfg := DefaultMetricsConfig()
	if !cfg.Enabled {
		t.Error("expected metrics to be enabled by default")
	}
	if cfg.ServiceName == "" {
		t.Error("expected non-empty service name")
	}
	if cfg.MaxLatencySamples <= 0 {
		t.Error("expected positive max latency samples")
	}
}
