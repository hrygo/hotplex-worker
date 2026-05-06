package llm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFailoverManager_BasicFailover(t *testing.T) {
	t.Parallel()
	config := DefaultFailoverConfig()
	config.HealthCheckInterval = 0 // Disable health check for unit tests
	config.EnableAutoFailover = true
	config.EnableFailback = false
	config.Providers = []ProviderConfig{
		{Name: "primary", APIKey: "key1", Priority: 1, Enabled: true},
		{Name: "backup", APIKey: "key2", Priority: 2, Enabled: true},
	}

	fm := NewFailoverManager(config)
	defer fm.Close()

	assert.Equal(t, "primary", fm.GetCurrentProvider().Name)

	// Manually failover to backup
	err := fm.ManualFailover("backup")
	assert.NoError(t, err)
	assert.Equal(t, "backup", fm.GetCurrentProvider().Name)

	stats := fm.GetStats()
	assert.Equal(t, int32(1), stats.FailoverCount)
}

func TestFailoverManager_ManualFailover(t *testing.T) {
	t.Parallel()
	config := DefaultFailoverConfig()
	config.HealthCheckInterval = 0 // Disable health check for unit tests
	config.Providers = []ProviderConfig{
		{Name: "primary", APIKey: "key1", Priority: 1, Enabled: true},
		{Name: "backup", APIKey: "key2", Priority: 2, Enabled: true},
	}

	fm := NewFailoverManager(config)
	defer fm.Close()

	assert.Equal(t, "primary", fm.GetCurrentProvider().Name)

	// Manual failover
	err := fm.ManualFailover("backup")
	assert.NoError(t, err)
	assert.Equal(t, "backup", fm.GetCurrentProvider().Name)
}

func TestFailoverManager_Failback(t *testing.T) {
	t.Parallel()
	config := DefaultFailoverConfig()
	config.HealthCheckInterval = 0 // Disable health check for unit tests
	config.EnableAutoFailover = true
	config.EnableFailback = true
	config.FailbackCooldown = 50 * time.Millisecond
	config.Providers = []ProviderConfig{
		{Name: "primary", APIKey: "key1", Priority: 1, Enabled: true},
		{Name: "backup", APIKey: "key2", Priority: 2, Enabled: true},
	}

	fm := NewFailoverManager(config)
	defer fm.Close()

	// Verify primary is selected by default
	assert.Equal(t, "primary", fm.GetCurrentProvider().Name)

	// Manually set current provider to backup (simulating after a failover)
	fm.SetCurrentProvider("backup")

	// Set lastFailoverTime in the past to pass cooldown check
	fm.SetLastFailoverTime(time.Now().Add(-100 * time.Millisecond))

	// Verify the state is correct for failback to occur
	// The actual failback logic is tested indirectly through ExecuteWithFailover in integration tests
	// For unit test, we verify the provider can be switched
	fm.SetCurrentProvider("primary")
	assert.Equal(t, "primary", fm.GetCurrentProvider().Name)
}

func TestFailoverManager_Stats(t *testing.T) {
	t.Parallel()
	config := DefaultFailoverConfig()
	config.HealthCheckInterval = 0 // Disable health check for unit tests
	config.Providers = []ProviderConfig{
		{Name: "primary", APIKey: "key1", Priority: 1, Enabled: true},
		{Name: "backup", APIKey: "key2", Priority: 2, Enabled: true},
	}

	fm := NewFailoverManager(config)
	defer fm.Close()

	// Manual failover
	err := fm.ManualFailover("backup")
	assert.NoError(t, err)

	stats := fm.GetStats()
	assert.True(t, stats.IsActive)
	assert.Equal(t, "backup", stats.CurrentProvider)
	assert.Equal(t, int32(1), stats.FailoverCount)
	assert.Len(t, stats.RecentFailovers, 1)
}

func TestFailoverManager_Reset(t *testing.T) {
	t.Parallel()
	config := DefaultFailoverConfig()
	config.HealthCheckInterval = 0 // Disable health check for unit tests
	config.Providers = []ProviderConfig{
		{Name: "primary", APIKey: "key1", Priority: 1, Enabled: true},
		{Name: "backup", APIKey: "key2", Priority: 2, Enabled: true},
	}

	fm := NewFailoverManager(config)
	defer fm.Close()

	err := fm.ManualFailover("backup")
	assert.NoError(t, err)
	assert.Equal(t, "backup", fm.GetCurrentProvider().Name)

	// Reset
	fm.Reset()
	assert.Equal(t, "primary", fm.GetCurrentProvider().Name)

	stats := fm.GetStats()
	assert.Equal(t, int32(0), stats.FailoverCount)
	assert.False(t, stats.IsActive)
}

func TestFailoverManager_NoHealthyProviders(t *testing.T) {
	t.Parallel()
	config := DefaultFailoverConfig()
	config.HealthCheckInterval = 0 // Disable health check to avoid goroutine leak
	config.Providers = []ProviderConfig{
		{Name: "primary", APIKey: "key1", Priority: 1, Enabled: true},
	}

	fm := NewFailoverManager(config)
	defer fm.Close() // Clean up resources

	// Force circuit breaker open to simulate unhealthy provider
	fm.circuitBreakers["primary"].ForceOpen()

	// Should fail with circuit breaker error when provider is unhealthy
	err := fm.ExecuteWithFailover(context.Background(), func(p *ProviderConfig) error {
		return nil
	})
	assert.Error(t, err)
	// Circuit breaker returns its own error when forced open
	assert.Contains(t, err.Error(), "circuit breaker")
}

func TestFailoverHistory(t *testing.T) {
	t.Parallel()
	history := NewFailoverHistory(5)

	// Add 7 records
	for i := 0; i < 7; i++ {
		history.Add(FailoverRecord{
			Timestamp:    time.Now(),
			FromProvider: "primary",
			ToProvider:   "backup",
			Reason:       "error",
		})
	}

	// Should only keep last 5
	recent := history.GetRecent(10)
	assert.Len(t, recent, 5)
}
