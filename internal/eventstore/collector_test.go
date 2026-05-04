package eventstore

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/events"
)

func TestCollector_CaptureDeltaString(t *testing.T) {
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())

	c.CaptureDeltaString("s1", 4, "Hello")
	c.CaptureDeltaString("s1", 5, " world")
	c.Capture("s1", 6, events.MessageEnd, nil, "outbound")

	require.NoError(t, c.Close())

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	require.Len(t, page.Events, 1)
	require.Equal(t, int64(4), page.Events[0].Seq)
	require.Equal(t, string(events.Message), page.Events[0].Type)

	var data map[string]any
	require.NoError(t, json.Unmarshal(page.Events[0].Data, &data))
	require.Equal(t, "Hello world", data["content"])
	require.Equal(t, float64(2), data["merged_count"])
}

func TestCollector_CaptureDeltaStringSizeFlush(t *testing.T) {
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())

	// First chunk: 3000 bytes < 4096 threshold
	c.CaptureDeltaString("s1", 1, strings.Repeat("a", 3000))
	// Second chunk: 3000+1100=4100 >= 4096 → immediate flush
	c.CaptureDeltaString("s1", 2, strings.Repeat("b", 1100))

	// Done triggers flush of any remaining (none in this case)
	c.Capture("s1", 3, events.Done, json.RawMessage(`{}`), "outbound")
	require.NoError(t, c.Close())

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	// Size-flushed Message + Done
	require.Len(t, page.Events, 2)

	require.Equal(t, string(events.Message), page.Events[0].Type)
	require.Equal(t, int64(1), page.Events[0].Seq)

	var data map[string]any
	require.NoError(t, json.Unmarshal(page.Events[0].Data, &data))
	require.Equal(t, float64(2), data["merged_count"])
	content, _ := data["content"].(string)
	require.Len(t, content, 4100)
}

func TestCollector_MessageEndFlushWithoutStore(t *testing.T) {
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())

	c.CaptureDeltaString("s1", 3, "Hello")
	c.CaptureDeltaString("s1", 4, " world")
	// MessageEnd triggers flush but is NOT stored
	c.Capture("s1", 5, events.MessageEnd, json.RawMessage(`{}`), "outbound")
	c.Capture("s1", 6, events.Done, json.RawMessage(`{}`), "outbound")

	require.NoError(t, c.Close())

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	// Message (flushed deltas) + Done. MessageEnd NOT stored.
	require.Len(t, page.Events, 2)
	require.Equal(t, string(events.Message), page.Events[0].Type)
	require.Equal(t, int64(3), page.Events[0].Seq)
	require.Equal(t, string(events.Done), page.Events[1].Type)
}

func TestCollector_ResetSession(t *testing.T) {
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())

	c.CaptureDeltaString("s1", 1, "old content to discard")

	// Simulate retry
	c.ResetSession("s1")

	// New content after retry
	c.CaptureDeltaString("s1", 10, "new content")
	c.Capture("s1", 11, events.MessageEnd, nil, "outbound")

	require.NoError(t, c.Close())

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	require.Len(t, page.Events, 1)

	var data map[string]any
	require.NoError(t, json.Unmarshal(page.Events[0].Data, &data))
	require.Equal(t, "new content", data["content"])
	require.Equal(t, float64(1), data["merged_count"])
}

func TestCollector_CreatedAtUsesFirstSeenAt(t *testing.T) {
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())

	before := time.Now()
	c.CaptureDeltaString("s1", 1, "first")
	time.Sleep(50 * time.Millisecond)
	c.CaptureDeltaString("s1", 2, "second")
	// Flush well after first delta
	time.Sleep(50 * time.Millisecond)
	c.Capture("s1", 3, events.MessageEnd, nil, "outbound")
	require.NoError(t, c.Close())

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	require.Len(t, page.Events, 1)

	createdAt := time.UnixMilli(page.Events[0].CreatedAt)
	// created_at should be close to before (first delta), not to flush time
	require.WithinDuration(t, before, createdAt, 50*time.Millisecond)
}

func TestCollector_ReplaySeqOrdering(t *testing.T) {
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())

	// Full turn: Input → State → Delta×2 → MessageEnd → ToolCall → Delta×2 → MessageEnd → Done
	c.Capture("s1", 1, events.Input, json.RawMessage(`{"content":"do it"}`), "inbound")
	c.Capture("s1", 2, events.State, json.RawMessage(`{"state":"running"}`), "outbound")

	c.CaptureDeltaString("s1", 4, "Hello")
	c.CaptureDeltaString("s1", 5, " world")
	c.Capture("s1", 6, events.MessageEnd, nil, "outbound")

	c.Capture("s1", 7, events.ToolCall, json.RawMessage(`{"name":"read"}`), "outbound")

	c.CaptureDeltaString("s1", 9, "Result")
	c.CaptureDeltaString("s1", 10, " done")
	c.Capture("s1", 11, events.MessageEnd, nil, "outbound")

	c.Capture("s1", 12, events.Done, json.RawMessage(`{}`), "outbound")
	require.NoError(t, c.Close())

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)

	// Input(1) State(2) Message(4) ToolCall(7) Message(9) Done(12)
	require.Len(t, page.Events, 6)

	seqs := make([]int64, len(page.Events))
	types := make([]string, len(page.Events))
	for i, e := range page.Events {
		seqs[i] = e.Seq
		types[i] = e.Type
	}

	require.Equal(t, []int64{1, 2, 4, 7, 9, 12}, seqs)
	require.Equal(t, []string{
		string(events.Input), string(events.State), string(events.Message),
		string(events.ToolCall), string(events.Message), string(events.Done),
	}, types)

	// Input (question) always before Messages (answer)
	require.Less(t, seqs[0], seqs[2])
	require.Less(t, seqs[0], seqs[4])
}

func TestCollector_ConcurrentFlushNoLoss(t *testing.T) {
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())

	const goroutines = 10
	const deltasPer = 100
	done := make(chan struct{})

	for g := range goroutines {
		go func(g int) {
			defer func() { done <- struct{}{} }()
			for d := range deltasPer {
				c.CaptureDeltaString("s1", int64(g*deltasPer+d+1), "x")
			}
		}(g)
	}
	for range goroutines {
		<-done
	}

	c.Capture("s1", int64(goroutines*deltasPer+1), events.Done, json.RawMessage(`{}`), "outbound")
	require.NoError(t, c.Close())

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 1000)
	require.NoError(t, err)

	totalChars := 0
	for _, e := range page.Events {
		if e.Type != string(events.Message) {
			continue
		}
		var data map[string]any
		require.NoError(t, json.Unmarshal(e.Data, &data))
		content, _ := data["content"].(string)
		totalChars += len(content)
	}
	require.Equal(t, goroutines*deltasPer, totalChars)
}

func TestCollector_TimerFlush(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping timer test in short mode")
	}

	store := newTestStore(t)
	c := NewCollector(store, slog.Default())
	defer func() { _ = c.Close() }()

	// Accumulate small content (< 4096) so size trigger won't fire
	c.CaptureDeltaString("s1", 1, "chunk1")
	c.CaptureDeltaString("s1", 2, "chunk2")

	// Wait for timer trigger (deltaFlushInterval = 2s + ticker margin)
	time.Sleep(deltaFlushInterval + 200*time.Millisecond)

	// Verify Message was written by timer flush (without Close)
	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	require.Len(t, page.Events, 1)
	require.Equal(t, string(events.Message), page.Events[0].Type)

	var data map[string]any
	require.NoError(t, json.Unmarshal(page.Events[0].Data, &data))
	require.Equal(t, "chunk1chunk2", data["content"])
}

func TestCollector_ResetSessionEmptyFlush(t *testing.T) {
	store := newTestStore(t)
	c := NewCollector(store, slog.Default())

	// No deltas accumulated, reset should be no-op
	c.ResetSession("s1")
	c.Capture("s1", 1, events.Done, json.RawMessage(`{}`), "outbound")
	require.NoError(t, c.Close())

	page, err := store.QueryBySession(context.Background(), "s1", 0, CursorLatest, 100)
	require.NoError(t, err)
	// Only Done, no Message
	require.Len(t, page.Events, 1)
	require.Equal(t, string(events.Done), page.Events[0].Type)
}
