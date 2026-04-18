package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/hotplex/hotplex-worker/internal/worker"
	"github.com/hotplex/hotplex-worker/internal/worker/base"
	"github.com/hotplex/hotplex-worker/internal/worker/proc"
	"github.com/hotplex/hotplex-worker/pkg/aep"
	"github.com/hotplex/hotplex-worker/pkg/events"
)

// Compile-time interface compliance checks.
var _ worker.Worker = (*Worker)(nil)

// Env whitelist for Claude Code worker.
var claudeCodeEnvWhitelist = []string{
	"HOME", "USER", "SHELL", "PATH", "TERM",
	"LANG", "LC_ALL", "PWD",
	// Claude Code CLI vars (兼容前缀)
	"CLAUDE_API_KEY", "CLAUDE_MODEL", "CLAUDE_BASE_URL",
	"CLAUDE_CODE_MODE", "CLAUDE_DISABLE_AUTO_PERMISSIONS",
	"CLAUDE_CODE_EXECPATH", "CLAUDE_CODE_ENTRYPOINT",
	"CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS",
	"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC",
	// Claude Code CLI prefix (catches all CLAUDE_CODE_* vars)
	"CLAUDE_CODE_",
	// Anthropic SDK vars (部分用户直接设置这些)
	"ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL", "ANTHROPIC_MODEL",
	"ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_BEDROCK_BASE_URL",
	"ANTHROPIC_VERTEX_BASE_URL", "ANTHROPIC_FOUNDRY_BASE_URL",
	"ANTHROPIC_CUSTOM_HEADERS",
	// External LLM API Keys
	"OPENAI_API_KEY", "DASHSCOPE_API_KEY", "MINIMAX_API_KEY",
	"ZHIPU_API_KEY", "DEEPSEEK_API_KEY", "OPENROUTER_API_KEY",
	// 安全配置
	"BASH_MAX_TIMEOUT_MS", "BASH_MAX_OUTPUT_LENGTH",
	"MAX_THINKING_TOKENS", "MAX_MCP_OUTPUT_TOKENS",
	"MCP_TIMEOUT", "MCP_TOOL_TIMEOUT",
	// OpenTelemetry (prefix-matched in BuildEnv)
	"OTEL_",
}

// Default session store directory.
const defaultSessionStoreDir = ".claude/projects"

// Worker implements the Claude Code worker adapter.
type Worker struct {
	*base.BaseWorker

	sessionID string

	// Protocol layers
	parser  *Parser
	mapper  *Mapper
	control *ControlHandler

	// Goroutine lifecycle
	cancel context.CancelFunc

	// Seq generation (atomic, no mutex needed)
	seq atomic.Int64

	// readLineFn reads the next line from stdout. If nil, readOutput uses
	// proc.ReadLine. Inject a func for unit testing without a real process.
	readLineFn func() (string, error)

	// testConn allows tests to inject a mock SessionConn without a real process.
	// When non-nil, Conn() returns this instead of BaseWorker.Conn().
	testConn worker.SessionConn
}

// New creates a new Claude Code worker.
func New() *Worker {
	return &Worker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
	}
}

// ─── Capabilities ─────────────────────────────────────────────────────────────

func (w *Worker) Type() worker.WorkerType { return worker.TypeClaudeCode }

func (w *Worker) SupportsResume() bool    { return true }
func (w *Worker) SupportsStreaming() bool { return true }
func (w *Worker) SupportsTools() bool     { return true }
func (w *Worker) EnvWhitelist() []string  { return claudeCodeEnvWhitelist }
func (w *Worker) SessionStoreDir() string { return defaultSessionStoreDir }
func (w *Worker) MaxTurns() int           { return 0 }
func (w *Worker) Modalities() []string    { return []string{"text", "code", "image"} }

// ─── Worker Lifecycle ─────────────────────────────────────────────────────────

func (w *Worker) Start(ctx context.Context, session worker.SessionInfo) error {
	w.Mu.Lock()
	defer w.Mu.Unlock()
	return w.startLocked(ctx, session, false)
}

func (w *Worker) Resume(ctx context.Context, session worker.SessionInfo) error {
	return w.startLocked(ctx, session, true)
}

func (w *Worker) startLocked(_ context.Context, session worker.SessionInfo, resume bool) error {
	if w.Proc != nil {
		return fmt.Errorf("claudecode: already started")
	}

	args := w.buildCLIArgs(session, resume)
	env := base.BuildEnv(session, claudeCodeEnvWhitelist, "claude-code")

	w.Proc = proc.New(proc.Opts{
		Logger:       w.Log,
		AllowedTools: session.AllowedTools,
	})

	bgCtx := context.Background()
	stdin, _, _, err := w.Proc.Start(bgCtx, "claude", args, env, session.ProjectDir)
	if err != nil {
		w.Proc = nil
		return fmt.Errorf("claudecode: start: %w", err)
	}

	childCtx, cancel := context.WithCancel(bgCtx)
	w.cancel = cancel

	w.sessionID = session.SessionID
	w.seq.Store(0)

	// readLineFn: use test override if set, otherwise real proc reader.
	if w.readLineFn == nil {
		w.readLineFn = w.Proc.ReadLine
	}

	w.parser = NewParser(w.Log)
	w.mapper = NewMapper(w.Log, session.SessionID, w.nextSeq)
	w.control = NewControlHandler(w.BaseWorker.Log, stdin)

	w.SetConnLocked(base.NewConn(w.BaseWorker.Log, stdin, session.UserID, session.SessionID))

	w.BaseWorker.StartTime = time.Now()
	w.BaseWorker.SetLastIO(w.BaseWorker.StartTime)

	go w.readOutput(childCtx)
	return nil
}

// buildCLIArgs constructs the Claude Code CLI argument list.
// Session mode:
//   - resume=true:  --resume <session-id>  (恢复已有会话)
//   - resume=false: --session-id <id>       (创建新会话)
func (w *Worker) buildCLIArgs(session worker.SessionInfo, resume bool) []string {
	args := []string{
		"--print",
		"--verbose", // Required for stream-json mode
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--dangerously-skip-permissions",
	}

	// Only two session modes:
	// - resume=true  → --resume <id>
	// - resume=false → --session-id <id>
	if resume {
		args = append(args, "--resume", aep.ParseSessionID(session.SessionID))
	} else {
		args = append(args, "--session-id", aep.ParseSessionID(session.SessionID))
	}

	if session.PermissionMode != "" {
		args = append(args, "--permission-mode", session.PermissionMode)
	}
	if session.SkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	}
	if len(session.DisallowedTools) > 0 {
		args = append(args, "--disallowed-tools", joinTools(session.DisallowedTools))
	}

	if len(session.AllowedModels) > 0 {
		args = append(args, "--model", session.AllowedModels[0])
	}
	if len(session.AllowedTools) > 0 {
		args = append(args, "--allowed-tools", joinTools(session.AllowedTools))
	}
	if session.SystemPromptReplace != "" {
		// --system-prompt replaces the default system prompt entirely
		args = append(args, "--system-prompt", session.SystemPromptReplace)
	} else if session.SystemPrompt != "" {
		// --append-system-prompt appends to the existing system prompt
		args = append(args, "--append-system-prompt", session.SystemPrompt)
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
	if session.IncludeHookEvents {
		args = append(args, "--include-hook-events")
	}
	if session.IncludePartialMessages {
		args = append(args, "--include-partial-messages")
	}

	return args
}

func (w *Worker) Input(ctx context.Context, content string, metadata map[string]any) error {
	conn := w.Conn()
	if conn == nil {
		return fmt.Errorf("claudecode: not started")
	}

	// Check if this is a permission response
	if metadata != nil {
		if permResp, ok := metadata["permission_response"].(map[string]any); ok {
			reqID, _ := permResp["request_id"].(string)
			allowed, _ := permResp["allowed"].(bool)
			reason, _ := permResp["reason"].(string)

			// Send permission response to Claude Code
			if err := w.control.SendPermissionResponse(reqID, allowed, reason); err != nil {
				return fmt.Errorf("claudecode: permission response: %w", err)
			}

			w.SetLastIO(time.Now())
			return nil
		}
	}

	// Normal input: use SendUserMessage for Claude Code's stream-json format
	// instead of AEP envelope format
	if baseConn, ok := conn.(*base.Conn); ok {
		if err := baseConn.SendUserMessage(ctx, content); err != nil {
			return fmt.Errorf("claudecode: input: %w", err)
		}
	} else {
		// Fallback to AEP envelope for tests with mock connections
		msg := events.NewEnvelope(
			aep.NewID(),
			w.sessionID,
			0, // seq assigned by hub
			events.Input,
			events.InputData{
				Content:  content,
				Metadata: metadata,
			},
		)
		if err := conn.Send(ctx, msg); err != nil {
			return fmt.Errorf("claudecode: input: %w", err)
		}
	}

	w.SetLastIO(time.Now())
	return nil
}

func (w *Worker) Terminate(ctx context.Context) error {
	// Cancel goroutines first
	if w.cancel != nil {
		w.cancel()
	}

	return w.BaseWorker.Terminate(ctx)
}

func (w *Worker) Conn() worker.SessionConn {
	if w.testConn != nil {
		return w.testConn
	}
	return w.BaseWorker.Conn()
}

func (w *Worker) Health() worker.WorkerHealth {
	return w.BaseWorker.Health(worker.TypeClaudeCode)
}

func (w *Worker) LastIO() time.Time {
	return w.BaseWorker.LastIO()
}

// ResetContext clears the worker runtime context.
// Claude Code does not support in-place context clearing, so this terminates the
// current process and starts a fresh one with --resume to recreate session files.
// The Gateway layer has already called sm.ClearContext() to clear SessionInfo.Context.
func (w *Worker) ResetContext(ctx context.Context) error {
	w.Mu.Lock()
	sessionID := w.sessionID
	w.Mu.Unlock()

	if err := w.Terminate(ctx); err != nil {
		return fmt.Errorf("claudecode: reset terminate: %w", err)
	}

	// Reconstruct session info from current worker state.
	conn := w.BaseWorker.Conn()
	var userID, projectDir string
	if conn != nil {
		userID = conn.UserID()
		projectDir = conn.SessionID() // same as sessionID for claudecode
	}
	if projectDir == "" {
		projectDir = sessionID
	}

	session := worker.SessionInfo{
		SessionID:  sessionID,
		UserID:     userID,
		ProjectDir: projectDir,
	}
	return w.Start(ctx, session)
}

// ─── Internal ────────────────────────────────────────────────────────────────

func (w *Worker) readOutput(ctx context.Context) {
	defer func() {
		if c := w.Conn(); c != nil {
			_ = c.Close()
		}
	}()

	w.Mu.Lock()
	if w.readLineFn == nil {
		w.Mu.Unlock()
		return
	}
	// Hold the lock during startup only; read loop below is unprotected so that
	// Terminate (which needs the lock) doesn't deadlock with a blocked scanner.
	readLineFn := w.readLineFn
	w.Mu.Unlock()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		line, err := readLineFn()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return
			}
			w.BaseWorker.Log.Error("claudecode: read line", "error", err)
			return
		}

		if line == "" {
			continue
		}

		workerEvents, err := w.parser.ParseLine(line)
		if err != nil {
			w.BaseWorker.Log.Warn("claudecode: parse line", "error", err, "line", line)
			continue
		}
		if len(workerEvents) == 0 {
			continue
		}

		w.SetLastIO(time.Now())

		// Map to AEP envelopes
		for _, evt := range workerEvents {
			switch evt.Type {
			case EventInterrupt:
				// Claude Code sent an interrupt — terminate gracefully.
				// Call BaseWorker.Terminate directly; no goroutine needed since
				// Terminate is not blocking and readOutput is already exiting.
				w.BaseWorker.Log.Info("claudecode: received interrupt, terminating")
				_ = w.BaseWorker.Terminate(context.Background())
				return

			case EventControl:
				cr, ok := evt.Payload.(*ControlRequestPayload)
				if !ok {
					continue
				}
				switch cr.Subtype {
				case string(ControlCanUseTool):
					// Forward to gateway for user approval
					var input map[string]any
					if len(cr.Input) > 0 {
						_ = json.Unmarshal(cr.Input, &input)
					}
					args := []string{`{}`}
					if len(input) > 0 {
						if s, err := json.Marshal(input); err == nil {
							args = []string{string(s)}
						}
					}
					env := events.NewEnvelope(
						aep.NewID(),
						w.sessionID,
						w.nextSeq(),
						events.PermissionRequest,
						events.PermissionRequestData{
							ID:          cr.RequestID,
							ToolName:    cr.ToolName,
							Description: cr.ToolName,
							Args:        args,
						},
					)
					w.trySend(env)
				default:
					// set_*, mcp_*, etc.: auto-success
					_, _ = w.control.HandlePayload(cr)
				}

			default:
				// Normal event mapping
				envs, err := w.mapper.Map(evt)
				if err != nil {
					w.BaseWorker.Log.Warn("claudecode: map event", "error", err)
					continue
				}
				if len(envs) == 0 {
					continue // Internal event, skip
				}
				for _, env := range envs {
					w.trySend(env)
				}
			}
		}
	}
}

// trySend non-blocking sends an envelope to the connection.
func (w *Worker) trySend(env *events.Envelope) {
	conn := w.Conn()
	if conn == nil {
		w.BaseWorker.Log.Warn("claudecode: trySend conn nil", "session_id", w.sessionID)
		return
	}

	// Duck-typed interface: *base.Conn (production) and mockConn (tests) both satisfy it.
	ts, ok := conn.(interface{ TrySend(*events.Envelope) bool })
	if !ok {
		w.BaseWorker.Log.Warn("claudecode: trySend conn type unsupported", "session_id", w.sessionID, "type", fmt.Sprintf("%T", conn))
		return
	}
	if !ts.TrySend(env) {
		w.BaseWorker.Log.Warn("claudecode: recv channel full, dropping", "session_id", w.sessionID, "event_type", env.Event.Type)
	}
}

// nextSeq generates the next sequence number.
func (w *Worker) nextSeq() int64 {
	return w.seq.Add(1)
}

// joinTools joins tool names with comma.
func joinTools(tools []string) string {
	return strings.Join(tools, ",")
}

// ─── Init ────────────────────────────────────────────────────────────────────

func init() {
	worker.Register(worker.TypeClaudeCode, func() (worker.Worker, error) {
		return &Worker{BaseWorker: base.NewBaseWorker(slog.Default(), nil)}, nil
	})
}

// ─── Session ID Compatibility ───────────────────────────────────────────────────

const (
	// csePrefix is the v2 infrastructure session ID prefix.
	csePrefix = "cse_"
	// sessionPrefix is the v1-compatible session ID prefix.
	sessionPrefix = "session_"
)

// ToCompatSessionID converts a v2 infrastructure session ID (cse_*) to
// the v1-compatible format (session_*). Non-cse IDs are returned unchanged.
func ToCompatSessionID(id string) string {
	if strings.HasPrefix(id, csePrefix) {
		return sessionPrefix + id[len(csePrefix):]
	}
	return id
}

// ToInfraSessionID converts a v1-compatible session ID (session_*) to
// the v2 infrastructure format (cse_*). Non-session IDs are returned unchanged.
func ToInfraSessionID(id string) string {
	if strings.HasPrefix(id, sessionPrefix) {
		return csePrefix + id[len(sessionPrefix):]
	}
	return id
}
