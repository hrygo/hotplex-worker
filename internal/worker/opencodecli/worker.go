package opencodecli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync/atomic"
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
var openCodeEnvWhitelist = []string{
	"HOME", "USER", "SHELL", "PATH", "TERM",
	"LANG", "LC_ALL", "PWD",
	// OpenAI SDK vars
	"OPENAI_API_KEY", "OPENAI_BASE_URL",
	// Anthropic vars (for providers using anthropic)
	"ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN",
	// OpenCode server vars
	"OPENCODE_BASE_URL", "OPENCODE_API_KEY",
	// Safety configs
	"BASH_MAX_TIMEOUT_MS", "BASH_MAX_OUTPUT_LENGTH",
	// OpenTelemetry (prefix-matched)
	"OTEL_",
}

// Worker implements the OpenCode CLI worker adapter.
// Each Input() call launches a new `opencode run` subprocess. The session ID
// is extracted from the first step_start event and cached for continuation.
type Worker struct {
	*base.BaseWorker

	sessionInfo worker.SessionInfo
	conn        *recvOnlyConn
	stdin       *os.File

	openCodeSessionID atomic.Value // string

	parser *Parser
	mapper *Mapper

	cancel context.CancelFunc
	seq    atomic.Int64

	readLineFn func() (string, error)
	testConn   worker.SessionConn
}

var _ worker.WorkerSessionIDHandler = (*Worker)(nil)

func (w *Worker) GetWorkerSessionID() string {
	if v := w.openCodeSessionID.Load(); v != nil {
		if sid, ok := v.(string); ok {
			return sid
		}
	}
	return ""
}

func (w *Worker) SetWorkerSessionID(id string) {
	w.openCodeSessionID.Store(id)
}

// New creates a new OpenCode CLI worker.
func New() *Worker {
	return &Worker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
	}
}

// ─── Capabilities ─────────────────────────────────────────────────────────────

func (w *Worker) Type() worker.WorkerType { return worker.TypeOpenCodeCLI }
func (w *Worker) SupportsResume() bool    { return true }
func (w *Worker) SupportsStreaming() bool { return true }
func (w *Worker) SupportsTools() bool     { return true }
func (w *Worker) EnvWhitelist() []string  { return openCodeEnvWhitelist }
func (w *Worker) SessionStoreDir() string { return ".opencode/sessions" }
func (w *Worker) MaxTurns() int           { return 0 }
func (w *Worker) Modalities() []string    { return []string{"text", "code"} }

// ─── Worker Lifecycle ─────────────────────────────────────────────────────────

// startLocked is the shared process startup sequence.
// Caller must hold Mu; startLocked releases it before returning.
// It terminates any existing proc, reinitializes stdin/stdout with a new
// subprocess, runs writeStdinFn, and starts the readOutput goroutine.
func (w *Worker) startLocked(ctx context.Context, session worker.SessionInfo, openCodeSID string, writeStdinFn func() error) error {
	if w.cancel != nil {
		w.cancel()
	}
	if w.Proc != nil {
		_ = w.Proc.Kill()
		_ = w.Proc.Close()
		w.Proc = nil
	}
	w.closeStdin()

	args := w.buildCLIArgs(session, openCodeSID)
	env := base.BuildEnv(session, openCodeEnvWhitelist, "opencode-cli")

	w.Proc = proc.New(proc.Opts{
		Logger:       w.Log,
		AllowedTools: session.AllowedTools,
	})

	var err error
	w.stdin, _, _, err = w.Proc.Start(ctx, "opencode", args, env, session.ProjectDir)
	if err != nil {
		w.Proc = nil
		w.Mu.Unlock()
		return err
	}

	if err := writeStdinFn(); err != nil {
		_ = w.Proc.Kill()
		_ = w.Proc.Close()
		w.Proc = nil
		w.stdin = nil
		w.Mu.Unlock()
		return err
	}

	// OpenCode CLI buffers input until stdin closes — close stdin to signal
	// the process to begin processing. This is safe because opencodecli
	// spawns a fresh subprocess on each Input() call.
	w.closeStdin()

	childCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel

	w.parser = NewParser(w.Log)
	w.mapper = NewMapper(w.Log, session.SessionID, w.nextSeq)

	w.BaseWorker.StartTime = time.Now()
	w.BaseWorker.SetLastIO(w.BaseWorker.StartTime)

	w.Mu.Unlock()
	go w.readOutput(childCtx)
	return nil
}

func (w *Worker) Start(ctx context.Context, session worker.SessionInfo) error {
	w.Mu.Lock()
	if w.Proc != nil {
		w.Mu.Unlock()
		return fmt.Errorf("opencodecli: already started")
	}
	w.sessionInfo = session

	if err := w.startLocked(ctx, session, "", func() error {
		return w.writeStdin(session.Args...)
	}); err != nil {
		return err
	}

	// startLocked releases the lock; initialize conn after it returns.
	w.conn = newRecvOnlyConn(session.UserID, session.SessionID, func() {
		w.closeStdin()
		if w.Proc != nil {
			_ = w.Proc.Close()
		}
	})
	return nil
}

func (w *Worker) Input(ctx context.Context, content string, metadata map[string]any) error {
	w.Mu.Lock()
	openCodeSID := ""
	if v := w.openCodeSessionID.Load(); v != nil {
		openCodeSID, _ = v.(string)
	}
	return w.startLocked(ctx, w.sessionInfo, openCodeSID, func() error {
		return w.writeStdin(content)
	})
}

func (w *Worker) Resume(ctx context.Context, session worker.SessionInfo) error {
	w.Mu.Lock()
	defer w.Mu.Unlock()

	w.sessionInfo = session

	openCodeSID := session.WorkerSessionID
	if openCodeSID == "" {
		if v := w.openCodeSessionID.Load(); v != nil {
			openCodeSID, _ = v.(string)
		}
	}

	// startLocked releases the lock; re-establish conn after it returns.
	if err := w.startLocked(ctx, session, openCodeSID, func() error { return nil }); err != nil {
		return err
	}

	w.conn = newRecvOnlyConn(session.UserID, session.SessionID, func() {
		w.closeStdin()
		if w.Proc != nil {
			_ = w.Proc.Close()
		}
	})
	return nil
}

// ResetContext clears the worker runtime context.
// OpenCode CLI does not support in-place clearing, so this terminates the
// current process and starts a fresh one with the same session ID.
func (w *Worker) ResetContext(ctx context.Context) error {
	w.Mu.Lock()

	openCodeSID := ""
	if v := w.openCodeSessionID.Load(); v != nil {
		openCodeSID, _ = v.(string)
	}

	return w.startLocked(ctx, w.sessionInfo, openCodeSID, func() error {
		return w.writeStdin(w.sessionInfo.Args...)
	})
}

// writeStdin writes messages as plain text (newline-separated) to subprocess stdin.
// OpenCode CLI reads the message via Bun.stdin.text().
func (w *Worker) writeStdin(msgs ...string) error {
	if w.stdin == nil {
		return fmt.Errorf("stdin not available")
	}
	for _, msg := range msgs {
		if _, err := fmt.Fprintln(w.stdin, msg); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) Terminate(ctx context.Context) error {
	if w.cancel != nil {
		w.cancel()
	}
	if w.conn != nil {
		_ = w.conn.Close()
	}
	return w.BaseWorker.Terminate(ctx)
}

func (w *Worker) Conn() worker.SessionConn {
	if w.testConn != nil {
		return w.testConn
	}
	return w.conn
}

func (w *Worker) Health() worker.WorkerHealth {
	w.Mu.Lock()
	defer w.Mu.Unlock()

	health := worker.WorkerHealth{
		Type:      worker.TypeOpenCodeCLI,
		SessionID: "",
		Running:   false,
		Healthy:   true,
		Uptime:    "0s",
	}

	if w.conn != nil {
		health.SessionID = w.conn.SessionID()
	}
	if w.Proc != nil {
		health.PID = w.Proc.PID()
		health.Running = w.Proc.IsRunning()
	}
	if !w.BaseWorker.StartTime.IsZero() {
		health.Uptime = time.Since(w.BaseWorker.StartTime).Round(time.Second).String()
	}
	return health
}

func (w *Worker) LastIO() time.Time {
	return w.BaseWorker.LastIO()
}

// ─── CLI Arguments ────────────────────────────────────────────────────────────

func (w *Worker) buildCLIArgs(session worker.SessionInfo, openCodeSessionID string) []string {
	args := []string{
		"run",
		"--format", "json",
	}

	if openCodeSessionID != "" {
		args = append(args, "--session", openCodeSessionID)
	} else if session.ContinueSession {
		args = append(args, "--continue")
	}

	if session.ForkSession {
		args = append(args, "--fork")
	}
	if session.PermissionMode != "" {
		args = append(args, "--permission-mode", session.PermissionMode)
	}
	if session.SkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}
	if session.MCPConfig != "" {
		args = append(args, "--mcp-config", session.MCPConfig)
		if session.StrictMCPConfig {
			args = append(args, "--strict-mcp-config")
		}
	}
	if session.MaxTurns > 0 {
		args = append(args, "--max-turns", fmt.Sprintf("%d", session.MaxTurns))
	}
	if session.Bare {
		args = append(args, "--bare")
	}
	for _, dir := range session.AllowedDirs {
		args = append(args, "--add-dir", dir)
	}
	if session.MaxBudgetUSD > 0 {
		args = append(args, "--max-budget-usd", fmt.Sprintf("%f", session.MaxBudgetUSD))
	}
	if session.JSONSchema != "" {
		args = append(args, "--json-schema", session.JSONSchema)
	}
	if session.SystemPromptReplace != "" {
		args = append(args, "--system-prompt", session.SystemPromptReplace)
	} else if session.SystemPrompt != "" {
		args = append(args, "--append-system-prompt", session.SystemPrompt)
	}
	if session.IncludeHookEvents {
		args = append(args, "--include-hook-events")
	}
	if session.IncludePartialMessages {
		args = append(args, "--include-partial-messages")
	}
	if session.ResumeSessionAt != "" {
		args = append(args, "--resume", session.ResumeSessionAt)
	}

	return args
}

// ─── Output Loop ──────────────────────────────────────────────────────────────

func (w *Worker) readOutput(ctx context.Context) {
	defer func() {
		// Only close the conn when the process exits on its own (EOF or error).
		// When ctx is cancelled (Input() relaunches or Terminate()), the conn
		// must stay open: Input's new readOutput goroutine will use the same conn,
		// and the bridge's forwardEvents goroutine reads from conn.Recv().
		if ctx.Err() == nil {
			if conn := w.Conn(); conn != nil {
				conn.Close()
			}
		}
	}()

	w.Mu.Lock()
	readLineFn := w.readLineFn
	parser := w.parser
	mapper := w.mapper
	w.Mu.Unlock()

	if readLineFn == nil {
		readLineFn = func() (string, error) {
			w.Mu.Lock()
			p := w.Proc
			w.Mu.Unlock()
			if p == nil {
				return "", io.EOF
			}
			return p.ReadLine()
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line, err := readLineFn()
		if err != nil {
			if err == io.EOF {
				return
			}
			w.Log.Error("opencodecli: read line", "error", err)
			return
		}

		if line == "" {
			continue
		}

		evts, err := parser.ParseLine(line)
		if err != nil {
			w.Log.Warn("opencodecli: parse line", "error", err)
			continue
		}

		w.BaseWorker.SetLastIO(time.Now())

		for _, evt := range evts {
			w.handleEvent(evt, mapper)
		}
	}
}

func (w *Worker) handleEvent(evt *WorkerEvent, mapper *Mapper) {
	switch evt.Type {
	case EventStepStart:
		if p, ok := evt.Payload.(*StepStartPayload); ok {
			w.openCodeSessionID.Store(p.SessionID)
			if w.conn != nil {
				w.conn.SetSessionID(p.SessionID)
			}
			mapper.UpdateSessionID(p.SessionID)
		}

	case EventStepFinish:
		stats := map[string]any{}
		if p, ok := evt.Payload.(*StepFinishPayload); ok {
			stats["reason"] = p.Reason
			stats["cost"] = p.Cost
			stats["tokens"] = p.Tokens
		}
		w.emitDone(stats, mapper)
		return

	case EventError:
		if p, ok := evt.Payload.(*ResultPayload); ok {
			w.emitError(p.Error, mapper)
			w.emitDone(map[string]any{"success": false}, mapper)
		}
		return

	default:
		envs, err := mapper.Map(evt)
		if err != nil {
			w.Log.Warn("opencodecli: map event", "error", err)
			return
		}
		for _, env := range envs {
			w.trySend(env)
		}
	}
}

func (w *Worker) emitDone(stats map[string]any, mapper *Mapper) {
	env := events.NewEnvelope(
		aep.NewID(),
		mapper.SessionID(),
		w.nextSeq(),
		events.Done,
		events.DoneData{Success: true, Stats: stats},
	)
	w.trySend(env)
}

func (w *Worker) emitError(msg string, mapper *Mapper) {
	env := events.NewEnvelope(
		aep.NewID(),
		mapper.SessionID(),
		w.nextSeq(),
		events.Error,
		events.ErrorData{Code: events.ErrCodeWorkerCrash, Message: msg},
	)
	w.trySend(env)
}

func (w *Worker) trySend(env *events.Envelope) {
	conn := w.Conn()
	if conn == nil {
		return
	}
	ts, ok := conn.(interface{ TrySend(*events.Envelope) bool })
	if !ok {
		return
	}
	if !ts.TrySend(env) {
		w.Log.Warn("opencodecli: recv channel full, dropping message")
	}
}

// closeStdin closes and nils the stdin pipe. Caller must hold w.Mu.
func (w *Worker) closeStdin() {
	if w.stdin != nil {
		w.stdin.Close()
		w.stdin = nil
	}
}

func (w *Worker) nextSeq() int64 {
	return w.seq.Add(1)
}

// ─── Init ─────────────────────────────────────────────────────────────────────

func init() {
	worker.Register(worker.TypeOpenCodeCLI, func() (worker.Worker, error) {
		return &Worker{BaseWorker: base.NewBaseWorker(slog.Default(), nil)}, nil
	})
}
