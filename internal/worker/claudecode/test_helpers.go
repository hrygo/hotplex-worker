package claudecode

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"

	"github.com/hotplex/hotplex-worker/pkg/events"
)

// newTestLogger creates a logger for testing.
func newTestLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
}

// mockConn implements worker.SessionConn for unit testing.
// It captures sent envelopes and provides a controllable receive channel.
// stdin is always io.Discard so control writes never panic.
type mockConn struct {
	userID    string
	sessionID string
	mu        sync.Mutex
	closed    int32 // accessed via atomic
	sent      []*events.Envelope
	recvCh    chan *events.Envelope
	blockSend int32 // accessed via atomic; 1 = send blocks forever
	stdin     io.Writer
}

func newMockConn(userID, sessionID string) *mockConn { //nolint:unparam // test helper
	return &mockConn{
		userID:    userID,
		sessionID: sessionID,
		recvCh:    make(chan *events.Envelope, 256),
		stdin:     io.Discard,
	}
}

func (m *mockConn) Send(_ context.Context, msg *events.Envelope) error {
	if atomic.LoadInt32(&m.blockSend) == 1 {
		// Simulate full channel by blocking forever (use with timeout in tests).
		<-make(chan struct{})
	}
	if atomic.LoadInt32(&m.closed) == 1 {
		return &mockConnError{msg: "connection closed"}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	return nil
}

func (m *mockConn) Recv() <-chan *events.Envelope { return m.recvCh }

func (m *mockConn) Close() error {
	if atomic.SwapInt32(&m.closed, 1) == 1 {
		return nil
	}
	close(m.recvCh)
	return nil
}

func (m *mockConn) UserID() string    { return m.userID }
func (m *mockConn) SessionID() string { return m.sessionID }

func (m *mockConn) sentEnvelopes() []*events.Envelope {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sent
}

func (m *mockConn) setBlockSend(v bool) {
	if v {
		atomic.StoreInt32(&m.blockSend, 1)
	} else {
		atomic.StoreInt32(&m.blockSend, 0)
	}
}

func (m *mockConn) StdinWriter() io.Writer { return m.stdin }

// TrySend is the non-blocking equivalent of Send. It is called by worker.trySend
// via a duck-typed interface check. blockSend=true causes TrySend to return false
// immediately (simulating a full downstream channel, dropping the message).
func (m *mockConn) TrySend(env *events.Envelope) bool {
	if atomic.LoadInt32(&m.blockSend) == 1 {
		return false
	}
	if atomic.LoadInt32(&m.closed) == 1 {
		return false
	}

	select {
	case m.recvCh <- env:
		m.mu.Lock()
		m.sent = append(m.sent, env)
		m.mu.Unlock()
		return true
	default:
		return false
	}
}

type mockConnError struct {
	msg string
}

func (e *mockConnError) Error() string { return e.msg }
