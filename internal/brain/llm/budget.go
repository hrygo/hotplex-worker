package llm

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"go.uber.org/atomic"
)

// BudgetPeriod defines the budget period type.
type BudgetPeriod string

const (
	// BudgetDaily - Daily budget reset.
	BudgetDaily BudgetPeriod = "daily"
	// BudgetWeekly - Weekly budget reset (Monday).
	BudgetWeekly BudgetPeriod = "weekly"
	// BudgetMonthly - Monthly budget reset (1st).
	BudgetMonthly BudgetPeriod = "monthly"
	// BudgetSession - Session-based budget (no auto-reset).
	BudgetSession BudgetPeriod = "session"
)

// BudgetAlertThreshold defines alert thresholds.
type BudgetAlertThreshold struct {
	// Percentage is the budget percentage (e.g., 80 for 80%).
	Percentage float64
	// Message is the alert message template.
	Message string
}

// BudgetConfig holds configuration for budget control.
type BudgetConfig struct {
	// Period is the budget period (daily/weekly/monthly/session).
	Period BudgetPeriod
	// Limit is the budget limit in USD.
	Limit float64
	// AlertThresholds is the list of alert thresholds.
	AlertThresholds []BudgetAlertThreshold
	// EnableHardLimit enables hard limit (reject requests over budget).
	EnableHardLimit bool
	// EnableSoftLimit enables soft limit (warn but allow).
	EnableSoftLimit bool
	// Logger for budget events.
	Logger *slog.Logger
}

// DefaultBudgetConfig returns sensible defaults.
func DefaultBudgetConfig() BudgetConfig {
	return BudgetConfig{
		Period:          BudgetDaily,
		Limit:           10.0, // $10 daily
		EnableHardLimit: false,
		EnableSoftLimit: true,
		AlertThresholds: []BudgetAlertThreshold{
			{Percentage: 80.0, Message: "Budget 80% consumed"},
			{Percentage: 90.0, Message: "Budget 90% consumed"},
		},
	}
}

// BudgetAlert represents a budget alert event.
type BudgetAlert struct {
	Timestamp   time.Time
	Period      BudgetPeriod
	CurrentCost float64
	Limit       float64
	Percentage  float64
	Message     string
	IsHardLimit bool
}

// BudgetTracker tracks budget usage and enforces limits.
type BudgetTracker struct {
	config          BudgetConfig
	sessionID       string
	currentCost     *atomic.Float64
	requestCount    *atomic.Int64
	periodStart     *atomic.Time
	periodEndCached *atomic.Time
	lastAlert       *atomic.Time
	alertsTriggered map[float64]bool

	// Callback for alerts
	alertCallback func(alert BudgetAlert)

	mu sync.RWMutex
}

// BudgetStats holds budget statistics.
type BudgetStats struct {
	CurrentCost     float64
	Limit           float64
	Remaining       float64
	PercentageUsed  float64
	RequestCount    int64
	PeriodStart     time.Time
	PeriodEnd       time.Time
	IsExceeded      bool
	AlertsTriggered []float64
}

// NewBudgetTracker creates a new budget tracker.
func NewBudgetTracker(config BudgetConfig, sessionID string) *BudgetTracker {
	bt := &BudgetTracker{
		config:          config,
		sessionID:       sessionID,
		currentCost:     atomic.NewFloat64(0),
		requestCount:    atomic.NewInt64(0),
		periodStart:     atomic.NewTime(time.Now()),
		periodEndCached: atomic.NewTime(time.Time{}),
		lastAlert:       atomic.NewTime(time.Time{}),
		alertsTriggered: make(map[float64]bool),
	}

	// Set period start based on period type
	bt.resetPeriodStart()

	return bt
}

// resetPeriodStart sets the period start time based on period type.
func (bt *BudgetTracker) resetPeriodStart() {
	now := time.Now()
	var start time.Time

	switch bt.config.Period {
	case BudgetDaily:
		start = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	case BudgetWeekly:
		// Start from Monday
		daysSinceMonday := int(now.Weekday())
		if daysSinceMonday == 0 {
			daysSinceMonday = 7
		}
		start = time.Date(now.Year(), now.Month(), now.Day()-daysSinceMonday+1, 0, 0, 0, 0, now.Location())
	case BudgetMonthly:
		start = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	case BudgetSession:
		start = now
	}

	bt.periodStart.Store(start)
}

// CheckBudget checks if a request is within budget.
// Returns (allowed, cost, error).
func (bt *BudgetTracker) CheckBudget(estimatedCost float64) (bool, float64, error) {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	currentCost := bt.currentCost.Load()
	newCost := currentCost + estimatedCost

	// Check if over budget
	if newCost > bt.config.Limit {
		if bt.config.EnableHardLimit {
			return false, 0, fmt.Errorf("budget exceeded: $%.4f > $%.4f limit", newCost, bt.config.Limit)
		}

		if bt.config.EnableSoftLimit {
			// Log warning but allow
			if bt.config.Logger != nil {
				bt.config.Logger.Warn("budget exceeded (soft limit)",
					"current", currentCost,
					"estimated", estimatedCost,
					"limit", bt.config.Limit)
			}
		}
	}

	return true, estimatedCost, nil
}

// TrackRequest tracks a request's cost against the budget.
func (bt *BudgetTracker) TrackRequest(cost float64) error {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	// Check period reset
	bt.checkPeriodReset()

	// Update cost
	bt.currentCost.Add(cost)
	bt.requestCount.Inc()

	// Check alerts
	bt.checkAlerts()

	return nil
}

// checkPeriodReset checks if the budget period has reset.
func (bt *BudgetTracker) checkPeriodReset() {
	now := time.Now()
	periodStart := bt.periodStart.Load()

	shouldReset := false
	switch bt.config.Period {
	case BudgetDaily:
		if now.Day() != periodStart.Day() || now.Month() != periodStart.Month() {
			shouldReset = true
		}
	case BudgetWeekly:
		// Check if we've passed a Monday
		if now.Weekday() == time.Monday && now.Hour() == 0 {
			shouldReset = true
		}
	case BudgetMonthly:
		if now.Month() != periodStart.Month() {
			shouldReset = true
		}
	case BudgetSession:
		// No auto-reset for session
	}

	if shouldReset {
		bt.currentCost.Store(0)
		bt.requestCount.Store(0)
		bt.alertsTriggered = make(map[float64]bool)
		bt.resetPeriodStart()
		// Cache the period end
		bt.periodEndCached.Store(bt.calculatePeriodEnd())

		if bt.config.Logger != nil {
			bt.config.Logger.Info("budget period reset",
				"period", bt.config.Period,
				"session", bt.sessionID)
		}
	}
}

// checkAlerts checks and triggers budget alerts.
func (bt *BudgetTracker) checkAlerts() {
	currentCost := bt.currentCost.Load()
	percentage := (currentCost / bt.config.Limit) * 100

	for _, threshold := range bt.config.AlertThresholds {
		if percentage < threshold.Percentage || bt.alertsTriggered[threshold.Percentage] {
			continue
		}
		// Limit alertsTriggered map size to prevent memoryLeak
		const maxAlerts = 100
		if len(bt.alertsTriggered) >= maxAlerts {
			// Clear old alerts and start fresh
			bt.alertsTriggered = make(map[float64]bool)
		}

		bt.alertsTriggered[threshold.Percentage] = true
		bt.lastAlert.Store(time.Now())

		alert := BudgetAlert{
			Timestamp:   time.Now(),
			Period:      bt.config.Period,
			CurrentCost: currentCost,
			Limit:       bt.config.Limit,
			Percentage:  percentage,
			Message:     threshold.Message,
			IsHardLimit: bt.config.EnableHardLimit && currentCost > bt.config.Limit,
		}

		if bt.config.Logger != nil {
			bt.config.Logger.Warn("budget alert triggered",
				"percentage", percentage,
				"current", currentCost,
				"limit", bt.config.Limit,
				"message", threshold.Message)
		}

		if bt.alertCallback != nil {
			bt.alertCallback(alert)
		}
	}
}

// SetAlertCallback sets a callback function for budget alerts.
func (bt *BudgetTracker) SetAlertCallback(callback func(alert BudgetAlert)) {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	bt.alertCallback = callback
}

// GetStats returns budget statistics.
func (bt *BudgetTracker) GetStats() BudgetStats {
	bt.mu.RLock()
	defer bt.mu.RUnlock()

	currentCost := bt.currentCost.Load()
	percentage := (currentCost / bt.config.Limit) * 100
	remaining := bt.config.Limit - currentCost

	var alertsTriggered []float64
	for percentage := range bt.alertsTriggered {
		alertsTriggered = append(alertsTriggered, percentage)
	}

	return BudgetStats{
		CurrentCost:     currentCost,
		Limit:           bt.config.Limit,
		Remaining:       remaining,
		PercentageUsed:  percentage,
		RequestCount:    bt.requestCount.Load(),
		PeriodStart:     bt.periodStart.Load(),
		PeriodEnd:       bt.periodEndCached.Load(),
		IsExceeded:      currentCost > bt.config.Limit,
		AlertsTriggered: alertsTriggered,
	}
}

// calculatePeriodEnd calculates the period end time.
func (bt *BudgetTracker) calculatePeriodEnd() time.Time {
	start := bt.periodStart.Load()

	switch bt.config.Period {
	case BudgetDaily:
		return start.Add(24 * time.Hour)
	case BudgetWeekly:
		return start.Add(7 * 24 * time.Hour)
	case BudgetMonthly:
		return start.AddDate(0, 1, 0)
	case BudgetSession:
		return start.Add(24 * 365 * time.Hour) // Far future
	}

	return start
}

// Reset manually resets the budget tracker.
func (bt *BudgetTracker) Reset() {
	bt.mu.Lock()
	defer bt.mu.Unlock()

	bt.currentCost.Store(0)
	bt.requestCount.Store(0)
	bt.alertsTriggered = make(map[float64]bool)
	bt.periodStart.Store(time.Now())

	if bt.config.Logger != nil {
		bt.config.Logger.Info("budget tracker manually reset",
			"session", bt.sessionID)
	}
}

// SetLimit updates the budget limit.
func (bt *BudgetTracker) SetLimit(limit float64) {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	bt.config.Limit = limit
}

// GetRemaining returns the remaining budget.
func (bt *BudgetTracker) GetRemaining() float64 {
	return bt.config.Limit - bt.currentCost.Load()
}

// IsExceeded returns true if budget is exceeded.
func (bt *BudgetTracker) IsExceeded() bool {
	return bt.currentCost.Load() > bt.config.Limit
}

// CheckAlerts checks and triggers budget alerts (public for testing).
func (bt *BudgetTracker) CheckAlerts() {
	bt.mu.Lock()
	defer bt.mu.Unlock()
	bt.checkAlerts()
}

// BudgetManager manages multiple budget trackers (e.g., per user/session).
type BudgetManager struct {
	config     BudgetConfig
	trackers   map[string]*BudgetTracker
	globalCost *atomic.Float64
	mu         sync.RWMutex
}

// NewBudgetManager creates a new budget manager.
func NewBudgetManager(config BudgetConfig) *BudgetManager {
	return &BudgetManager{
		config:     config,
		trackers:   make(map[string]*BudgetTracker),
		globalCost: atomic.NewFloat64(0),
	}
}

// GetTracker gets or creates a budget tracker for a session.
func (bm *BudgetManager) GetTracker(sessionID string) *BudgetTracker {
	bm.mu.RLock()
	tracker, ok := bm.trackers[sessionID]
	bm.mu.RUnlock()

	if ok {
		return tracker
	}

	bm.mu.Lock()
	defer bm.mu.Unlock()

	// Double-check after acquiring write lock
	if tracker, ok := bm.trackers[sessionID]; ok {
		return tracker
	}

	tracker = NewBudgetTracker(bm.config, sessionID)
	bm.trackers[sessionID] = tracker
	return tracker
}

// TrackRequest tracks a request across all trackers.
func (bm *BudgetManager) TrackRequest(sessionID string, cost float64) error {
	tracker := bm.GetTracker(sessionID)
	bm.globalCost.Add(cost)
	return tracker.TrackRequest(cost)
}

// GetGlobalStats returns global budget statistics.
func (bm *BudgetManager) GetGlobalStats() BudgetStats {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	globalCost := bm.globalCost.Load()
	percentage := (globalCost / bm.config.Limit) * 100

	return BudgetStats{
		CurrentCost:    globalCost,
		Limit:          bm.config.Limit,
		Remaining:      bm.config.Limit - globalCost,
		PercentageUsed: percentage,
		RequestCount:   0, // Would need to aggregate
		IsExceeded:     globalCost > bm.config.Limit,
	}
}

// GetAllTrackers returns all budget trackers.
func (bm *BudgetManager) GetAllTrackers() map[string]*BudgetTracker {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	result := make(map[string]*BudgetTracker)
	for k, v := range bm.trackers {
		result[k] = v
	}
	return result
}

// RemoveTracker removes a budget tracker.
func (bm *BudgetManager) RemoveTracker(sessionID string) {
	bm.mu.Lock()
	defer bm.mu.Unlock()
	delete(bm.trackers, sessionID)
}

// ResetAll resets all budget trackers.
func (bm *BudgetManager) ResetAll() {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	for _, tracker := range bm.trackers {
		tracker.Reset()
	}
	bm.globalCost.Store(0)
}
