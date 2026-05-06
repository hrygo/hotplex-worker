package llm

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"go.uber.org/atomic"
)

// ProviderConfig represents configuration for an LLM provider.
type ProviderConfig struct {
	// Name is the provider identifier (e.g., "openai", "dashscope", "anthropic").
	Name string
	// APIKey is the API key for this provider.
	APIKey string
	// Endpoint is the optional custom endpoint.
	Endpoint string
	// Models is the list of models available from this provider.
	Models []string
	// Priority is the failover priority (1 = primary, higher = backup).
	Priority int
	// Enabled indicates if this provider is available.
	Enabled bool
	// Timeout is the request timeout for this provider.
	Timeout time.Duration
	// MaxRetries is the max retry attempts for this provider.
	MaxRetries int
}

// FailoverRecord represents a failover event record.
type FailoverRecord struct {
	Timestamp    time.Time
	FromProvider string
	ToProvider   string
	Reason       string
	Duration     time.Duration
	Success      bool
}

// FailoverConfig holds configuration for multi-provider failover.
type FailoverConfig struct {
	// Providers is the list of configured providers.
	Providers []ProviderConfig
	// EnableAutoFailover enables automatic failover on errors.
	EnableAutoFailover bool
	// EnableFailback enables automatic failback to primary.
	EnableFailback bool
	// FailbackCooldown is the wait time before attempting failback.
	FailbackCooldown time.Duration
	// HealthCheckInterval is how often to check provider health.
	HealthCheckInterval time.Duration
	// MaxFailoverAttempts is max consecutive failovers before giving up.
	MaxFailoverAttempts int
	// Logger for failover events.
	Logger *slog.Logger
	// HistorySize is the number of failover records to keep.
	HistorySize int
}

// DefaultFailoverConfig returns sensible defaults.
func DefaultFailoverConfig() FailoverConfig {
	return FailoverConfig{
		EnableAutoFailover:  true,
		EnableFailback:      true,
		FailbackCooldown:    5 * time.Minute,
		HealthCheckInterval: 30 * time.Second,
		MaxFailoverAttempts: 3,
		HistorySize:         100,
		Logger:              nil, // Set by caller if needed
	}
}

// FailoverHistory maintains a circular buffer of failover records.
type FailoverHistory struct {
	records []FailoverRecord
	index   int
	size    int
	mu      sync.RWMutex
}

// NewFailoverHistory creates a new failover history buffer.
func NewFailoverHistory(size int) *FailoverHistory {
	return &FailoverHistory{
		records: make([]FailoverRecord, 0, size),
		size:    size,
	}
}

// Add adds a failover record.
func (h *FailoverHistory) Add(record FailoverRecord) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.records) < h.size {
		h.records = append(h.records, record)
	} else {
		h.records[h.index] = record
		h.index = (h.index + 1) % h.size
	}
}

// GetRecent returns the most recent failover records.
func (h *FailoverHistory) GetRecent(count int) []FailoverRecord {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if count > len(h.records) {
		count = len(h.records)
	}

	result := make([]FailoverRecord, count)
	start := len(h.records) - count
	copy(result, h.records[start:])
	return result
}

// FailoverManager manages multi-provider failover logic.
type FailoverManager struct {
	config          FailoverConfig
	providers       map[string]*ProviderConfig
	providerOrder   []*ProviderConfig
	currentProvider *ProviderConfig
	circuitBreakers map[string]*CircuitBreaker

	// State
	isFailoverActive *atomic.Bool
	failoverCount    *atomic.Int32
	lastFailoverTime *atomic.Time
	currentIndex     *atomic.Int32

	// Health monitoring
	healthStatus map[string]bool
	healthMu     sync.RWMutex

	// History
	history *FailoverHistory

	// Lifecycle
	stopCh chan struct{}

	mu sync.RWMutex
}

// FailoverStats holds failover statistics.
type FailoverStats struct {
	IsActive           bool
	CurrentProvider    string
	FailoverCount      int32
	LastFailoverTime   time.Time
	HealthyProviders   []string
	UnhealthyProviders []string
	RecentFailovers    []FailoverRecord
}

// NewFailoverManager creates a new failover manager.
func NewFailoverManager(config FailoverConfig) *FailoverManager {
	fm := &FailoverManager{
		config:           config,
		providers:        make(map[string]*ProviderConfig),
		circuitBreakers:  make(map[string]*CircuitBreaker),
		isFailoverActive: atomic.NewBool(false),
		failoverCount:    atomic.NewInt32(0),
		lastFailoverTime: atomic.NewTime(time.Time{}),
		currentIndex:     atomic.NewInt32(0),
		healthStatus:     make(map[string]bool),
		history:          NewFailoverHistory(config.HistorySize),
	}

	// Initialize providers
	for i := range config.Providers {
		provider := &config.Providers[i]
		if provider.Enabled {
			fm.providers[provider.Name] = provider

			// Create circuit breaker for each provider
			cbConfig := DefaultCircuitBreakerConfig()
			cbConfig.Name = fmt.Sprintf("failover-%s", provider.Name)
			cbConfig.Logger = config.Logger
			fm.circuitBreakers[provider.Name] = NewCircuitBreaker(cbConfig)
		}
	}

	// Sort providers by priority
	fm.sortProvidersByPriority()

	// Set initial provider
	if len(fm.providerOrder) > 0 {
		fm.currentProvider = fm.providerOrder[0]
	}

	// Start health check if configured
	if config.HealthCheckInterval > 0 {
		fm.stopCh = make(chan struct{})
		go fm.startHealthCheck()
	}

	return fm
}

// sortProvidersByPriority sorts providers by priority (ascending).
func (fm *FailoverManager) sortProvidersByPriority() {
	fm.providerOrder = make([]*ProviderConfig, 0, len(fm.providers))
	for _, p := range fm.providers {
		fm.providerOrder = append(fm.providerOrder, p)
	}

	// Use slices.SortFunc for O(n log n) instead of bubble sort O(n²)
	slices.SortFunc(fm.providerOrder, func(a, b *ProviderConfig) int {
		return a.Priority - b.Priority
	})
}

// startHealthCheck periodically checks provider health.
func (fm *FailoverManager) startHealthCheck() {
	ticker := time.NewTicker(fm.config.HealthCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fm.checkProviderHealth()
		case <-fm.stopCh:
			return
		}
	}
}

// checkProviderHealth checks health of all providers.
func (fm *FailoverManager) checkProviderHealth() {
	fm.healthMu.Lock()
	defer fm.healthMu.Unlock()

	for name, cb := range fm.circuitBreakers {
		fm.healthStatus[name] = cb.IsHealthy()
	}
}

// ExecuteWithFailover executes a function with automatic failover.
func (fm *FailoverManager) ExecuteWithFailover(ctx context.Context, fn func(provider *ProviderConfig) error) error {
	if fm.currentProvider == nil {
		return fmt.Errorf("no providers configured")
	}

	attempts := 0
	var startProvider *ProviderConfig

	// Get initial provider
	fm.mu.RLock()
	startProvider = fm.currentProvider
	fm.mu.RUnlock()

	for attempts < fm.config.MaxFailoverAttempts {
		// Get current provider
		fm.mu.RLock()
		provider := fm.currentProvider
		fm.mu.RUnlock()

		if provider == nil {
			return fmt.Errorf("no provider available")
		}

		// Check if provider is healthy
		if !fm.isProviderHealthy(provider.Name) {
			if fm.config.Logger != nil {
				fm.config.Logger.Warn("provider unhealthy, attempting failover",
					"provider", provider.Name)
			}
			if !fm.failoverToNextProvider(provider.Name) {
				return fmt.Errorf("failover failed: no healthy providers available")
			}
			attempts++
			continue
		}

		// Execute with circuit breaker
		cb := fm.circuitBreakers[provider.Name]
		err := cb.Execute(ctx, func() error {
			return fn(provider)
		})

		if err == nil {
			// Success - check if we should failback to primary
			if fm.config.EnableFailback && provider != startProvider {
				fm.tryFailback(startProvider)
			}
			return nil
		}

		// Error occurred - check if we should failover
		if fm.config.EnableAutoFailover {
			if fm.config.Logger != nil {
				fm.config.Logger.Warn("provider error, attempting failover",
					"provider", provider.Name,
					"error", err)
			}

			if fm.failoverToNextProvider(provider.Name) {
				attempts++
				continue
			}
		}

		return err
	}

	return fmt.Errorf("max failover attempts reached")
}

// failoverToNextProvider attempts to switch to the next healthy provider.
func (fm *FailoverManager) failoverToNextProvider(fromProvider string) bool {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	currentIdx := int(fm.currentIndex.Load())

	for i := 1; i < len(fm.providerOrder); i++ {
		nextIdx := (currentIdx + i) % len(fm.providerOrder)
		nextProvider := fm.providerOrder[nextIdx]

		if nextProvider.Name == fromProvider {
			continue
		}

		if fm.isProviderHealthy(nextProvider.Name) {
			oldProvider := fm.currentProvider
			fm.currentProvider = nextProvider
			fm.currentIndex.Store(int32(nextIdx))
			fm.isFailoverActive.Store(true)
			fm.failoverCount.Inc()
			fm.lastFailoverTime.Store(time.Now())

			// Record failover
			fm.history.Add(FailoverRecord{
				Timestamp:    time.Now(),
				FromProvider: oldProvider.Name,
				ToProvider:   nextProvider.Name,
				Reason:       "error or unhealthy",
				Success:      true,
			})

			if fm.config.Logger != nil {
				fm.config.Logger.Info("failover successful",
					"from", oldProvider.Name,
					"to", nextProvider.Name)
			}
			return true
		}
	}

	return false
}

// tryFailback attempts to failback to the primary provider.
func (fm *FailoverManager) tryFailback(primary *ProviderConfig) {
	if primary == nil || fm.currentProvider == primary {
		return
	}

	// Check cooldown
	if time.Since(fm.lastFailoverTime.Load()) < fm.config.FailbackCooldown {
		return
	}

	if fm.isProviderHealthy(primary.Name) {
		fm.mu.Lock()
		oldProvider := fm.currentProvider
		fm.currentProvider = primary
		fm.currentIndex.Store(0)
		fm.isFailoverActive.Store(false)
		fm.mu.Unlock()

		// Record failback
		fm.history.Add(FailoverRecord{
			Timestamp:    time.Now(),
			FromProvider: oldProvider.Name,
			ToProvider:   primary.Name,
			Reason:       "primary recovered",
			Success:      true,
		})

		if fm.config.Logger != nil {
			fm.config.Logger.Info("failback to primary successful",
				"from", oldProvider.Name,
				"to", primary.Name)
		}
	}
}

// isProviderHealthy checks if a provider is healthy.
func (fm *FailoverManager) isProviderHealthy(name string) bool {
	fm.healthMu.RLock()
	defer fm.healthMu.RUnlock()

	healthy, ok := fm.healthStatus[name]
	if !ok {
		// Unknown - assume healthy
		return true
	}
	return healthy
}

// GetCurrentProvider returns the current active provider.
func (fm *FailoverManager) GetCurrentProvider() *ProviderConfig {
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	return fm.currentProvider
}

// GetStats returns failover statistics.
func (fm *FailoverManager) GetStats() FailoverStats {
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	fm.healthMu.RLock()
	var healthy, unhealthy []string
	for name, isHealthy := range fm.healthStatus {
		if isHealthy {
			healthy = append(healthy, name)
		} else {
			unhealthy = append(unhealthy, name)
		}
	}
	fm.healthMu.RUnlock()

	currentProvider := ""
	if fm.currentProvider != nil {
		currentProvider = fm.currentProvider.Name
	}

	return FailoverStats{
		IsActive:           fm.isFailoverActive.Load(),
		CurrentProvider:    currentProvider,
		FailoverCount:      fm.failoverCount.Load(),
		LastFailoverTime:   fm.lastFailoverTime.Load(),
		HealthyProviders:   healthy,
		UnhealthyProviders: unhealthy,
		RecentFailovers:    fm.history.GetRecent(10),
	}
}

// Reset resets failover state to initial configuration.
func (fm *FailoverManager) Reset() {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	if len(fm.providerOrder) > 0 {
		fm.currentProvider = fm.providerOrder[0]
		fm.currentIndex.Store(0)
	}
	fm.isFailoverActive.Store(false)
	fm.failoverCount.Store(0)
	fm.lastFailoverTime.Store(time.Time{})

	// Reset all circuit breakers
	for _, cb := range fm.circuitBreakers {
		cb.Reset()
	}

	if fm.config.Logger != nil {
		fm.config.Logger.Info("failover manager reset")
	}
}

// ManualFailover manually switches to a specific provider.
func (fm *FailoverManager) ManualFailover(providerName string) error {
	fm.mu.Lock()
	defer fm.mu.Unlock()

	provider, ok := fm.providers[providerName]
	if !ok {
		return fmt.Errorf("provider not found: %s", providerName)
	}

	oldProvider := fm.currentProvider
	fm.currentProvider = provider
	fm.isFailoverActive.Store(true)
	fm.failoverCount.Inc()
	fm.lastFailoverTime.Store(time.Now())

	// Record manual failover
	fm.history.Add(FailoverRecord{
		Timestamp:    time.Now(),
		FromProvider: oldProvider.Name,
		ToProvider:   providerName,
		Reason:       "manual",
		Success:      true,
	})

	if fm.config.Logger != nil {
		fm.config.Logger.Info("manual failover",
			"from", oldProvider.Name,
			"to", providerName)
	}

	return nil
}

// SetCurrentProvider sets the current provider (for testing).
func (fm *FailoverManager) SetCurrentProvider(name string) {
	fm.mu.Lock()
	defer fm.mu.Unlock()
	for _, p := range fm.providers {
		if p.Name == name {
			fm.currentProvider = p
			break
		}
	}
}

// SetLastFailoverTime sets the last failover time (for testing).
func (fm *FailoverManager) SetLastFailoverTime(t time.Time) {
	fm.lastFailoverTime.Store(t)
}

// Close stops the health check goroutine and cleans up resources.
// Should be called when the FailoverManager is no longer needed.
func (fm *FailoverManager) Close() {
	if fm.stopCh != nil {
		close(fm.stopCh)
	}
}
