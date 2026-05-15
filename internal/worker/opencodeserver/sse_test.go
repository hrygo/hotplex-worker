package opencodeserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// ─── Test Helpers ──────────────────────────────────────────────────────────────

// newSingletonWithSSE creates a SingletonProcessManager wired to a mock SSE server.
func newSingletonWithSSE(t *testing.T, handler http.HandlerFunc) (*SingletonProcessManager, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	s := &SingletonProcessManager{
		log:         slog.Default().With("component", "test-singleton"),
		client:      srv.Client(),
		sseClient:   srv.Client(),
		httpAddr:    srv.URL,
		subscribers: make(map[string]chan *events.Envelope),
	}
	return s, srv
}

// ocsEvent builds a JSON OCS global event with the given type and properties.
func ocsEvent(t *testing.T, eventType string, props map[string]any) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"payload": map[string]any{
			"type":       eventType,
			"properties": props,
		},
	})
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

// patchBackoff sets fast backoff values for testing and restores on cleanup.
func patchBackoff(t *testing.T) {
	t.Helper()
	origInitial := sseBackoffInitial
	origMax := sseBackoffMax
	sseBackoffInitial = 1 * time.Millisecond
	sseBackoffMax = 2 * time.Millisecond
	t.Cleanup(func() {
		sseBackoffInitial = origInitial
		sseBackoffMax = origMax
	})
}

// ─── Global SSE → EventBus Dispatch Tests ─────────────────────────────────────

func TestReadGlobalSSE_DispatchesMessagePartUpdated(t *testing.T) {
	t.Parallel()

	s, _ := newSingletonWithSSE(t, func(rw http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/global/event", r.URL.Path)
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)

		evt := ocsEvent(t, "message.part.updated", map[string]any{
			"sessionID": "ses_1",
			"part":      map[string]any{"type": "text", "text": "hello"},
		})
		fmt.Fprint(rw, evt)
		flusher.Flush()
		<-r.Context().Done()
	})

	ch := s.Subscribe("ses_1")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go s.readGlobalSSE(ctx)

	got := collectN(t, ch, 1)
	require.Equal(t, events.MessageDelta, got[0].Event.Type)
	require.Equal(t, "ses_1", got[0].SessionID)
	s.Unsubscribe("ses_1")
}

func TestReadGlobalSSE_DispatchesSessionStatus(t *testing.T) {
	t.Parallel()

	s, _ := newSingletonWithSSE(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)

		evt := ocsEvent(t, "session.status", map[string]any{
			"sessionID": "ses_1",
			"status":    map[string]any{"type": "idle"},
		})
		fmt.Fprint(rw, evt)
		flusher.Flush()
		<-r.Context().Done()
	})

	ch := s.Subscribe("ses_1")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go s.readGlobalSSE(ctx)

	got := collectN(t, ch, 1)
	require.Equal(t, events.Done, got[0].Event.Type)
	s.Unsubscribe("ses_1")
}

func TestReadGlobalSSE_DispatchesPartDelta(t *testing.T) {
	t.Parallel()

	s, _ := newSingletonWithSSE(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)

		// Streaming delta events — the real-time text from OCS.
		evt1 := ocsEvent(t, "message.part.delta", map[string]any{
			"sessionID": "ses_1",
			"field":     "text",
			"delta":     "Hel",
		})
		evt2 := ocsEvent(t, "message.part.delta", map[string]any{
			"sessionID": "ses_1",
			"field":     "text",
			"delta":     "lo",
		})
		fmt.Fprint(rw, evt1)
		flusher.Flush()
		fmt.Fprint(rw, evt2)
		flusher.Flush()
		<-r.Context().Done()
	})

	ch := s.Subscribe("ses_1")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go s.readGlobalSSE(ctx)

	got := collectN(t, ch, 2)
	require.Equal(t, events.MessageDelta, got[0].Event.Type)
	require.Equal(t, "Hel", got[0].Event.Data.(events.MessageDeltaData).Content)
	require.Equal(t, "lo", got[1].Event.Data.(events.MessageDeltaData).Content)
	s.Unsubscribe("ses_1")
}

func TestReadGlobalSSE_DispatchesSessionIdle(t *testing.T) {
	t.Parallel()

	s, _ := newSingletonWithSSE(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)

		evt := ocsEvent(t, "session.idle", map[string]any{
			"sessionID": "ses_1",
		})
		fmt.Fprint(rw, evt)
		flusher.Flush()
		<-r.Context().Done()
	})

	ch := s.Subscribe("ses_1")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go s.readGlobalSSE(ctx)

	got := collectN(t, ch, 1)
	require.Equal(t, events.Done, got[0].Event.Type)
	s.Unsubscribe("ses_1")
}

func TestReadGlobalSSE_DispatchesSessionError(t *testing.T) {
	t.Parallel()

	s, _ := newSingletonWithSSE(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)

		evt := ocsEvent(t, "session.error", map[string]any{
			"sessionID": "ses_1",
			"error":     map[string]any{"name": "APIError", "data": map[string]any{"message": "rate limited"}},
		})
		fmt.Fprint(rw, evt)
		flusher.Flush()
		<-r.Context().Done()
	})

	ch := s.Subscribe("ses_1")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go s.readGlobalSSE(ctx)

	got := collectN(t, ch, 1)
	require.Equal(t, events.Error, got[0].Event.Type)
	require.Equal(t, "rate limited", got[0].Event.Data.(events.ErrorData).Message)
	s.Unsubscribe("ses_1")
}

func TestReadGlobalSSE_DispatchesPermissionAsked(t *testing.T) {
	t.Parallel()

	s, _ := newSingletonWithSSE(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)

		evt := ocsEvent(t, "permission.asked", map[string]any{
			"sessionID": "ses_1",
			"id":        "perm_1",
			"metadata":  map[string]any{"tool": "bash"},
		})
		fmt.Fprint(rw, evt)
		flusher.Flush()
		<-r.Context().Done()
	})

	ch := s.Subscribe("ses_1")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go s.readGlobalSSE(ctx)

	got := collectN(t, ch, 1)
	require.Equal(t, events.Raw, got[0].Event.Type)
	data := got[0].Event.Data.(events.RawData)
	require.Equal(t, "ocs:permission.asked", data.Kind)
	s.Unsubscribe("ses_1")
}

func TestReadGlobalSSE_SkipsSyncEvents(t *testing.T) {
	t.Parallel()

	s, _ := newSingletonWithSSE(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)

		syncEvt := ocsEvent(t, "sync", map[string]any{"sessionID": "ses_1"})
		fmt.Fprint(rw, syncEvt)
		flusher.Flush()

		connEvt := ocsEvent(t, "server.connected", nil)
		fmt.Fprint(rw, connEvt)
		flusher.Flush()

		hbEvt := ocsEvent(t, "server.heartbeat", nil)
		fmt.Fprint(rw, hbEvt)
		flusher.Flush()

		gdEvt := ocsEvent(t, "global.disposed", nil)
		fmt.Fprint(rw, gdEvt)
		flusher.Flush()

		msgEvt := ocsEvent(t, "message.part.updated", map[string]any{
			"sessionID": "ses_1",
			"part":      map[string]any{"type": "text", "text": "hi"},
		})
		fmt.Fprint(rw, msgEvt)
		flusher.Flush()
		<-r.Context().Done()
	})

	ch := s.Subscribe("ses_1")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go s.readGlobalSSE(ctx)

	got := collectN(t, ch, 1)
	require.Equal(t, events.MessageDelta, got[0].Event.Type)
	s.Unsubscribe("ses_1")
}

func TestReadGlobalSSE_IgnoresEmptyLinesAndComments(t *testing.T) {
	t.Parallel()

	s, _ := newSingletonWithSSE(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)

		fmt.Fprint(rw, "\n\n")
		fmt.Fprint(rw, ": this is a comment\n")
		fmt.Fprint(rw, "retry: 5000\n")
		fmt.Fprint(rw, "event: message\n")

		evt := ocsEvent(t, "message.part.updated", map[string]any{
			"sessionID": "ses_1",
			"part":      map[string]any{"type": "text", "text": "after_noise"},
		})
		fmt.Fprint(rw, evt)
		flusher.Flush()
		<-r.Context().Done()
	})

	ch := s.Subscribe("ses_1")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go s.readGlobalSSE(ctx)

	got := collectN(t, ch, 1)
	require.Equal(t, events.MessageDelta, got[0].Event.Type)
	s.Unsubscribe("ses_1")
}

func TestReadGlobalSSE_MultipleSessions(t *testing.T) {
	t.Parallel()

	s, _ := newSingletonWithSSE(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)

		for _, sid := range []string{"ses_A", "ses_B"} {
			evt := ocsEvent(t, "message.part.updated", map[string]any{
				"sessionID": sid,
				"part":      map[string]any{"type": "text", "text": "msg_" + sid},
			})
			fmt.Fprint(rw, evt)
		}
		flusher.Flush()
		<-r.Context().Done()
	})

	chA := s.Subscribe("ses_A")
	chB := s.Subscribe("ses_B")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go s.readGlobalSSE(ctx)

	gotA := collectN(t, chA, 1)
	require.Equal(t, "ses_A", gotA[0].SessionID)

	gotB := collectN(t, chB, 1)
	require.Equal(t, "ses_B", gotB[0].SessionID)

	s.Unsubscribe("ses_A")
	s.Unsubscribe("ses_B")
}

func TestReadGlobalSSE_UnsubscribeDuringDispatch(t *testing.T) {
	t.Parallel()

	s, _ := newSingletonWithSSE(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)

		evt := ocsEvent(t, "message.part.updated", map[string]any{
			"sessionID": "ses_1",
			"part":      map[string]any{"type": "text", "text": "first"},
		})
		fmt.Fprint(rw, evt)
		flusher.Flush()

		time.Sleep(100 * time.Millisecond)

		evt2 := ocsEvent(t, "message.part.updated", map[string]any{
			"sessionID": "ses_1",
			"part":      map[string]any{"type": "text", "text": "after_unsub"},
		})
		fmt.Fprint(rw, evt2)
		flusher.Flush()
		<-r.Context().Done()
	})

	ch := s.Subscribe("ses_1")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go s.readGlobalSSE(ctx)

	got := collectN(t, ch, 1)
	require.Equal(t, "first", got[0].Event.Data.(events.MessageDeltaData).Content)

	// Unsubscribe — second event should be silently dropped (no panic).
	s.Unsubscribe("ses_1")

	_, ok := <-ch
	require.False(t, ok, "channel should be closed after unsubscribe")
}

// NOTE: Tests that patch package-level backoff vars must NOT use t.Parallel()
// to avoid data races with concurrent goroutines reading those vars.

func TestReadGlobalSSE_EOF_Reconnects(t *testing.T) {
	patchBackoff(t)

	var reqCount atomic.Int32
	s, _ := newSingletonWithSSE(t, func(rw http.ResponseWriter, r *http.Request) {
		n := reqCount.Add(1)
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)

		evt := ocsEvent(t, "message.part.updated", map[string]any{
			"sessionID": "ses_1",
			"part":      map[string]any{"type": "text", "text": fmt.Sprintf("round%d", n)},
		})
		fmt.Fprint(rw, evt)
		flusher.Flush()

		if n < 3 {
			return
		}
		<-r.Context().Done()
	})

	ch := s.Subscribe("ses_1")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go s.readGlobalSSE(ctx)

	got := collectN(t, ch, 3)
	require.Equal(t, "round1", got[0].Event.Data.(events.MessageDeltaData).Content)
	require.Equal(t, "round2", got[1].Event.Data.(events.MessageDeltaData).Content)
	require.Equal(t, "round3", got[2].Event.Data.(events.MessageDeltaData).Content)
	s.Unsubscribe("ses_1")
}

func TestReadGlobalSSE_HTTPError_Reconnects(t *testing.T) {
	patchBackoff(t)

	var reqCount atomic.Int32
	s, _ := newSingletonWithSSE(t, func(rw http.ResponseWriter, r *http.Request) {
		n := reqCount.Add(1)
		if n <= 2 {
			rw.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)

		evt := ocsEvent(t, "message.part.updated", map[string]any{
			"sessionID": "ses_1",
			"part":      map[string]any{"type": "text", "text": "after_503"},
		})
		fmt.Fprint(rw, evt)
		flusher.Flush()
		<-r.Context().Done()
	})

	ch := s.Subscribe("ses_1")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go s.readGlobalSSE(ctx)

	got := collectN(t, ch, 1)
	require.Equal(t, "after_503", got[0].Event.Data.(events.MessageDeltaData).Content)
	s.Unsubscribe("ses_1")
}

func TestReadGlobalSSE_MaxReconnects_Stops(t *testing.T) {
	origMax := sseMaxReconnects
	sseMaxReconnects = 3
	patchBackoff(t)
	t.Cleanup(func() { sseMaxReconnects = origMax })

	s, _ := newSingletonWithSSE(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusServiceUnavailable)
	})

	ch := s.Subscribe("ses_1")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go s.readGlobalSSE(ctx)

	select {
	case env, ok := <-ch:
		if ok && env != nil {
			t.Fatal("unexpected event after max reconnects")
		}
	case <-time.After(3 * time.Second):
		// Expected: no events within timeout, goroutine exited.
	}
	s.Unsubscribe("ses_1")
}

func TestReadGlobalSSE_ContextCancel_Stops(t *testing.T) {
	t.Parallel()

	s, _ := newSingletonWithSSE(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)

		evt := ocsEvent(t, "message.part.updated", map[string]any{
			"sessionID": "ses_1",
			"part":      map[string]any{"type": "text", "text": "hi"},
		})
		fmt.Fprint(rw, evt)
		flusher.Flush()
		<-r.Context().Done()
	})

	ch := s.Subscribe("ses_1")

	ctx, cancel := context.WithCancel(t.Context())
	go s.readGlobalSSE(ctx)

	got := collectN(t, ch, 1)
	require.Equal(t, "hi", got[0].Event.Data.(events.MessageDeltaData).Content)

	cancel()
	time.Sleep(200 * time.Millisecond)
}

func TestReadGlobalSSE_Backpressure_DropOnFull(t *testing.T) {
	t.Parallel()

	s, _ := newSingletonWithSSE(t, func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)

		for i := range 10 {
			evt := ocsEvent(t, "message.part.updated", map[string]any{
				"sessionID": "ses_1",
				"part":      map[string]any{"type": "text", "text": fmt.Sprintf("msg%d", i)},
			})
			fmt.Fprint(rw, evt)
		}
		flusher.Flush()
		<-r.Context().Done()
	})

	// Subscribe with a small-buffered channel manually.
	s.busMu.Lock()
	ch := make(chan *events.Envelope, 2)
	s.subscribers["ses_1"] = ch
	s.busMu.Unlock()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go s.readGlobalSSE(ctx)

	got := collectN(t, ch, 1)
	require.Equal(t, events.MessageDelta, got[0].Event.Type)
	s.Unsubscribe("ses_1")
}

func TestReadGlobalSSE_EmptyStream_Backoff(t *testing.T) {
	patchBackoff(t)

	var reqCount atomic.Int32
	s, _ := newSingletonWithSSE(t, func(rw http.ResponseWriter, r *http.Request) {
		n := reqCount.Add(1)
		if n < 3 {
			rw.WriteHeader(http.StatusOK)
			return
		}
		rw.Header().Set("Content-Type", "text/event-stream")
		flusher := rw.(http.Flusher)
		evt := ocsEvent(t, "message.part.updated", map[string]any{
			"sessionID": "ses_1",
			"part":      map[string]any{"type": "text", "text": "after_empty"},
		})
		fmt.Fprint(rw, evt)
		flusher.Flush()
		<-r.Context().Done()
	})

	ch := s.Subscribe("ses_1")

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go s.readGlobalSSE(ctx)

	got := collectN(t, ch, 1)
	require.Equal(t, "after_empty", got[0].Event.Data.(events.MessageDeltaData).Content)
	s.Unsubscribe("ses_1")
}

// ─── Worker forwardBusEvents Tests ────────────────────────────────────────────

func newWorkerWithBusCh(t *testing.T) (*Worker, chan *events.Envelope) {
	t.Helper()
	w := New()
	recvCh := make(chan *events.Envelope, 256)
	w.httpConn = &conn{
		sessionID: "ses_test",
		userID:    "u_test",
		recvCh:    recvCh,
		log:       w.Log,
	}
	busCh := make(chan *events.Envelope, 16)
	return w, busCh
}

func TestForwardBusEvents_MessageDelta(t *testing.T) {
	t.Parallel()

	w, busCh := newWorkerWithBusCh(t)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go w.forwardBusEvents(ctx, "ses_test", busCh)

	env := events.NewEnvelope("id1", "ses_test", 1, events.MessageDelta,
		events.MessageDeltaData{Content: "hello"})
	busCh <- env

	got := collectN(t, w.httpConn.recvCh, 1)
	require.Equal(t, events.MessageDelta, got[0].Event.Type)
}

func TestForwardBusEvents_PermissionAsked(t *testing.T) {
	t.Parallel()

	w, busCh := newWorkerWithBusCh(t)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go w.forwardBusEvents(ctx, "ses_test", busCh)

	props, _ := json.Marshal(map[string]any{
		"id":       "perm_1",
		"metadata": map[string]any{"tool": "bash"},
	})
	// Use json.RawMessage so the Raw field is the correct type after round-trip.
	rawEnv := events.NewEnvelope("id1", "ses_test", 0, events.Raw,
		events.RawData{Kind: "ocs:permission.asked", Raw: json.RawMessage(props)})
	busCh <- rawEnv

	got := collectN(t, w.httpConn.recvCh, 1)
	require.Equal(t, events.PermissionRequest, got[0].Event.Type)
}

func TestForwardBusEvents_QuestionAsked(t *testing.T) {
	t.Parallel()

	w, busCh := newWorkerWithBusCh(t)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go w.forwardBusEvents(ctx, "ses_test", busCh)

	props, _ := json.Marshal(map[string]any{
		"id":        "q_1",
		"questions": []map[string]any{{"id": "q1", "title": "Confirm?"}},
	})
	rawEnv := events.NewEnvelope("id1", "ses_test", 0, events.Raw,
		events.RawData{Kind: "ocs:question.asked", Raw: json.RawMessage(props)})
	busCh <- rawEnv

	got := collectN(t, w.httpConn.recvCh, 1)
	require.Equal(t, events.QuestionRequest, got[0].Event.Type)
}

func TestForwardBusEvents_ChannelClosed_Stops(t *testing.T) {
	t.Parallel()

	w, busCh := newWorkerWithBusCh(t)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go w.forwardBusEvents(ctx, "ses_test", busCh)

	close(busCh)

	require.Eventually(t, func() bool {
		select {
		case _, ok := <-w.httpConn.recvCh:
			return !ok
		default:
			return true
		}
	}, 3*time.Second, 50*time.Millisecond, "forwardBusEvents should exit without closing recvCh")
}

func TestForwardBusEvents_ContextCancel_Stops(t *testing.T) {
	t.Parallel()

	w, busCh := newWorkerWithBusCh(t)

	ctx, cancel := context.WithCancel(t.Context())
	go w.forwardBusEvents(ctx, "ses_test", busCh)

	env := events.NewEnvelope("id1", "ses_test", 1, events.MessageDelta,
		events.MessageDeltaData{Content: "hi"})
	busCh <- env

	got := collectN(t, w.httpConn.recvCh, 1)
	require.Equal(t, "hi", got[0].Event.Data.(events.MessageDeltaData).Content)

	cancel()
	time.Sleep(200 * time.Millisecond)
}

func TestForwardBusEvents_ConnNil_Stops(t *testing.T) {
	t.Parallel()

	w, busCh := newWorkerWithBusCh(t)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go w.forwardBusEvents(ctx, "ses_test", busCh)

	env := events.NewEnvelope("id1", "ses_test", 1, events.MessageDelta,
		events.MessageDeltaData{Content: "first"})
	busCh <- env
	got := collectN(t, w.httpConn.recvCh, 1)
	require.Equal(t, "first", got[0].Event.Data.(events.MessageDeltaData).Content)

	w.Mu.Lock()
	w.httpConn = nil
	w.Mu.Unlock()

	env2 := events.NewEnvelope("id2", "ses_test", 2, events.MessageDelta,
		events.MessageDeltaData{Content: "after_nil"})
	busCh <- env2

	time.Sleep(300 * time.Millisecond)
}

// ─── Subscribe / Unsubscribe Tests ────────────────────────────────────────────

func TestSubscribe_Idempotent(t *testing.T) {
	t.Parallel()

	s, _ := newSingletonWithSSE(t, nil)

	ch1 := s.Subscribe("ses_1")
	ch2 := s.Subscribe("ses_1")
	require.Equal(t, ch1, ch2, "Subscribe should return same channel for same session")
	s.Unsubscribe("ses_1")
}

func TestUnsubscribe_DoubleSafe(t *testing.T) {
	t.Parallel()

	s, _ := newSingletonWithSSE(t, nil)

	ch := s.Subscribe("ses_1")
	s.Unsubscribe("ses_1")
	s.Unsubscribe("ses_1")

	_, ok := <-ch
	require.False(t, ok, "channel should be closed")
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

	s, _ := newSingletonWithSSE(t, nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	start := time.Now()
	s.sseBackoffSleep(ctx, 5)
	elapsed := time.Since(start)

	require.Less(t, elapsed, 100*time.Millisecond, "backoff should return immediately on cancelled context")
}

func TestSSEBackoffSleep_Elapses(t *testing.T) {
	s, _ := newSingletonWithSSE(t, nil)
	ctx := context.Background()

	origInitial := sseBackoffInitial
	sseBackoffInitial = 10 * time.Millisecond
	t.Cleanup(func() { sseBackoffInitial = origInitial })

	start := time.Now()
	s.sseBackoffSleep(ctx, 0)
	elapsed := time.Since(start)

	require.GreaterOrEqual(t, elapsed, 1*time.Millisecond, "backoff should have waited")
	require.Less(t, elapsed, 200*time.Millisecond, "backoff should not have taken too long")
}

// ─── Compile-time interface check ─────────────────────────────────────────────

func TestConn_ImplementsInputRecoverer(t *testing.T) {
	t.Parallel()
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

// ─── Unreferenced import guard ────────────────────────────────────────────────

var _ = strings.NewReader
var _ = aep.NewID
