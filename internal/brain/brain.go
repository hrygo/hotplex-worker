package brain

import (
	"context"
	"errors"
	"sync"

	"github.com/hrygo/hotplex/internal/brain/llm"
)

// ErrBrainNotConfigured is returned when a brain feature is used without a configured LLM backend.
var ErrBrainNotConfigured = errors.New("brain not configured")

// Compile-time interface verification
var (
	_ Brain           = (*enhancedBrainWrapper)(nil)
	_ StreamingBrain  = (*enhancedBrainWrapper)(nil)
	_ RoutableBrain   = (*enhancedBrainWrapper)(nil)
	_ ObservableBrain = (*enhancedBrainWrapper)(nil)
)

// ChatOptions controls LLM generation parameters.
// Re-exported from llm package for convenience.
type ChatOptions = llm.ChatOptions

// Brain represents the core "System 1" intelligence for HotPlex.
// It provides fast, structured, and low-cost reasoning capabilities.
type Brain interface {
	// Chat generates a plain text response for a given prompt.
	// Best used for simple questions, greetings, or summarization.
	Chat(ctx context.Context, prompt string) (string, error)

	// ChatWithOptions generates a response with fine-grained control over LLM parameters.
	// Zero-value fields use provider defaults (MaxTokens=0 → provider default, Temperature=0 → provider default).
	ChatWithOptions(ctx context.Context, prompt string, opts ChatOptions) (string, error)

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

// HealthStatus represents the health status of the Brain service.
// Re-exported from llm package for convenience.
type HealthStatus = llm.HealthStatus

var (
	globalBrainMu sync.RWMutex
	globalBrain   Brain
)

// Global returns the globally configured Brain instance.
// If no brain is configured, it returns nil.
func Global() Brain {
	globalBrainMu.RLock()
	defer globalBrainMu.RUnlock()
	return globalBrain
}

// SetGlobal sets the global Brain instance.
func SetGlobal(b Brain) {
	globalBrainMu.Lock()
	defer globalBrainMu.Unlock()
	globalBrain = b
}

// GetRouter returns the global router if the brain supports routing.
func GetRouter() *llm.Router {
	if rb, ok := Global().(interface{ GetRouter() *llm.Router }); ok {
		return rb.GetRouter()
	}
	return nil
}

// GetRateLimiter returns the global rate limiter if available.
func GetRateLimiter() *llm.RateLimiter {
	if rb, ok := Global().(interface{ GetRateLimiter() *llm.RateLimiter }); ok {
		return rb.GetRateLimiter()
	}
	return nil
}
