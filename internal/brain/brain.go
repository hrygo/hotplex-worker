package brain

import (
	"context"

	"github.com/hrygo/hotplex/internal/brain/llm"
)

// Compile-time interface verification
var (
	_ Brain           = (*enhancedBrainWrapper)(nil)
	_ StreamingBrain  = (*enhancedBrainWrapper)(nil)
	_ RoutableBrain   = (*enhancedBrainWrapper)(nil)
	_ ObservableBrain = (*enhancedBrainWrapper)(nil)
)

// Brain represents the core "System 1" intelligence for HotPlex.
// It provides fast, structured, and low-cost reasoning capabilities.
type Brain interface {
	// Chat generates a plain text response for a given prompt.
	// Best used for simple questions, greetings, or summarization.
	Chat(ctx context.Context, prompt string) (string, error)

	// Analyze performs structured analysis and returns the result in the target struct.
	// The target must be a pointer to a struct that can be unmarshaled from JSON.
	// Useful for intent routing, safety checks, and complex data extraction.
	Analyze(ctx context.Context, prompt string, target any) error
}

// StreamingBrain extends Brain with streaming capabilities.
// It provides token-by-token streaming for real-time responses.
type StreamingBrain interface {
	Brain

	// ChatStream returns a channel that streams tokens as they are generated.
	// The channel is closed when the stream completes or an error occurs.
	// Best used for long responses, real-time UI updates, or progressive rendering.
	ChatStream(ctx context.Context, prompt string) (<-chan string, error)
}

// RoutableBrain extends Brain with model routing capability.
type RoutableBrain interface {
	Brain

	// ChatWithModel generates a response using a specific model.
	ChatWithModel(ctx context.Context, model string, prompt string) (string, error)

	// AnalyzeWithModel performs analysis using a specific model.
	AnalyzeWithModel(ctx context.Context, model string, prompt string, target any) error
}

// ObservableBrain provides observability and metrics access.
type ObservableBrain interface {
	Brain

	// GetMetrics returns current metrics statistics.
	GetMetrics() llm.MetricsStats

	// GetCostCalculator returns the cost calculator.
	GetCostCalculator() *llm.CostCalculator
}

// ResilientBrain extends Brain with circuit breaker and failover capabilities.
type ResilientBrain interface {
	Brain

	// GetCircuitBreaker returns the circuit breaker.
	GetCircuitBreaker() *llm.CircuitBreaker

	// GetFailoverManager returns the failover manager.
	GetFailoverManager() *llm.FailoverManager

	// ResetCircuitBreaker manually resets the circuit breaker.
	ResetCircuitBreaker()

	// ManualFailover manually switches to a specific provider.
	ManualFailover(providerName string) error
}

// BudgetControlledBrain extends Brain with budget control capabilities.
type BudgetControlledBrain interface {
	Brain

	// GetBudgetTracker returns the budget tracker for a session.
	GetBudgetTracker(sessionID string) *llm.BudgetTracker

	// GetBudgetManager returns the budget manager.
	GetBudgetManager() *llm.BudgetManager
}

// PriorityBrain extends Brain with priority queue capabilities.
type PriorityBrain interface {
	Brain

	// GetPriorityScheduler returns the priority scheduler.
	GetPriorityScheduler() *llm.PriorityScheduler

	// SubmitWithPriority submits a request with specified priority.
	SubmitWithPriority(ctx context.Context, prompt string, priority llm.Priority) (string, error)
}

// HealthStatus represents the health status of the Brain service.
// Re-exported from llm package for convenience.
type HealthStatus = llm.HealthStatus

var (
	globalBrain Brain
)

// Global returns the globally configured Brain instance.
// If no brain is configured, it returns nil.
func Global() Brain {
	return globalBrain
}

// SetGlobal sets the global Brain instance.
func SetGlobal(b Brain) {
	globalBrain = b
}

// GetRouter returns the global router if the brain supports routing.
func GetRouter() *llm.Router {
	if rb, ok := globalBrain.(interface{ GetRouter() *llm.Router }); ok {
		return rb.GetRouter()
	}
	return nil
}

// GetRateLimiter returns the global rate limiter if available.
func GetRateLimiter() *llm.RateLimiter {
	if rb, ok := globalBrain.(interface{ GetRateLimiter() *llm.RateLimiter }); ok {
		return rb.GetRateLimiter()
	}
	return nil
}
