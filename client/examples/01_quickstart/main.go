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
	"github.com/hrygo/hotplex/client/examples/internal/demo"
)

func main() {
	gatewayURL := demo.EnvOr("HOTPLEX_GATEWAY_URL", "ws://localhost:8888/ws")
	apiKey := demo.EnvOr("HOTPLEX_API_KEY", "test-api-key")
	task := demo.EnvOr("HOTPLEX_TASK", "What is 2+2? Answer in one sentence.")

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

	fmt.Printf("\n🚀 HotPlex Gateway - Quick Start\n")
	fmt.Printf("--------------------------------\n")
	fmt.Printf("Connected  session=%s  state=%s\n\n", ack.SessionID, ack.State)

	// Background listener for deltas only.
	go func() {
		for evt := range c.Events() {
			if evt.Type == client.EventMessageDelta {
				if d, ok := evt.AsMessageDeltaData(); ok {
					fmt.Print(d.Content)
				}
			}
		}
	}()

	fmt.Printf("> %s\n", task)
	doneData, err := c.SendInputAsync(ctx, task)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\n\n❌ Error: %v\n", err)
		return
	}

	fmt.Printf("\n\n✅ Task completed: success=%v\n", doneData.Success)
	if doneData.Stats != nil {
		fmt.Printf("   Duration: %vms\n", doneData.Stats["duration_ms"])
		fmt.Printf("   Tokens:   %v\n", doneData.Stats["total_tokens"])
		fmt.Printf("   Cost:     $%v\n", doneData.Stats["cost_usd"])
	}
}
