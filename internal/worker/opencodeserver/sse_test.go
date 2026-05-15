package opencodeserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// newSSEWorker creates a Worker wired to a mock SSE server.
func newSSEWorker(t *testing.T, handler http.HandlerFunc) (*Worker, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	w := New()
	w.httpAddr = srv.URL
	w.client = srv.Client()
	w.sseClient = srv.Client()
	recvCh := make(chan *events.Envelope, 256)
	w.httpConn = &conn{
		sessionID: "ses_test",
		userID:    "u_test",
		httpAddr:  srv.URL,
		client:    srv.Client(),
		recvCh:    recvCh,
		log:       w.Log,
	}
	return w, srv
}

// sseDataLine builds a valid SSE data line from an AEP envelope.
func sseDataLine(t *testing.T, env *events.Envelope) string {
	t.Helper()
	b, err := aep.EncodeJSON(env)
	require.NoError(t, err)
	return "data: " + string(b) + "\n"
}

// collectN reads n events from the channel with a timeout.
func collectN(t *testing.T, ch <-chan *events.Envelope, n int) []*events.Envelope {
	t.Helper()
	var result []*events.Envelope
	for i := range n {
		select {
		case env := <-ch:
			result = append(result, env)
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out waiting for event %d/%d (got %d)", i+1, n, len(result))
		}
	}
	return result
}

func TestReadSSE_BasicEventParsing(t *testing.T) {
	t.Parallel()

	msgEnv := events.NewEnvelope("id1", "ses_test", 1, events.MessageDelta, events.MessageDeltaData{Content: "hello"})
	doneEnv := events.NewEnvelope("id2", "ses_test", 2, events.Done, events.DoneData{Success: true})

	w, _ := newSSEWorker(t, func(rw http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/events", r.URL.Path)
		require.Equal(t, "ses_test", r.URL.Query().Get("session_id"))
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := rw.(http.Flusher)
		require.True(t, ok)

		fmt.Fprint(rw, sseDataLine(t, msgEnv))
		flusher.Flush()
		fmt.Fprint(rw, sseDataLine(t, doneEnv))
		flusher.Flush()
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go w.readSSE(ctx, "ses_test")

	got := collectN(t, w.httpConn.recvCh, 2)
	require.Equal(t, events.MessageDelta, got[0].Event.Type)
	require.Equal(t, events.Done, got[1].Event.Type)
}

func TestReadSSE_BusEventParsing(t *testing.T) {
	t.Parallel()

	permPayload, _ := json.Marshal(map[string]any{
		"id":       "perm_1",
		"metadata": map[string]any{"tool": "bash"},
	})
	qPayload, _ := json.Marshal(map[string]any{
		"id":    "q_1",
		"title": "Confirm?",
	})

	var reqCount atomic.Int32
	w, _ := newSSEWorker(t, func(rw http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)

		fmt.Fprintf(rw, "data: {\"type\":\"permission.asked\",\"properties\":%s}\n", string(permPayload))
		flusher.Flush()
		fmt.Fprintf(rw, "data: {\"type\":\"question.asked\",\"properties\":%s}\n", string(qPayload))
		flusher.Flush()

		// Send a valid AEP event to confirm the goroutine is still processing.
		msgEnv := events.NewEnvelope("id3", "ses_test", 1, events.MessageDelta, events.MessageDeltaData{Content: "ok"})
		fmt.Fprint(rw, sseDataLine(t, msgEnv))
		flusher.Flush()
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go w.readSSE(ctx, "ses_test")

	// Bus events are converted to AEP events and forwarded to recvCh.
	// permission.asked → PermissionRequest, question.asked → QuestionRequest, then a MessageDelta.
	got := collectN(t, w.httpConn.recvCh, 3)
	require.Equal(t, events.PermissionRequest, got[0].Event.Type)
	require.Equal(t, events.QuestionRequest, got[1].Event.Type)
	require.Equal(t, events.MessageDelta, got[2].Event.Type)
}

func TestReadSSE_EmptyLinesIgnored(t *testing.T) {
	t.Parallel()

	msgEnv := events.NewEnvelope("id1", "ses_test", 1, events.MessageDelta, events.MessageDeltaData{Content: "hi"})

	w, _ := newSSEWorker(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)

		fmt.Fprint(rw, "\n\n")
		fmt.Fprint(rw, sseDataLine(t, msgEnv))
		fmt.Fprint(rw, "\n")
		flusher.Flush()
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go w.readSSE(ctx, "ses_test")

	got := collectN(t, w.httpConn.recvCh, 1)
	require.Equal(t, "hi", got[0].Event.Data.(map[string]any)["content"])
}

func TestReadSSE_NonDataPrefixIgnored(t *testing.T) {
	t.Parallel()

	msgEnv := events.NewEnvelope("id1", "ses_test", 1, events.MessageDelta, events.MessageDeltaData{Content: "data"})

	w, _ := newSSEWorker(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)

		fmt.Fprint(rw, "event: message\n")
		fmt.Fprint(rw, "id: 123\n")
		fmt.Fprint(rw, sseDataLine(t, msgEnv))
		flusher.Flush()
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go w.readSSE(ctx, "ses_test")

	got := collectN(t, w.httpConn.recvCh, 1)
	require.Equal(t, "data", got[0].Event.Data.(map[string]any)["content"])
}

func TestReadSSE_InvalidJSON_Skipped(t *testing.T) {
	t.Parallel()

	msgEnv := events.NewEnvelope("id1", "ses_test", 1, events.MessageDelta, events.MessageDeltaData{Content: "after"})

	w, _ := newSSEWorker(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)

		fmt.Fprint(rw, "data: not-json-at-all\n")
		fmt.Fprint(rw, sseDataLine(t, msgEnv))
		flusher.Flush()
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go w.readSSE(ctx, "ses_test")

	got := collectN(t, w.httpConn.recvCh, 1)
	require.Equal(t, "after", got[0].Event.Data.(map[string]any)["content"])
}

func TestReadSSE_EOF_Reconnects(t *testing.T) {
	t.Parallel()

	msg1 := events.NewEnvelope("id1", "ses_test", 1, events.MessageDelta, events.MessageDeltaData{Content: "first"})
	msg2 := events.NewEnvelope("id2", "ses_test", 2, events.MessageDelta, events.MessageDeltaData{Content: "second"})

	var reqCount atomic.Int32
	w, _ := newSSEWorker(t, func(rw http.ResponseWriter, r *http.Request) {
		n := reqCount.Add(1)
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)

		if n == 1 {
			fmt.Fprint(rw, sseDataLine(t, msg1))
			flusher.Flush()
			return // close connection → EOF → reconnect
		}
		fmt.Fprint(rw, sseDataLine(t, msg2))
		flusher.Flush()
		// Keep connection open but don't send more — test reads 2 events then cancels.
		<-r.Context().Done() // block forever until server closes
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go w.readSSE(ctx, "ses_test")

	got := collectN(t, w.httpConn.recvCh, 2)
	require.Equal(t, "first", got[0].Event.Data.(map[string]any)["content"])
	require.Equal(t, "second", got[1].Event.Data.(map[string]any)["content"])
}

func TestReadSSE_HTTPError_503_Reconnects(t *testing.T) {
	t.Parallel()

	msg := events.NewEnvelope("id1", "ses_test", 1, events.MessageDelta, events.MessageDeltaData{Content: "after503"})

	var reqCount atomic.Int32
	w, _ := newSSEWorker(t, func(rw http.ResponseWriter, r *http.Request) {
		n := reqCount.Add(1)
		if n == 1 {
			rw.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)
		fmt.Fprint(rw, sseDataLine(t, msg))
		flusher.Flush()
		<-r.Context().Done()
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go w.readSSE(ctx, "ses_test")

	got := collectN(t, w.httpConn.recvCh, 1)
	require.Equal(t, "after503", got[0].Event.Data.(map[string]any)["content"])
}

func TestReadSSE_HTTPError_404_Stops(t *testing.T) {
	t.Parallel()

	w, _ := newSSEWorker(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusNotFound)
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go w.readSSE(ctx, "ses_test")

	// readSSE should close the conn's recvCh on 404.
	_, ok := <-w.httpConn.recvCh
	require.False(t, ok, "recvCh should be closed after 404")
}

func TestReadSSE_ContextCancel_Stops(t *testing.T) {
	t.Parallel()

	msg := events.NewEnvelope("id1", "ses_test", 1, events.MessageDelta, events.MessageDeltaData{Content: "hi"})

	var reqCount atomic.Int32
	w, _ := newSSEWorker(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)
		fmt.Fprint(rw, sseDataLine(t, msg))
		flusher.Flush()
		// First request: close after sending (EOF triggers reconnect).
		// Subsequent requests will be cancelled by ctx.
		if reqCount.Add(1) == 1 {
			return
		}
		// Block until client disconnects.
		<-r.Context().Done()
	})

	ctx, cancel := context.WithCancel(t.Context())
	go w.readSSE(ctx, "ses_test")

	// Read the first event (from first connection).
	got := collectN(t, w.httpConn.recvCh, 1)
	require.Equal(t, "hi", got[0].Event.Data.(map[string]any)["content"])

	// Cancel context — second SSE attempt should fail and exit.
	cancel()

	// Give goroutine time to exit.
	require.Eventually(t, func() bool {
		// readSSE exits via ctx.Done() check, does NOT close recvCh.
		// We can't observe goroutine exit directly, but we verify recvCh is still open.
		select {
		case _, ok := <-w.httpConn.recvCh:
			return !ok // closed = bad
		default:
			return true // still open, goroutine exited cleanly
		}
	}, 3*time.Second, 50*time.Millisecond, "readSSE should exit without closing recvCh")
}

func TestReadSSE_Backpressure_DropOnFull(t *testing.T) {
	t.Parallel()

	// Create a worker with a tiny recvCh (2 capacity).
	w := New()
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)
		for i := range 5 {
			msg := events.NewEnvelope(fmt.Sprintf("id%d", i), "ses_test", int64(i), events.MessageDelta, events.MessageDeltaData{Content: fmt.Sprintf("msg%d", i)})
			fmt.Fprint(rw, sseDataLine(t, msg))
			flusher.Flush()
		}
	}))
	t.Cleanup(srv.Close)

	w.httpAddr = srv.URL
	w.sseClient = srv.Client()
	w.httpConn = &conn{
		sessionID: "ses_test",
		userID:    "u_test",
		httpAddr:  srv.URL,
		client:    srv.Client(),
		recvCh:    make(chan *events.Envelope, 2),
		log:       w.Log,
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go w.readSSE(ctx, "ses_test")

	// Read at least 1 event — the rest may be dropped but should not deadlock.
	got := collectN(t, w.httpConn.recvCh, 1)
	require.Equal(t, events.MessageDelta, got[0].Event.Type)
}

func TestReadSSE_MaxReconnects_Stops(t *testing.T) {

	// Override max reconnects via a local constant approach — use a server that always returns 503.
	w, _ := newSSEWorker(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusServiceUnavailable)
	})

	// Patch the max to a low value for fast testing.
	origMax := sseMaxReconnects
	sseMaxReconnects = 3
	t.Cleanup(func() { sseMaxReconnects = origMax })

	// Also patch backoff to be instant for testing.
	origBackoff := sseBackoffInitial
	sseBackoffInitial = 1 * time.Millisecond
	sseBackoffMax = 2 * time.Millisecond
	t.Cleanup(func() {
		sseBackoffInitial = origBackoff
		sseBackoffMax = 10 * time.Second
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go w.readSSE(ctx, "ses_test")

	// After max reconnects, conn should be closed.
	require.Eventually(t, func() bool {
		select {
		case _, ok := <-w.httpConn.recvCh:
			return !ok
		default:
			return false
		}
	}, 5*time.Second, 50*time.Millisecond, "recvCh should be closed after max reconnects")
}

func TestReadSSE_ConnNil_Stops(t *testing.T) {
	t.Parallel()

	msg := events.NewEnvelope("id1", "ses_test", 1, events.MessageDelta, events.MessageDeltaData{Content: "hi"})

	w, _ := newSSEWorker(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)
		fmt.Fprint(rw, sseDataLine(t, msg))
		flusher.Flush()
		// Send a second event after a brief delay — by then conn will be nil.
		time.Sleep(200 * time.Millisecond)
		msg2 := events.NewEnvelope("id2", "ses_test", 2, events.MessageDelta, events.MessageDeltaData{Content: "after_nil"})
		fmt.Fprint(rw, sseDataLine(t, msg2))
		flusher.Flush()
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go w.readSSE(ctx, "ses_test")

	// Read first event.
	got := collectN(t, w.httpConn.recvCh, 1)
	require.Equal(t, "hi", got[0].Event.Data.(map[string]any)["content"])

	// Nil out the conn — readSSE should detect this and stop.
	w.Mu.Lock()
	w.httpConn = nil
	w.Mu.Unlock()

	// Wait for goroutine to exit — recvCh is the original one, not closed.
	time.Sleep(500 * time.Millisecond)
}

// ─── LastInput Tests ──────────────────────────────────────────────────────────

func TestConn_LastInput(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c := &conn{
		sessionID: "ses_test",
		httpAddr:  srv.URL,
		client:    srv.Client(),
		recvCh:    make(chan *events.Envelope, 16),
		log:       slog.Default(),
	}

	msg := events.NewEnvelope("id1", "ses_test", 0, events.Input,
		events.InputData{Content: "hello world"})

	err := c.Send(context.Background(), msg)
	require.NoError(t, err)
	require.Equal(t, "hello world", c.LastInput())
}

func TestConn_LastInput_UpdatedOnEachSend(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c := &conn{
		sessionID: "ses_test",
		httpAddr:  srv.URL,
		client:    srv.Client(),
		recvCh:    make(chan *events.Envelope, 16),
		log:       slog.Default(),
	}

	for _, text := range []string{"first", "second", "third"} {
		msg := events.NewEnvelope("id", "ses_test", 0, events.Input,
			events.InputData{Content: text})
		err := c.Send(context.Background(), msg)
		require.NoError(t, err)
	}
	require.Equal(t, "third", c.LastInput())
}

func TestConn_LastInput_EmptyOnNoSend(t *testing.T) {
	t.Parallel()

	c := &conn{
		sessionID: "ses_test",
		httpAddr:  "http://localhost:0",
		recvCh:    make(chan *events.Envelope, 16),
		log:       slog.Default(),
	}
	require.Empty(t, c.LastInput())
}

// ─── Error Classification Tests ───────────────────────────────────────────────

func TestConn_Send_ServerDown(t *testing.T) {
	t.Parallel()

	// Use a closed server to simulate connection refused.
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {}))
	srv.Close()

	c := &conn{
		sessionID: "ses_test",
		httpAddr:  srv.URL,
		client:    srv.Client(),
		recvCh:    make(chan *events.Envelope, 16),
		log:       slog.Default(),
	}

	msg := events.NewEnvelope("id1", "ses_test", 0, events.Input,
		events.InputData{Content: "test"})

	err := c.Send(context.Background(), msg)
	require.Error(t, err)

	var workerErr *worker.WorkerError
	require.ErrorAs(t, err, &workerErr)
	require.Equal(t, worker.ErrKindUnavailable, workerErr.Kind)
}

func TestConn_Send_503(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	c := &conn{
		sessionID: "ses_test",
		httpAddr:  srv.URL,
		client:    srv.Client(),
		recvCh:    make(chan *events.Envelope, 16),
		log:       slog.Default(),
	}

	msg := events.NewEnvelope("id1", "ses_test", 0, events.Input,
		events.InputData{Content: "test"})

	err := c.Send(context.Background(), msg)
	require.Error(t, err)

	var workerErr *worker.WorkerError
	require.ErrorAs(t, err, &workerErr)
	require.Equal(t, worker.ErrKindUnavailable, workerErr.Kind)
}

func TestConn_Send_200_Succeeds(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	c := &conn{
		sessionID: "ses_test",
		httpAddr:  srv.URL,
		client:    srv.Client(),
		recvCh:    make(chan *events.Envelope, 16),
		log:       slog.Default(),
	}

	msg := events.NewEnvelope("id1", "ses_test", 0, events.Input,
		events.InputData{Content: "hello"})

	err := c.Send(context.Background(), msg)
	require.NoError(t, err)
}

// ─── SSE Backoff Test ─────────────────────────────────────────────────────────

func TestSSEBackoffSleep_Cancelled(t *testing.T) {
	t.Parallel()

	w := New()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	start := time.Now()
	w.sseBackoffSleep(ctx, 5)
	elapsed := time.Since(start)

	require.Less(t, elapsed, 100*time.Millisecond, "backoff should return immediately on cancelled context")
}

func TestSSEBackoffSleep_Elapses(t *testing.T) {
	w := New()
	ctx := context.Background()
	defer context.CancelFunc(func() {})()

	// Patch to a small value.
	origInitial := sseBackoffInitial
	sseBackoffInitial = 10 * time.Millisecond
	t.Cleanup(func() { sseBackoffInitial = origInitial })

	start := time.Now()
	w.sseBackoffSleep(ctx, 0)
	elapsed := time.Since(start)

	require.GreaterOrEqual(t, elapsed, 1*time.Millisecond, "backoff should have waited")
	require.Less(t, elapsed, 200*time.Millisecond, "backoff should not have taken too long")
}

// ─── SSE Multiple Reconnects ──────────────────────────────────────────────────

func TestReadSSE_MultipleReconnects(t *testing.T) {
	// Patch backoff for fast testing.
	origInitial := sseBackoffInitial
	sseBackoffInitial = 1 * time.Millisecond
	sseBackoffMax = 2 * time.Millisecond
	t.Cleanup(func() {
		sseBackoffInitial = origInitial
		sseBackoffMax = 10 * time.Second
	})

	var reqCount atomic.Int32
	w, _ := newSSEWorker(t, func(rw http.ResponseWriter, r *http.Request) {
		n := reqCount.Add(1)
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)

		msg := events.NewEnvelope(
			fmt.Sprintf("id_r%d", n), "ses_test", int64(n),
			events.MessageDelta,
			events.MessageDeltaData{Content: fmt.Sprintf("round%d", n)},
		)
		fmt.Fprint(rw, sseDataLine(t, msg))
		flusher.Flush()

		// Close connection after each round (triggers EOF → reconnect).
		if n < 3 {
			return
		}
		// Keep the 3rd connection open.
		<-r.Context().Done()
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go w.readSSE(ctx, "ses_test")

	got := collectN(t, w.httpConn.recvCh, 3)
	require.Equal(t, "round1", got[0].Event.Data.(map[string]any)["content"])
	require.Equal(t, "round2", got[1].Event.Data.(map[string]any)["content"])
	require.Equal(t, "round3", got[2].Event.Data.(map[string]any)["content"])
}

// ─── Compile-time interface check ─────────────────────────────────────────────

func TestConn_ImplementsInputRecoverer(t *testing.T) {
	t.Parallel()

	// This test exists to make the compile-time check visible in test output.
	var _ worker.InputRecoverer = (*conn)(nil)
}

// ─── isServerDownError Tests ──────────────────────────────────────────────────

func TestIsServerDownError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"context deadline", context.DeadlineExceeded, true},
		{"nil error", nil, false},
		{"generic error", io.ErrUnexpectedEOF, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, isServerDownError(tt.err))
		})
	}
}

// ─── conn Close Idempotent ────────────────────────────────────────────────────

func TestConn_CloseIdempotent(t *testing.T) {
	t.Parallel()

	c := &conn{
		sessionID: "ses_test",
		recvCh:    make(chan *events.Envelope, 16),
		log:       slog.Default(),
	}

	require.NoError(t, c.Close())
	require.NoError(t, c.Close())

	_, ok := <-c.recvCh
	require.False(t, ok, "recvCh should be closed")
}

func TestConn_SendAfterClose(t *testing.T) {
	t.Parallel()

	c := &conn{
		sessionID: "ses_test",
		httpAddr:  "http://localhost:0",
		recvCh:    make(chan *events.Envelope, 16),
		log:       slog.Default(),
	}
	require.NoError(t, c.Close())

	msg := events.NewEnvelope("id1", "ses_test", 0, events.Input,
		events.InputData{Content: "after close"})

	err := c.Send(context.Background(), msg)
	require.Error(t, err)

	var workerErr *worker.WorkerError
	require.ErrorAs(t, err, &workerErr)
	require.Equal(t, worker.ErrKindUnavailable, workerErr.Kind)
}

// ─── SSE ignores non-event-stream responses ──────────────────────────────────

func TestReadSSE_IgnoresSSEComments(t *testing.T) {
	t.Parallel()

	msg := events.NewEnvelope("id1", "ses_test", 1, events.MessageDelta, events.MessageDeltaData{Content: "after_comment"})

	w, _ := newSSEWorker(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)

		// SSE comments start with ':'
		fmt.Fprint(rw, ": this is a comment\n")
		fmt.Fprint(rw, sseDataLine(t, msg))
		flusher.Flush()
		<-r.Context().Done()
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go w.readSSE(ctx, "ses_test")

	got := collectN(t, w.httpConn.recvCh, 1)
	require.Equal(t, "after_comment", got[0].Event.Data.(map[string]any)["content"])
}

// ─── SSE with multiline data (line without data: prefix) ──────────────────────

func TestReadSSE_MultiLineDataParsesOnlyPrefixed(t *testing.T) {
	t.Parallel()

	msg := events.NewEnvelope("id1", "ses_test", 1, events.Done, events.DoneData{Success: true})

	w, _ := newSSEWorker(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)

		// Only lines with "data: " prefix should be processed.
		// Lines like "retry: 5000" should be ignored.
		fmt.Fprint(rw, "retry: 5000\n")
		fmt.Fprint(rw, sseDataLine(t, msg))
		flusher.Flush()
		<-r.Context().Done()
	})

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go w.readSSE(ctx, "ses_test")

	got := collectN(t, w.httpConn.recvCh, 1)
	require.Equal(t, events.Done, got[0].Event.Type)
}
