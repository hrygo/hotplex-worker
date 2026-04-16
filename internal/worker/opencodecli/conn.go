package opencodecli

import (
	"context"
	"sync"

	"github.com/hotplex/hotplex-worker/pkg/events"
)

// recvOnlyConn implements worker.SessionConn for OpenCode CLI.
// OpenCode CLI reads input from stdin as plain text (not NDJSON), so this
// connection only provides the recv direction. The stdin pipe is managed
// by proc.Manager; we hold a reference to it only for Close() cleanup.
type recvOnlyConn struct {
	userID    string
	sessionID string
	recvCh    chan *events.Envelope
	onClose   func() // called before closing recvCh
	mu        sync.Mutex
	closed    bool
}

// newRecvOnlyConn creates a recv-only connection.
func newRecvOnlyConn(userID, sessionID string, onClose func()) *recvOnlyConn {
	return &recvOnlyConn{
		userID:    userID,
		sessionID: sessionID,
		recvCh:    make(chan *events.Envelope, 256),
		onClose:   onClose,
	}
}

// Send is a no-op: OpenCode CLI does not receive NDJSON over stdin.
func (c *recvOnlyConn) Send(ctx context.Context, msg *events.Envelope) error {
	return nil
}

// Recv returns the channel of AEP envelopes from the worker.
func (c *recvOnlyConn) Recv() <-chan *events.Envelope {
	return c.recvCh
}

// TrySend non-blocking sends an envelope to the receive channel.
func (c *recvOnlyConn) TrySend(env *events.Envelope) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	select {
	case c.recvCh <- env:
		return true
	default:
		return false
	}
}

// Close terminates the connection.
func (c *recvOnlyConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	if c.onClose != nil {
		c.onClose()
	}
	close(c.recvCh)

	return nil
}

// UserID returns the user who owns this session.
func (c *recvOnlyConn) UserID() string {
	return c.userID
}

// SessionID returns the OpenCode internal session identifier.
func (c *recvOnlyConn) SessionID() string {
	return c.sessionID
}

// SetSessionID updates the OpenCode internal session ID.
func (c *recvOnlyConn) SetSessionID(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionID = id
}
