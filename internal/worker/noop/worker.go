package noop

import (
	"context"
	"io"

	"hotplex-worker/internal/worker"
	"hotplex-worker/pkg/events"
)

// Compile-time interface compliance checks.
var (
	_ worker.Worker       = (*Worker)(nil)
	_ worker.SessionConn  = (*Conn)(nil)
	_ worker.Capabilities = (*Capabilities)(nil)
)

// ─── NoOp implementations ───────────────────────────────────────────────────

// Capabilities is a stub Capabilities for framework testing.
type Capabilities struct{}

func (Capabilities) Type() worker.WorkerType { return worker.TypeUnknown }
func (Capabilities) SupportsResume() bool    { return false }
func (Capabilities) SupportsStreaming() bool { return false }
func (Capabilities) SupportsTools() bool     { return false }
func (Capabilities) EnvWhitelist() []string  { return nil }
func (Capabilities) SessionStoreDir() string { return "" }

// Conn is a stub SessionConn that drops all messages.
type Conn struct {
	sessionID string
	userID    string
	recvCh    chan *events.Envelope
}

func NewConn(sessionID, userID string) *Conn {
	return &Conn{
		sessionID: sessionID,
		userID:    userID,
		recvCh:    make(chan *events.Envelope),
	}
}

func (c *Conn) Send(_ context.Context, _ *events.Envelope) error {
	return nil
}

func (c *Conn) Recv() <-chan *events.Envelope {
	return c.recvCh
}

func (c *Conn) Close() error {
	close(c.recvCh)
	return nil
}

func (c *Conn) UserID() string    { return c.userID }
func (c *Conn) SessionID() string { return c.sessionID }

// Worker is a stub worker that implements the Worker interface but does nothing.
type Worker struct {
	Capabilities
	conn *Conn
}

func NewWorker() *Worker {
	return &Worker{}
}

func (w *Worker) Start(_ context.Context, _ worker.SessionInfo) error {
	return nil
}

func (w *Worker) Input(_ context.Context, _ string, _ map[string]any) error {
	return worker.ErrNotImplemented
}

func (w *Worker) Resume(_ context.Context, _ worker.SessionInfo) error {
	return worker.ErrNotImplemented
}

func (w *Worker) Terminate(_ context.Context) error {
	return nil
}

func (w *Worker) Kill() error {
	return nil
}

func (w *Worker) Wait() (int, error) {
	return 0, io.EOF
}

func (w *Worker) Conn() worker.SessionConn {
	return w.conn
}

// SetConn sets the SessionConn (used by tests).
func (w *Worker) SetConn(conn *Conn) {
	w.conn = conn
}
