package noop

import (
	"context"
	"io"
	"time"

	"github.com/hotplex/hotplex-worker/internal/worker"
	"github.com/hotplex/hotplex-worker/pkg/events"
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
func (Capabilities) MaxTurns() int           { return 0 }
func (Capabilities) Modalities() []string    { return nil }

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
	return nil // noop: resume is a no-op
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

// Health returns a stub health report. In production workers this queries
// the actual subprocess state (PID, running flag, uptime, etc.).
func (w *Worker) Health() worker.WorkerHealth {
	return worker.WorkerHealth{
		Type:    w.Type(),
		Healthy: true,
		Running: false,
		Uptime:  "0s",
	}
}

// LastIO returns the zero time for the noop worker (no I/O tracking).
func (w *Worker) LastIO() time.Time {
	return time.Time{}
}

// ResetContext is a no-op for the noop worker.
func (w *Worker) ResetContext(_ context.Context) error {
	return nil
}
