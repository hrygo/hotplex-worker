//go:build ignore

// Complete example demonstrating all AEP v1 client features.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/hotplex/hotplex-go-client"
)

// allowedPermissions is a map of tool names that are auto-approved.
var allowedPermissions = map[string]bool{
	"Read":     true,
	"Edit":     true,
	"Write":    true,
	"Grep":     true,
	"Glob":     true,
	"Bash":     false, // require approval
	"WebFetch": false,
}

type stats struct {
	startTime  time.Time
	toolCalls  int
	inputToks  int64
	outputToks int64
	costUSD    float64
	model      string
}

func main() {
	gatewayURL := getEnv("HOTPLEX_GATEWAY_URL", "ws://localhost:8888")
	signingKey := getEnv("HOTPLEX_SIGNING_KEY", "")
	sessionID := os.Getenv("HOTPLEX_SESSION_ID")
	task := getEnv("HOTPLEX_TASK", "List the files in the current directory and count them.")
	autoApprove := os.Getenv("HOTPLEX_AUTO_APPROVE") == "1"

	if signingKey == "" {
		log.Fatal("HOTPLEX_SIGNING_KEY env var is required")
	}

	// Generate token.
	gen, err := client.NewTokenGenerator(signingKey)
	if err != nil {
		log.Fatalf("create token generator: %v", err)
	}
	token, err := gen.Generate("example-user", []string{"read", "write"}, 1*time.Hour)
	if err != nil {
		log.Fatalf("generate token: %v", err)
	}

	// Create client with reconnect.
	ctx, cancel := context.WithCancel(context.Background())
	c, err := client.New(ctx,
		client.URL(gatewayURL),
		client.WorkerType(getEnv("HOTPLEX_WORKER_TYPE", "claude_code")),
		client.AuthToken(token),
		client.ClientSessionID(getEnv("HOTPLEX_CLIENT_SESSION_ID", "")),
	)
	if err != nil {
		log.Fatalf("create client: %v", err)
	}

	st := &stats{startTime: time.Now()}

	// Handle events.
	go handleEvents(c, st, autoApprove)

	// Connect or resume.
	var ack *client.InitAckData
	if sessionID != "" {
		fmt.Printf("Resuming session: %s\n", sessionID)
		ack, err = c.Resume(ctx, sessionID)
	} else {
		fmt.Println("Starting new session...")
		ack, err = c.Connect(ctx)
	}
	if err != nil {
		log.Fatalf("connection failed: %v", err)
	}
	fmt.Printf("Session: %s | State: %s | Worker: %s\n",
		ack.SessionID, ack.State, ack.ServerCaps.WorkerType)
	if ack.ServerCaps.Tools != nil {
		fmt.Printf("Tools: %s\n", strings.Join(ack.ServerCaps.Tools, ", "))
	}

	// Graceful shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutdown requested...")
		cancel()
		if err := c.SendControl(context.Background(), "terminate"); err != nil {
			log.Printf("send terminate: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
		c.Close()
		os.Exit(0)
	}()

	// Send task.
	fmt.Printf("> %s\n", truncate(task, 80))
	if err := c.SendInput(ctx, task); err != nil {
		log.Fatalf("send input: %v", err)
	}

	// Block.
	select {}
}

func handleEvents(c *client.Client, st *stats, autoApprove bool) {
	for evt := range c.Events() {
		switch evt.Type {
		case client.EventMessageDelta:
			if content := fieldStr(evt.Data, "content"); content != "" {
				fmt.Print(content)
			}
		case client.EventMessageStart:
			role := fieldStr(evt.Data, "role")
			fmt.Printf("\n[message start: %s]\n", role)
		case client.EventMessageEnd:
			fmt.Println()
		case client.EventToolCall:
			st.toolCalls++
			name := fieldStr(evt.Data, "name")
			if args := fieldStr(evt.Data, "input"); args != "" {
				fmt.Printf("\n[tool: %s(%s)]\n", name, truncate(args, 100))
			} else {
				fmt.Printf("\n[tool: %s]\n", name)
			}
		case client.EventToolResult:
			output := fieldStr(evt.Data, "output")
			if output != "" {
				fmt.Printf("[tool result: %s]\n", truncate(output, 100))
			}
			if errStr := fieldStr(evt.Data, "error"); errStr != "" {
				fmt.Printf("[tool error: %s]\n", errStr)
			}
		case client.EventReasoning:
			content := fieldStr(evt.Data, "content")
			if content != "" {
				fmt.Printf("\n[reasoning: %s]\n", truncate(content, 120))
			}
		case client.EventPermissionRequest:
			toolName := fieldStr(evt.Data, "tool_name")
			desc := fieldStr(evt.Data, "description")
			id := fieldStr(evt.Data, "id")
			fmt.Printf("\n[permission: %s] %s\n", toolName, desc)
			if autoApprove || allowedPermissions[toolName] {
				fmt.Printf("[auto-approving: %s]\n", toolName)
				_ = c.SendPermissionResponse(context.Background(), id, true, "auto-approved")
			} else {
				fmt.Printf("[permission denied: %s — set HOTPLEX_AUTO_APPROVE=1 to auto-approve]\n", toolName)
				_ = c.SendPermissionResponse(context.Background(), id, false, "not in allowlist")
			}
		case client.EventState:
			state := fieldStr(evt.Data, "state")
			fmt.Printf("\n[state: %s]\n", state)
		case client.EventDone:
			fmt.Println("\n" + strings.Repeat("=", 50))
			fmt.Println("Session complete")
			if data, ok := evt.Data.(map[string]any); ok {
				if success, ok := data["success"].(bool); ok {
					fmt.Printf("Success: %v\n", success)
				}
				if stats, ok := data["stats"].(map[string]any); ok {
					if v, ok := stats["duration_ms"].(float64); ok {
						fmt.Printf("Duration: %.1fs\n", v/1000)
					}
					if v, ok := stats["input_tokens"].(float64); ok {
						st.inputToks = int64(v)
						fmt.Printf("Input tokens: %d\n", st.inputToks)
					}
					if v, ok := stats["output_tokens"].(float64); ok {
						st.outputToks = int64(v)
						fmt.Printf("Output tokens: %d\n", st.outputToks)
					}
					if v, ok := stats["cost_usd"].(float64); ok {
						st.costUSD = v
						fmt.Printf("Cost: $%.4f\n", st.costUSD)
					}
					if v, ok := stats["model"].(string); ok {
						st.model = v
						fmt.Printf("Model: %s\n", st.model)
					}
				}
				if dropped, ok := data["dropped"].(bool); ok && dropped {
					fmt.Println("WARNING: Some events were dropped (backpressure)")
				}
			}
			fmt.Println(strings.Repeat("=", 50))
			fmt.Printf("Session: %s\n", c.SessionID())
			c.Close()
			os.Exit(0)
		case client.EventError:
			errMsg := fieldStr(evt.Data, "message")
			code := fieldStr(evt.Data, "code")
			fmt.Fprintf(os.Stderr, "\n[ERROR %s] %s\n", code, errMsg)
			os.Exit(1)
		case client.EventControl:
			action := fieldStr(evt.Data, "action")
			fmt.Printf("\n[control: %s]\n", action)
		case client.EventPong:
			fmt.Print("[pong]")
		}
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// fieldStr returns the value of a map field as a string.
// If the field is not a string, json.Marshal is used as fallback.
func fieldStr(data any, key string) string {
	m, ok := data.(map[string]any)
	if !ok {
		return ""
	}
	v, ok := m[key]
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "..."
}
