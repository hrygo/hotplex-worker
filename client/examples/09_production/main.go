// 09_production — Full production-grade integration example.
//
// Combines: JWT/API Key auth, session resume, signal handling,
// streaming output, tool permission policy, usage statistics,
// and graceful shutdown.
//
// Usage:
//
//	HOTPLEX_API_KEY=test-api-key go run ./09_production
//	HOTPLEX_SIGNING_KEY=<key> go run ./09_production
//	HOTPLEX_SESSION_ID=<id> go run ./09_production    # resume existing session
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	client "github.com/hrygo/hotplex/client"
)

var allowPolicy = map[string]bool{
	"Read":  true,
	"Glob":  true,
	"Grep":  true,
	"Edit":  true,
	"Write": true,
	"Bash":  false,
}

type sessionStats struct {
	startTime  time.Time
	toolCalls  int
	inputToks  int64
	outputToks int64
	costUSD    float64
	model      string
}

func main() {
	gatewayURL := envOr("HOTPLEX_GATEWAY_URL", "ws://localhost:8888/ws")
	signingKey := envOr("HOTPLEX_SIGNING_KEY", "")
	apiKey := envOr("HOTPLEX_API_KEY", "")
	sessionID := os.Getenv("HOTPLEX_SESSION_ID")
	workerType := envOr("HOTPLEX_WORKER_TYPE", "claude_code")
	task := envOr("HOTPLEX_TASK", "List the files in the current directory and count them.")

	// Auth: JWT, API Key, or none (for dev).
	var authToken string
	if signingKey != "" {
		gen, err := client.NewTokenGenerator(signingKey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: token generator: %v\n", err)
			os.Exit(1) //nolint:gocritic // example exit
		}
		token, err := gen.Generate("production-user", []string{"read", "write"}, 1*time.Hour)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: generate token: %v\n", err)
			os.Exit(1) //nolint:gocritic // example exit
		}
		authToken = token
		fmt.Println("Auth: JWT")
	} else if apiKey != "" {
		fmt.Println("Auth: API Key")
	} else {
		fmt.Println("Auth: none (development mode)")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Default to test-api-key if no auth provided (development).
	if apiKey == "" && authToken == "" {
		apiKey = "test-api-key"
	}

	opts := []client.Option{
		client.URL(gatewayURL),
		client.WorkerType(workerType),
		client.AutoReconnect(true),
		client.Logger(slog.Default()),
	}
	if authToken != "" {
		opts = append(opts, client.AuthToken(authToken))
	}
	if apiKey != "" {
		opts = append(opts, client.APIKey(apiKey))
	}

	c, err := client.New(ctx, opts...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: create client: %v\n", err)
		os.Exit(1) //nolint:gocritic // example exit
	}
	defer func() { _ = c.Close() }() //nolint:errcheck // example cleanup

	st := &sessionStats{startTime: time.Now()}

	// Signal handling.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\n\nShutdown requested...")
		_ = c.SendControl(context.Background(), client.ControlActionTerminate)
		time.Sleep(500 * time.Millisecond)
		_ = c.Close() //nolint:errcheck // signal cleanup
		cancel()
		os.Exit(0)
	}()

	go handleEvents(c, st)

	// Connect or Resume.
	var ack *client.InitAckData
	if sessionID != "" {
		fmt.Printf("Resuming session: %s\n", sessionID)
		ack, err = c.Resume(ctx, sessionID)
	} else {
		fmt.Println("Starting new session...")
		ack, err = c.Connect(ctx)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: connection failed: %v\n", err)
		return
	}

	fmt.Printf("Session:  %s\n", ack.SessionID)
	fmt.Printf("State:    %s\n", ack.State)
	fmt.Printf("Worker:   %s\n", ack.ServerCaps.WorkerType)
	fmt.Printf("Resume:   %v\n", ack.ServerCaps.SupportsResume)
	if ack.ServerCaps.Tools != nil {
		fmt.Printf("Tools:    %s\n", strings.Join(ack.ServerCaps.Tools, ", "))
	}

	fmt.Printf("\n> %s\n", truncate(task, 80))
	if err := c.SendInput(ctx, task); err != nil {
		fmt.Fprintf(os.Stderr, "Error: send input: %v\n", err)
		return
	}

	<-ctx.Done()
}

func handleEvents(c *client.Client, st *sessionStats) {
	for evt := range c.Events() {
		switch evt.Type {
		case client.EventMessageStart:
			fmt.Printf("\n[%s] ", fieldStr(evt.Data, "role"))
		case client.EventMessageDelta:
			fmt.Print(fieldStr(evt.Data, "content"))
		case client.EventMessageEnd:
			fmt.Println()
		case client.EventToolCall:
			st.toolCalls++
			fmt.Printf("\n  [tool: %s]\n", fieldStr(evt.Data, "name"))
		case client.EventToolResult:
			if output := fieldStr(evt.Data, "output"); output != "" {
				fmt.Printf("  [result: %s]\n", truncate(output, 120))
			}
		case client.EventReasoning:
			if content := fieldStr(evt.Data, "content"); content != "" {
				fmt.Printf("\n  [reasoning: %s]\n", truncate(content, 120))
			}
		case client.EventPermissionRequest:
			if d, ok := evt.AsPermissionRequestData(); ok {
				if allowPolicy[d.ToolName] {
					_ = c.SendPermissionResponse(context.Background(), d.ID, true, "auto-approved")
				} else {
					_ = c.SendPermissionResponse(context.Background(), d.ID, false, "requires manual review")
				}
			}
		case client.EventState:
			if d, ok := evt.Data.(map[string]any); ok {
				fmt.Printf("\n[state: %s]\n", d["state"])
			}
		case client.EventDone:
			printDoneSummary(c, st, evt)
			_ = c.Close() //nolint:errcheck // session done
			os.Exit(0)
		case client.EventError:
			if d, ok := evt.AsErrorData(); ok {
				fmt.Fprintf(os.Stderr, "\n[ERROR %s] %s\n", d.Code, d.Message)
			} else {
				fmt.Fprintf(os.Stderr, "\n[ERROR] %v\n", evt.Data)
			}
			_ = c.Close() //nolint:errcheck // error exit
			os.Exit(1)    //nolint:gocritic // example exit
		}
	}
}

func printDoneSummary(c *client.Client, st *sessionStats, evt client.Event) {
	done, ok := evt.AsDoneData()
	if !ok {
		return
	}

	banner("Session Complete")
	fmt.Printf("Session ID:  %s\n", c.SessionID())
	fmt.Printf("Duration:    %.1fs\n", time.Since(st.startTime).Seconds())
	fmt.Printf("Tool calls:  %d\n", st.toolCalls)
	fmt.Printf("Success:     %v\n", done.Success)
	fmt.Printf("Dropped:     %v\n", done.Dropped)

	if done.Stats != nil {
		if v := fieldFloat64(done.Stats, "input_tokens"); v > 0 {
			st.inputToks = int64(v)
			fmt.Printf("Input tok:   %d\n", st.inputToks)
		}
		if v := fieldFloat64(done.Stats, "output_tokens"); v > 0 {
			st.outputToks = int64(v)
			fmt.Printf("Output tok:  %d\n", st.outputToks)
		}
		if v := fieldFloat64(done.Stats, "cost_usd"); v > 0 {
			st.costUSD = v
			fmt.Printf("Cost:        $%.4f\n", st.costUSD)
		}
		if v := fieldStr(done.Stats, "model"); v != "" {
			st.model = v
			fmt.Printf("Model:       %s\n", st.model)
		}
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

func fieldFloat64(data any, key string) float64 {
	m, ok := data.(map[string]any)
	if !ok {
		return 0
	}
	v, ok := m[key]
	if !ok {
		return 0
	}
	f, _ := v.(float64)
	return f
}

func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "..."
}
