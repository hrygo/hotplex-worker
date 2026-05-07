package llm

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/sashabaranov/go-openai"
)

// ChatStream returns a channel that streams tokens as they are generated.
// The channel is closed when the stream completes or an error occurs.
func (c *OpenAIClient) ChatStream(ctx context.Context, prompt string) (<-chan string, error) {
	stream, err := c.client.CreateChatCompletionStream(
		ctx,
		openai.ChatCompletionRequest{
			Model: c.model,
			Messages: []openai.ChatCompletionMessage{
				{
					Role:    openai.ChatMessageRoleUser,
					Content: prompt,
				},
			},
			Stream: true,
		},
	)

	if err != nil {
		return nil, fmt.Errorf("openai stream error: %w", err)
	}

	ch := make(chan string)

	go func() {
		defer close(ch)
		defer func() { _ = stream.Close() }()

		for {
			response, err := stream.Recv()
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				c.logger.Error("stream receive error", "error", err)
				return
			}

			if len(response.Choices) > 0 && response.Choices[0].Delta.Content != "" {
				select {
				case ch <- response.Choices[0].Delta.Content:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	return ch, nil
}
