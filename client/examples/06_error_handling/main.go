// 06_error_handling — Robust error handling patterns: retry, timeout, error classification.
//
// Usage:
//
//	HOTPLEX_API_KEY=test-api-key go run ./06_error_handling
package main

import (
	"context"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hotplex/hotplex-go-client"
)

func main() {
	gatewayURL := envOr("HOTPLEX_GATEWAY_URL", "ws://localhost:8888/ws")
	apiKey := envOr("HOTPLEX_API_KEY", "test-api-key")
	task := envOr("HOTPLEX_TASK", "Say hello in one sentence.")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sigCh; cancel() }()

	// Connect with retry + exponential backoff.
	c, err := connectWithRetry(ctx, gatewayURL, apiKey, 3)
	if err != nil {
		log.Fatalf("failed to connect after retries: %v", err)
	}
	defer c.Close()

	// Send with per-operation timeout.
	sendCtx, sendCancel := context.WithTimeout(ctx, 15*time.Second)
	if err := c.SendInput(sendCtx, task); err != nil {
		sendCancel()
		if sendCtx.Err() == context.DeadlineExceeded {
			fmt.Fprintln(os.Stderr, "Send timed out.")
		} else {
			fmt.Fprintf(os.Stderr, "Send failed: %v\n", err)
		}
		return
	}
	sendCancel()
	fmt.Printf("> %s\n\n", task)

	// Event loop with error classification.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range c.Events() {
			switch evt.Type {
			case client.EventMessageDelta:
				fmt.Print(fieldStr(evt.Data, "content"))
			case client.EventDone:
				fmt.Println("\nDone.")
				return
			case client.EventError:
				code := fieldStr(evt.Data, "code")
				msg := fieldStr(evt.Data, "message")
				switch code {
				case string(client.ErrCodeSessionBusy):
					fmt.Fprintf(os.Stderr, "\nRecoverable: session busy — %s\n", msg)
				case string(client.ErrCodeUnauthorized):
					fmt.Fprintf(os.Stderr, "\nFatal: unauthorized — %s\n", msg)
					return
				case string(client.ErrCodeSessionNotFound):
					fmt.Fprintf(os.Stderr, "\nFatal: session not found — %s\n", msg)
					return
				default:
					fmt.Fprintf(os.Stderr, "\nError [%s]: %s\n", code, msg)
					return
				}
			}
		}
	}()

	select {
	case <-done:
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, "Context cancelled.")
	}
}

func connectWithRetry(ctx context.Context, gatewayURL, apiKey string, maxAttempts int) (*client.Client, error) {
	var lastErr error
	for attempt := range maxAttempts {
		if attempt > 0 {
			delay := time.Duration(500+rand.IntN(500)) * time.Millisecond * time.Duration(1<<min(attempt-1, 4))
			fmt.Printf("Retry #%d in %v...\n", attempt, delay)
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}

		c, err := client.New(ctx,
			client.URL(gatewayURL),
			client.WorkerType("claude_code"),
			client.APIKey(apiKey),
		)
		if err != nil {
			lastErr = fmt.Errorf("create client: %w", err)
			fmt.Fprintf(os.Stderr, "Attempt %d failed: %v\n", attempt+1, lastErr)
			continue
		}

		ack, err := c.Connect(ctx)
		if err != nil {
			c.Close()
			lastErr = fmt.Errorf("connect: %w", err)
			fmt.Fprintf(os.Stderr, "Attempt %d failed: %v\n", attempt+1, lastErr)
			continue
		}

		fmt.Printf("Connected on attempt %d — session=%s\n", attempt+1, ack.SessionID)
		return c, nil
	}
	return nil, fmt.Errorf("all %d attempts failed, last: %w", maxAttempts, lastErr)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func fieldStr(data any, key string) string {
	m, ok := data.(map[string]any)
	if !ok {
		return ""
	}
	v := m[key]
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
