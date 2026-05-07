package llm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockLLMClient is a mock implementation for testing.
type MockLLMClient struct {
	mock.Mock
}

func (m *MockLLMClient) Chat(ctx context.Context, prompt string) (string, error) {
	args := m.Called(ctx, prompt)
	return args.String(0), args.Error(1)
}

func (m *MockLLMClient) ChatWithOptions(ctx context.Context, prompt string, opts ChatOptions) (string, error) {
	args := m.Called(ctx, prompt, opts)
	return args.String(0), args.Error(1)
}

func (m *MockLLMClient) Analyze(ctx context.Context, prompt string, target any) error {
	args := m.Called(ctx, prompt, target)
	return args.Error(0)
}

func (m *MockLLMClient) ChatStream(ctx context.Context, prompt string) (<-chan string, error) {
	args := m.Called(ctx, prompt)
	return args.Get(0).(<-chan string), args.Error(1)
}

func (m *MockLLMClient) HealthCheck(ctx context.Context) HealthStatus {
	args := m.Called(ctx)
	return args.Get(0).(HealthStatus)
}

func TestRetryClient_SuccessOnFirstTry(t *testing.T) {
	t.Parallel()
	mockClient := new(MockLLMClient)
	mockClient.On("Chat", mock.Anything, "test prompt").Return("success", nil)

	retryClient := NewRetryClient(mockClient, 3, 100, 1000)

	result, err := retryClient.Chat(context.Background(), "test prompt")

	assert.NoError(t, err)
	assert.Equal(t, "success", result)
	mockClient.AssertExpectations(t)
}

func TestRetryClient_SuccessAfterRetry(t *testing.T) {
	t.Parallel()
	mockClient := new(MockLLMClient)
	mockClient.On("Chat", mock.Anything, "test prompt").Return("", assert.AnError).Once()
	mockClient.On("Chat", mock.Anything, "test prompt").Return("success", nil)

	retryClient := NewRetryClient(mockClient, 3, 10, 100)

	result, err := retryClient.Chat(context.Background(), "test prompt")

	assert.NoError(t, err)
	assert.Equal(t, "success", result)
	mockClient.AssertExpectations(t)
}

func TestRetryClient_ExhaustsRetries(t *testing.T) {
	t.Parallel()
	mockClient := new(MockLLMClient)
	mockClient.On("Chat", mock.Anything, "test prompt").Return("", assert.AnError).Times(4)

	retryClient := NewRetryClient(mockClient, 3, 10, 100)

	_, err := retryClient.Chat(context.Background(), "test prompt")

	assert.Error(t, err)
	mockClient.AssertExpectations(t)
}

func TestRetryClient_NoRetriesWhenDisabled(t *testing.T) {
	t.Parallel()
	mockClient := new(MockLLMClient)
	mockClient.On("Chat", mock.Anything, "test prompt").Return("", assert.AnError).Once()

	retryClient := NewRetryClient(mockClient, 0, 100, 1000)

	_, err := retryClient.Chat(context.Background(), "test prompt")

	assert.Error(t, err)
	mockClient.AssertExpectations(t)
}

func TestCachedClient_CacheHit(t *testing.T) {
	t.Parallel()
	mockClient := new(MockLLMClient)
	mockClient.On("Chat", mock.Anything, "test prompt").Return("cached response", nil).Once()

	cachedClient := NewCachedClient(mockClient, 100)

	// First call - cache miss
	result1, err := cachedClient.Chat(context.Background(), "test prompt")
	assert.NoError(t, err)
	assert.Equal(t, "cached response", result1)

	// Second call - cache hit
	result2, err := cachedClient.Chat(context.Background(), "test prompt")
	assert.NoError(t, err)
	assert.Equal(t, "cached response", result2)

	mockClient.AssertExpectations(t)
}

func TestCachedClient_CacheMiss(t *testing.T) {
	t.Parallel()
	mockClient := new(MockLLMClient)
	mockClient.On("Chat", mock.Anything, "prompt1").Return("response1", nil)
	mockClient.On("Chat", mock.Anything, "prompt2").Return("response2", nil)

	cachedClient := NewCachedClient(mockClient, 100)

	result1, _ := cachedClient.Chat(context.Background(), "prompt1")
	result2, _ := cachedClient.Chat(context.Background(), "prompt2")

	assert.Equal(t, "response1", result1)
	assert.Equal(t, "response2", result2)
	mockClient.AssertExpectations(t)
}

func TestCachedClient_ClearCache(t *testing.T) {
	t.Parallel()
	mockClient := new(MockLLMClient)
	mockClient.On("Chat", mock.Anything, "test prompt").Return("response", nil).Twice()

	cachedClient := NewCachedClient(mockClient, 100)

	// First call - cache miss
	_, _ = cachedClient.Chat(context.Background(), "test prompt")

	// Clear cache
	cachedClient.ClearCache()

	// Second call - cache miss again
	_, _ = cachedClient.Chat(context.Background(), "test prompt")

	mockClient.AssertExpectations(t)
}

func TestCachedClient_MakeKey_UsesHash(t *testing.T) {
	t.Parallel()
	mockClient := new(MockLLMClient)
	cachedClient := NewCachedClient(mockClient, 100)

	key1 := cachedClient.makeKey("hello world", false)
	key2 := cachedClient.makeKey("hello world", false)
	key3 := cachedClient.makeKey("hello world", true)

	// Same prompt should produce same key
	assert.Equal(t, key1, key2)
	// Key should NOT be the raw prompt (security: no plaintext in memory maps)
	assert.NotEqual(t, "hello world", key1)
	// Key should have "chat:" prefix for non-analyze
	assert.Contains(t, key1, "chat:")
	// Key should have "analyze:" prefix for analyze
	assert.Contains(t, key3, "analyze:")
	// Chat and analyze keys should differ for same prompt
	assert.NotEqual(t, key1, key3)
}
