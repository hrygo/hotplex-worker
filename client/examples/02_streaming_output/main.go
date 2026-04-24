// 02_streaming_output — Assemble streaming message deltas into a complete response.
//
// Demonstrates the full message lifecycle: message.start → message.delta (N) → message.end.
//
// Usage:
//
//	HOTPLEX_API_KEY=test-api-key go run ./02_streaming_output
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	client "github.com/hrygo/hotplex/client"
)

func main() {
	gatewayURL := envOr("HOTPLEX_GATEWAY_URL", "ws://localhost:8888/ws")
	apiKey := envOr("HOTPLEX_API_KEY", "test-api-key")
	task := envOr("HOTPLEX_TASK", "Explain what WebSocket is in 3 sentences.")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	signal.Notify(make(chan os.Signal, 1), syscall.SIGINT, syscall.SIGTERM)

	c, err := client.New(ctx,
		client.URL(gatewayURL),
		client.WorkerType("claude_code"),
		client.APIKey(apiKey),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: create client: %v\n", err)
		os.Exit(1) //nolint:gocritic // example exit
	}
	defer func() { _ = c.Close() }() //nolint:errcheck // example cleanup

	ack, err := c.Connect(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: connect: %v\n", err)
		return
	}
	fmt.Printf("Session: %s\n\n", ack.SessionID)

	var (
		buf      strings.Builder
		deltaN   int
		msgCount int
	)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range c.Events() {
			switch evt.Type {
			case client.EventMessageStart:
				msgCount++
				fmt.Printf("[message #%d start — role: %s]\n", msgCount, fieldStr(evt.Data, "role"))
			case client.EventMessageDelta:
				deltaN++
				content := fieldStr(evt.Data, "content")
				fmt.Print(content)
				buf.WriteString(content)
			case client.EventMessageEnd:
				fmt.Printf("\n[message #%d end — %d deltas received]\n", msgCount, deltaN)
				deltaN = 0
			case client.EventDone:
				fmt.Printf("\n--- Summary ---\nFull response (%d chars):\n%s\n", buf.Len(), buf.String())
				return
			case client.EventError:
				fmt.Fprintf(os.Stderr, "Error: %s\n", fieldStr(evt.Data, "message"))
				return
			}
		}
	}()

	fmt.Printf("> %s\n", task)
	if err := c.SendInput(ctx, task); err != nil {
		fmt.Fprintf(os.Stderr, "Error: send input: %v\n", err)
		return
	}

	select {
	case <-done:
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, "Timeout.")
	}
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
