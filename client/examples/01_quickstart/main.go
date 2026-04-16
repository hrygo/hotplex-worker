// 01_quickstart — Minimum viable example: connect, send one message, print response, exit.
//
// Usage:
//
//	HOTPLEX_API_KEY=test-api-key go run ./01_quickstart
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hotplex/hotplex-go-client"
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
	)
	if err != nil {
		log.Fatalf("create client: %v", err)
	}
	defer c.Close()

	ack, err := c.Connect(ctx)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	fmt.Printf("Connected  session=%s  state=%s\n", ack.SessionID, ack.State)

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
				fmt.Fprintf(os.Stderr, "\nError: %s — %s\n",
					fieldStr(evt.Data, "code"), fieldStr(evt.Data, "message"))
				return
			}
		}
	}()

	fmt.Printf("> %s\n", task)
	if err := c.SendInput(ctx, task); err != nil {
		log.Fatalf("send input: %v", err)
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
