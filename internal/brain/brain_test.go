package brain

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBrainInterface_Compatibility(t *testing.T) {
	// Test that enhancedBrainWrapper satisfies the Brain interface
	var _ Brain = (*enhancedBrainWrapper)(nil)
}

func TestStreamingBrainInterface_Extension(t *testing.T) {
	// Test that enhancedBrainWrapper satisfies the StreamingBrain interface
	var _ StreamingBrain = (*enhancedBrainWrapper)(nil)
}

func TestRoutableBrainInterface(t *testing.T) {
	// Test that enhancedBrainWrapper satisfies the RoutableBrain interface
	var _ RoutableBrain = (*enhancedBrainWrapper)(nil)
}

func TestObservableBrainInterface(t *testing.T) {
	// Test that enhancedBrainWrapper satisfies the ObservableBrain interface
	var _ ObservableBrain = (*enhancedBrainWrapper)(nil)
}

func TestConfig_LoadFromEnv(t *testing.T) {
	// Set test environment variables
	_ = os.Setenv("HOTPLEX_BRAIN_API_KEY", "test-key")
	_ = os.Setenv("HOTPLEX_BRAIN_PROVIDER", "openai")
	_ = os.Setenv("HOTPLEX_BRAIN_MODEL", "gpt-4o")
	_ = os.Setenv("HOTPLEX_BRAIN_TIMEOUT_S", "30")
	_ = os.Setenv("HOTPLEX_BRAIN_CACHE_SIZE", "500")
	_ = os.Setenv("HOTPLEX_BRAIN_MAX_RETRIES", "5")
	_ = os.Setenv("HOTPLEX_BRAIN_RETRY_MIN_WAIT_MS", "200")
	_ = os.Setenv("HOTPLEX_BRAIN_RETRY_MAX_WAIT_MS", "3000")

	defer func() {
		_ = os.Unsetenv("HOTPLEX_BRAIN_API_KEY")
		_ = os.Unsetenv("HOTPLEX_BRAIN_PROVIDER")
		_ = os.Unsetenv("HOTPLEX_BRAIN_MODEL")
		_ = os.Unsetenv("HOTPLEX_BRAIN_TIMEOUT_S")
		_ = os.Unsetenv("HOTPLEX_BRAIN_CACHE_SIZE")
		_ = os.Unsetenv("HOTPLEX_BRAIN_MAX_RETRIES")
		_ = os.Unsetenv("HOTPLEX_BRAIN_RETRY_MIN_WAIT_MS")
		_ = os.Unsetenv("HOTPLEX_BRAIN_RETRY_MAX_WAIT_MS")
	}()

	config := LoadConfigFromEnv()

	assert.True(t, config.Enabled)
	assert.Equal(t, "openai", config.Model.Provider)
	assert.Equal(t, "gpt-4o", config.Model.Model)
	assert.Equal(t, 30, config.Model.TimeoutS)
	assert.Equal(t, 500, config.Cache.Size)
	assert.Equal(t, 5, config.Retry.MaxAttempts)
	assert.Equal(t, 200, config.Retry.MinWaitMs)
	assert.Equal(t, 3000, config.Retry.MaxWaitMs)
}

func TestConfig_DefaultValues(t *testing.T) {
	// Clear environment variables
	_ = os.Unsetenv("HOTPLEX_BRAIN_API_KEY")
	_ = os.Unsetenv("HOTPLEX_BRAIN_PROVIDER")
	_ = os.Unsetenv("HOTPLEX_BRAIN_MODEL")
	_ = os.Unsetenv("HOTPLEX_BRAIN_TIMEOUT_S")
	_ = os.Unsetenv("HOTPLEX_BRAIN_CACHE_SIZE")
	_ = os.Unsetenv("HOTPLEX_BRAIN_MAX_RETRIES")
	_ = os.Unsetenv("HOTPLEX_BRAIN_RETRY_MIN_WAIT_MS")
	_ = os.Unsetenv("HOTPLEX_BRAIN_RETRY_MAX_WAIT_MS")
	_ = os.Unsetenv("ANTHROPIC_API_KEY")
	_ = os.Unsetenv("OPENAI_API_KEY")
	_ = os.Unsetenv("DEEPSEEK_API_KEY")
	_ = os.Unsetenv("HOTPLEX_PROVIDER_TYPE")           // Prevent CLI extractor interference
	_ = os.Setenv("HOTPLEX_BRAIN_API_KEY", "test-key") // Required to use HOTPLEX_BRAIN_*
	_ = os.Setenv("HOTPLEX_BRAIN_PROVIDER", "openai")  // Ensure predictable provider for default test

	config := LoadConfigFromEnv()

	assert.True(t, config.Enabled) // Enabled when API key is set
	assert.Equal(t, "openai", config.Model.Provider)
	assert.Equal(t, "gpt-4o", config.Model.Model)
	assert.Equal(t, 30, config.Model.TimeoutS)
	assert.Equal(t, 1000, config.Cache.Size)
	assert.Equal(t, 3, config.Retry.MaxAttempts)
	assert.Equal(t, 100, config.Retry.MinWaitMs)
	assert.Equal(t, 5000, config.Retry.MaxWaitMs)
}

func TestGlobalBrain_Access(t *testing.T) {
	// Test global brain accessors
	assert.Nil(t, Global(), "global brain should be nil initially")

	// Create a mock brain
	mockBrain := &mockBrain{}
	SetGlobal(mockBrain)

	assert.Equal(t, mockBrain, Global(), "global brain should be set")
}

// mockBrain is a simple mock implementation for testing
type mockBrain struct{}

func (m *mockBrain) Chat(ctx context.Context, prompt string) (string, error) {
	return "mock response", nil
}

func (m *mockBrain) Analyze(ctx context.Context, prompt string, target any) error {
	return nil
}

func (m *mockBrain) ChatStream(ctx context.Context, prompt string) (<-chan string, error) {
	ch := make(chan string)
	close(ch)
	return ch, nil
}

func (m *mockBrain) HealthCheck(ctx context.Context) HealthStatus {
	return HealthStatus{Healthy: true}
}

func TestHealthStatus_Structure(t *testing.T) {
	status := HealthStatus{
		Healthy:   true,
		Provider:  "openai",
		Model:     "gpt-4o-mini",
		LatencyMs: 100,
		Error:     "",
	}

	assert.True(t, status.Healthy)
	assert.Equal(t, "openai", status.Provider)
	assert.Equal(t, "gpt-4o-mini", status.Model)
	assert.Equal(t, int64(100), status.LatencyMs)
	assert.Empty(t, status.Error)
}

func TestTimeoutApplication(t *testing.T) {
	// Test that timeout is properly applied in brainWrapper
	mockBrain := &slowMockBrain{}
	SetGlobal(mockBrain)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := Global().Chat(ctx, "test")
	elapsed := time.Since(start)

	// Should timeout before the 1-second sleep completes
	assert.Error(t, err)
	assert.Less(t, elapsed, 500*time.Millisecond, "should timeout quickly")
}

// slowMockBrain simulates a slow brain implementation
type slowMockBrain struct{}

func (m *slowMockBrain) Chat(ctx context.Context, prompt string) (string, error) {
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-time.After(1 * time.Second):
		return "response", nil
	}
}

func (m *slowMockBrain) Analyze(ctx context.Context, prompt string, target any) error {
	_, err := m.Chat(ctx, prompt)
	return err
}

func (m *slowMockBrain) ChatStream(ctx context.Context, prompt string) (<-chan string, error) {
	ch := make(chan string)
	close(ch)
	return ch, nil
}

func (m *slowMockBrain) HealthCheck(ctx context.Context) HealthStatus {
	return HealthStatus{Healthy: true}
}
