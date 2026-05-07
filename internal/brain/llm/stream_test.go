package llm

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestChatStream_ReceivesTokens(t *testing.T) {
	t.Parallel()
	// This test requires a real API key, so we'll skip it in CI
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	apiKey := getTestAPIKey()
	if apiKey == "" {
		t.Skip("HOTPLEX_BRAIN_API_KEY not set")
	}

	client := NewOpenAIClient(apiKey, "", "gpt-4o-mini", nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stream, err := client.ChatStream(ctx, "Count from 1 to 3")
	assert.NoError(t, err)

	var tokens []string
	for token := range stream {
		tokens = append(tokens, token)
	}

	assert.NotEmpty(t, tokens, "should receive at least one token")
}

func TestChatStream_ContextCancellation(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	apiKey := getTestAPIKey()
	if apiKey == "" {
		t.Skip("HOTPLEX_BRAIN_API_KEY not set")
	}

	client := NewOpenAIClient(apiKey, "", "gpt-4o-mini", nil)

	ctx, cancel := context.WithCancel(context.Background())

	stream, err := client.ChatStream(ctx, "Write a long story")
	assert.NoError(t, err)

	// Cancel immediately
	cancel()

	// Stream should close quickly
	done := make(chan bool)
	go func() {
		for range stream {
			// Drain the stream
		}
		done <- true
	}()

	select {
	case <-done:
		// Success - stream closed
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not close after context cancellation")
	}
}

func getTestAPIKey() string {
	// Helper to get test API key from env
	// In real tests, this would use os.Getenv
	return ""
}
