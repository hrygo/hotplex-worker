// Package worker defines the interfaces that all worker adapters must implement.
package worker

import (
	"context"
	"errors"

	"hotplex-worker/pkg/events"
)

// ErrNotImplemented is returned for unimplemented worker methods.
var ErrNotImplemented = errors.New("worker: not implemented")

// ─── SessionConn ─────────────────────────────────────────────────────────────

// SessionConn represents the bidirectional communication channel between
// the gateway and a worker's runtime process. It is the data plane interface.
type SessionConn interface {
	// Send delivers a message to the worker runtime.
	Send(ctx context.Context, msg *events.Envelope) error

	// Recv returns a channel that yields messages from the worker runtime.
	// The channel is closed when the connection is closed.
	Recv() <-chan *events.Envelope

	// Close terminates the connection and releases resources.
	Close() error

	// UserID returns the user who owns this session.
	UserID() string

	// SessionID returns the session identifier.
	SessionID() string
}

// ─── Capabilities ───────────────────────────────────────────────────────────

// Capabilities describes what a worker adapter supports.
type Capabilities interface {
	// Type returns the worker type identifier (e.g. "claude_code", "opencode_cli").
	Type() WorkerType

	// SupportsResume returns true if the worker can resume a previous session.
	SupportsResume() bool

	// SupportsStreaming returns true if the worker emits streaming (delta) events.
	SupportsStreaming() bool

	// SupportsTools returns true if the worker exposes tool call capabilities.
	SupportsTools() bool

	// EnvWhitelist returns the set of environment variable names this worker
	// is allowed to receive (empty = all allowed).
	EnvWhitelist() []string

	// SessionStoreDir returns the directory where the worker stores session state,
	// or empty string if the worker does not persist sessions.
	SessionStoreDir() string
}

// WorkerType is the string identifier for a worker implementation.
type WorkerType string

const (
	TypeClaudeCode  WorkerType = "claude_code"
	TypeOpenCodeCLI WorkerType = "opencode_cli"
	TypeOpenCodeSrv WorkerType = "opencode_server"
	TypePimon       WorkerType = "pi-mono"
	TypeUnknown     WorkerType = "unknown"
)

// ─── Worker ─────────────────────────────────────────────────────────────────

// Worker is the main interface that all worker adapters must implement.
// The gateway communicates with a worker exclusively through this interface.
type Worker interface {
	Capabilities

	// Start launches the worker runtime for the given session.
	// It blocks until the runtime is ready to receive input or an error occurs.
	Start(ctx context.Context, session SessionInfo) error

	// Input delivers a user message to the worker runtime.
	Input(ctx context.Context, content string, metadata map[string]any) error

	// Resume reattaches to an existing session using the sessionID from session.Info.
	// Returns ErrSessionNotFound if the session cannot be located.
	Resume(ctx context.Context, session SessionInfo) error

	// Terminate gracefully stops the worker runtime.
	// It sends SIGTERM first, then SIGKILL after grace period.
	Terminate(ctx context.Context) error

	// Kill immediately terminates the worker runtime with SIGKILL.
	Kill() error

	// Wait blocks until the worker runtime exits, returning the exit code.
	Wait() (int, error)

	// Conn returns the SessionConn for this worker, or nil if not started.
	Conn() SessionConn
}

// SessionInfo contains metadata about a session needed by the worker to start/resume.
type SessionInfo struct {
	SessionID  string
	UserID     string
	ProjectDir string
	Env        map[string]string
	Args       []string
}
