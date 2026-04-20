package slack

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReconnectBackoff_Next_ExponentialGrowth(t *testing.T) {
	baseDelay := 1 * time.Second
	maxDelay := 60 * time.Second
	b := newReconnectBackoff(baseDelay, maxDelay)

	// First attempt: should be approximately 1s (0.5s to 1s due to jitter)
	d1 := b.Next()
	require.GreaterOrEqual(t, d1, 500*time.Millisecond)
	require.Less(t, d1, 1*time.Second+1)

	// Second attempt: should be approximately 2s (1s to 2s due to jitter)
	d2 := b.Next()
	require.GreaterOrEqual(t, d2, 1*time.Second)
	require.Less(t, d2, 2*time.Second+1)

	// Third attempt: should be approximately 4s (2s to 4s due to jitter)
	d3 := b.Next()
	require.GreaterOrEqual(t, d3, 2*time.Second)
	require.Less(t, d3, 4*time.Second+1)

	// Fourth attempt: should be approximately 8s (4s to 8s due to jitter)
	d4 := b.Next()
	require.GreaterOrEqual(t, d4, 4*time.Second)
	require.Less(t, d4, 8*time.Second+1)
}

func TestReconnectBackoff_Next_MaxDelayCap(t *testing.T) {
	baseDelay := 10 * time.Second
	maxDelay := 30 * time.Second
	b := newReconnectBackoff(baseDelay, maxDelay)

	// After enough attempts, should cap at maxDelay
	var lastDelay time.Duration
	for i := 0; i < 10; i++ {
		lastDelay = b.Next()
	}

	// Should be capped at maxDelay/2 to maxDelay (due to jitter applied after capping)
	require.LessOrEqual(t, lastDelay, maxDelay)
	require.GreaterOrEqual(t, lastDelay, maxDelay/2)
}

func TestReconnectBackoff_Next_JitterProducesDifferentValues(t *testing.T) {
	baseDelay := 1 * time.Second
	maxDelay := 60 * time.Second

	// Collect multiple samples from fresh backoff instances
	samples := make([]time.Duration, 20)
	for i := range samples {
		b := newReconnectBackoff(baseDelay, maxDelay)
		samples[i] = b.Next()
	}

	// Check that not all samples are identical (jitter should produce variation)
	allSame := true
	first := samples[0]
	for _, s := range samples[1:] {
		if s != first {
			allSame = false
			break
		}
	}
	require.False(t, allSame, "jitter should produce different values across samples")
}

func TestReconnectBackoff_Reset(t *testing.T) {
	baseDelay := 1 * time.Second
	maxDelay := 60 * time.Second
	b := newReconnectBackoff(baseDelay, maxDelay)

	// Advance several attempts
	for i := 0; i < 5; i++ {
		b.Next()
	}

	// Reset
	b.Reset()

	// After reset, should be back to approximately baseDelay
	d := b.Next()
	require.GreaterOrEqual(t, d, 500*time.Millisecond)
	require.Less(t, d, 1*time.Second+1)
}

func TestReconnectBackoff_Next_JitterRange(t *testing.T) {
	baseDelay := 2 * time.Second
	maxDelay := 60 * time.Second
	b := newReconnectBackoff(baseDelay, maxDelay)

	// First attempt: baseDelay * 2^0 = 2s
	// After jitter: delay should be in [1s, 2s)
	d := b.Next()
	require.GreaterOrEqual(t, d, 1*time.Second)
	require.Less(t, d, 2*time.Second)
}

func TestReconnectBackoff_ConcurrentAccess(t *testing.T) {
	baseDelay := 1 * time.Second
	maxDelay := 60 * time.Second
	b := newReconnectBackoff(baseDelay, maxDelay)

	// Run concurrent calls to Next() and Reset()
	done := make(chan bool, 4)

	go func() {
		for i := 0; i < 100; i++ {
			_ = b.Next()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			_ = b.Next()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 50; i++ {
			b.Reset()
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 100; i++ {
			_ = b.Next()
		}
		done <- true
	}()

	// Wait for all goroutines to complete
	for i := 0; i < 4; i++ {
		<-done
	}

	// If we got here without panic, concurrent access is safe
	require.True(t, true)
}

func TestReconnectBackoff_ZeroBaseDelay(t *testing.T) {
	baseDelay := time.Duration(0)
	maxDelay := 60 * time.Second
	b := newReconnectBackoff(baseDelay, maxDelay)

	// With zero base delay, should immediately use maxDelay
	d := b.Next()
	require.GreaterOrEqual(t, d, maxDelay/2)
	require.LessOrEqual(t, d, maxDelay)
}
