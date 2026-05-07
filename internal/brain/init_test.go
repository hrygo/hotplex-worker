package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/brain/llm"
)

// ========================================
// Mock LLM Client for Brain Init Tests
// ========================================

// mockLLMClientForBrain implements llmClient interface for testing.
type mockLLMClientForBrain struct {
	chatFn       func(ctx context.Context, prompt string) (string, error)
	analyzeFn    func(ctx context.Context, prompt string, target any) error
	streamFn     func(ctx context.Context, prompt string) (<-chan string, error)
	healthFn     func(ctx context.Context) HealthStatus
	chatCount    int
	analyzeCount int
}

func (m *mockLLMClientForBrain) Chat(ctx context.Context, prompt string) (string, error) {
	m.chatCount++
	if m.chatFn != nil {
		return m.chatFn(ctx, prompt)
	}
	return "mock chat response", nil
}

func (m *mockLLMClientForBrain) ChatWithOptions(ctx context.Context, prompt string, opts llm.ChatOptions) (string, error) {
	return m.Chat(ctx, prompt)
}

func (m *mockLLMClientForBrain) Analyze(ctx context.Context, prompt string, target any) error {
	m.analyzeCount++
	if m.analyzeFn != nil {
		return m.analyzeFn(ctx, prompt, target)
	}
	return json.Unmarshal([]byte(`{"result": "mock"}`), target)
}

func (m *mockLLMClientForBrain) ChatStream(ctx context.Context, prompt string) (<-chan string, error) {
	if m.streamFn != nil {
		return m.streamFn(ctx, prompt)
	}
	ch := make(chan string)
	go func() {
		defer close(ch)
		ch <- "token1"
		ch <- "token2"
	}()
	return ch, nil
}

func (m *mockLLMClientForBrain) HealthCheck(ctx context.Context) HealthStatus {
	if m.healthFn != nil {
		return m.healthFn(ctx)
	}
	return HealthStatus{Healthy: true}
}

// ========================================
// enhancedBrainWrapper Tests
// ========================================

func TestEnhancedBrainWrapper_Chat(t *testing.T) {
	mockClient := &mockLLMClientForBrain{}
	wrapper := &enhancedBrainWrapper{
		client: mockClient,
		config: Config{Model: ModelConfig{Model: "gpt-4o"}},
		logger: slog.Default(),
	}

	result, err := wrapper.Chat(context.Background(), "hello")
	require.NoError(t, err)
	assert.Equal(t, "mock chat response", result)
	assert.Equal(t, 1, mockClient.chatCount)
}

func TestEnhancedBrainWrapper_Analyze(t *testing.T) {
	mockClient := &mockLLMClientForBrain{}
	wrapper := &enhancedBrainWrapper{
		client: mockClient,
		config: Config{Model: ModelConfig{Model: "gpt-4o"}},
		logger: slog.Default(),
	}

	var result struct {
		Result string `json:"result"`
	}
	err := wrapper.Analyze(context.Background(), "analyze this", &result)
	require.NoError(t, err)
	assert.Equal(t, "mock", result.Result)
	assert.Equal(t, 1, mockClient.analyzeCount)
}

func TestEnhancedBrainWrapper_ChatWithModel(t *testing.T) {
	mockClient := &mockLLMClientForBrain{}
	wrapper := &enhancedBrainWrapper{
		client: mockClient,
		config: Config{Model: ModelConfig{Model: "gpt-4o"}},
		logger: slog.Default(),
	}

	// With explicit model
	result, err := wrapper.ChatWithModel(context.Background(), "gpt-4o-mini", "hello")
	require.NoError(t, err)
	assert.Equal(t, "mock chat response", result)
}

func TestEnhancedBrainWrapper_ChatWithModel_DefaultModel(t *testing.T) {
	mockClient := &mockLLMClientForBrain{}
	wrapper := &enhancedBrainWrapper{
		client: mockClient,
		config: Config{Model: ModelConfig{Model: "gpt-4o"}},
		logger: slog.Default(),
	}

	// Empty model string should fall back to config default
	result, err := wrapper.ChatWithModel(context.Background(), "", "hello")
	require.NoError(t, err)
	assert.Equal(t, "mock chat response", result)
}

func TestEnhancedBrainWrapper_ChatWithModel_Timeout(t *testing.T) {
	mockClient := &mockLLMClientForBrain{
		chatFn: func(ctx context.Context, prompt string) (string, error) {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(5 * time.Second):
				return "slow response", nil
			}
		},
	}
	wrapper := &enhancedBrainWrapper{
		client: mockClient,
		config: Config{Model: ModelConfig{Model: "gpt-4o", TimeoutS: 1}},
		logger: slog.Default(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	start := time.Now()
	_, err := wrapper.Chat(ctx, "test")
	elapsed := time.Since(start)

	assert.Error(t, err)
	assert.Less(t, elapsed, 3*time.Second)
}

func TestEnhancedBrainWrapper_AnalyzeWithModel(t *testing.T) {
	mockClient := &mockLLMClientForBrain{}
	wrapper := &enhancedBrainWrapper{
		client: mockClient,
		config: Config{Model: ModelConfig{Model: "gpt-4o"}},
		logger: slog.Default(),
	}

	var result struct {
		Result string `json:"result"`
	}
	err := wrapper.AnalyzeWithModel(context.Background(), "gpt-4o-mini", "analyze", &result)
	require.NoError(t, err)
	assert.Equal(t, "mock", result.Result)
}

func TestEnhancedBrainWrapper_HealthCheck(t *testing.T) {
	mockClient := &mockLLMClientForBrain{
		healthFn: func(ctx context.Context) HealthStatus {
			return HealthStatus{Healthy: true, Provider: "mock", Model: "gpt-4o"}
		},
	}
	wrapper := &enhancedBrainWrapper{
		client: mockClient,
		config: Config{},
	}

	status := wrapper.HealthCheck(context.Background())
	assert.True(t, status.Healthy)
	assert.Equal(t, "mock", status.Provider)
}

// ========================================
// applyTimeout Tests
// ========================================

func TestEnhancedBrainWrapper_ApplyTimeout_WithConfig(t *testing.T) {
	wrapper := &enhancedBrainWrapper{
		config:  Config{Model: ModelConfig{TimeoutS: 10}},
		timeout: 10 * time.Second, // Pre-computed timeout
	}

	ctx := context.Background()
	modifiedCtx, cancel := wrapper.applyTimeout(ctx)
	defer cancel()

	// The context should have a deadline
	deadline, ok := modifiedCtx.Deadline()
	assert.True(t, ok, "context should have a deadline")
	assert.WithinDuration(t, time.Now().Add(10*time.Second), deadline, time.Second)
}

func TestEnhancedBrainWrapper_ApplyTimeout_NoConfig(t *testing.T) {
	wrapper := &enhancedBrainWrapper{
		config: Config{Model: ModelConfig{TimeoutS: 0}},
	}

	ctx := context.Background()
	modifiedCtx, cancel := wrapper.applyTimeout(ctx)
	defer cancel()

	// The context should NOT have a deadline
	_, ok := modifiedCtx.Deadline()
	assert.False(t, ok)
}

// ========================================
// selectModel Tests
// ========================================

func TestEnhancedBrainWrapper_SelectModel_ExplicitModel(t *testing.T) {
	wrapper := &enhancedBrainWrapper{
		config: Config{Model: ModelConfig{Model: "gpt-4o"}},
	}

	model := wrapper.selectModel(context.Background(), "gpt-4o-mini", "chat")
	assert.Equal(t, "gpt-4o-mini", model)
}

func TestEnhancedBrainWrapper_SelectModel_NoRouter(t *testing.T) {
	wrapper := &enhancedBrainWrapper{
		config: Config{Model: ModelConfig{Model: "gpt-4o"}},
		router: nil,
	}

	model := wrapper.selectModel(context.Background(), "", "chat")
	assert.Equal(t, "gpt-4o", model)
}

// ========================================
// applyRateLimit Tests
// ========================================

func TestEnhancedBrainWrapper_ApplyRateLimit_NoLimiter(t *testing.T) {
	wrapper := &enhancedBrainWrapper{
		rateLimiter: nil,
	}

	err := wrapper.applyRateLimit(context.Background(), "gpt-4o")
	assert.NoError(t, err)
}

// ========================================
// startMetricsTimer Tests
// ========================================

func TestEnhancedBrainWrapper_StartMetricsTimer_NilMetrics(t *testing.T) {
	wrapper := &enhancedBrainWrapper{
		metrics: nil,
	}

	timer := wrapper.startMetricsTimer("gpt-4o", "chat")
	assert.Nil(t, timer)
}

// ========================================
// recordMetrics Tests
// ========================================

func TestEnhancedBrainWrapper_RecordMetrics_NilTimer(t *testing.T) {
	wrapper := &enhancedBrainWrapper{}
	// Should not panic
	wrapper.recordMetrics(nil, "gpt-4o", "prompt", "result", nil)
}

func TestEnhancedBrainWrapper_RecordMetrics_NoCostCalc(t *testing.T) {
	mockClient := &mockLLMClientForBrain{}
	wrapper := &enhancedBrainWrapper{
		client: mockClient,
		config: Config{},
		// metrics is nil, so timer will be nil
	}
	// Should not panic
	wrapper.recordMetrics(nil, "gpt-4o", "prompt", "result", nil)
}

// ========================================
// recordMetricsForAnalyze Tests
// ========================================

func TestEnhancedBrainWrapper_RecordMetricsForAnalyze_NilTimer(t *testing.T) {
	wrapper := &enhancedBrainWrapper{}
	wrapper.recordMetricsForAnalyze(nil, "gpt-4o", "prompt", nil)
}

// ========================================
// ChatStream Tests
// ========================================

func TestEnhancedBrainWrapper_ChatStream(t *testing.T) {
	mockClient := &mockLLMClientForBrain{}
	wrapper := &enhancedBrainWrapper{
		client: mockClient,
		config: Config{Model: ModelConfig{Model: "gpt-4o"}},
	}

	stream, err := wrapper.ChatStream(context.Background(), "hello")
	require.NoError(t, err)
	require.NotNil(t, stream)

	tokens := []string{}
	for token := range stream {
		tokens = append(tokens, token)
	}
	assert.Equal(t, []string{"token1", "token2"}, tokens)
}

func TestEnhancedBrainWrapper_ChatStream_Error(t *testing.T) {
	expectedErr := fmt.Errorf("stream error")
	mockClient := &mockLLMClientForBrain{
		streamFn: func(ctx context.Context, prompt string) (<-chan string, error) {
			return nil, expectedErr
		},
	}
	wrapper := &enhancedBrainWrapper{
		client: mockClient,
		config: Config{Model: ModelConfig{Model: "gpt-4o"}},
	}

	_, err := wrapper.ChatStream(context.Background(), "hello")
	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
}

func TestEnhancedBrainWrapper_ChatStream_NilStream(t *testing.T) {
	mockClient := &mockLLMClientForBrain{
		streamFn: func(ctx context.Context, prompt string) (<-chan string, error) {
			return nil, nil
		},
	}
	wrapper := &enhancedBrainWrapper{
		client: mockClient,
		config: Config{Model: ModelConfig{Model: "gpt-4o"}},
	}

	stream, err := wrapper.ChatStream(context.Background(), "hello")
	require.NoError(t, err)

	// nil stream should result in empty output
	tokens := []string{}
	if stream != nil {
		for token := range stream {
			tokens = append(tokens, token)
		}
	}
	assert.Empty(t, tokens)
}

func TestEnhancedBrainWrapper_ChatStream_Timeout(t *testing.T) {
	mockClient := &mockLLMClientForBrain{
		streamFn: func(ctx context.Context, prompt string) (<-chan string, error) {
			ch := make(chan string)
			go func() {
				defer close(ch)
				ch <- "token1"
				time.Sleep(5 * time.Second)
				ch <- "token2"
			}()
			return ch, nil
		},
	}
	// Use TimeoutS=0 to avoid applyTimeout's defer cancel issue
	wrapper := &enhancedBrainWrapper{
		client: mockClient,
		config: Config{Model: ModelConfig{Model: "gpt-4o"}},
	}

	// Use a short context timeout to test stream interruption
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	stream, err := wrapper.ChatStream(ctx, "hello")
	require.NoError(t, err)

	tokens := []string{}
	if stream != nil {
		for token := range stream {
			tokens = append(tokens, token)
		}
	}
	// Should get token1 but not token2 due to timeout
	assert.Equal(t, []string{"token1"}, tokens)
}

func TestEnhancedBrainWrapper_ChatStream_WithCostCalc(t *testing.T) {
	mockClient := &mockLLMClientForBrain{}
	costCalc := llm.NewCostCalculator()
	wrapper := &enhancedBrainWrapper{
		client:         mockClient,
		config:         Config{Model: ModelConfig{Model: "gpt-4o"}},
		costCalculator: costCalc,
	}

	stream, err := wrapper.ChatStream(context.Background(), "hello")
	require.NoError(t, err)

	tokens := []string{}
	if stream != nil {
		for token := range stream {
			tokens = append(tokens, token)
		}
	}
	assert.Equal(t, []string{"token1", "token2"}, tokens)
}

// ========================================
// GetMetrics Tests
// ========================================

func TestEnhancedBrainWrapper_GetMetrics_NilMetrics(t *testing.T) {
	wrapper := &enhancedBrainWrapper{
		metrics: nil,
	}

	stats := wrapper.GetMetrics()
	assert.Empty(t, stats.TotalRequests)
}

// ========================================
// GetCostCalculator Tests
// ========================================

func TestEnhancedBrainWrapper_GetCostCalculator(t *testing.T) {
	costCalc := llm.NewCostCalculator()
	wrapper := &enhancedBrainWrapper{
		costCalculator: costCalc,
	}

	result := wrapper.GetCostCalculator()
	assert.NotNil(t, result)
}

// ========================================
// GetRouter Tests
// ========================================

func TestEnhancedBrainWrapper_GetRouter_Nil(t *testing.T) {
	wrapper := &enhancedBrainWrapper{
		router: nil,
	}

	result := wrapper.GetRouter()
	assert.Nil(t, result)
}

// ========================================
// GetRateLimiter Tests
// ========================================

func TestEnhancedBrainWrapper_GetRateLimiter_Nil(t *testing.T) {
	wrapper := &enhancedBrainWrapper{
		rateLimiter: nil,
	}

	result := wrapper.GetRateLimiter()
	assert.Nil(t, result)
}

// ========================================
// Init Tests
// ========================================

func TestInit_Disabled(t *testing.T) {
	// Save and restore global state
	oldBrain := globalBrain
	defer func() { globalBrain = oldBrain }()

	// Clear global brain to simulate fresh start
	globalBrain = nil

	// Clear all env vars that could enable the brain
	t.Setenv("HOTPLEX_BRAIN_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("SILICONFLOW_API_KEY", "")
	t.Setenv("HOTPLEX_BRAIN_WORKER_EXTRACT", "false")

	err := Init(slog.Default())
	assert.NoError(t, err)
	assert.Nil(t, Global())
}

// ========================================
// Interface Compliance
// ========================================

func TestEnhancedBrainWrapper_ImplementsLLMClient(t *testing.T) {
	// Compile-time check already exists in brain.go,
	// but verify the wrapper can be assigned
	var _ llm.LLMClient = (*enhancedBrainWrapper)(nil)
}

// ========================================
// ConfigIntent Structure Tests
// ========================================

func TestConfigIntent_Fields(t *testing.T) {
	intent := &ConfigIntent{
		Action:     "set",
		Target:     "model",
		Value:      "opus",
		Confidence: 0.95,
	}

	assert.Equal(t, "set", intent.Action)
	assert.Equal(t, "model", intent.Target)
	assert.Equal(t, "opus", intent.Value)
	assert.Equal(t, 0.95, intent.Confidence)
}

func TestConfigIntent_JSONRoundtrip(t *testing.T) {
	intent := ConfigIntent{
		Action: "get",
		Target: "provider",
		Extra:  map[string]interface{}{"key": "value"},
	}

	data, err := json.Marshal(intent)
	require.NoError(t, err)

	var decoded ConfigIntent
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, intent.Action, decoded.Action)
	assert.Equal(t, intent.Target, decoded.Target)
}

// ========================================
// GuardResult Structure Tests
// ========================================

func TestGuardResult_Fields(t *testing.T) {
	result := &GuardResult{
		Safe:           false,
		ThreatLevel:    ThreatLevelHigh,
		ThreatType:     "prompt_injection",
		Reason:         "matched dangerous pattern",
		MatchedPattern: `(?i)jailbreak`,
		Action:         "block",
		SanitizedInput: "",
	}

	assert.False(t, result.Safe)
	assert.Equal(t, ThreatLevelHigh, result.ThreatLevel)
	assert.Equal(t, "prompt_injection", result.ThreatType)
	assert.Equal(t, "block", result.Action)
	assert.Equal(t, "matched dangerous pattern", result.Reason)
	assert.Equal(t, `(?i)jailbreak`, result.MatchedPattern)
	assert.Equal(t, "", result.SanitizedInput)
}

func TestGuardResult_AllThreatLevels(t *testing.T) {
	levels := []ThreatLevel{
		ThreatLevelNone,
		ThreatLevelLow,
		ThreatLevelMedium,
		ThreatLevelHigh,
		ThreatLevelCritical,
	}
	for _, level := range levels {
		result := GuardResult{ThreatLevel: level}
		assert.Equal(t, level, result.ThreatLevel)
	}
}

// ========================================
// ChatStream with Context Cancellation
// ========================================

func TestEnhancedBrainWrapper_ChatStream_ContextCancelled(t *testing.T) {
	mockClient := &mockLLMClientForBrain{
		streamFn: func(ctx context.Context, prompt string) (<-chan string, error) {
			ch := make(chan string)
			go func() {
				defer close(ch)
				// Slow producer
				for i := 0; i < 100; i++ {
					select {
					case <-ctx.Done():
						return
					case ch <- fmt.Sprintf("token%d", i):
						time.Sleep(10 * time.Millisecond)
					}
				}
			}()
			return ch, nil
		},
	}
	wrapper := &enhancedBrainWrapper{
		client: mockClient,
		config: Config{Model: ModelConfig{Model: "gpt-4o"}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	stream, err := wrapper.ChatStream(ctx, "hello")
	require.NoError(t, err)

	count := 0
	if stream != nil {
		for range stream {
			count++
		}
	}
	// Should have received some tokens but not all 100
	assert.Greater(t, count, 0)
	assert.Less(t, count, 100)
}

// ========================================
// ChatStream With Metrics
// ========================================

func TestEnhancedBrainWrapper_ChatStream_RecordsMetrics(t *testing.T) {
	mockClient := &mockLLMClientForBrain{}
	wrapper := &enhancedBrainWrapper{
		client:         mockClient,
		config:         Config{Model: ModelConfig{Model: "gpt-4o"}},
		costCalculator: llm.NewCostCalculator(),
	}

	stream, err := wrapper.ChatStream(context.Background(), "hello")
	require.NoError(t, err)

	if stream != nil {
		for range stream {
		}
	}
	// Should not panic and cost calculator should have been called
}

// ========================================
// Integration: Wrapper with Router
// ========================================

func TestEnhancedBrainWrapper_WithRouter(t *testing.T) {
	mockClient := &mockLLMClientForBrain{}

	// We can't easily create a real router, so test the nil-router path
	wrapper := &enhancedBrainWrapper{
		client: mockClient,
		config: Config{Model: ModelConfig{Model: "gpt-4o"}},
		router: nil,
	}

	// selectModel with nil router should return the configured default
	model := wrapper.selectModel(context.Background(), "", "")
	assert.Equal(t, "gpt-4o", model)
}

// ========================================
// Record metrics with cost calculator
// ========================================

func TestEnhancedBrainWrapper_RecordMetrics_WithCostCalc(t *testing.T) {
	calc := llm.NewCostCalculator()
	wrapper := &enhancedBrainWrapper{
		config:         Config{Model: ModelConfig{Model: "gpt-4o"}},
		costCalculator: calc,
	}
	// Should not panic, timer is nil so it returns early
	wrapper.recordMetrics(nil, "gpt-4o", "prompt text", "result text", nil)
}

func TestEnhancedBrainWrapper_RecordMetricsForAnalyze_WithCostCalc(t *testing.T) {
	calc := llm.NewCostCalculator()
	wrapper := &enhancedBrainWrapper{
		config:         Config{Model: ModelConfig{Model: "gpt-4o"}},
		costCalculator: calc,
	}
	// Should not panic, timer is nil so it returns early
	wrapper.recordMetricsForAnalyze(nil, "gpt-4o", "prompt text", nil)
}

// ========================================
// ChatWithModel rate limited
// ========================================

func TestEnhancedBrainWrapper_ChatWithModel_RateLimited(t *testing.T) {
	mockClient := &mockLLMClientForBrain{
		chatFn: func(ctx context.Context, prompt string) (string, error) {
			return "response", nil
		},
	}

	// Create a rate limiter with zero burst - requests will be queued and eventually timeout
	rateLimiter := llm.NewRateLimiter(llm.RateLimitConfig{
		RequestsPerSecond: 0,
		BurstSize:         0,
		MaxQueueSize:      1,
		QueueTimeout:      1 * time.Millisecond,
	})
	defer rateLimiter.Close()

	wrapper := &enhancedBrainWrapper{
		client:      mockClient,
		config:      Config{Model: ModelConfig{Model: "gpt-4o"}},
		rateLimiter: rateLimiter,
	}

	_, err := wrapper.ChatWithModel(context.Background(), "", "hello")
	assert.Error(t, err)
}

// ========================================
// Additional config tests
// ========================================

func TestConfig_Defaults_WhenNoEnv(t *testing.T) {
	// Clear provider type to prevent CLI extractor from interfering
	t.Setenv("HOTPLEX_BRAIN_WORKER_EXTRACT", "false")
	// Set required API key to activate HOTPLEX_BRAIN_* env vars
	t.Setenv("HOTPLEX_BRAIN_API_KEY", "test-key")
	t.Setenv("HOTPLEX_BRAIN_PROVIDER", "openai")

	config := LoadConfigFromEnv()

	assert.True(t, config.Enabled) // Now enabled since we set API key
	assert.Equal(t, "openai", config.Model.Provider)
	assert.Equal(t, "gpt-4o", config.Model.Model)
	assert.Equal(t, "openai", config.Model.Protocol)
	assert.Equal(t, 30, config.Model.TimeoutS)
}

func TestConfig_AnthropicViaWorkerExtract(t *testing.T) {
	_, err := NewClaudeCodeExtractor().Extract()
	hasClaudeCode := err == nil
	if !hasClaudeCode {
		t.Skip("No ~/.claude/settings.json found")
	}

	t.Setenv("HOTPLEX_BRAIN_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("SILICONFLOW_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("HOTPLEX_BRAIN_WORKER_EXTRACT", "true")

	config := LoadConfigFromEnv()

	assert.True(t, config.Enabled)
	assert.Equal(t, "anthropic", config.Model.Provider)
	assert.Equal(t, "anthropic", config.Model.Protocol)
}

func TestConfig_SiliconflowViaSystemEnv(t *testing.T) {
	t.Setenv("HOTPLEX_BRAIN_API_KEY", "")
	t.Setenv("HOTPLEX_BRAIN_WORKER_EXTRACT", "false")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("SILICONFLOW_API_KEY", "sf-test-key")

	config := LoadConfigFromEnv()

	assert.True(t, config.Enabled)
	assert.Equal(t, "openai", config.Model.Provider)
	assert.Equal(t, "deepseek-ai/DeepSeek-V3", config.Model.Model)
}

func TestConfig_DeepseekViaSystemEnv(t *testing.T) {
	t.Setenv("HOTPLEX_BRAIN_API_KEY", "")
	t.Setenv("HOTPLEX_BRAIN_WORKER_EXTRACT", "false")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("SILICONFLOW_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "ds-test-key")

	config := LoadConfigFromEnv()

	assert.True(t, config.Enabled)
	assert.Equal(t, "openai", config.Model.Provider)
	assert.Equal(t, "deepseek-chat", config.Model.Model)
}

func TestConfig_OpenAIViaSystemEnv(t *testing.T) {
	t.Setenv("HOTPLEX_BRAIN_API_KEY", "")
	t.Setenv("HOTPLEX_BRAIN_WORKER_EXTRACT", "false")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "oa-test-key")

	config := LoadConfigFromEnv()

	assert.True(t, config.Enabled)
	assert.Equal(t, "openai", config.Model.Provider)
	assert.Equal(t, "openai", config.Model.Protocol)
}

func TestConfig_EndpointOverride(t *testing.T) {
	t.Setenv("HOTPLEX_BRAIN_API_KEY", "test-key")
	t.Setenv("HOTPLEX_BRAIN_ENDPOINT", "https://custom.api.example.com/v1")

	config := LoadConfigFromEnv()

	assert.Equal(t, "https://custom.api.example.com/v1", config.Model.Endpoint)
}

func TestConfig_AnthropicViaSystemEnvWithEndpoint(t *testing.T) {
	t.Setenv("HOTPLEX_BRAIN_API_KEY", "")
	t.Setenv("HOTPLEX_BRAIN_WORKER_EXTRACT", "false")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "ant-test-key")
	t.Setenv("ANTHROPIC_BASE_URL", "https://custom.anthropic.com")

	config := LoadConfigFromEnv()

	assert.True(t, config.Enabled)
	assert.Equal(t, "anthropic", config.Model.Provider)
	assert.Equal(t, "https://custom.anthropic.com", config.Model.Endpoint)
}

func TestConfig_AllSubconfigsHaveDefaults(t *testing.T) {
	t.Setenv("HOTPLEX_BRAIN_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("SILICONFLOW_API_KEY", "")
	t.Setenv("HOTPLEX_BRAIN_WORKER_EXTRACT", "false")

	config := LoadConfigFromEnv()

	// Cache
	assert.True(t, config.Cache.Enabled)
	assert.Greater(t, config.Cache.Size, 0)

	// Retry
	assert.True(t, config.Retry.Enabled)
	assert.Greater(t, config.Retry.MaxAttempts, 0)
	assert.Greater(t, config.Retry.MinWaitMs, 0)
	assert.Greater(t, config.Retry.MaxWaitMs, 0)

	// Metrics
	assert.True(t, config.Metrics.Enabled)
	assert.NotEmpty(t, config.Metrics.ServiceName)

	// Guard
	assert.True(t, config.Guard.Enabled)
	assert.True(t, config.Guard.InputGuardEnabled)
	assert.True(t, config.Guard.OutputGuardEnabled)
	assert.False(t, config.Guard.Chat2ConfigEnabled)
	assert.Greater(t, config.Guard.MaxInputLength, 0)

	// Memory
	assert.True(t, config.Memory.Enabled)
	assert.Greater(t, config.Memory.TokenThreshold, 0)

	// Intent Router
	assert.True(t, config.IntentRouter.Enabled)
	assert.Greater(t, config.IntentRouter.ConfidenceThreshold, 0.0)
	assert.Greater(t, config.IntentRouter.CacheSize, 0)

	// Rate Limit
	assert.False(t, config.RateLimit.Enabled)
	assert.Greater(t, config.RateLimit.RPS, 0.0)
	assert.Greater(t, config.RateLimit.Burst, 0)
}

// ========================================
// getBoolEnv Tests
// ========================================

func TestGetBoolEnv(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		fallback bool
		expected bool
	}{
		{"true string", "true", false, true},
		{"false string", "false", true, false},
		{"invalid", "notbool", false, false},
		{"invalid with true fallback", "notbool", true, true},
		{"empty env", "", true, true},
		{"empty env false fallback", "", false, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.value != "" {
				t.Setenv("TEST_BOOL_ENV", tc.value)
			}
			result := getBoolEnv("TEST_BOOL_ENV", tc.fallback)
			assert.Equal(t, tc.expected, result)
		})
	}
}

// ========================================
// getIntEnv Tests
// ========================================

func TestGetIntEnv(t *testing.T) {
	t.Setenv("TEST_INT_ENV", "42")
	result := getIntEnv("TEST_INT_ENV", 0)
	assert.Equal(t, 42, result)

	result = getIntEnv("TEST_INT_NONEXISTENT", 99)
	assert.Equal(t, 99, result)

	t.Setenv("TEST_INT_INVALID", "notanumber")
	result = getIntEnv("TEST_INT_INVALID", 77)
	assert.Equal(t, 77, result)
}

// ========================================
// getFloatEnv Tests
// ========================================

func TestGetFloatEnv(t *testing.T) {
	t.Setenv("TEST_FLOAT_ENV", "3.14")
	result := getFloatEnv("TEST_FLOAT_ENV", 0.0)
	assert.InDelta(t, 3.14, result, 0.001)

	result = getFloatEnv("TEST_FLOAT_NONEXISTENT", 1.5)
	assert.InDelta(t, 1.5, result, 0.001)
}

// ========================================
// getDurationEnv Tests
// ========================================

func TestGetDurationEnv_DurationString(t *testing.T) {
	t.Setenv("TEST_DUR_ENV", "30s")
	result := getDurationEnv("TEST_DUR_ENV", 5*time.Second)
	assert.Equal(t, 30*time.Second, result)
}

func TestGetDurationEnv_SecondsString(t *testing.T) {
	t.Setenv("TEST_DUR_ENV", "60")
	result := getDurationEnv("TEST_DUR_ENV", 5*time.Second)
	assert.Equal(t, 60*time.Second, result)
}

func TestGetDurationEnv_Fallback(t *testing.T) {
	result := getDurationEnv("TEST_DUR_NONEXISTENT", 10*time.Second)
	assert.Equal(t, 10*time.Second, result)
}

func TestGetDurationEnv_InvalidString(t *testing.T) {
	t.Setenv("TEST_DUR_ENV", "notaduration")
	result := getDurationEnv("TEST_DUR_ENV", 10*time.Second)
	assert.Equal(t, 10*time.Second, result)
}

// ========================================
// parseRouterModels Tests
// ========================================

func TestParseRouterModels_Empty(t *testing.T) {
	result := parseRouterModels("")
	assert.Nil(t, result)
}

func TestParseRouterModels_SingleModel(t *testing.T) {
	result := parseRouterModels("gpt-4o:openai:0.03:0.06:500")
	require.Len(t, result, 1)
	assert.Equal(t, "gpt-4o", result[0].Name)
	assert.Equal(t, "openai", result[0].Provider)
	assert.InDelta(t, 0.03, result[0].CostPer1KInput, 0.001)
	assert.InDelta(t, 0.06, result[0].CostPer1KOutput, 0.001)
	assert.Equal(t, int64(500), result[0].AvgLatencyMs)
	assert.True(t, result[0].Enabled)
}

func TestParseRouterModels_MultipleModels(t *testing.T) {
	result := parseRouterModels("gpt-4o:openai:0.03:0.06:500;claude-3:anthropic:0.015:0.075:800")
	require.Len(t, result, 2)
	assert.Equal(t, "gpt-4o", result[0].Name)
	assert.Equal(t, "claude-3", result[1].Name)
}

func TestParseRouterModels_TooFewFields(t *testing.T) {
	result := parseRouterModels("gpt-4o:openai")
	assert.Nil(t, result)
}

func TestParseRouterModels_WhitespaceHandling(t *testing.T) {
	// parseRouterModels trims whitespace around parts but not within fields
	// So "gpt-4o " as a model name will be preserved
	result := parseRouterModels("gpt-4o:openai:0.03:0.06:500")
	require.Len(t, result, 1)
	assert.Equal(t, "gpt-4o", result[0].Name)

	// Test with outer whitespace (trimmed by strings.TrimSpace on parts)
	result = parseRouterModels(" gpt-4o:openai:0.03:0.06:500 ")
	require.Len(t, result, 1)
	assert.Equal(t, "gpt-4o", result[0].Name)
}

// ========================================
// parseStringList Tests
// ========================================

func TestParseStringList_Empty(t *testing.T) {
	result := parseStringList("")
	assert.Nil(t, result)
}

func TestParseStringList_Single(t *testing.T) {
	result := parseStringList("admin-user")
	require.Len(t, result, 1)
	assert.Equal(t, "admin-user", result[0])
}

func TestParseStringList_Multiple(t *testing.T) {
	result := parseStringList("user1,user2,user3")
	require.Len(t, result, 3)
	assert.Equal(t, []string{"user1", "user2", "user3"}, result)
}

func TestParseStringList_WithWhitespace(t *testing.T) {
	result := parseStringList(" user1 , user2 , user3 ")
	require.Len(t, result, 3)
	assert.Equal(t, []string{"user1", "user2", "user3"}, result)
}

func TestParseStringList_EmptyElements(t *testing.T) {
	result := parseStringList("user1,,user2,")
	require.Len(t, result, 2)
	assert.Equal(t, []string{"user1", "user2"}, result)
}

// ========================================
// LoadConfigFromEnv with OpenAI base URL
// ========================================

func TestConfig_OpenAIEndpointFallback(t *testing.T) {
	t.Setenv("HOTPLEX_BRAIN_API_KEY", "")
	t.Setenv("HOTPLEX_BRAIN_WORKER_EXTRACT", "false")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "oa-test-key")
	t.Setenv("OPENAI_BASE_URL", "https://custom.openai.com/v1")

	config := LoadConfigFromEnv()

	assert.Equal(t, "https://custom.openai.com/v1", config.Model.Endpoint)
}

// ========================================
// Env var helpers with t.Setenv
// ========================================

func TestGetEnv_Empty(t *testing.T) {
	t.Setenv("TEST_GETENV_EMPTY", "")
	result := getEnv("TEST_GETENV_EMPTY", "fallback")
	assert.Equal(t, "fallback", result)
}

func TestGetIntEnv_WithSetenv(t *testing.T) {
	t.Setenv("TEST_INTVAR", "256")
	result := getIntEnv("TEST_INTVAR", 0)
	assert.Equal(t, 256, result)
}

func TestGetBoolEnv_WithSetenv(t *testing.T) {
	t.Setenv("TEST_BOOLVAR", "true")
	result := getBoolEnv("TEST_BOOLVAR", false)
	assert.True(t, result)
}

func TestGetFloatEnv_WithSetenv(t *testing.T) {
	t.Setenv("TEST_FLOATVAR", "99.9")
	result := getFloatEnv("TEST_FLOATVAR", 0.0)
	assert.InDelta(t, 99.9, result, 0.01)
}

func TestGetDurationEnv_WithDurationString(t *testing.T) {
	t.Setenv("TEST_DURVAR", "5m")
	result := getDurationEnv("TEST_DURVAR", 0)
	assert.Equal(t, 5*time.Minute, result)
}

// ========================================
// Strings helpers used in guard
// ========================================

func TestTruncateForAnalysis_ExactBoundary(t *testing.T) {
	s := strings.Repeat("x", 500)
	result := truncate(s)
	assert.Equal(t, s, result)
	assert.NotContains(t, result, "...")
}

func TestTruncateForAnalysis_OneOver(t *testing.T) {
	s := strings.Repeat("x", 501)
	result := truncate(s)
	assert.Len(t, result, 500) // truncate ensures max 500 chars
	assert.True(t, strings.HasSuffix(result, "..."))
}

func TestTruncateForAnalysis_Empty(t *testing.T) {
	result := truncate("")
	assert.Equal(t, "", result)
}

// ========================================
// Init brain config override tests
// ========================================

func TestConfig_MetricsServiceName(t *testing.T) {
	t.Setenv("HOTPLEX_BRAIN_API_KEY", "key")
	t.Setenv("HOTPLEX_BRAIN_METRICS_SERVICE_NAME", "custom-service")

	config := LoadConfigFromEnv()
	assert.Equal(t, "custom-service", config.Metrics.ServiceName)
}

func TestConfig_RouterStrategy(t *testing.T) {
	t.Setenv("HOTPLEX_BRAIN_API_KEY", "key")
	t.Setenv("HOTPLEX_BRAIN_ROUTER_STRATEGY", "latency_priority")

	config := LoadConfigFromEnv()
	assert.Equal(t, "latency_priority", config.Router.DefaultStage)
}

func TestConfig_GuardSensitivity(t *testing.T) {
	t.Setenv("HOTPLEX_BRAIN_API_KEY", "key")
	t.Setenv("HOTPLEX_BRAIN_GUARD_SENSITIVITY", "high")

	config := LoadConfigFromEnv()
	assert.Equal(t, "high", config.Guard.Sensitivity)
}

func TestConfig_AdminUsers(t *testing.T) {
	t.Setenv("HOTPLEX_BRAIN_API_KEY", "key")
	t.Setenv("HOTPLEX_BRAIN_ADMIN_USERS", "user1,user2,user3")

	config := LoadConfigFromEnv()
	assert.Equal(t, []string{"user1", "user2", "user3"}, config.Guard.AdminUsers)
}

func TestConfig_OpenCodeWorkerExtract(t *testing.T) {
	t.Setenv("HOTPLEX_BRAIN_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("SILICONFLOW_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("ANTHROPIC_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("SILICONFLOW_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")

	config := LoadConfigFromEnv()

	// If real opencode config exists and has the provider/model format,
	// provider/protocol should equal the parsed provider name.
	// If no config found, falls through to system env scan.
	// Either way: provider and protocol should match (both non-empty when enabled).
	if config.Enabled {
		assert.Equal(t, config.Model.Provider, config.Model.Protocol,
			"Provider and Protocol should match for opencode")
		assert.NotEmpty(t, config.Model.Model)
		assert.NotEmpty(t, config.Model.Provider)
	}
}

// ========================================
// recordMetrics with real timer
// ========================================

func TestEnhancedBrainWrapper_RecordMetrics_WithRealTimer(t *testing.T) {
	mockClient := &mockLLMClientForBrain{
		chatFn: func(ctx context.Context, prompt string) (string, error) {
			return "response text", nil
		},
	}
	metricsCollector := llm.NewMetricsCollector(llm.MetricsConfig{
		Enabled:           true,
		ServiceName:       "test",
		MaxLatencySamples: 1000,
	})
	costCalc := llm.NewCostCalculator()
	wrapper := &enhancedBrainWrapper{
		client:         mockClient,
		config:         Config{Model: ModelConfig{Model: "gpt-4o"}},
		metrics:        metricsCollector,
		costCalculator: costCalc,
	}

	result, err := wrapper.Chat(context.Background(), "test prompt")
	require.NoError(t, err)
	assert.Equal(t, "response text", result)

	stats := wrapper.GetMetrics()
	assert.Greater(t, stats.TotalRequests, int64(0))
}

func TestEnhancedBrainWrapper_RecordMetricsForAnalyze_WithRealTimer(t *testing.T) {
	mockClient := &mockLLMClientForBrain{}
	metricsCollector := llm.NewMetricsCollector(llm.MetricsConfig{
		Enabled:           true,
		ServiceName:       "test",
		MaxLatencySamples: 1000,
	})
	costCalc := llm.NewCostCalculator()
	wrapper := &enhancedBrainWrapper{
		client:         mockClient,
		config:         Config{Model: ModelConfig{Model: "gpt-4o"}},
		metrics:        metricsCollector,
		costCalculator: costCalc,
	}

	var result struct {
		Result string `json:"result"`
	}
	err := wrapper.Analyze(context.Background(), "analyze this", &result)
	require.NoError(t, err)
	assert.Equal(t, "mock", result.Result)
}

func TestEnhancedBrainWrapper_RecordMetrics_ErrorPath(t *testing.T) {
	expectedErr := fmt.Errorf("chat error")
	mockClient := &mockLLMClientForBrain{
		chatFn: func(ctx context.Context, prompt string) (string, error) {
			return "", expectedErr
		},
	}
	metricsCollector := llm.NewMetricsCollector(llm.MetricsConfig{
		Enabled:           true,
		ServiceName:       "test",
		MaxLatencySamples: 1000,
	})
	costCalc := llm.NewCostCalculator()
	wrapper := &enhancedBrainWrapper{
		client:         mockClient,
		config:         Config{Model: ModelConfig{Model: "gpt-4o"}},
		metrics:        metricsCollector,
		costCalculator: costCalc,
	}

	_, err := wrapper.Chat(context.Background(), "test")
	assert.Error(t, err)

	stats := wrapper.GetMetrics()
	// Error requests still get recorded
	assert.Greater(t, stats.TotalRequests, int64(0))
}

// ========================================
// GetMetrics with metrics
// ========================================

func TestEnhancedBrainWrapper_GetMetrics_WithMetrics(t *testing.T) {
	metricsCollector := llm.NewMetricsCollector(llm.MetricsConfig{
		Enabled:           true,
		ServiceName:       "test-service",
		MaxLatencySamples: 1000,
	})
	wrapper := &enhancedBrainWrapper{
		metrics: metricsCollector,
	}

	stats := wrapper.GetMetrics()
	assert.Equal(t, int64(0), stats.TotalRequests)
}

// ========================================
// AnalyzeWithModel error path
// ========================================

func TestEnhancedBrainWrapper_AnalyzeWithModel_Error(t *testing.T) {
	expectedErr := fmt.Errorf("analyze error")
	mockClient := &mockLLMClientForBrain{
		analyzeFn: func(ctx context.Context, prompt string, target any) error {
			return expectedErr
		},
	}
	wrapper := &enhancedBrainWrapper{
		client: mockClient,
		config: Config{Model: ModelConfig{Model: "gpt-4o"}},
	}

	var result struct{}
	err := wrapper.AnalyzeWithModel(context.Background(), "", "analyze", &result)
	assert.Error(t, err)
	assert.Equal(t, expectedErr, err)
}

// ========================================
// GetRouter / GetRateLimiter with values
// ========================================

func TestEnhancedBrainWrapper_GetRouter_WithValue(t *testing.T) {
	router := llm.NewRouter(llm.RouterConfig{
		DefaultStrategy: llm.StrategyCostPriority,
		Logger:          slog.Default(),
	}, nil)
	wrapper := &enhancedBrainWrapper{
		router: router,
	}

	result := wrapper.GetRouter()
	assert.NotNil(t, result)
}

func TestEnhancedBrainWrapper_GetRateLimiter_WithValue(t *testing.T) {
	rl := llm.NewRateLimiter(llm.RateLimitConfig{
		RequestsPerSecond: 10,
		BurstSize:         20,
	})
	defer rl.Close()
	wrapper := &enhancedBrainWrapper{
		rateLimiter: rl,
	}

	result := wrapper.GetRateLimiter()
	assert.NotNil(t, result)
}

// ========================================
// brain.go global functions
// ========================================

func TestGetRouter_WithRoutableBrain(t *testing.T) {
	oldBrain := globalBrain
	defer func() { globalBrain = oldBrain }()

	router := llm.NewRouter(llm.RouterConfig{
		DefaultStrategy: llm.StrategyCostPriority,
		Logger:          slog.Default(),
	}, nil)
	wrapper := &enhancedBrainWrapper{
		router: router,
	}
	globalBrain = wrapper

	result := GetRouter()
	assert.NotNil(t, result)
}

func TestGetRouter_NonRoutableBrain(t *testing.T) {
	oldBrain := globalBrain
	defer func() { globalBrain = oldBrain }()

	globalBrain = &mockBrainForGuard{}

	result := GetRouter()
	assert.Nil(t, result)
}

func TestGetRateLimiter_WithBrain(t *testing.T) {
	oldBrain := globalBrain
	defer func() { globalBrain = oldBrain }()

	rl := llm.NewRateLimiter(llm.RateLimitConfig{
		RequestsPerSecond: 10,
		BurstSize:         20,
	})
	defer rl.Close()
	wrapper := &enhancedBrainWrapper{
		rateLimiter: rl,
	}
	globalBrain = wrapper

	result := GetRateLimiter()
	assert.NotNil(t, result)
}

func TestGetRateLimiter_NonMatchingBrain(t *testing.T) {
	oldBrain := globalBrain
	defer func() { globalBrain = oldBrain }()

	globalBrain = &mockBrainForGuard{}

	result := GetRateLimiter()
	assert.Nil(t, result)
}

// ========================================
// ChatStream with nil return and metrics
// ========================================

func TestEnhancedBrainWrapper_ChatStream_NilStreamWithMetrics(t *testing.T) {
	mockClient := &mockLLMClientForBrain{
		streamFn: func(ctx context.Context, prompt string) (<-chan string, error) {
			return nil, nil
		},
	}
	metricsCollector := llm.NewMetricsCollector(llm.MetricsConfig{
		Enabled:           true,
		ServiceName:       "test",
		MaxLatencySamples: 1000,
	})
	costCalc := llm.NewCostCalculator()
	wrapper := &enhancedBrainWrapper{
		client:         mockClient,
		config:         Config{Model: ModelConfig{Model: "gpt-4o"}},
		metrics:        metricsCollector,
		costCalculator: costCalc,
	}

	stream, err := wrapper.ChatStream(context.Background(), "hello")
	require.NoError(t, err)

	tokens := []string{}
	if stream != nil {
		for token := range stream {
			tokens = append(tokens, token)
		}
	}
	assert.Empty(t, tokens)
}

func TestEnhancedBrainWrapper_ChatStream_StreamErrorWithMetrics(t *testing.T) {
	expectedErr := fmt.Errorf("stream error")
	mockClient := &mockLLMClientForBrain{
		streamFn: func(ctx context.Context, prompt string) (<-chan string, error) {
			return nil, expectedErr
		},
	}
	metricsCollector := llm.NewMetricsCollector(llm.MetricsConfig{
		Enabled:           true,
		ServiceName:       "test",
		MaxLatencySamples: 1000,
	})
	wrapper := &enhancedBrainWrapper{
		client:  mockClient,
		config:  Config{Model: ModelConfig{Model: "gpt-4o"}},
		metrics: metricsCollector,
	}

	_, err := wrapper.ChatStream(context.Background(), "hello")
	assert.Error(t, err)
}

// ========================================
// parseRouterModels edge cases
// ========================================

func TestParseRouterModels_InvalidCostFields(t *testing.T) {
	// Non-numeric cost fields should default to 0
	result := parseRouterModels("model:provider:notanumber:notanumber:notanumber")
	require.Len(t, result, 1)
	assert.Equal(t, "model", result[0].Name)
	assert.Equal(t, float64(0), result[0].CostPer1KInput)
	assert.Equal(t, float64(0), result[0].CostPer1KOutput)
	assert.Equal(t, int64(0), result[0].AvgLatencyMs)
}

func TestParseRouterModels_EmptyParts(t *testing.T) {
	// Empty parts between semicolons should be skipped
	result := parseRouterModels(";model:provider:0.01:0.02:100;;")
	require.Len(t, result, 1)
	assert.Equal(t, "model", result[0].Name)
}

func TestParseRouterModels_MultipleValidAndInvalid(t *testing.T) {
	result := parseRouterModels("valid:prov:0.01:0.02:100;invalid;another:prov:0.03:0.04:200")
	require.Len(t, result, 2)
	assert.Equal(t, "valid", result[0].Name)
	assert.Equal(t, "another", result[1].Name)
}
