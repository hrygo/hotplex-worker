package llm

import (
	"context"
	"fmt"
)

// CostEstimator estimates the cost of a request.
type CostEstimator func(prompt string, model string) float64

// DefaultCostEstimator provides a simple cost estimation.
// Uses approximate pricing: $0.001 per 1K prompt tokens.
func DefaultCostEstimator(prompt string, model string) float64 {
	// Rough estimate: 4 chars per token, $0.001 per 1K tokens
	tokens := float64(len(prompt)) / 4
	return tokens * 0.000001 // $0.001 per 1K tokens
}

// BudgetClient wraps an LLM client with budget tracking.
// It checks budget before each request and tracks costs after.
type BudgetClient struct {
	client    LLMClient
	tracker   *BudgetTracker
	estimator CostEstimator
	model     string
}

// NewBudgetClient creates a new budget-aware client wrapper.
func NewBudgetClient(client LLMClient, tracker *BudgetTracker, model string, estimator CostEstimator) *BudgetClient {
	if estimator == nil {
		estimator = DefaultCostEstimator
	}
	return &BudgetClient{
		client:    client,
		tracker:   tracker,
		estimator: estimator,
		model:     model,
	}
}

// checkAndTrackBudget validates budget before request and tracks cost after.
// This helper eliminates duplicate budget checking logic across methods.
func (b *BudgetClient) checkAndTrackBudget(prompt string) (float64, error) {
	estimatedCost := b.estimator(prompt, b.model)
	allowed, _, err := b.tracker.CheckBudget(estimatedCost)
	if err != nil {
		return 0, err
	}
	if !allowed {
		return 0, fmt.Errorf("budget exceeded for session")
	}
	return estimatedCost, nil
}

// Chat implements the Chat method with budget tracking.
func (b *BudgetClient) Chat(ctx context.Context, prompt string) (string, error) {
	estimatedCost, err := b.checkAndTrackBudget(prompt)
	if err != nil {
		return "", err
	}

	result, err := b.client.Chat(ctx, prompt)
	if err != nil {
		return "", err
	}

	_ = b.tracker.TrackRequest(estimatedCost)
	return result, nil
}

// Analyze implements the Analyze method with budget tracking.
func (b *BudgetClient) Analyze(ctx context.Context, prompt string, target any) error {
	estimatedCost, err := b.checkAndTrackBudget(prompt)
	if err != nil {
		return err
	}

	err = b.client.Analyze(ctx, prompt, target)
	if err != nil {
		return err
	}

	_ = b.tracker.TrackRequest(estimatedCost)
	return nil
}

// ChatStream implements the ChatStream method with budget tracking.
func (b *BudgetClient) ChatStream(ctx context.Context, prompt string) (<-chan string, error) {
	estimatedCost, err := b.checkAndTrackBudget(prompt)
	if err != nil {
		return nil, err
	}

	result, err := b.client.ChatStream(ctx, prompt)
	if err != nil {
		return nil, err
	}

	_ = b.tracker.TrackRequest(estimatedCost)
	return result, nil
}

// HealthCheck delegates to the underlying client.
func (b *BudgetClient) HealthCheck(ctx context.Context) HealthStatus {
	return b.client.HealthCheck(ctx)
}

// GetTracker returns the underlying budget tracker.
func (b *BudgetClient) GetTracker() *BudgetTracker {
	return b.tracker
}

// Compile-time interface compliance verification.
var _ LLMClient = (*BudgetClient)(nil)
