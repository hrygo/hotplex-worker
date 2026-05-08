package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/sashabaranov/go-openai"
)

// ChatOptions controls LLM generation parameters.
// Zero-value fields use the provider's default.
type ChatOptions struct {
	MaxTokens    int      // 0 = provider default (Anthropic: 4096, OpenAI: API default)
	Temperature  *float64 // nil = provider default; use FloatPtr(0) for deterministic output
	SystemPrompt string   // optional system message; empty = no system message
}

// FloatPtr returns a pointer to the given float64 value.
// Useful for constructing ChatOptions with explicit Temperature values.
//
//nolint:newexpr // &v is the idiomatic way to get a pointer to a specific value
func FloatPtr(v float64) *float64 { return &v }

// LLMClient defines the interface for LLM interactions.
// All client wrappers must implement this interface.
type LLMClient interface {
	Chat(ctx context.Context, prompt string) (string, error)
	ChatWithOptions(ctx context.Context, prompt string, opts ChatOptions) (string, error)
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
	return healthCheckFromChat(ctx, c.Chat, "openai", c.model)
}

// buildMessages constructs the message list with an optional system prompt.
func buildMessages(systemPrompt, userPrompt string) []openai.ChatCompletionMessage {
	if systemPrompt != "" {
		return []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: systemPrompt},
			{Role: openai.ChatMessageRoleUser, Content: userPrompt},
		}
	}
	return []openai.ChatCompletionMessage{
		{Role: openai.ChatMessageRoleUser, Content: userPrompt},
	}
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

func (c *OpenAIClient) ChatWithOptions(ctx context.Context, prompt string, opts ChatOptions) (string, error) {
	msgs := buildMessages(opts.SystemPrompt, prompt)
	req := openai.ChatCompletionRequest{
		Model:    c.model,
		Messages: msgs,
	}
	if opts.MaxTokens > 0 {
		req.MaxTokens = opts.MaxTokens
	}
	if opts.Temperature != nil {
		req.Temperature = float32(*opts.Temperature)
	}

	resp, err := c.client.CreateChatCompletion(ctx, req)
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
	prompt = ensureJSONPrompt(prompt)

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

	content := cleanJSONResponse(resp.Choices[0].Message.Content)
	if err := json.Unmarshal([]byte(content), target); err != nil {
		return formatUnmarshalError(err, content)
	}

	return nil
}
