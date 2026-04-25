package base

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// ---------------------------------------------------------------------------
// Conn.Send
// ---------------------------------------------------------------------------

func TestConn_Send_WritesNDJSON(t *testing.T) {
	t.Parallel()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	t.Cleanup(func() { _ = w.Close() })

	conn := NewConn(slog.Default(), w, "user1", "sess1")

	env := &events.Envelope{
		ID:        "msg-1",
		SessionID: "sess1",
		Seq:       1,
		Event: events.Event{
			Type: events.MessageStart,
			Data: map[string]string{"text": "hello"},
		},
	}

	err = conn.Send(context.Background(), env)
	require.NoError(t, err)

	// Close write side so Read can EOF.
	require.NoError(t, w.Close())

	// Read back what was written and verify it's valid NDJSON.
	data := make([]byte, 4096)
	n, err := r.Read(data)
	require.NoError(t, err)
	require.Greater(t, n, 0)

	var decoded events.Envelope
	require.NoError(t, json.Unmarshal(data[:n], &decoded))
	require.Equal(t, "msg-1", decoded.ID)
	require.Equal(t, events.MessageStart, decoded.Event.Type)
}

func TestConn_Send_ClosedConn(t *testing.T) {
	t.Parallel()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	t.Cleanup(func() { _ = w.Close() })

	conn := NewConn(slog.Default(), w, "user1", "sess1")
	require.NoError(t, conn.Close())

	err = conn.Send(context.Background(), &events.Envelope{
		ID:        "msg-x",
		SessionID: "sess1",
		Seq:       1,
		Event:     events.Event{Type: events.MessageStart},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "connection closed")

	// Drain the read end so pipe cleanup is clean.
	_, _ = r.Read(make([]byte, 1))
}

func TestConn_Send_NilStdin(t *testing.T) {
	t.Parallel()

	conn := NewConn(slog.Default(), nil, "user1", "sess1")

	// aep.Encode on nil writer should panic or error.
	// Send with nil stdin should return an error from aep.Encode.
	err := conn.Send(context.Background(), &events.Envelope{
		ID:        "msg-x",
		SessionID: "sess1",
		Seq:       1,
		Event:     events.Event{Type: events.MessageStart},
	})
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// WriteAll
// ---------------------------------------------------------------------------

func TestWriteAll_Pipe(t *testing.T) {
	t.Parallel()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	t.Cleanup(func() { _ = w.Close() })

	payload := []byte("hello writeAll\n")
	require.NoError(t, WriteAll(int(w.Fd()), payload))

	require.NoError(t, w.Close())

	buf := make([]byte, 128)
	n, err := r.Read(buf)
	require.NoError(t, err)
	require.Equal(t, string(payload), string(buf[:n]))
}

func TestWriteAll_LargePayload(t *testing.T) {
	t.Parallel()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	t.Cleanup(func() { _ = w.Close() })

	// Write a payload larger than a typical pipe buffer (usually 64KB on Linux, 8KB on macOS).
	payload := make([]byte, 256*1024)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	// Drain the read end concurrently so WriteAll doesn't block on a full pipe buffer.
	gotCh := make(chan []byte, 1)
	go func() {
		var got []byte
		buf := make([]byte, 32*1024)
		for {
			n, readErr := r.Read(buf)
			got = append(got, buf[:n]...)
			if readErr != nil {
				break
			}
		}
		gotCh <- got
	}()

	require.NoError(t, WriteAll(int(w.Fd()), payload))
	require.NoError(t, w.Close())

	got := <-gotCh
	require.Equal(t, len(payload), len(got))
	require.Equal(t, payload, got)
}

func TestWriteAll_ClosedFD(t *testing.T) {
	t.Parallel()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	// Close write end, then try writing to its fd.
	require.NoError(t, w.Close())

	err = WriteAll(int(w.Fd()), []byte("should fail"))
	require.Error(t, err)
}

// ---------------------------------------------------------------------------
// Conn.SendUserMessage
// ---------------------------------------------------------------------------

func TestConn_SendUserMessage_WritesClaudeFormat(t *testing.T) {
	t.Parallel()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	t.Cleanup(func() { _ = w.Close() })

	conn := NewConn(slog.Default(), w, "user1", "sess1")

	err = conn.SendUserMessage(context.Background(), "what is 1+1?")
	require.NoError(t, err)

	// Close write side so Read can EOF.
	require.NoError(t, w.Close())

	data := make([]byte, 4096)
	n, readErr := r.Read(data)
	require.NoError(t, readErr)
	require.Greater(t, n, 0)

	// Verify JSON structure matches claudeUserMessage.
	var msg claudeUserMessage
	require.NoError(t, json.Unmarshal(data[:n], &msg))
	require.Equal(t, "user", msg.Type)
	require.Equal(t, "user", msg.Message.Role)
	require.Len(t, msg.Message.Content, 1)
	require.Equal(t, "text", msg.Message.Content[0].Type)
	require.Equal(t, "what is 1+1?", msg.Message.Content[0].Text)
}

func TestConn_SendUserMessage_CapturesLastInput(t *testing.T) {
	t.Parallel()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	t.Cleanup(func() { _ = w.Close() })

	conn := NewConn(slog.Default(), w, "user1", "sess1")

	// Initially empty.
	require.Equal(t, "", conn.LastInput())

	require.NoError(t, conn.SendUserMessage(context.Background(), "first message"))
	require.Equal(t, "first message", conn.LastInput())

	require.NoError(t, conn.SendUserMessage(context.Background(), "second message"))
	require.Equal(t, "second message", conn.LastInput())

	// Drain pipe so Close doesn't block on write-end.
	_, _ = r.Read(make([]byte, 4096))
}

func TestConn_SendUserMessage_ClosedConn(t *testing.T) {
	t.Parallel()

	conn := NewConn(slog.Default(), nil, "user1", "sess1")
	require.NoError(t, conn.Close())

	err := conn.SendUserMessage(context.Background(), "should fail")
	require.Error(t, err)
	require.Contains(t, err.Error(), "connection closed")
}

// ---------------------------------------------------------------------------
// Conn.TrySend
// ---------------------------------------------------------------------------

func TestConn_TrySend_Success(t *testing.T) {
	t.Parallel()

	conn := NewConn(slog.Default(), nil, "user1", "sess1")
	t.Cleanup(func() { _ = conn.Close() })

	env := &events.Envelope{
		ID:        "try-1",
		SessionID: "sess1",
		Event:     events.Event{Type: events.MessageStart},
	}

	require.True(t, conn.TrySend(env))

	// Verify the envelope was put on the recv channel.
	select {
	case got := <-conn.Recv():
		require.Equal(t, "try-1", got.ID)
	default:
		require.Fail(t, "expected envelope on Recv channel")
	}
}

func TestConn_TrySend_FullChannel(t *testing.T) {
	t.Parallel()

	// Create a conn with a tiny recvCh (NewConn uses 256 buffer, so fill it).
	conn := NewConn(slog.Default(), nil, "user1", "sess1")
	t.Cleanup(func() { _ = conn.Close() })

	// Fill the channel buffer (256 is the default from NewConn).
	for i := 0; i < 256; i++ {
		require.True(t, conn.TrySend(&events.Envelope{
			ID:        "fill",
			SessionID: "sess1",
			Event:     events.Event{Type: events.MessageDelta},
		}))
	}

	// Next TrySend should return false (channel full).
	require.False(t, conn.TrySend(&events.Envelope{
		ID:        "overflow",
		SessionID: "sess1",
		Event:     events.Event{Type: events.MessageDelta},
	}))
}

// ---------------------------------------------------------------------------
// Conn.Close
// ---------------------------------------------------------------------------

func TestConn_Close_Idempotent(t *testing.T) {
	t.Parallel()

	conn := NewConn(slog.Default(), nil, "user1", "sess1")

	// First close should succeed.
	require.NoError(t, conn.Close())

	// Second close should also succeed (idempotent).
	require.NoError(t, conn.Close())

	// Third close for good measure.
	require.NoError(t, conn.Close())
}

func TestConn_Close_ClosesStdin(t *testing.T) {
	t.Parallel()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })

	conn := NewConn(slog.Default(), w, "user1", "sess1")
	require.NoError(t, conn.Close())

	// After Close, the write-end should be closed; reading from read-end should get EOF.
	buf := make([]byte, 1)
	_, readErr := r.Read(buf)
	require.Error(t, readErr) // EOF or similar
}

func TestConn_Close_ClosesRecvCh(t *testing.T) {
	t.Parallel()

	conn := NewConn(slog.Default(), nil, "user1", "sess1")
	recvCh := conn.Recv()

	require.NoError(t, conn.Close())

	// Channel should be closed; reading should yield zero-value + false.
	_, ok := <-recvCh
	require.False(t, ok, "Recv channel should be closed after Close")
}

// ---------------------------------------------------------------------------
// Conn.LastInput
// ---------------------------------------------------------------------------

func TestConn_LastInput_Empty(t *testing.T) {
	t.Parallel()

	conn := NewConn(slog.Default(), nil, "user1", "sess1")
	t.Cleanup(func() { _ = conn.Close() })

	require.Equal(t, "", conn.LastInput())
}

// ---------------------------------------------------------------------------
// BaseWorker.SetConnLocked
// ---------------------------------------------------------------------------

func TestBaseWorker_SetConnLocked(t *testing.T) {
	t.Parallel()

	w := NewBaseWorker(slog.Default(), nil)

	conn := NewConn(slog.Default(), nil, "locked-user", "locked-sess")

	// Caller must hold the lock (as documented).
	w.Mu.Lock()
	w.SetConnLocked(conn)
	w.Mu.Unlock()

	got := w.Conn()
	require.NotNil(t, got)
	require.Equal(t, "locked-user", got.UserID())
	require.Equal(t, "locked-sess", got.SessionID())
}

func TestBaseWorker_SetConnLocked_Nil(t *testing.T) {
	t.Parallel()

	w := NewBaseWorker(slog.Default(), nil)

	// First set a real conn.
	w.SetConn(NewConn(slog.Default(), nil, "user1", "sess1"))
	require.NotNil(t, w.Conn())

	// SetConnLocked(nil) should clear it.
	w.Mu.Lock()
	w.SetConnLocked(nil)
	w.Mu.Unlock()

	require.Nil(t, w.Conn())
}

// ---------------------------------------------------------------------------
// BaseWorker.IncResetGeneration / LoadResetGeneration
// ---------------------------------------------------------------------------

func TestBaseWorker_ResetGeneration_Initial(t *testing.T) {
	t.Parallel()

	w := NewBaseWorker(slog.Default(), nil)
	require.Equal(t, int64(0), w.LoadResetGeneration())
}

func TestBaseWorker_ResetGeneration_IncAndLoad(t *testing.T) {
	t.Parallel()

	w := NewBaseWorker(slog.Default(), nil)

	got := w.IncResetGeneration()
	require.Equal(t, int64(1), got)
	require.Equal(t, int64(1), w.LoadResetGeneration())

	got = w.IncResetGeneration()
	require.Equal(t, int64(2), got)
	require.Equal(t, int64(2), w.LoadResetGeneration())
}

func TestBaseWorker_ResetGeneration_Concurrent(t *testing.T) {
	t.Parallel()

	w := NewBaseWorker(slog.Default(), nil)

	const increments = 1000
	var wg sync.WaitGroup
	wg.Add(increments)

	// Launch multiple goroutines incrementing in parallel.
	for i := 0; i < increments; i++ {
		go func() {
			defer wg.Done()
			_ = w.IncResetGeneration()
		}()
	}

	wg.Wait()

	// After all goroutines complete, the final value must be exactly increments.
	require.Equal(t, int64(increments), w.LoadResetGeneration())
}

// ---------------------------------------------------------------------------
// Integration: Send produces decodable NDJSON via pipe
// ---------------------------------------------------------------------------

func TestConn_Send_RoundTrip(t *testing.T) {
	t.Parallel()

	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { _ = r.Close() })
	t.Cleanup(func() { _ = w.Close() })

	conn := NewConn(slog.Default(), w, "user1", "sess1")

	original := &events.Envelope{
		ID:        "roundtrip-1",
		SessionID: "sess1",
		Seq:       42,
		Event: events.Event{
			Type: events.Done,
			Data: events.DoneData{Success: true},
		},
	}

	require.NoError(t, conn.Send(context.Background(), original))
	require.NoError(t, w.Close())

	// Read and decode using aep codec.
	scanner := bufio.NewScanner(r)
	require.True(t, scanner.Scan())

	decoded, decodeErr := aep.DecodeLine(scanner.Bytes())
	require.NoError(t, decodeErr)
	require.Equal(t, "roundtrip-1", decoded.ID)
	require.Equal(t, events.Done, decoded.Event.Type)
	require.Equal(t, int64(42), decoded.Seq)
}
