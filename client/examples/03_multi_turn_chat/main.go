// 03_multi_turn_chat — Interactive multi-turn CLI chat.
//
// Reads user input from stdin in a loop. Demonstrates how a single session
// handles multiple sequential inputs with idle-state gating.
//
// Usage:
//
//	HOTPLEX_API_KEY=test-api-key go run ./03_multi_turn_chat
package main

import (
	"bufio"
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

	ctx, cancel := context.WithCancel(context.Background())
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
		fmt.Fprintf(os.Stderr, "Error: create client: %v\n", err)
		os.Exit(1) //nolint:gocritic // example exit
	}
	defer func() { _ = c.Close() }() //nolint:errcheck // example cleanup

	ack, err := c.Connect(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: connect: %v\n", err)
		return
	}
	fmt.Printf("Chat session: %s\nType a message and press Enter. Type 'exit' or 'quit' to end.\n\n", ack.SessionID)

	ready := make(chan struct{}, 1)
	ready <- struct{}{}

	go func() {
		for evt := range c.Events() {
			switch evt.Type {
			case client.EventMessageDelta:
				if d, ok := evt.Data.(map[string]any); ok {
					fmt.Print(d["content"])
				}
			case client.EventMessageEnd:
				fmt.Println()
			case client.EventState:
				if fieldStr(evt.Data, "state") == string(client.StateIdle) {
					select {
					case ready <- struct{}{}:
					default:
					}
				}
			case client.EventDone:
				if done, ok := evt.AsDoneData(); ok {
					fmt.Printf("\nDone (success=%v).\n", done.Success)
				}
				cancel()
				return
			case client.EventError:
				fmt.Fprintf(os.Stderr, "\nError: %s\n", fieldStr(evt.Data, "message"))
				cancel()
				return
			}
		}
	}()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ready:
		}

		fmt.Print("\n> ")
		if !scanner.Scan() {
			return
		}
		line := scanner.Text()
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" {
			fmt.Println("Bye.")
			return
		}

		sendCtx, sendCancel := context.WithTimeout(ctx, 10*time.Second)
		if err := c.SendInput(sendCtx, line); err != nil {
			sendCancel()
			fmt.Fprintf(os.Stderr, "Send failed: %v\n", err)
			return
		}
		sendCancel()
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
