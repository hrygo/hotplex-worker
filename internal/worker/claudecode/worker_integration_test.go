package claudecode

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/worker/base"
	"github.com/hrygo/hotplex/pkg/events"
)

func TestReadOutput_ResultSuccess(t *testing.T) {
	t.Parallel()

	w := NewWithMocks()
	mc := newMockConn("user1", "session1")
	w.testConn = mc

	lines := []string{`{"type":"result","is_error":false,"result":"all good","duration_ms":100}`}
	var idx atomic.Int64
	w.readLineFn = func() (string, error) {
		i := int(idx.Add(1) - 1)
		if i >= len(lines) {
			return "", io.EOF
		}
		return lines[i], nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	// recver drains mc.recvCh so TrySend never blocks.
	var recverWg sync.WaitGroup
	recverWg.Add(1)
	go func() {
		defer recverWg.Done()
		for range mc.Recv() {
		}
	}()

	var readWg sync.WaitGroup
	readWg.Add(1)
	go func() {
		defer readWg.Done()
		_ = mc.Recv() // satisfy defer Close guard
		w.readOutput(ctx)
	}()

	readWg.Wait()
	cancel()
	recverWg.Wait()

	sent := mc.sentEnvelopes()
	require.Len(t, sent, 1)
	require.Equal(t, events.Done, sent[0].Event.Type)
}

func TestReadOutput_ResultError(t *testing.T) {
	t.Parallel()

	w := NewWithMocks()
	mc := newMockConn("user1", "session1")
	w.testConn = mc

	lines := []string{`{"type":"result","is_error":true,"result":"something went wrong"}`}
	var idx atomic.Int64
	w.readLineFn = func() (string, error) {
		i := int(idx.Add(1) - 1)
		if i >= len(lines) {
			return "", io.EOF
		}
		return lines[i], nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	var recverWg sync.WaitGroup
	recverWg.Add(1)
	go func() {
		defer recverWg.Done()
		for range mc.Recv() {
		}
	}()

	var readWg sync.WaitGroup
	readWg.Add(1)
	go func() {
		defer readWg.Done()
		_ = mc.Recv()
		w.readOutput(ctx)
	}()

	readWg.Wait()
	cancel()
	recverWg.Wait()

	sent := mc.sentEnvelopes()
	require.Len(t, sent, 2)
	require.Equal(t, events.Error, sent[0].Event.Type)
	require.Equal(t, events.Done, sent[1].Event.Type)
}

func TestReadOutput_ParseError_Continues(t *testing.T) {
	t.Parallel()

	w := NewWithMocks()
	mc := newMockConn("user1", "session1")
	w.testConn = mc

	lines := []string{
		`{invalid json`,
		`{"type":"result","is_error":false,"result":"ok"}`,
	}
	var idx atomic.Int64
	w.readLineFn = func() (string, error) {
		i := int(idx.Add(1) - 1)
		if i >= len(lines) {
			return "", io.EOF
		}
		return lines[i], nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	var recverWg sync.WaitGroup
	recverWg.Add(1)
	go func() {
		defer recverWg.Done()
		for range mc.Recv() {
		}
	}()

	var readWg sync.WaitGroup
	readWg.Add(1)
	go func() {
		defer readWg.Done()
		_ = mc.Recv()
		w.readOutput(ctx)
	}()

	readWg.Wait()
	cancel()
	recverWg.Wait()

	sent := mc.sentEnvelopes()
	require.Len(t, sent, 1)
	require.Equal(t, events.Done, sent[0].Event.Type)
}

func TestReadOutput_TrySendDropOnFullChannel(t *testing.T) {
	t.Parallel()

	w := NewWithMocks()
	mc := newMockConn("user1", "session1")
	mc.setBlockSend(true) // send always blocks → TrySend drops silently
	w.testConn = mc

	lines := []string{`{"type":"result","is_error":false,"result":"ok"}`}
	var idx atomic.Int64
	w.readLineFn = func() (string, error) {
		i := int(idx.Add(1) - 1)
		if i >= len(lines) {
			return "", io.EOF
		}
		return lines[i], nil
	}

	ctx, cancel := context.WithCancel(context.Background())

	var recverWg sync.WaitGroup
	recverWg.Add(1)
	go func() {
		defer recverWg.Done()
		for range mc.Recv() {
		}
	}()

	var readWg sync.WaitGroup
	readWg.Add(1)
	go func() {
		defer readWg.Done()
		_ = mc.Recv()
		w.readOutput(ctx)
	}()

	readWg.Wait()
	cancel()
	recverWg.Wait()

	sent := mc.sentEnvelopes()
	require.Len(t, sent, 0)
}

func TestReadOutput_NilReadLineFn_ReturnsEarly(t *testing.T) {
	t.Parallel()

	w := NewWithMocks()
	mc := newMockConn("user1", "session1")
	w.testConn = mc
	// w.readLineFn stays nil → readOutput returns immediately without panic

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	var panicked bool
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
			wg.Done()
		}()
		_ = mc.Recv()
		w.readOutput(ctx)
	}()

	wg.Wait()
	require.False(t, panicked)
}

func TestInput_PermissionResponse(t *testing.T) {
	t.Parallel()

	w := NewWithMocks()
	mc := newMockConn("user1", "session1")
	mc.setBlockSend(true)
	w.testConn = mc

	permResp := map[string]any{
		"permission_response": map[string]any{
			"request_id": "req_abc",
			"allowed":    true,
			"reason":     "testing",
		},
	}

	err := w.Input(context.Background(), "approve it", permResp)
	require.NoError(t, err)
	// control.SendPermissionResponse called; no conn.Send to mc (stdin blocked)
}

func TestInput_NotStarted_ReturnsError(t *testing.T) {
	t.Parallel()

	w := New()
	err := w.Input(context.Background(), "hello", nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "not started")
}

// NewWithMocks creates a Worker with protocol layers initialised for testing.
// The worker's readLineFn must be set before readOutput is called.
func NewWithMocks() *Worker {
	return &Worker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		parser:     NewParser(slog.Default()),
		mapper:     NewMapper(slog.Default(), "test-session", func() int64 { return 1 }),
		// stdin is set by the test via w.testConn = mc; mc.StdinWriter() = io.Discard
		control: NewControlHandler(slog.Default(), io.Discard),
	}
}
