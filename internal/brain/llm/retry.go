package llm

import (
	"context"
	"time"

	"github.com/cenkalti/backoff/v4"
)

// RetryClient wraps an LLM client with retry and backoff logic.
type RetryClient struct {
	client         LLMClient
	maxRetries     int
	retryMinWaitMs int
	retryMaxWaitMs int
}

// NewRetryClient creates a new retry client wrapper.
// Set maxRetries to 0 to disable retries.
func NewRetryClient(client LLMClient, maxRetries, retryMinWaitMs, retryMaxWaitMs int) *RetryClient {
	return &RetryClient{
		client:         client,
		maxRetries:     maxRetries,
		retryMinWaitMs: retryMinWaitMs,
		retryMaxWaitMs: retryMaxWaitMs,
	}
}

// Chat implements the Chat method with retry logic.
func (r *RetryClient) Chat(ctx context.Context, prompt string) (string, error) {
	if r.maxRetries == 0 {
		return r.client.Chat(ctx, prompt)
	}

	var result string
	err := r.executeWithBackoff(ctx, func() error {
		var err error
		result, err = r.client.Chat(ctx, prompt)
		return err
	})

	return result, err
}

// Analyze implements the Analyze method with retry logic.
func (r *RetryClient) Analyze(ctx context.Context, prompt string, target any) error {
	if r.maxRetries == 0 {
		return r.client.Analyze(ctx, prompt, target)
	}

	return r.executeWithBackoff(ctx, func() error {
		return r.client.Analyze(ctx, prompt, target)
	})
}

// ChatStream implements the ChatStream method with retry logic.
func (r *RetryClient) ChatStream(ctx context.Context, prompt string) (<-chan string, error) {
	// For streaming, we don't retry mid-stream, but we can retry the initial connection
	if r.maxRetries == 0 {
		return r.client.ChatStream(ctx, prompt)
	}

	var result <-chan string
	err := r.executeWithBackoff(ctx, func() error {
		var err error
		result, err = r.client.ChatStream(ctx, prompt)
		return err
	})

	return result, err
}

// HealthCheck delegates to the underlying client.
func (r *RetryClient) HealthCheck(ctx context.Context) HealthStatus {
	return r.client.HealthCheck(ctx)
}

// executeWithBackoff executes a function with exponential backoff retry logic.
func (r *RetryClient) executeWithBackoff(ctx context.Context, fn func() error) error {
	if r.maxRetries == 0 {
		return fn()
	}

	// Create exponential backoff
	b := backoff.NewExponentialBackOff()
	b.InitialInterval = time.Duration(r.retryMinWaitMs) * time.Millisecond
	b.MaxInterval = time.Duration(r.retryMaxWaitMs) * time.Millisecond
	b.MaxElapsedTime = time.Duration(r.retryMaxWaitMs*r.maxRetries) * time.Millisecond

	// Limit retries
	retryCount := 0
	maxAttempts := r.maxRetries + 1 // maxRetries is additional retries beyond first attempt

	var lastErr error
	err := backoff.RetryNotify(func() error {
		lastErr = fn()
		if lastErr != nil {
			retryCount++
			if retryCount >= maxAttempts {
				return backoff.Permanent(lastErr)
			}
			return lastErr
		}
		return nil
	}, backoff.WithContext(b, ctx), nil)

	if err != nil {
		return err
	}

	return lastErr
}
