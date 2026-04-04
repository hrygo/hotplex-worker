package opencodecli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/hotplex/hotplex-worker/internal/worker"
	"github.com/hotplex/hotplex-worker/internal/worker/base"
	"github.com/hotplex/hotplex-worker/internal/worker/proc"
	"github.com/hotplex/hotplex-worker/pkg/aep"
	"github.com/hotplex/hotplex-worker/pkg/events"
)

// Compile-time interface compliance check.
var _ worker.Worker = (*Worker)(nil)

// Env whitelist for OpenCode CLI worker.
var openCodeCLIEnvWhitelist = []string{
	"HOME", "USER", "SHELL", "PATH", "TERM",
	"LANG", "LC_ALL", "PWD",
	"OPENAI_API_KEY", "OPENAI_BASE_URL",
	"OPENCODE_API_KEY", "OPENCODE_BASE_URL",
}

// Worker implements the OpenCode CLI worker adapter.
type Worker struct {
	Base *base.BaseWorker

	mu        sync.Mutex
	sessionID string // extracted from step_start event
	started   bool
}

// New creates a new OpenCode CLI worker.
func New() *Worker {
	return &Worker{
		Base: base.NewBaseWorker(slog.Default(), nil),
	}
}

// ─── Capabilities ─────────────────────────────────────────────────────────────

func (w *Worker) Type() worker.WorkerType { return worker.TypeOpenCodeCLI }

func (w *Worker) SupportsResume() bool    { return false }
func (w *Worker) SupportsStreaming() bool { return true }
func (w *Worker) SupportsTools() bool     { return true }
func (w *Worker) EnvWhitelist() []string  { return openCodeCLIEnvWhitelist }
func (w *Worker) SessionStoreDir() string { return "" } // CLI doesn't persist sessions
func (w *Worker) MaxTurns() int           { return 0 }
func (w *Worker) Modalities() []string    { return []string{"text", "code"} }

// ─── Worker ─────────────────────────────────────────────────────────────────

func (w *Worker) Start(ctx context.Context, session worker.SessionInfo) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.Base.Proc != nil {
		return fmt.Errorf("opencodecli: already started")
	}

	// Build command arguments: opencode run --format json
	args := []string{
		"run",
		"--format", "json",
	}

	// Build environment using shared BuildEnv.
	env := base.BuildEnv(session, openCodeCLIEnvWhitelist, "opencode-cli")

	// Create process manager.
	w.Base.Proc = proc.New(proc.Opts{
		Logger:       w.Base.Log,
		AllowedTools: session.AllowedTools,
	})

	// Start the process.
	stdin, _, _, err := w.Base.Proc.Start(ctx, "opencode", args, env, session.ProjectDir)
	if err != nil {
		w.Base.Proc = nil
		return fmt.Errorf("opencodecli: start: %w", err)
	}

	// Create session connection using base.Conn.
	conn := base.NewConn(w.Base.Log, stdin, session.UserID, "")
	w.Base.SetConn(conn)

	w.Base.StartTime = time.Now()
	w.Base.SetLastIO(w.Base.StartTime)
	w.started = true

	// Start output reader goroutine.
	go w.readOutput(session.SessionID)

	return nil
}

func (w *Worker) Input(ctx context.Context, content string, metadata map[string]any) error {
	conn := w.Base.Conn()
	if conn == nil {
		return fmt.Errorf("opencodecli: not started")
	}

	// Wait for session ID to be extracted if not yet set.
	// The gateway may send the first input before we extract session ID.
	sessionID := conn.SessionID()
	if sessionID == "" {
		sessionID = "pending"
	}

	msg := events.NewEnvelope(
		aep.NewID(),
		sessionID,
		0, // seq assigned by hub
		events.Input,
		events.InputData{
			Content:  content,
			Metadata: metadata,
		},
	)

	if err := conn.Send(ctx, msg); err != nil {
		return fmt.Errorf("opencodecli: input: %w", err)
	}

	w.Base.SetLastIO(time.Now())

	return nil
}

func (w *Worker) Resume(ctx context.Context, session worker.SessionInfo) error {
	return fmt.Errorf("opencodecli: resume not supported")
}

func (w *Worker) Health() worker.WorkerHealth {
	return w.Base.Health(worker.TypeOpenCodeCLI)
}

func (w *Worker) Conn() worker.SessionConn {
	return w.Base.Conn()
}

func (w *Worker) LastIO() time.Time {
	return w.Base.LastIO()
}

func (w *Worker) Terminate(ctx context.Context) error {
	return w.Base.Terminate(ctx)
}

func (w *Worker) Kill() error {
	return w.Base.Kill()
}

func (w *Worker) Wait() (int, error) {
	return w.Base.Wait()
}

// ─── Internal ────────────────────────────────────────────────────────────────

func (w *Worker) readOutput(defaultSessionID string) {
	defer func() {
		c := w.Base.Conn()
		if c != nil {
			c.Close()
		}
	}()

	w.Base.Mu.Lock()
	proc := w.Base.Proc
	w.Base.Mu.Unlock()
	if proc == nil {
		return
	}

	for {
		line, err := proc.ReadLine()
		if err != nil {
			if err == io.EOF {
				return
			}
			w.Base.Log.Error("opencodecli: read line", "error", err)
			return
		}

		if line == "" {
			continue
		}

		// Try to extract session ID from step_start event.
		if w.sessionID == "" {
			w.tryExtractSessionID(line)
		}

		env, err := aep.DecodeLine([]byte(line))
		if err != nil {
			w.Base.Log.Warn("opencodecli: decode line", "error", err, "line", line)
			continue
		}

		// Update session ID if extracted.
		if w.sessionID != "" {
			w.Base.Mu.Lock()
			if c, ok := w.Base.Conn().(*base.Conn); ok {
				c.SetSessionID(w.sessionID)
			}
			w.Base.Mu.Unlock()
		}

		w.Base.SetLastIO(time.Now())

		w.Base.Mu.Lock()
		conn, ok := w.Base.Conn().(*base.Conn)
		w.Base.Mu.Unlock()
		if !ok || conn == nil {
			return
		}

		// Override session ID if we've extracted it.
		if w.sessionID != "" {
			env.SessionID = w.sessionID
		} else {
			env.SessionID = defaultSessionID
		}

		if !conn.TrySend(env) {
			w.Base.Log.Warn("opencodecli: recv channel full, dropping message")
		}
	}
}

func (w *Worker) tryExtractSessionID(line string) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return
	}

	// Check if this is a step_start event.
	if typ, ok := raw["type"]; ok {
		if string(typ) == `"step_start"` || string(typ) == "step_start" {
			if data, ok := raw["data"]; ok {
				var stepData struct {
					SessionID string `json:"session_id"`
					ID        string `json:"id"`
				}
				if err := json.Unmarshal(data, &stepData); err != nil {
					return
				}
				if stepData.SessionID != "" {
					w.mu.Lock()
					w.sessionID = stepData.SessionID
					w.mu.Unlock()
					w.Base.Log.Info("opencodecli: extracted session ID", "session_id", w.sessionID)
					return
				}
				if stepData.ID != "" {
					w.mu.Lock()
					w.sessionID = stepData.ID
					w.mu.Unlock()
					w.Base.Log.Info("opencodecli: extracted session ID from id", "session_id", w.sessionID)
				}
			}
		}
	}
}

// ─── Init ────────────────────────────────────────────────────────────────────

func init() {
	worker.Register(worker.TypeOpenCodeCLI, func() (worker.Worker, error) {
		return &Worker{Base: base.NewBaseWorker(slog.Default(), nil)}, nil
	})
}
