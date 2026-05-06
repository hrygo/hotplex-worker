package llm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBudgetTracker_BasicTracking(t *testing.T) {
	t.Parallel()
	config := DefaultBudgetConfig()
	config.Limit = 10.0
	config.Period = BudgetSession

	tracker := NewBudgetTracker(config, "test-session")

	// Track some requests
	_ = tracker.TrackRequest(2.0)
	_ = tracker.TrackRequest(3.0)
	_ = tracker.TrackRequest(1.5)

	stats := tracker.GetStats()
	assert.InDelta(t, 6.5, stats.CurrentCost, 0.01)
	assert.InDelta(t, 3.5, stats.Remaining, 0.01)
	assert.InDelta(t, 65.0, stats.PercentageUsed, 0.01)
	assert.Equal(t, int64(3), stats.RequestCount)
}

func TestBudgetTracker_BudgetExceeded(t *testing.T) {
	t.Parallel()
	config := DefaultBudgetConfig()
	config.Limit = 5.0
	config.EnableHardLimit = true
	config.Period = BudgetSession

	tracker := NewBudgetTracker(config, "test-session")

	// Track within budget
	err := tracker.TrackRequest(2.0)
	assert.NoError(t, err)
	err = tracker.TrackRequest(2.0)
	assert.NoError(t, err)

	// Try to exceed budget
	allowed, cost, err := tracker.CheckBudget(2.0)
	assert.False(t, allowed)
	assert.Zero(t, cost)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "budget exceeded")
}

func TestBudgetTracker_SoftLimit(t *testing.T) {
	t.Parallel()
	config := DefaultBudgetConfig()
	config.Limit = 5.0
	config.EnableHardLimit = false
	config.EnableSoftLimit = true
	config.Period = BudgetSession

	tracker := NewBudgetTracker(config, "test-session")

	// Track within budget
	err := tracker.TrackRequest(4.0)
	assert.NoError(t, err)

	// Try to exceed with soft limit - should allow
	allowed, cost, err := tracker.CheckBudget(2.0)
	assert.True(t, allowed)
	assert.InDelta(t, 2.0, cost, 0.01)
	assert.NoError(t, err)
}

func TestBudgetTracker_Alerts(t *testing.T) {
	t.Parallel()
	config := DefaultBudgetConfig()
	config.Limit = 10.0
	config.AlertThresholds = []BudgetAlertThreshold{
		{Percentage: 80.0, Message: "80% alert"},
		{Percentage: 90.0, Message: "90% alert"},
	}
	config.Period = BudgetSession

	alertTriggered := false
	var triggeredAlert BudgetAlert

	tracker := NewBudgetTracker(config, "test-session")
	tracker.SetAlertCallback(func(alert BudgetAlert) {
		alertTriggered = true
		triggeredAlert = alert
	})

	// Track to 80%
	var err error
	err = tracker.TrackRequest(8.0)
	assert.NoError(t, err)

	assert.True(t, alertTriggered)
	assert.InDelta(t, 80.0, triggeredAlert.Percentage, 0.01)
	assert.Equal(t, "80% alert", triggeredAlert.Message)

	// Track to 90%
	alertTriggered = false
	err = tracker.TrackRequest(1.0)
	assert.NoError(t, err)

	assert.True(t, alertTriggered)
	assert.InDelta(t, 90.0, triggeredAlert.Percentage, 0.01)
}

func TestBudgetTracker_PeriodReset(t *testing.T) {
	t.Parallel()
	config := DefaultBudgetConfig()
	config.Limit = 10.0
	config.Period = BudgetDaily

	tracker := NewBudgetTracker(config, "test-session")
	var err error
	err = tracker.TrackRequest(5.0)
	assert.NoError(t, err)

	// Manually trigger period reset by changing period start
	tracker.periodStart.Store(time.Now().AddDate(0, 0, 2)) // 2 days in future

	// Track another request - should trigger reset
	err = tracker.TrackRequest(2.0)
	assert.NoError(t, err)

	stats := tracker.GetStats()
	assert.InDelta(t, 2.0, stats.CurrentCost, 0.01) // Should have reset
}

func TestBudgetTracker_ManualReset(t *testing.T) {
	t.Parallel()
	config := DefaultBudgetConfig()
	config.Limit = 10.0
	config.Period = BudgetSession

	tracker := NewBudgetTracker(config, "test-session")
	err := tracker.TrackRequest(5.0)
	assert.NoError(t, err)

	// Manual reset
	tracker.Reset()

	stats := tracker.GetStats()
	assert.InDelta(t, 0.0, stats.CurrentCost, 0.01)
	assert.Equal(t, int64(0), stats.RequestCount)
}

func TestBudgetTracker_SetLimit(t *testing.T) {
	t.Parallel()
	config := DefaultBudgetConfig()
	config.Limit = 10.0
	config.Period = BudgetSession

	tracker := NewBudgetTracker(config, "test-session")
	err := tracker.TrackRequest(5.0)
	assert.NoError(t, err)

	// Update limit
	tracker.SetLimit(20.0)

	stats := tracker.GetStats()
	assert.InDelta(t, 20.0, stats.Limit, 0.01)
	assert.InDelta(t, 25.0, stats.PercentageUsed, 0.01) // 5/20 = 25%
}

func TestBudgetManager_MultipleSessions(t *testing.T) {
	t.Parallel()
	config := DefaultBudgetConfig()
	config.Limit = 10.0
	config.Period = BudgetSession

	manager := NewBudgetManager(config)

	// Create trackers for different sessions
	tracker1 := manager.GetTracker("session-1")
	tracker2 := manager.GetTracker("session-2")

	var err error
	err = tracker1.TrackRequest(3.0)
	assert.NoError(t, err)
	err = tracker2.TrackRequest(5.0)
	assert.NoError(t, err)

	stats1 := tracker1.GetStats()
	stats2 := tracker2.GetStats()

	assert.InDelta(t, 3.0, stats1.CurrentCost, 0.01)
	assert.InDelta(t, 5.0, stats2.CurrentCost, 0.01)

	// Track via manager to update global cost (simulate additional requests)
	err = manager.TrackRequest("session-1", 1.0)
	assert.NoError(t, err)
	err = manager.TrackRequest("session-2", 2.0)
	assert.NoError(t, err)

	// Global stats should include costs tracked via manager
	// Note: tracker.TrackRequest does NOT update globalCost, only manager.TrackRequest does
	globalStats := manager.GetGlobalStats()
	assert.InDelta(t, 3.0, globalStats.CurrentCost, 0.01) // 1.0 + 2.0 from manager.TrackRequest
}

func TestBudgetManager_RemoveTracker(t *testing.T) {
	t.Parallel()
	config := DefaultBudgetConfig()
	config.Limit = 10.0
	config.Period = BudgetSession

	manager := NewBudgetManager(config)
	manager.GetTracker("session-1")
	manager.GetTracker("session-2")

	manager.RemoveTracker("session-1")

	trackers := manager.GetAllTrackers()
	_, exists := trackers["session-1"]
	assert.False(t, exists)
	_, exists = trackers["session-2"]
	assert.True(t, exists)
}

func TestBudgetPeriod_String(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "daily", string(BudgetDaily))
	assert.Equal(t, "weekly", string(BudgetWeekly))
	assert.Equal(t, "monthly", string(BudgetMonthly))
	assert.Equal(t, "session", string(BudgetSession))
}
