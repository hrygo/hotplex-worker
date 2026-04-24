// 01_quickstart — Minimum viable example: connect, send one message, print response, exit.
//
// Usage:
//
//	HOTPLEX_API_KEY=test-api-key go run ./01_quickstart
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	client "github.com/hrygo/hotplex/client"
)

func main() {
	gatewayURL := envOr("HOTPLEX_GATEWAY_URL", "ws://localhost:8888/ws")
	apiKey := envOr("HOTPLEX_API_KEY", "test-api-key")
	task := envOr("HOTPLEX_TASK", "What is 2+2? Answer in one sentence.")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nInterrupted.")
		cancel()
	}()

	c, err := client.New(ctx,
		client.URL(gatewayURL),
		client.WorkerType("claude_code"),
		client.APIKey(apiKey),
		client.AutoReconnect(true),
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
	fmt.Printf("Connected  session=%s  state=%s\n", ack.SessionID, ack.State)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range c.Events() {
			switch evt.Type {
			case client.EventMessageDelta:
				if d, ok := evt.Data.(map[string]any); ok {
					fmt.Print(d["content"])
				}
			case client.EventDone:
				if done, ok := evt.AsDoneData(); ok {
					fmt.Printf("\nDone (success=%v).\n", done.Success)
				}
				return
			case client.EventError:
				if errData, ok := evt.AsErrorData(); ok {
					fmt.Fprintf(os.Stderr, "\nError: %s — %s\n", errData.Code, errData.Message)
				}
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
		if ctx.Err() == context.DeadlineExceeded {
			fmt.Fprintln(os.Stderr, "Timeout.")
		}
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
