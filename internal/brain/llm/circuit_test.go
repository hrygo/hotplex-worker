package llm

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCircuitBreaker_StateTransitions(t *testing.T) {
	t.Parallel()
	config := DefaultCircuitBreakerConfig()
	config.MaxFailures = 3
	config.Timeout = 50 * time.Millisecond
	config.HalfOpenMaxRequests = 1
	config.SuccessThreshold = 1
	cb := NewCircuitBreaker(config)

	// Initial state should be closed
	assert.Equal(t, CircuitClosed, cb.GetState())

	// Fail 3 times to open circuit
	for i := 0; i < 3; i++ {
		err := cb.Execute(context.Background(), func() error {
			return errors.New("test error")
		})
		assert.Error(t, err)
	}

	// Circuit should be open
	assert.Equal(t, CircuitOpen, cb.GetState())

	// Wait for timeout
	time.Sleep(100 * time.Millisecond)

	// Execute in half-open state - should succeed and close
	err := cb.Execute(context.Background(), func() error {
		return nil
	})
	assert.NoError(t, err)

	// Circuit should be closed now
	assert.Equal(t, CircuitClosed, cb.GetState())
}

func TestCircuitBreaker_ManualReset(t *testing.T) {
	t.Parallel()
	config := DefaultCircuitBreakerConfig()
	config.MaxFailures = 1
	cb := NewCircuitBreaker(config)

	// Fail once to open circuit
	err := cb.Execute(context.Background(), func() error {
		return errors.New("test error")
	})
	assert.Error(t, err)
	assert.Equal(t, CircuitOpen, cb.GetState())

	// Manual reset
	cb.Reset()
	assert.Equal(t, CircuitClosed, cb.GetState())
}

func TestCircuitBreaker_ForceOpen(t *testing.T) {
	t.Parallel()
	config := DefaultCircuitBreakerConfig()
	cb := NewCircuitBreaker(config)

	// Force open
	cb.ForceOpen()
	assert.Equal(t, CircuitOpen, cb.GetState())

	// Execute should fail even without actual errors
	err := cb.Execute(context.Background(), func() error {
		return nil
	})
	assert.Error(t, err)

	// Reset and verify
	cb.Reset()
	assert.Equal(t, CircuitClosed, cb.GetState())
}

func TestCircuitBreaker_ForceClose(t *testing.T) {
	t.Parallel()
	config := DefaultCircuitBreakerConfig()
	config.MaxFailures = 1
	cb := NewCircuitBreaker(config)

	// Fail to open circuit
	err := cb.Execute(context.Background(), func() error {
		return errors.New("test error")
	})
	assert.Error(t, err)
	assert.Equal(t, CircuitOpen, cb.GetState())

	// Force close
	cb.ForceClose()
	assert.Equal(t, CircuitClosed, cb.GetState())

	// Execute should succeed
	err = cb.Execute(context.Background(), func() error {
		return nil
	})
	assert.NoError(t, err)
}

func TestCircuitBreaker_Stats(t *testing.T) {
	t.Parallel()
	config := DefaultCircuitBreakerConfig()
	cb := NewCircuitBreaker(config)

	// Execute some requests
	for i := 0; i < 5; i++ {
		_ = cb.Execute(context.Background(), func() error {
			if i%2 == 0 {
				return errors.New("error")
			}
			return nil
		})
	}

	stats := cb.GetStats()
	assert.Equal(t, uint64(5), stats.TotalRequests)
	assert.Equal(t, uint64(3), stats.FailRequests)    // 0, 2, 4 failed
	assert.Equal(t, uint64(2), stats.SuccessRequests) // 1, 3 succeeded
}

func TestCircuitBreaker_Concurrent(t *testing.T) {
	t.Parallel()
	config := DefaultCircuitBreakerConfig()
	config.MaxFailures = 100
	cb := NewCircuitBreaker(config)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_ = cb.Execute(context.Background(), func() error {
					return nil
				})
			}
		}()
	}

	wg.Wait()
	stats := cb.GetStats()
	assert.Equal(t, uint64(100), stats.TotalRequests)
}
