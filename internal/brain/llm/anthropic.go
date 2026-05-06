package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicClient implements Anthropic-specific LLM interactions using the official SDK.
type AnthropicClient struct {
	client anthropic.Client
	model  string
	logger *slog.Logger
}

// Ensure AnthropicClient implements the LLMClient interface.
var _ LLMClient = (*AnthropicClient)(nil)

// NewAnthropicClient creates a new Anthropic client using the official SDK.
func NewAnthropicClient(apiKey, endpoint, model string, logger *slog.Logger) *AnthropicClient {
	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
	}
	if endpoint != "" {
		opts = append(opts, option.WithBaseURL(endpoint))
	}

	c := anthropic.NewClient(opts...)

	return &AnthropicClient{
		client: c,
		model:  model,
		logger: logger,
	}
}

// Chat generates a simple plain text completion using the Messages API.
func (c *AnthropicClient) Chat(ctx context.Context, prompt string) (string, error) {
	params := anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: 4096,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
		},
	}

	resp, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return "", fmt.Errorf("anthropic chat error: %w", err)
	}

	if len(resp.Content) == 0 {
		return "", fmt.Errorf("zero content in response")
	}

	var result strings.Builder
	for _, block := range resp.Content {
		if block.Type == "text" {
			result.WriteString(block.Text)
		}
	}

	return result.String(), nil
}

// Analyze requests JSON formatted output.
func (c *AnthropicClient) Analyze(ctx context.Context, prompt string, target any) error {
	if !strings.Contains(strings.ToLower(prompt), "json") {
		prompt += "\n\nIMPORTANT: Return ONLY valid JSON."
	}

	respText, err := c.Chat(ctx, prompt)
	if err != nil {
		return err
	}

	cleaned := strings.TrimSpace(respText)
	if strings.HasPrefix(cleaned, "```") {
		lines := strings.Split(cleaned, "\n")
		if len(lines) >= 2 && (strings.HasPrefix(lines[0], "```json") || strings.HasPrefix(lines[0], "```")) {
			cleaned = strings.Join(lines[1:len(lines)-1], "\n")
		}
	}

	err = json.Unmarshal([]byte(cleaned), target)
	if err != nil {
		return fmt.Errorf("failed to unmarshal JSON content: %w. CONTENT: %s", err, cleaned)
	}

	return nil
}

// ChatStream returns a channel that streams tokens as they are generated.
func (c *AnthropicClient) ChatStream(ctx context.Context, prompt string) (<-chan string, error) {
	streamChan := make(chan string)

	go func() {
		defer close(streamChan)

		params := anthropic.MessageNewParams{
			Model:     c.model,
			MaxTokens: 4096,
			Messages: []anthropic.MessageParam{
				anthropic.NewUserMessage(anthropic.NewTextBlock(prompt)),
			},
		}

		stream := c.client.Messages.NewStreaming(ctx, params)
		defer func() { _ = stream.Close() }()

		for stream.Next() {
			event := stream.Current()
			if event.Type == "content_block_delta" {
				// The SDK uses a union for Delta.
				// We need to use the Text field if available.
				if event.Delta.Text != "" {
					streamChan <- event.Delta.Text
				}
			}
		}

		if err := stream.Err(); err != nil {
			c.logger.Error("Anthropic stream error", "error", err)
		}
	}()

	return streamChan, nil
}

// HealthCheck performs a simple health check.
func (c *AnthropicClient) HealthCheck(ctx context.Context) HealthStatus {
	start := time.Now()
	_, err := c.Chat(ctx, "ping")
	latency := time.Since(start).Milliseconds()

	status := HealthStatus{
		Healthy:   err == nil,
		Provider:  "anthropic",
		Model:     c.model,
		LatencyMs: latency,
	}
	if err != nil {
		status.Error = err.Error()
	}
	return status
}
