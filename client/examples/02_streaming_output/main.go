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
	"github.com/hrygo/hotplex/client/examples/internal/demo"
)

func main() {
	gatewayURL := demo.EnvOr("HOTPLEX_GATEWAY_URL", "ws://localhost:8888/ws")
	apiKey := demo.EnvOr("HOTPLEX_API_KEY", "test-api-key")
	task := demo.EnvOr("HOTPLEX_TASK", "Explain what WebSocket is in 3 sentences.")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sigCh; cancel() }()

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
				fmt.Printf("[message #%d start — role: %s]\n", msgCount, demo.FieldStr(evt.Data, "role"))
			case client.EventMessageDelta:
				deltaN++
				content := demo.FieldStr(evt.Data, "content")
				fmt.Print(content)
				buf.WriteString(content)
			case client.EventMessageEnd:
				fmt.Printf("\n[message #%d end — %d deltas received]\n", msgCount, deltaN)
				deltaN = 0
			case client.EventDone:
				fmt.Printf("\n--- Summary ---\nFull response (%d chars):\n%s\n", buf.Len(), buf.String())
				return
			case client.EventError:
				fmt.Fprintf(os.Stderr, "Error: %s\n", demo.FieldStr(evt.Data, "message"))
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
