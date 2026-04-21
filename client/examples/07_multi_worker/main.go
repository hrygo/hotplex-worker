// 07_multi_worker — Test all registered worker types sequentially.
//
// Usage:
//
//	HOTPLEX_API_KEY=test-api-key go run ./07_multi_worker
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	client "github.com/hotplex/hotplex-go-client"
)

var workerTypes = []string{
	"claude_code",
	"opencode_server",
	"acpx",
}

func main() {
	gatewayURL := envOr("HOTPLEX_GATEWAY_URL", "ws://localhost:8888/ws")
	apiKey := envOr("HOTPLEX_API_KEY", "test-api-key")
	task := envOr("HOTPLEX_TASK", "Respond with 'OK' and nothing else.")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sigCh; cancel() }()

	fmt.Printf("%-20s %-10s %-10s %s\n", "WORKER", "STATUS", "STATE", "DETAIL")
	fmt.Println(strings.Repeat("-", 65))

	for _, wt := range workerTypes {
		if ctx.Err() != nil {
			break
		}
		testWorker(ctx, gatewayURL, apiKey, wt, task)
	}

	banner("Summary")
	fmt.Printf("Tested %d worker types.\n", len(workerTypes))
}

func testWorker(ctx context.Context, gatewayURL, apiKey, workerType, task string) {
	c, err := client.New(ctx,
		client.URL(gatewayURL),
		client.WorkerType(workerType),
		client.APIKey(apiKey),
	)
	if err != nil {
		fmt.Printf("%-20s %-10s — %v\n", workerType, "FAIL", err)
		return
	}
	defer func() { _ = c.Close() }() //nolint:errcheck // example cleanup

	ack, err := c.Connect(ctx)
	if err != nil {
		fmt.Printf("%-20s %-10s — %v\n", workerType, "FAIL", err)
		return
	}
	fmt.Printf("%-20s %-10s %-10s session=%s\n",
		workerType, "CONNECTED", ack.State, ack.SessionID)

	done := make(chan result, 1)
	go func() {
		var r result
		for evt := range c.Events() {
			switch evt.Type {
			case client.EventMessageDelta:
				r.output += fieldStr(evt.Data, "content")
			case client.EventDone:
				r.success = true
				done <- r
				return
			case client.EventError:
				r.err = fmt.Sprintf("%s: %s", fieldStr(evt.Data, "code"), fieldStr(evt.Data, "message"))
				done <- r
				return
			}
		}
	}()

	sendCtx, sendCancel := context.WithTimeout(ctx, 10*time.Second)
	_ = c.SendInput(sendCtx, task)
	sendCancel()

	select {
	case r := <-done:
		status := "PASS"
		detail := truncate(r.output, 40)
		if r.err != "" {
			status = "ERROR"
			detail = r.err
		}
		fmt.Printf("  -> %-10s %s\n", status, detail)
	case <-time.After(60 * time.Second):
		fmt.Printf("  -> %-10s %s\n", "TIMEOUT", "no response in 60s")
	case <-ctx.Done():
		fmt.Printf("  -> %-10s %s\n", "CANCELLED", ctx.Err())
	}

	time.Sleep(500 * time.Millisecond)
}

type result struct {
	success bool
	output  string
	err     string
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

func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max]) + "..."
}
