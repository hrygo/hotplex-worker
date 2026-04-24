// 06_error_handling — Robust error handling patterns: error classification, timeout, retry.
//
// Usage:
//
//	HOTPLEX_API_KEY=test-api-key go run ./06_error_handling
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
	task := demo.EnvOr("HOTPLEX_TASK", "Say hello in one sentence.")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sigCh; cancel() }()

	// NOTE: For simple use cases, client.AutoReconnect(true) handles transparent
	// reconnection. This example focuses on error classification patterns.
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
	fmt.Printf("Connected — session=%s\n\n", ack.SessionID)

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
				fmt.Print(demo.FieldStr(evt.Data, "content"))
			case client.EventDone:
				fmt.Println("\nDone.")
				return
			case client.EventError:
				errData, ok := evt.AsErrorData()
				if !ok {
					fmt.Fprintf(os.Stderr, "\nGeneric Error: %v\n", evt.Data)
					return
				}
				switch errData.Code {
				case client.ErrCodeSessionBusy:
					fmt.Fprintf(os.Stderr, "\nRecoverable: session busy — %s\n", errData.Message)
				case client.ErrCodeUnauthorized:
					fmt.Fprintf(os.Stderr, "\nFatal: unauthorized — %s\n", errData.Message)
					return
				case client.ErrCodeSessionNotFound:
					fmt.Fprintf(os.Stderr, "\nFatal: session not found — %s\n", errData.Message)
					return
				default:
					fmt.Fprintf(os.Stderr, "\nError [%s]: %s\n", errData.Code, errData.Message)
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
