package e2e_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	client "github.com/hrygo/hotplex/client"

	"github.com/hrygo/hotplex/internal/worker"
)

func TestE2E_SendInputReceiveEvents(t *testing.T) {
	for _, wt := range allWorkerTypes {
		t.Run(wt.name, func(t *testing.T) {
			t.Parallel()
			tg := setupTestGateway(t)
			c := connectClient(t, tg, wt.workerType)

			ack, err := c.Connect(context.Background())
			require.NoError(t, err)
			require.NotEmpty(t, ack.SessionID)

			err = c.SendInput(context.Background(), "Hello, worker!")
			require.NoError(t, err)

			evts := collectEvents(t, c.Events(), 10*time.Second)

			require.True(t, hasEventType(evts, client.EventState), "expected state event")
			require.True(t, hasEventType(evts, client.EventMessageStart), "expected message.start event")
			require.True(t, hasEventType(evts, client.EventMessageDelta), "expected message.delta event")
			require.True(t, hasEventType(evts, client.EventMessageEnd), "expected message.end event")
			require.True(t, hasEventType(evts, client.EventDone), "expected done event")

			require.NoError(t, c.Close())
		})
	}
}

func TestE2E_PingPong(t *testing.T) {
	tg := setupTestGateway(t)
	c := connectClient(t, tg, string(worker.TypeClaudeCode))

	_, err := c.Connect(context.Background())
	require.NoError(t, err)

	err = c.SendInput(context.Background(), "test")
	require.NoError(t, err)

	_ = collectEvents(t, c.Events(), 5*time.Second)

	require.NoError(t, c.Close())
}

func TestE2E_MultipleWorkers(t *testing.T) {
	tg := setupTestGateway(t)

	var wg sync.WaitGroup
	results := make(chan []client.Event, len(allWorkerTypes))

	for _, wt := range allWorkerTypes {
		wg.Add(1)
		go func(workerType string) {
			defer wg.Done()

			c := connectClient(t, tg, workerType)
			defer c.Close()

			_, err := c.Connect(context.Background())
			if err != nil {
				results <- nil
				return
			}

			err = c.SendInput(context.Background(), "hello from "+workerType)
			if err != nil {
				results <- nil
				return
			}

			evts := collectEvents(t, c.Events(), 10*time.Second)
			results <- evts
		}(wt.workerType)
	}

	wg.Wait()
	close(results)

	count := 0
	for evts := range results {
		if evts != nil {
			count++
			require.True(t, hasEventType(evts, client.EventDone), "expected done event")
		}
	}
	require.Equal(t, len(allWorkerTypes), count, "all workers should produce events")
}

func TestE2E_EventSeqMonotonic(t *testing.T) {
	tg := setupTestGateway(t)
	c := connectClient(t, tg, string(worker.TypeClaudeCode))

	_, err := c.Connect(context.Background())
	require.NoError(t, err)

	err = c.SendInput(context.Background(), "check seq ordering")
	require.NoError(t, err)

	evts := collectEvents(t, c.Events(), 5*time.Second)

	var lastSeq int64
	for _, evt := range evts {
		if evt.Seq > 0 && evt.Type != client.EventInitAck {
			require.Greater(t, evt.Seq, lastSeq,
				"seq should be monotonic: last=%d current=%d type=%s", lastSeq, evt.Seq, evt.Type)
			lastSeq = evt.Seq
		}
	}

	require.NoError(t, c.Close())
}

// TestE2E_MultipleInputsSequential: after the first input produces a done event,
// the client's recvPump exits (IsTerminalEvent). Subsequent inputs are accepted
// by the gateway but events cannot be received on the same connection.
func TestE2E_MultipleInputsSequential(t *testing.T) {
	tg := setupTestGateway(t)
	c := connectClient(t, tg, string(worker.TypeClaudeCode))

	_, err := c.Connect(context.Background())
	require.NoError(t, err)

	err = c.SendInput(context.Background(), "message 0")
	require.NoError(t, err)
	evts := collectEvents(t, c.Events(), 5*time.Second)
	require.True(t, hasEventType(evts, client.EventDone), "expected done event for first input")

	err = c.SendInput(context.Background(), "message 1")
	require.NoError(t, err)

	require.NoError(t, c.Close())
}

func TestE2E_DoneDataSuccess(t *testing.T) {
	tg := setupTestGateway(t)
	c := connectClient(t, tg, string(worker.TypeClaudeCode))

	_, err := c.Connect(context.Background())
	require.NoError(t, err)

	err = c.SendInput(context.Background(), "check done data")
	require.NoError(t, err)

	evts := collectEvents(t, c.Events(), 5*time.Second)

	doneEvt := findEvent(evts, client.EventDone)
	require.NotNil(t, doneEvt, "expected done event")

	data, ok := doneEvt.Data.(map[string]any)
	require.True(t, ok)
	success, _ := data["success"].(bool)
	require.True(t, success, "done.success should be true")

	require.NoError(t, c.Close())
}

func TestE2E_MessageDeltaContent(t *testing.T) {
	tg := setupTestGateway(t)
	c := connectClient(t, tg, string(worker.TypeClaudeCode))

	_, err := c.Connect(context.Background())
	require.NoError(t, err)

	err = c.SendInput(context.Background(), "Hello World")
	require.NoError(t, err)

	evts := collectEvents(t, c.Events(), 5*time.Second)

	var deltaContent string
	for _, evt := range evts {
		if evt.Type == client.EventMessageDelta {
			if data, ok := evt.Data.(map[string]any); ok {
				if content, ok := data["content"].(string); ok {
					deltaContent += content
				}
			}
		}
	}
	require.Contains(t, deltaContent, "Hello World",
		"delta content should contain the input text")

	require.NoError(t, c.Close())
}

func TestE2E_AuthFailure(t *testing.T) {
	tg := setupTestGateway(t)

	c, err := client.New(context.Background(),
		client.URL(tg.wsURL()),
		client.WorkerType(string(worker.TypeClaudeCode)),
	)
	require.NoError(t, err)

	_, err = c.Connect(context.Background())
	require.Error(t, err, "expected auth failure without API key")
}

func TestE2E_LargeInput(t *testing.T) {
	tg := setupTestGateway(t)
	c := connectClient(t, tg, string(worker.TypeClaudeCode))

	_, err := c.Connect(context.Background())
	require.NoError(t, err)

	largeContent := strings.Repeat("x", 10000)
	err = c.SendInput(context.Background(), largeContent)
	require.NoError(t, err)

	evts := collectEvents(t, c.Events(), 5*time.Second)
	require.True(t, hasEventType(evts, client.EventDone), "expected done event for large input")

	require.NoError(t, c.Close())
}
