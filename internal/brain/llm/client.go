package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/sashabaranov/go-openai"
)

// LLMClient defines the interface for LLM interactions.
// All client wrappers must implement this interface.
type LLMClient interface {
	Chat(ctx context.Context, prompt string) (string, error)
	Analyze(ctx context.Context, prompt string, target any) error
	ChatStream(ctx context.Context, prompt string) (<-chan string, error)
	HealthCheck(ctx context.Context) HealthStatus
}

// Compile-time interface compliance verification.
var (
	_ LLMClient = (*OpenAIClient)(nil)
	_ LLMClient = (*AnthropicClient)(nil)
	_ LLMClient = (*RateLimitedClient)(nil)
	_ LLMClient = (*CachedClient)(nil)
	_ LLMClient = (*RetryClient)(nil)
	_ LLMClient = (*CircuitClient)(nil)
	_ LLMClient = (*MetricsClient)(nil)
	_ LLMClient = (*BudgetClient)(nil)
)

// OpenAIClient implements OpenAI-compatible LLM interactions.
// It can be used for OpenAI, DeepSeek, Groq, etc.
type OpenAIClient struct {
	client *openai.Client
	model  string
	logger *slog.Logger
}

// HealthStatus represents the health status of an LLM client.
// Exported for use by the brain package.
type HealthStatus struct {
	Healthy   bool
	Provider  string
	Model     string
	LatencyMs int64
	Error     string
}

// HealthChecker provides health check capability.
type HealthChecker interface {
	HealthCheck(ctx context.Context) HealthStatus
}

// NewOpenAIClient creates a new OpenAI compatible client.
func NewOpenAIClient(apiKey, endpoint, model string, logger *slog.Logger) *OpenAIClient {
	config := openai.DefaultConfig(apiKey)
	if endpoint != "" {
		config.BaseURL = endpoint
	}

	return &OpenAIClient{
		client: openai.NewClientWithConfig(config),
		model:  model,
		logger: logger,
	}
}

// HealthCheck performs a simple health check by making a minimal API call.
func (c *OpenAIClient) HealthCheck(ctx context.Context) HealthStatus {
	start := time.Now()

	// Simple ping with minimal prompt
	_, err := c.Chat(ctx, "ping")
	latency := time.Since(start).Milliseconds()

	status := HealthStatus{
		Provider:  "openai",
		Model:     c.model,
		LatencyMs: latency,
	}

	if err != nil {
		status.Healthy = false
		status.Error = err.Error()
	} else {
		status.Healthy = true
	}

	return status
}

// Chat generates a simple plain text completion.
func (c *OpenAIClient) Chat(ctx context.Context, prompt string) (string, error) {
	resp, err := c.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: c.model,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
		},
	)

	if err != nil {
		return "", fmt.Errorf("openai chat error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("zero choices in response")
	}

	return resp.Choices[0].Message.Content, nil
}

// Analyze requests JSON formatted output from the model.
// It uses "ResponseFormat: {Type: JSON_OBJECT}" to ensure model compatibility for structured reasoning.
func (c *OpenAIClient) Analyze(ctx context.Context, prompt string, target any) error {
	// Instruct the model to return JSON if it's not explicitly in the prompt
	if !strings.Contains(strings.ToLower(prompt), "json") {
		prompt = prompt + "\n\nIMPORTANT: Return ONLY valid JSON."
	}

	resp, err := c.client.CreateChatCompletion(
		ctx,
		openai.ChatCompletionRequest{
			Model: c.model,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
			ResponseFormat: &openai.ChatCompletionResponseFormat{
				Type: openai.ChatCompletionResponseFormatTypeJSONObject,
			},
		},
	)

	if err != nil {
		return fmt.Errorf("openai analyze error: %w", err)
	}

	if len(resp.Choices) == 0 {
		return fmt.Errorf("zero choices in response")
	}

	content := resp.Choices[0].Message.Content
	err = json.Unmarshal([]byte(content), target)
	if err != nil {
		return fmt.Errorf("failed to unmarshal JSON content: %w. CONTENT: %s", err, content)
	}

	return nil
}
