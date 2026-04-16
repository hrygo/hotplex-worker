// 05_permission_handling — Tool permission request/response flow with auto-approve policy.
//
// Usage:
//
//	HOTPLEX_API_KEY=test-api-key go run ./05_permission_handling
//	HOTPLEX_AUTO_APPROVE=1 go run ./05_permission_handling
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/hotplex/hotplex-go-client"
)

// Customize this map for your security requirements.
var allowPolicy = map[string]bool{
	"Read":  true,
	"Glob":  true,
	"Grep":  true,
	"Edit":  true,
	"Write": true,
	"Bash":  false,
}

func main() {
	gatewayURL := envOr("HOTPLEX_GATEWAY_URL", "ws://localhost:8888/ws")
	apiKey := envOr("HOTPLEX_API_KEY", "test-api-key")
	autoApproveAll := os.Getenv("HOTPLEX_AUTO_APPROVE") == "1"
	task := envOr("HOTPLEX_TASK", "Read the file go.mod and tell me the Go version.")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
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
		log.Fatalf("create client: %v", err)
	}
	defer c.Close()

	ack, err := c.Connect(ctx)
	if err != nil {
		log.Fatalf("connect: %v", err)
	}
	fmt.Printf("Session: %s\n\n", ack.SessionID)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range c.Events() {
			switch evt.Type {
			case client.EventMessageDelta:
				fmt.Print(fieldStr(evt.Data, "content"))
			case client.EventToolCall:
				fmt.Printf("\n  [tool call: %s]\n", fieldStr(evt.Data, "name"))
			case client.EventPermissionRequest:
				id := fieldStr(evt.Data, "id")
				toolName := fieldStr(evt.Data, "tool_name")
				desc := fieldStr(evt.Data, "description")
				fmt.Printf("\n  [permission request] tool=%s  desc=%s\n", toolName, truncate(desc, 80))

				if autoApproveAll || allowPolicy[toolName] {
					fmt.Printf("  -> Approved (%s)\n", toolName)
					_ = c.SendPermissionResponse(context.Background(), id, true, "auto-approved")
				} else {
					fmt.Printf("  -> Denied  (%s)\n", toolName)
					_ = c.SendPermissionResponse(context.Background(), id, false, "not in allowlist")
				}
			case client.EventDone:
				fmt.Println("\n\nDone.")
				return
			case client.EventError:
				fmt.Fprintf(os.Stderr, "\nError: %s\n", fieldStr(evt.Data, "message"))
				return
			}
		}
	}()

	fmt.Printf("> %s\n\n", task)
	if err := c.SendInput(ctx, task); err != nil {
		log.Fatalf("send: %v", err)
	}

	select {
	case <-done:
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, "Timeout or cancelled.")
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

func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "..."
}
