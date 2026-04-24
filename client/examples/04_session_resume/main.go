// 04_session_resume — Session persistence and resume across connections.
//
// Phase 1: Create a session, send input, capture the session ID.
// Phase 2: Create a NEW client, resume the session by ID, send follow-up input.
//
// Usage:
//
//	HOTPLEX_API_KEY=test-api-key go run ./04_session_resume
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// Phase 1: Create session.
	banner("Phase 1 — Create Session")

	c1, err := client.New(ctx,
		client.URL(gatewayURL),
		client.WorkerType("claude_code"),
		client.APIKey(apiKey),
		client.ClientSessionID("resume-demo-session"),
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: create client 1: %v\n", err)
		os.Exit(1) //nolint:gocritic // example exit
	}

	ack1, err := c1.Connect(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: connect 1: %v\n", err)
		return
	}
	sessionID := ack1.SessionID
	fmt.Printf("Created session: %s\n", sessionID)

	runAndPrint(ctx, c1, "Remember this number: 42")

	_ = c1.Close() //nolint:errcheck // example cleanup
	fmt.Println("Client 1 closed. Waiting 2 seconds...")
	time.Sleep(2 * time.Second)

	// Phase 2: Resume session.
	banner("Phase 2 — Resume Session")

	c2, err := client.New(ctx, client.URL(gatewayURL), client.APIKey(apiKey))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: create client 2: %v\n", err)
		os.Exit(1) //nolint:gocritic // example exit
	}
	defer func() { _ = c2.Close() }() //nolint:errcheck // example cleanup

	ack2, err := c2.Resume(ctx, sessionID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: resume failed: %v\n", err)
		return
	}
	fmt.Printf("Resumed session: %s  state=%s\n", ack2.SessionID, ack2.State)

	runAndPrint(ctx, c2, "What number did I ask you to remember?")

	banner("Done")
	fmt.Println("Session resume successful — context preserved across connections.")
}

func runAndPrint(ctx context.Context, c *client.Client, input string) {
	fmt.Printf("> %s\n\n", input)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range c.Events() {
			switch evt.Type {
			case client.EventMessageDelta:
				fmt.Print(fieldStr(evt.Data, "content"))
			case client.EventDone:
				fmt.Println("\n[done]")
				return
			case client.EventError:
				fmt.Fprintf(os.Stderr, "\nError: %s\n", fieldStr(evt.Data, "message"))
				return
			}
		}
	}()

	sendCtx, sendCancel := context.WithTimeout(ctx, 10*time.Second)
	if err := c.SendInput(sendCtx, input); err != nil {
		sendCancel()
		fmt.Fprintf(os.Stderr, "Error: send: %v\n", err)
		return
	}
	sendCancel()

	select {
	case <-done:
	case <-ctx.Done():
	case <-time.After(90 * time.Second):
		fmt.Fprintln(os.Stderr, "Timeout.")
	}
}

func banner(title string) {
	w := len(title) + 4
	if w < 50 {
		w = 50
	}
	fmt.Println()
	fmt.Println(strings.Repeat("=", w))
	fmt.Printf("  %s\n", title)
	fmt.Println(strings.Repeat("=", w))
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
