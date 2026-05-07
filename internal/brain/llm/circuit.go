package llm

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/sony/gobreaker"
	"go.uber.org/atomic"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState string

const (
	// CircuitClosed - Normal operation, requests pass through.
	CircuitClosed CircuitState = "closed"
	// CircuitOpen - Circuit is tripped, requests fail fast.
	CircuitOpen CircuitState = "open"
	// CircuitHalfOpen - Testing if service has recovered.
	CircuitHalfOpen CircuitState = "half-open"
)

// CircuitBreakerConfig holds configuration for circuit breaker.
type CircuitBreakerConfig struct {
	// Name is the circuit breaker name (for logging/metrics).
	Name string
	// MaxFailures is the number of failures before opening circuit.
	MaxFailures uint32
	// Interval is the time window for counting failures.
	Interval time.Duration
	// Timeout is how long circuit stays open before half-open.
	Timeout time.Duration
	// HalfOpenMaxRequests is max requests allowed in half-open state.
	HalfOpenMaxRequests uint32
	// SuccessThreshold is successes needed in half-open to close.
	SuccessThreshold uint32
	// Logger for circuit state changes.
	Logger *slog.Logger
}

// DefaultCircuitBreakerConfig returns sensible defaults.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		Name:                "default",
		MaxFailures:         5,
		Interval:            60 * time.Second,
		Timeout:             30 * time.Second,
		HalfOpenMaxRequests: 3,
		SuccessThreshold:    2,
	}
}

// CircuitBreaker wraps gobreaker with additional HotPlex-specific features.
type CircuitBreaker struct {
	breaker *gobreaker.CircuitBreaker
	config  CircuitBreakerConfig
	state   *atomic.String
	mu      sync.RWMutex

	// Metrics
	totalRequests   *atomic.Uint64
	successRequests *atomic.Uint64
	failRequests    *atomic.Uint64
	circuitOpens    *atomic.Uint64
	lastStateChange *atomic.Time

	// Manual control
	forceOpen   *atomic.Bool
	forceClosed *atomic.Bool
}

// CircuitBreakerStats holds circuit breaker statistics.
type CircuitBreakerStats struct {
	State           CircuitState
	TotalRequests   uint64
	SuccessRequests uint64
	FailRequests    uint64
	CircuitOpens    uint64
	LastStateChange time.Time
	IsForced        bool
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	cb := &CircuitBreaker{
		config:          config,
		state:           atomic.NewString(string(CircuitClosed)),
		totalRequests:   atomic.NewUint64(0),
		successRequests: atomic.NewUint64(0),
		failRequests:    atomic.NewUint64(0),
		circuitOpens:    atomic.NewUint64(0),
		lastStateChange: atomic.NewTime(time.Now()),
		forceOpen:       atomic.NewBool(false),
		forceClosed:     atomic.NewBool(false),
	}

	settings := gobreaker.Settings{
		Name:        config.Name,
		MaxRequests: config.HalfOpenMaxRequests,
		Interval:    config.Interval,
		Timeout:     config.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= config.MaxFailures
		},
		OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
			cb.onStateChange(from, to)
		},
	}

	cb.breaker = gobreaker.NewCircuitBreaker(settings)
	return cb
}

// onStateChange handles circuit state changes.
func (cb *CircuitBreaker) onStateChange(from, to gobreaker.State) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	var newState CircuitState
	switch to {
	case gobreaker.StateClosed:
		newState = CircuitClosed
	case gobreaker.StateOpen:
		newState = CircuitOpen
		cb.circuitOpens.Inc()
	case gobreaker.StateHalfOpen:
		newState = CircuitHalfOpen
	}

	cb.state.Store(string(newState))
	cb.lastStateChange.Store(time.Now())

	if cb.config.Logger != nil {
		cb.config.Logger.Info("circuit breaker state changed",
			"name", cb.config.Name,
			"from", from.String(),
			"to", to.String(),
			"newState", newState)
	}
}

// Execute executes a function with circuit breaker protection.
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() error) error {
	cb.totalRequests.Inc()

	// Check manual override
	if cb.forceOpen.Load() && !cb.forceClosed.Load() {
		cb.failRequests.Inc()
		return fmt.Errorf("circuit breaker is manually forced open")
	}

	if cb.forceClosed.Load() {
		cb.successRequests.Inc()
		return fn()
	}

	// Wrap function for gobreaker
	wrappedFn := func() error {
		return fn()
	}

	// Execute with circuit breaker
	_, err := cb.breaker.Execute(func() (interface{}, error) {
		return nil, wrappedFn()
	})

	if err != nil {
		cb.failRequests.Inc()
	} else {
		cb.successRequests.Inc()
	}

	return err
}

// ExecuteWithResult executes a function that returns a value with circuit breaker protection.
func (cb *CircuitBreaker) ExecuteWithResult(ctx context.Context, fn func() (interface{}, error)) (interface{}, error) {
	cb.totalRequests.Inc()

	// Check manual override
	if cb.forceOpen.Load() && !cb.forceClosed.Load() {
		cb.failRequests.Inc()
		return nil, fmt.Errorf("circuit breaker is manually forced open")
	}

	if cb.forceClosed.Load() {
		cb.successRequests.Inc()
		return fn()
	}

	// Execute with circuit breaker
	result, err := cb.breaker.Execute(func() (interface{}, error) {
		return fn()
	})

	if err != nil {
		cb.failRequests.Inc()
	} else {
		cb.successRequests.Inc()
	}

	return result, err
}

// GetState returns the current circuit state.
func (cb *CircuitBreaker) GetState() CircuitState {
	stateStr := cb.state.Load()
	return CircuitState(stateStr)
}

// GetStats returns circuit breaker statistics.
func (cb *CircuitBreaker) GetStats() CircuitBreakerStats {
	return CircuitBreakerStats{
		State:           cb.GetState(),
		TotalRequests:   cb.totalRequests.Load(),
		SuccessRequests: cb.successRequests.Load(),
		FailRequests:    cb.failRequests.Load(),
		CircuitOpens:    cb.circuitOpens.Load(),
		LastStateChange: cb.lastStateChange.Load(),
		IsForced:        cb.forceOpen.Load() || cb.forceClosed.Load(),
	}
}

// Reset manually resets the circuit breaker to closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.forceOpen.Store(false)
	cb.forceClosed.Store(false)
	cb.state.Store(string(CircuitClosed))
	cb.lastStateChange.Store(time.Now())

	if cb.config.Logger != nil {
		cb.config.Logger.Info("circuit breaker manually reset",
			"name", cb.config.Name)
	}
}

// ForceOpen manually opens the circuit (for maintenance/emergency).
func (cb *CircuitBreaker) ForceOpen() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.forceOpen.Store(true)
	cb.forceClosed.Store(false)
	cb.state.Store(string(CircuitOpen))
	cb.lastStateChange.Store(time.Now())
	cb.circuitOpens.Inc()

	if cb.config.Logger != nil {
		cb.config.Logger.Warn("circuit breaker manually forced open",
			"name", cb.config.Name)
	}
}

// ForceClose manually closes the circuit (override protection).
func (cb *CircuitBreaker) ForceClose() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.forceOpen.Store(false)
	cb.forceClosed.Store(true)
	cb.state.Store(string(CircuitClosed))
	cb.lastStateChange.Store(time.Now())

	if cb.config.Logger != nil {
		cb.config.Logger.Info("circuit breaker manually forced closed",
			"name", cb.config.Name)
	}
}

// IsHealthy returns true if circuit is closed or half-open.
func (cb *CircuitBreaker) IsHealthy() bool {
	state := cb.GetState()
	return state == CircuitClosed || state == CircuitHalfOpen
}

// GetFailureRate returns the current failure rate (last interval).
func (cb *CircuitBreaker) GetFailureRate() float64 {
	total := cb.totalRequests.Load()
	if total == 0 {
		return 0.0
	}
	fails := cb.failRequests.Load()
	return float64(fails) / float64(total)
}

// CircuitClient wraps an LLM client with circuit breaker protection.
type CircuitClient struct {
	client  LLMClient
	breaker *CircuitBreaker
}

// NewCircuitClient creates a new circuit breaker protected client wrapper.
func NewCircuitClient(client LLMClient, breaker *CircuitBreaker) *CircuitClient {
	return &CircuitClient{
		client:  client,
		breaker: breaker,
	}
}

// Chat implements the Chat method with circuit breaker protection.
func (c *CircuitClient) Chat(ctx context.Context, prompt string) (string, error) {
	var result string
	err := c.breaker.Execute(ctx, func() error {
		var err error
		result, err = c.client.Chat(ctx, prompt)
		return err
	})
	return result, err
}

func (c *CircuitClient) ChatWithOptions(ctx context.Context, prompt string, opts ChatOptions) (string, error) {
	var result string
	err := c.breaker.Execute(ctx, func() error {
		var err error
		result, err = c.client.ChatWithOptions(ctx, prompt, opts)
		return err
	})
	return result, err
}

// Analyze implements the Analyze method with circuit breaker protection.
func (c *CircuitClient) Analyze(ctx context.Context, prompt string, target any) error {
	return c.breaker.Execute(ctx, func() error {
		return c.client.Analyze(ctx, prompt, target)
	})
}

// ChatStream implements the ChatStream method with circuit breaker protection.
func (c *CircuitClient) ChatStream(ctx context.Context, prompt string) (<-chan string, error) {
	var result <-chan string
	err := c.breaker.Execute(ctx, func() error {
		var err error
		result, err = c.client.ChatStream(ctx, prompt)
		return err
	})
	return result, err
}

// HealthCheck implements the HealthCheck method.
func (c *CircuitClient) HealthCheck(ctx context.Context) HealthStatus {
	return c.client.HealthCheck(ctx)
}

// Client returns the underlying client for component extraction.
func (c *CircuitClient) Client() LLMClient {
	return c.client
}

// GetCircuitBreaker returns the underlying circuit breaker for monitoring.
func (c *CircuitClient) GetCircuitBreaker() *CircuitBreaker {
	return c.breaker
}
