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
	"os"
	"os/signal"
	"syscall"
	"time"

	client "github.com/hrygo/hotplex/client"
	"github.com/hrygo/hotplex/client/examples/internal/demo"
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
	gatewayURL := demo.EnvOr("HOTPLEX_GATEWAY_URL", "ws://localhost:8888/ws")
	apiKey := demo.EnvOr("HOTPLEX_API_KEY", "test-api-key")
	autoApproveAll := os.Getenv("HOTPLEX_AUTO_APPROVE") == "1"
	task := demo.EnvOr("HOTPLEX_TASK", "Read the file go.mod and tell me the Go version.")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sigCh; cancel() }()

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
	fmt.Printf("Session: %s\n\n", ack.SessionID)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for evt := range c.Events() {
			switch evt.Type {
			case client.EventMessageDelta:
				fmt.Print(demo.FieldStr(evt.Data, "content"))
			case client.EventToolCall:
				if d, ok := evt.AsToolCallData(); ok {
					fmt.Printf("\n  [tool call: %s]\n", d.Name)
				}
			case client.EventPermissionRequest:
				if d, ok := evt.AsPermissionRequestData(); ok {
					fmt.Printf("\n  [permission request] tool=%s  desc=%s\n", d.ToolName, demo.Truncate(d.Description, 80))

					if autoApproveAll || allowPolicy[d.ToolName] {
						fmt.Printf("  -> Approved (%s)\n", d.ToolName)
						_ = c.SendPermissionResponse(context.Background(), d.ID, true, "auto-approved") //nolint:errcheck // example manual response
					} else {
						fmt.Printf("  -> Denied  (%s)\n", d.ToolName)
						_ = c.SendPermissionResponse(context.Background(), d.ID, false, "not in allowlist") //nolint:errcheck // example manual response
					}
				}
			case client.EventDone:
				if d, ok := evt.AsDoneData(); ok {
					fmt.Printf("\n\nDone (success=%v).\n", d.Success)
				}
				return
			case client.EventError:
				if errData, ok := evt.AsErrorData(); ok {
					fmt.Fprintf(os.Stderr, "\nError [%s]: %s\n", errData.Code, errData.Message)
				}
				return
			}
		}
	}()

	fmt.Printf("> %s\n\n", task)
	if err := c.SendInput(ctx, task); err != nil {
		fmt.Fprintf(os.Stderr, "Error: send: %v\n", err)
		return
	}

	select {
	case <-done:
	case <-ctx.Done():
		fmt.Fprintln(os.Stderr, "Timeout or cancelled.")
	}
}
