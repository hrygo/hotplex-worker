package llm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRouter_SelectModel_ByCost(t *testing.T) {
	t.Parallel()
	router := NewRouter(RouterConfig{
		DefaultStrategy: StrategyCostPriority,
		Models: []ModelConfig{
			{Name: "expensive-model", Provider: "openai", CostPer1KInput: 0.01, CostPer1KOutput: 0.03, Enabled: true},
			{Name: "cheap-model", Provider: "openai", CostPer1KInput: 0.00015, CostPer1KOutput: 0.0006, Enabled: true},
			{Name: "medium-model", Provider: "openai", CostPer1KInput: 0.005, CostPer1KOutput: 0.015, Enabled: true},
		},
	}, nil)

	model, err := router.SelectModel(context.Background(), ScenarioChat, StrategyCostPriority)
	require.NoError(t, err)
	assert.Equal(t, "cheap-model", model.Name)
}

func TestRouter_SelectModel_ByLatency(t *testing.T) {
	t.Parallel()
	router := NewRouter(RouterConfig{
		DefaultStrategy: StrategyLatencyPriority,
		Models: []ModelConfig{
			{Name: "slow-model", Provider: "openai", AvgLatencyMs: 1000, Enabled: true},
			{Name: "fast-model", Provider: "openai", AvgLatencyMs: 100, Enabled: true},
			{Name: "medium-model", Provider: "openai", AvgLatencyMs: 500, Enabled: true},
		},
	}, nil)

	model, err := router.SelectModel(context.Background(), ScenarioChat, StrategyLatencyPriority)
	require.NoError(t, err)
	assert.Equal(t, "fast-model", model.Name)
}

func TestRouter_SelectModel_ScenarioMapping(t *testing.T) {
	t.Parallel()
	router := NewRouter(RouterConfig{
		DefaultStrategy: StrategyCostPriority,
		Models: []ModelConfig{
			{Name: "chat-model", Provider: "openai", Enabled: true},
			{Name: "analyze-model", Provider: "openai", Enabled: true},
		},
		ScenarioModelMap: map[Scenario]string{
			ScenarioChat:    "chat-model",
			ScenarioAnalyze: "analyze-model",
		},
	}, nil)

	// Test chat scenario
	model, err := router.SelectModel(context.Background(), ScenarioChat, StrategyCostPriority)
	require.NoError(t, err)
	assert.Equal(t, "chat-model", model.Name)

	// Test analyze scenario
	model, err = router.SelectModel(context.Background(), ScenarioAnalyze, StrategyCostPriority)
	require.NoError(t, err)
	assert.Equal(t, "analyze-model", model.Name)
}

func TestRouter_DetectScenario(t *testing.T) {
	t.Parallel()
	router := NewRouter(RouterConfig{}, nil)

	tests := []struct {
		prompt   string
		expected Scenario
	}{
		{"Hello, how are you?", ScenarioChat},
		{"Write a function to sort an array", ScenarioCode},
		{"Analyze this data and return JSON", ScenarioAnalyze},
		{"Explain why the sky is blue", ScenarioReasoning},
		{"Debug this code", ScenarioCode},
		{"Extract entities from text", ScenarioAnalyze},
		{"Solve this math problem", ScenarioReasoning},
	}

	for _, tt := range tests {
		t.Run(tt.prompt, func(t *testing.T) {
			scenario := router.DetectScenario(tt.prompt)
			assert.Equal(t, tt.expected, scenario)
		})
	}
}

func TestRouter_NoEnabledModels(t *testing.T) {
	t.Parallel()
	router := NewRouter(RouterConfig{
		DefaultStrategy: StrategyCostPriority,
		Models: []ModelConfig{
			{Name: "disabled-model", Provider: "openai", Enabled: false},
		},
	}, nil)

	_, err := router.SelectModel(context.Background(), ScenarioChat, StrategyCostPriority)
	assert.Error(t, err)
}

func TestCostCalculator_CalculateCost(t *testing.T) {
	t.Parallel()
	cc := NewCostCalculator()

	cost, err := cc.CalculateCost("gpt-4o-mini", 1000, 2000)
	require.NoError(t, err)
	// 0.00015 * 1 + 0.0006 * 2 = 0.00135
	assert.InDelta(t, 0.00135, cost, 0.00001)
}

func TestCostCalculator_CountTokens(t *testing.T) {
	t.Parallel()
	cc := NewCostCalculator()

	tests := []struct {
		text      string
		minTokens int
		maxTokens int
	}{
		{"", 0, 0},
		{"Hello", 1, 5},
		{"Hello world, how are you?", 5, 10},
	}

	for _, tt := range tests {
		tokens := cc.CountTokens(tt.text)
		assert.GreaterOrEqual(t, tokens, tt.minTokens)
		assert.LessOrEqual(t, tokens, tt.maxTokens)
	}
}

func TestCostCalculator_TrackSession(t *testing.T) {
	t.Parallel()
	cc := NewCostCalculator()

	session, cost, err := cc.TrackRequest("session-1", "gpt-4o-mini", 1000, 2000)
	require.NoError(t, err)
	assert.InDelta(t, 0.00135, cost, 0.00001)
	assert.Equal(t, int64(1000), session.TotalInput)
	assert.Equal(t, int64(2000), session.TotalOutput)
	assert.Equal(t, int64(1), session.RequestCount)

	// Second request (incremental cost: 500 input + 1000 output)
	session, cost, err = cc.TrackRequest("session-1", "gpt-4o-mini", 500, 1000)
	require.NoError(t, err)
	// Incremental cost: 500*0.00015/1000 + 1000*0.0006/1000 = 0.000075 + 0.0006 = 0.000675
	assert.InDelta(t, 0.000675, cost, 0.00001)
	assert.Equal(t, int64(1500), session.TotalInput)
	assert.Equal(t, int64(3000), session.TotalOutput)
	assert.Equal(t, int64(2), session.RequestCount)
}

func TestCostCalculator_BudgetAlert(t *testing.T) {
	t.Parallel()
	cc := NewCostCalculator()

	// Set very low budget
	cc.SetSessionBudget("session-1", 0.001)

	// First request should trigger alert
	session, _, err := cc.TrackRequest("session-1", "gpt-4o-mini", 1000, 2000)
	require.NoError(t, err)
	assert.True(t, session.BudgetAlerted)
}

func TestRateLimiter_Allow(t *testing.T) {
	t.Parallel()
	rl := NewRateLimiter(RateLimitConfig{
		RequestsPerSecond: 10,
		BurstSize:         5,
		MaxQueueSize:      10,
		QueueTimeout:      time.Second,
	})
	defer rl.Close()

	// Should allow burst size requests immediately
	for i := 0; i < 5; i++ {
		assert.True(t, rl.Allow())
	}

	// Next request should be rate limited (but not queued in Allow)
	assert.False(t, rl.Allow())
}

func TestRateLimiter_Wait(t *testing.T) {
	t.Parallel()
	rl := NewRateLimiter(RateLimitConfig{
		RequestsPerSecond: 100, // High rate for fast test
		BurstSize:         2,
		MaxQueueSize:      10,
		QueueTimeout:      2 * time.Second,
	})
	defer rl.Close()

	ctx := context.Background()

	// First two should succeed immediately
	err := rl.Wait(ctx)
	assert.NoError(t, err)

	err = rl.Wait(ctx)
	assert.NoError(t, err)

	// Third should wait in queue but succeed
	err = rl.Wait(ctx)
	assert.NoError(t, err)
}

func TestRateLimiter_QueueFull(t *testing.T) {
	t.Parallel()
	rl := NewRateLimiter(RateLimitConfig{
		RequestsPerSecond: 0.1, // Very slow
		BurstSize:         1,
		MaxQueueSize:      2,
		QueueTimeout:      100 * time.Millisecond,
	})
	defer rl.Close()

	ctx := context.Background()

	// First request succeeds
	err := rl.Wait(ctx)
	assert.NoError(t, err)

	// Fill the queue (blocking calls)
	done1 := make(chan error)
	done2 := make(chan error)
	go func() { done1 <- rl.Wait(ctx) }()
	go func() { done2 <- rl.Wait(ctx) }()

	// Give goroutines time to start
	time.Sleep(50 * time.Millisecond)

	// Next should fail (queue full) or timeout
	err = rl.Wait(ctx)
	assert.Error(t, err)
	// Error should be either "queue full" or timeout
	assert.True(t,
		containsSubstring(err.Error(), "queue full") ||
			containsSubstring(err.Error(), "timeout"),
		"expected queue full or timeout error, got: %v", err)
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) &&
		(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
			findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestRateLimiter_GetStats(t *testing.T) {
	t.Parallel()
	rl := NewRateLimiter(RateLimitConfig{
		RequestsPerSecond: 10,
		BurstSize:         5,
		MaxQueueSize:      10,
		QueueTimeout:      time.Second,
	})
	defer rl.Close()

	// Make some requests
	rl.Allow()
	rl.Allow()
	rl.Allow()

	stats := rl.GetStats()
	assert.Equal(t, int64(3), stats.TotalRequests)
	assert.Equal(t, int64(0), stats.RejectedRequests)
}

func TestMetricsCollector_RecordRequest(t *testing.T) {
	t.Parallel()
	mc := NewMetricsCollector(MetricsConfig{
		Enabled:           false, // Disable OTel for unit test
		ServiceName:       "test",
		MaxLatencySamples: 100,
	})

	// Record several requests
	mc.RecordRequest("gpt-4o-mini", "chat", 100, 200, 0.001, 150.0, nil)
	mc.RecordRequest("gpt-4o-mini", "chat", 150, 250, 0.0015, 200.0, nil)
	mc.RecordRequest("gpt-4o-mini", "chat", 200, 300, 0.002, 100.0, nil)

	stats := mc.GetStats()
	assert.Equal(t, int64(3), stats.TotalRequests)
	assert.Equal(t, int64(450), stats.TotalInputTokens)
	assert.Equal(t, int64(750), stats.TotalOutputTokens)
	assert.InDelta(t, 0.0045, stats.TotalCost, 0.0001)
	assert.InDelta(t, 150.0, stats.AvgLatencyMs, 50.0)
}

func TestMetricsCollector_ErrorTracking(t *testing.T) {
	t.Parallel()
	mc := NewMetricsCollector(MetricsConfig{
		Enabled:           false,
		ServiceName:       "test",
		MaxLatencySamples: 100,
	})

	// Record successful and failed requests
	mc.RecordRequest("gpt-4o-mini", "chat", 100, 200, 0.001, 150.0, nil)
	mc.RecordRequest("gpt-4o-mini", "chat", 100, 200, 0.0, 200.0, assert.AnError)
	mc.RecordRequest("gpt-4o-mini", "chat", 100, 200, 0.001, 100.0, nil)

	stats := mc.GetStats()
	assert.Equal(t, int64(3), stats.TotalRequests)
	assert.Equal(t, int64(1), stats.TotalErrors)
	assert.InDelta(t, 0.333, stats.ErrorRate, 0.01)
}

func TestRequestTimer(t *testing.T) {
	t.Parallel()
	mc := NewMetricsCollector(MetricsConfig{
		Enabled:           false,
		ServiceName:       "test",
		MaxLatencySamples: 100,
	})

	timer := NewRequestTimer(mc, "gpt-4o-mini", "chat")
	time.Sleep(50 * time.Millisecond) // Simulate work
	timer.Record(100, 200, 0.001, nil)

	stats := mc.GetStats()
	assert.Equal(t, int64(1), stats.TotalRequests)
	assert.GreaterOrEqual(t, stats.AvgLatencyMs, 50.0)
}

func TestDefaultModelPricing(t *testing.T) {
	t.Parallel()
	pricing := DefaultModelPricing()
	assert.Greater(t, len(pricing), 10)

	// Check specific models
	found := make(map[string]bool)
	for _, p := range pricing {
		found[p.ModelName] = true
		assert.Greater(t, p.CostPer1KInput, 0.0)
		assert.Greater(t, p.CostPer1KOutput, 0.0)
	}

	assert.True(t, found["gpt-4o-mini"])
	assert.True(t, found["claude-3-haiku"])
	assert.True(t, found["qwen-plus"])
}
