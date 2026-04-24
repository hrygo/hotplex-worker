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

	client "github.com/hrygo/hotplex/client"
	"github.com/hrygo/hotplex/client/examples/internal/demo"
)

var workerTypes = []string{
	"claude_code",
	"opencode_server",
	"acpx",
}

func main() {
	gatewayURL := demo.EnvOr("HOTPLEX_GATEWAY_URL", "ws://localhost:8888/ws")
	apiKey := demo.EnvOr("HOTPLEX_API_KEY", "test-api-key")
	task := demo.EnvOr("HOTPLEX_TASK", "Respond with 'OK' and nothing else.")

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

	demo.Banner("Summary")
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
				r.output += demo.FieldStr(evt.Data, "content")
			case client.EventDone:
				r.success = true
				done <- r
				return
			case client.EventError:
				r.err = fmt.Sprintf("%s: %s", demo.FieldStr(evt.Data, "code"), demo.FieldStr(evt.Data, "message"))
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
		detail := demo.Truncate(r.output, 40)
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
