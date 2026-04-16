// Package worker defines the interfaces that all worker adapters must implement.
package worker

import (
	"context"
	"errors"
	"time"

	"github.com/hotplex/hotplex-worker/pkg/events"
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

	// MaxTurns returns the maximum number of turns (input/output cycles) allowed
	// per session, or 0 if unlimited.
	MaxTurns() int

	// Modalities returns the supported content modalities (e.g. "text", "code", "image").
	Modalities() []string
}

// WorkerType is the string identifier for a worker implementation.
type WorkerType string

const (
	TypeClaudeCode  WorkerType = "claude_code"
	TypeOpenCodeCLI WorkerType = "opencode_cli"
	TypeOpenCodeSrv WorkerType = "opencode_server"
	TypeACPX        WorkerType = "acpx"
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

	// Health returns a snapshot of the worker's runtime health.
	Health() WorkerHealth

	// LastIO returns the time of the last I/O activity (input sent or output received).
	// Used by GC zombie detection to identify stuck workers.
	// Implementations that don't track I/O should return the zero time.Time.
	LastIO() time.Time

	// ResetContext clears the worker's runtime context.
	// The worker decides the implementation:
	//   - Workers that support in-place reset: send internal reset signal
	//   - Others: terminate + start (physically deletes session files)
	// Note: Gateway layer has already called sm.ClearContext() to clear SessionInfo.Context.
	ResetContext(ctx context.Context) error
}

// WorkerHealth reports the runtime health of a worker process.
type WorkerHealth struct {
	Type      WorkerType `json:"type"`
	SessionID string     `json:"session_id"`
	PID       int        `json:"pid"`
	Running   bool       `json:"running"`
	Healthy   bool       `json:"healthy"`
	Uptime    string     `json:"uptime"`
	Error     string     `json:"error,omitempty"`
}

// WorkerSessionIDHandler is an optional interface for workers that manage
// their own internal session IDs separate from the Gateway session ID.
// Bridge detects implementations via type assertion and uses them to
// persist/restore worker-internal session IDs for resume support.
type WorkerSessionIDHandler interface {
	SetWorkerSessionID(id string)
	GetWorkerSessionID() string
}

// SessionInfo contains metadata about a session needed by the worker to start/resume.
type SessionInfo struct {
	SessionID    string
	UserID       string
	ProjectDir   string
	Env          map[string]string
	Args         []string
	AllowedTools []string // tools allowed for this session (from InitConfig.AllowedTools)

	// WorkerSessionID is the internal session ID used by the worker runtime.
	// For workers that manage their own session state (OpenCode CLI, OpenCode Server),
	// this field carries the worker-internal session ID for persistence and resume.
	// Empty for workers that use Gateway SessionID directly (Claude Code).
	WorkerSessionID string
	AllowedModels   []string // models allowed for this session

	// PermissionMode controls how the worker handles permission requests.
	// Valid values: "default", "plan", "auto-accept".
	PermissionMode string
	// SkipPermissions bypasses all permission checks (equivalent to --dangerously-skip-permissions).
	SkipPermissions bool
	// DisallowedTools lists tools that the worker should NOT use.
	DisallowedTools []string
	// SystemPrompt is appended to the worker's default system prompt (--append-system-prompt).
	SystemPrompt string
	// SystemPromptReplace, if non-empty, replaces the default system prompt entirely (--system-prompt).
	// Takes precedence over SystemPrompt when set.
	SystemPromptReplace string
	// MCPConfig is the path to a JSON file with MCP server configuration (--mcp-config).
	MCPConfig string
	// StrictMCPConfig restricts MCP servers to only those specified in MCPConfig (--strict-mcp-config).
	StrictMCPConfig bool
	// ContinueSession resumes the latest session in the current directory without a session ID.
	ContinueSession bool
	// ForkSession, when resuming, creates a new session ID instead of reusing the existing one.
	ForkSession bool
	// MaxTurns limits the number of agentic turns in non-interactive mode.
	MaxTurns int
	// Bare runs Claude Code in minimal mode, skipping hooks, LSP, and plugin sync.
	Bare bool
	// AllowedDirs lists additional directories the worker can access (--add-dir).
	AllowedDirs []string
	// MaxBudgetUSD caps API spending per session (--max-budget-usd).
	MaxBudgetUSD float64
	// JSONSchema validates structured output against a JSON Schema (--json-schema).
	JSONSchema string
	// ResumeSessionAt restores the session up to and including the specified
	// assistant message ID, discarding later history (--resume-session-at).
	ResumeSessionAt string
	// RewindFiles restores files to their state at the specified user message ID
	// and exits (--rewind-files).
	RewindFiles string
	// IncludeHookEvents exposes all hook lifecycle events in the output stream
	// (--include-hook-events).
	IncludeHookEvents bool
	// IncludePartialMessages exposes partial message blocks as they arrive
	// (--include-partial-messages).
	IncludePartialMessages bool
}
