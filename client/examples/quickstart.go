//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/hotplex/hotplex-go-client"
)

func main() {
	gatewayURL := getEnv("HOTPLEX_GATEWAY_URL", "ws://localhost:8888")
	signingKey := getEnv("HOTPLEX_SIGNING_KEY", "")
	task := getEnv("HOTPLEX_TASK", "What is 2+2? Respond in one sentence.")

	// Generate JWT token.
	gen, err := client.NewTokenGenerator(signingKey)
	if err != nil {
		log.Fatalf("create token generator: %v", err)
	}
	token, err := gen.Generate("example-user", []string{"read", "write"}, 1*time.Hour)
	if err != nil {
		log.Fatalf("generate token: %v", err)
	}

	// Create client.
	ctx := context.Background()
	c, err := client.New(ctx,
		client.URL(gatewayURL),
		client.WorkerType("claude_code"),
		client.AuthToken(token),
	)
	if err != nil {
		log.Fatalf("create client: %v", err)
	}
	defer c.Close()

	// Connect (new session).
	ack, err := c.Connect(ctx)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	fmt.Printf("Session: %s | State: %s\n", ack.SessionID, ack.State)

	// Handle events in background goroutine.
	go func() {
		for evt := range c.Events() {
			switch evt.Kind {
			case client.KindMessageDelta:
				if content, ok := evt.Data.(map[string]any); ok {
					if text, ok := content["content"].(string); ok {
						fmt.Print(text)
					}
				}
			case client.KindDone:
				fmt.Println("\n--- done ---")
				os.Exit(0)
			case client.KindError:
				if err, ok := evt.Data.(map[string]any); ok {
					fmt.Fprintf(os.Stderr, "\nerror: %v\n", err["message"])
				}
				os.Exit(1)
			}
		}
	}()

	// Send input.
	if err := c.SendInput(ctx, task); err != nil {
		log.Fatalf("send input: %v", err)
	}

	// Block forever (events handled in goroutine).
	select {}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
