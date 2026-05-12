package claudecode

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/security"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
	"github.com/hrygo/hotplex/internal/worker/proc"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// Compile-time interface compliance checks.
var _ worker.Worker = (*Worker)(nil)

// commandParts stores the space-split command (binary + optional prefix args).
// Thread-safe via atomic.Value. Default: ["claude"].
var commandParts atomic.Value // []string

// permissionPrompt controls whether --permission-prompt-tool stdio is passed to Claude Code.
var permissionPrompt atomic.Value // bool

// permissionAutoApprove lists tool names to auto-approve without user interaction.
var permissionAutoApprove atomic.Value // []string

func init() {
	commandParts.Store([]string{"claude"})
	permissionPrompt.Store(false)
	permissionAutoApprove.Store([]string{})
}

// InitConfig applies Claude Code worker configuration.
func InitConfig(cfg config.ClaudeCodeConfig) {
	cmd := cfg.Command
	if cmd == "" {
		cmd = "claude"
	}
	parts := strings.Fields(cmd)
	commandParts.Store(parts)
	permissionPrompt.Store(cfg.PermissionPrompt)
	autoApprove := cfg.PermissionAutoApprove
	if autoApprove == nil {
		autoApprove = []string{}
	}
	permissionAutoApprove.Store(autoApprove)
	if err := security.RegisterCommand(parts[0]); err != nil {
		slog.Default().Error("claudecode: failed to register command", "command", parts[0], "err", err)
	}
}

// autoApproveTool checks if a control request's tool name is in the auto-approve list.
// If matched, it sends an allow response and returns true.
func autoApproveTool(ctrl *ControlHandler, cr *ControlRequestPayload) bool {
	list := permissionAutoApprove.Load()
	if list == nil {
		return false
	}
	toolNames, ok := list.([]string)
	if !ok || !slices.Contains(toolNames, cr.ToolName) {
		return false
	}
	ctrl.log.Info("auto-approved permission request", "tool", cr.ToolName, "request_id", cr.RequestID)
	_ = ctrl.SendPermissionResponse(cr.RequestID, true, "auto-approved")
	return true
}

// Env blocklist for Claude Code worker.
// All os.Environ() vars are passed through by default, except those listed here.
// Gateway-internal secrets use the HOTPLEX_ prefix to prevent leakage.
var claudeCodeEnvBlocklist = []string{
	// Nested agent detection — must never propagate to worker subprocess.
	"CLAUDECODE",
	// Gateway-internal secrets (prefix match blocks all HOTPLEX_* vars;
	// HOTPLEX_SESSION_ID and HOTPLEX_WORKER_TYPE are added separately in BuildEnv).
	"HOTPLEX_",
}

// Default session store directory.
const defaultSessionStoreDir = ".claude/projects"

// Worker implements the Claude Code worker adapter.
type Worker struct {
	*base.BaseWorker

	sessionID   string
	projectDir  string             // original working directory for the worker process
	origSession worker.SessionInfo // first Start's session info, reused by ResetContext

	// Protocol layers
	parser  *Parser
	mapper  *Mapper
	control *ControlHandler

	// Goroutine lifecycle
	cancel context.CancelFunc

	// Seq generation (atomic, no mutex needed)
	seq atomic.Int64

	// tempFiles tracks temp files created for --*-file flags (system prompt,
	// MCP config). Cleaned up in Terminate. Using files avoids Windows cmd.exe
	// mangling XML/JSON characters (<, >) in inline arguments.
	tempFiles []string

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
func (w *Worker) EnvBlocklist() []string  { return claudeCodeEnvBlocklist }
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
	w.Mu.Lock()
	defer w.Mu.Unlock()
	return w.startLocked(ctx, session, true)
}

func (w *Worker) startLocked(_ context.Context, session worker.SessionInfo, resume bool) error {
	if w.Proc != nil {
		return fmt.Errorf("claudecode: already started")
	}

	// When creating a new session (--session-id), clean up leftover files from
	// previous sessions to prevent "already in use" errors from Claude Code CLI.
	if !resume {
		w.sessionID = session.SessionID
		if err := w.deleteSessionFiles(); err != nil {
			w.Log.Warn("claudecode: pre-start session file cleanup failed", "err", err)
		}
	}

	args, err := w.buildCLIArgs(session, resume)
	if err != nil {
		return fmt.Errorf("claudecode: build args: %w", err)
	}
	w.Proc = proc.New(proc.Opts{
		Logger:       w.Log,
		AllowedTools: session.AllowedTools,
	})
	w.Proc.SetPIDKey(session.SessionID)

	// Split command into binary + optional prefix args (e.g. "ccr code" → binary="ccr", prefix=["code"]).
	parts, _ := commandParts.Load().([]string)
	if parts == nil {
		parts = []string{"claude"}
	}
	binary := parts[0]
	fullArgs := make([]string, 0, len(parts)-1+len(args))
	fullArgs = append(fullArgs, parts[1:]...)
	fullArgs = append(fullArgs, args...)

	bgCtx := context.Background()
	stdin, _, _, err := w.Proc.Start(bgCtx, binary, fullArgs, base.BuildEnv(session, claudeCodeEnvBlocklist, "claude-code"), session.ProjectDir)
	if err != nil {
		w.cleanupTempFiles()
		w.Proc = nil
		return fmt.Errorf("claudecode: start: %w", err)
	}

	childCtx, cancel := context.WithCancel(bgCtx)
	w.cancel = cancel

	w.sessionID = session.SessionID
	w.projectDir = session.ProjectDir
	w.seq.Store(0)

	// Preserve original session info for ResetContext (only on first Start).
	if w.origSession.SessionID == "" {
		w.origSession = session
	}

	// readLineFn: use test override if set, otherwise real proc reader.
	if w.readLineFn == nil {
		w.readLineFn = w.Proc.ReadLine
	}

	w.parser = NewParser(w.Log)
	w.mapper = NewMapper(w.Log, session.SessionID, w.nextSeq)
	w.control = NewControlHandler(w.BaseWorker.Log, stdin)

	bc := base.NewConn(w.BaseWorker.Log, stdin, session.UserID, session.SessionID)
	w.SetConnLocked(bc)
	// Share Conn's mutex with ControlHandler so all stdin writes are serialized
	// through a single lock, preventing interleaved NDJSON on the shared stdin fd.
	w.control.SetWriteMu(bc.WriteMu())

	w.BaseWorker.StartTime = time.Now()
	w.BaseWorker.SetLastIO(w.BaseWorker.StartTime)

	go w.readOutput(childCtx)
	return nil
}

// buildCLIArgs constructs the Claude Code CLI argument list.
// Session mode:
//   - resume=true:  --resume <session-id>  (恢复已有会话)
//   - resume=false: --session-id <id>       (创建新会话)
func (w *Worker) buildCLIArgs(session worker.SessionInfo, resume bool) ([]string, error) {
	args := []string{
		"--print",
		"--verbose", // Required for stream-json mode
		"--output-format", "stream-json",
		"--input-format", "stream-json",
	}

	// Conditionally enable permission prompt tool for interaction chain.
	// When disabled, Claude Code auto-denies ask results in headless mode.
	if pp, _ := permissionPrompt.Load().(bool); pp {
		args = append(args, "--permission-prompt-tool", "stdio")
	}

	// Only two session modes:
	// - resume=true  → --resume <id>
	// - resume=false → --session-id <id>
	if resume {
		args = append(args, "--resume", aep.ParseSessionID(session.SessionID))
	} else {
		args = append(args, "--session-id", aep.ParseSessionID(session.SessionID))
	}

	// Permission mode: default bypass (preserves existing behavior), configurable override.
	// --permission-prompt-tool stdio + --dangerously-skip-permissions (both enabled):
	//   - Normal operations: step 2a bypass → allow (no control_request)
	//   - Bypass-immune ops (.claude/, .git/, shell config): step 1g → ask → control_request
	// --permission-prompt-tool disabled (default):
	//   - All ask results auto-denied by Claude Code in headless mode
	if session.SkipPermissions {
		args = append(args, "--dangerously-skip-permissions")
	} else if session.PermissionMode != "" {
		args = append(args, "--permission-mode", session.PermissionMode)
	} else {
		args = append(args, "--dangerously-skip-permissions") // default bypass
	}
	if len(session.DisallowedTools) > 0 {
		args = append(args, "--disallowed-tools", joinTools(session.DisallowedTools))
	}

	if len(session.AllowedModels) > 0 {
		if err := security.ValidateModel(session.AllowedModels[0]); err != nil {
			slog.Warn("claudecode: model rejected by security policy",
				"model", session.AllowedModels[0],
				"session_id", session.SessionID,
				"error", err,
			)
		} else {
			args = append(args, "--model", session.AllowedModels[0])
		}
	}
	if len(session.AllowedTools) > 0 {
		args = append(args, "--allowed-tools", joinTools(session.AllowedTools))
	}
	// System prompt injection: use temp files instead of inline arguments.
	// On Windows, CLI wrappers (e.g. ccr.cmd) pass args through cmd.exe which
	// interprets < and > as I/O redirection, mangling XML content in prompts.
	// File-based injection (--*-file flags) avoids this entirely.
	if session.SystemPromptReplace != "" {
		path, err := w.writeTempFile("system-prompt", session.SystemPromptReplace)
		if err != nil {
			return nil, fmt.Errorf("write system prompt file: %w", err)
		}
		args = append(args, "--system-prompt-file", path)
	} else if session.SystemPrompt != "" {
		path, err := w.writeTempFile("append-system-prompt", session.SystemPrompt)
		if err != nil {
			return nil, fmt.Errorf("write append system prompt file: %w", err)
		}
		args = append(args, "--append-system-prompt-file", path)
	}
	if session.MCPConfig != "" {
		path, err := w.writeTempFile("mcp-config", session.MCPConfig)
		if err != nil {
			return nil, fmt.Errorf("write MCP config file: %w", err)
		}
		args = append(args, "--mcp-config", path)
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

	return args, nil
}

func (w *Worker) Input(ctx context.Context, content string, metadata map[string]any) error {
	conn := w.Conn()
	if conn == nil {
		return fmt.Errorf("claudecode: not started")
	}

	// Check if this is a control response (permission, question, or elicitation)
	handled, err := base.DispatchMetadata(ctx, metadata, w)
	if err != nil {
		return err
	}
	if handled {
		w.SetLastIO(time.Now())
		return nil
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

// CloseInput signals EOF on the worker's stdin so it exits after processing.
func (w *Worker) CloseInput() error {
	if conn, ok := w.Conn().(*base.Conn); ok {
		return conn.CloseInput()
	}
	return nil
}

func (w *Worker) HandlePermissionResponse(_ context.Context, reqID string, allowed bool, reason string) error {
	w.Log.Info("claudecode: sending permission response to stdin",
		"request_id", reqID,
		"allowed", allowed,
		"session_id", w.sessionID)
	if err := w.control.SendPermissionResponse(reqID, allowed, reason); err != nil {
		return fmt.Errorf("claudecode: permission response: %w", err)
	}
	return nil
}

func (w *Worker) HandleQuestionResponse(_ context.Context, reqID string, answers map[string]string) error {
	if err := w.control.SendQuestionResponse(reqID, answers); err != nil {
		return fmt.Errorf("claudecode: question response: %w", err)
	}
	return nil
}

func (w *Worker) HandleElicitationResponse(_ context.Context, reqID, action string, content map[string]any) error {
	if err := w.control.SendElicitationResponse(reqID, action, content); err != nil {
		return fmt.Errorf("claudecode: elicitation response: %w", err)
	}
	return nil
}

func (w *Worker) Terminate(ctx context.Context) error {
	// Cancel goroutines first
	if w.cancel != nil {
		w.cancel()
	}

	w.cleanupTempFiles()
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

// SendControlRequest sends a control request to Claude Code and waits for the response.
func (w *Worker) SendControlRequest(ctx context.Context, subtype string, body map[string]any) (map[string]any, error) {
	w.Mu.Lock()
	if w.Proc == nil || !w.Proc.IsRunning() {
		w.Mu.Unlock()
		return nil, fmt.Errorf("claudecode: worker process is not running")
	}
	ctrl := w.control
	w.Mu.Unlock()

	if ctrl == nil {
		return nil, fmt.Errorf("claudecode: control handler not initialized")
	}
	return ctrl.SendControlRequest(ctx, subtype, body)
}

func (w *Worker) LastIO() time.Time {
	return w.BaseWorker.LastIO()
}

// ResetContext clears the worker runtime context for a fresh start.
// Claude Code does not support in-place context clearing, so this:
//  1. Terminates the current process
//  2. Deletes session files from ~/.claude/projects/*/<id>.jsonl and related paths
//  3. Starts a fresh process with --session-id (same ID, no files to conflict)
//
// The original session configuration (AllowedTools, SystemPrompt, MCPConfig, etc.)
// is preserved from the first Start call via origSession.
//
// The caller (Bridge.ResetSession) must set intentionalExit before calling this
// so that forwardEvents skips crash handling for the old process.
func (w *Worker) ResetContext(ctx context.Context) error {
	w.Mu.Lock()
	orig := w.origSession
	w.Mu.Unlock()

	if err := w.Terminate(ctx); err != nil {
		return fmt.Errorf("claudecode: reset terminate: %w", err)
	}

	// Delete session files so --session-id won't hit "already in use".
	if err := w.deleteSessionFiles(); err != nil {
		w.Log.Warn("claudecode: failed to delete session files, reset may fail", "session_id", w.sessionID, "err", err)
	}

	// Reset readLineFn so the next Start() assigns the new Proc.ReadLine.
	// Without this, the second Start reuses the OLD Proc.ReadLine which reads
	// from the terminated process's stdout pipe, causing readOutput to exit
	// immediately on EOF → forwardEvents force-kills the new worker after 2s.
	w.Mu.Lock()
	w.readLineFn = nil
	w.Mu.Unlock()

	// Reuse original session config (AllowedTools, SystemPrompt, MCPConfig, etc.)
	// but clear WorkerSessionID since the Claude session files were deleted.
	orig.WorkerSessionID = ""
	return w.Start(ctx, orig)
}

// sessionFileGlobs returns glob patterns for Claude Code session files.
// Used by deleteSessionFiles to clean up all session-related artifacts.
func sessionFileGlobs(homeDir, parsedID string) []string {
	return []string{
		filepath.Join(homeDir, ".claude", "projects", "*", parsedID+".jsonl"),
		filepath.Join(homeDir, ".claude", "projects", "*", parsedID),
		filepath.Join(homeDir, ".claude", "session-env", parsedID),
	}
}

// HasSessionFiles checks whether the JSONL conversation file exists on disk.
// Only JSONL files indicate a resumable session; empty session-env directories
// are insufficient and must not trigger the resume path.
func (w *Worker) HasSessionFiles(sessionID string) bool {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Fail-open: assume files exist so normal resume path is attempted.
		w.Log.Warn("claudecode: cannot resolve home dir, assuming files exist",
			"session_id", sessionID, "err", err)
		return true
	}
	parsedID := aep.ParseSessionID(sessionID)
	pattern := filepath.Join(homeDir, ".claude", "projects", "*", parsedID+".jsonl")
	matches, _ := filepath.Glob(pattern)
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && !fi.IsDir() {
			return true
		}
	}
	return false
}

// deleteSessionFiles removes Claude session files to prevent "already in use" errors on reset.
func (w *Worker) deleteSessionFiles() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}

	parsedID := aep.ParseSessionID(w.sessionID)
	patterns := sessionFileGlobs(homeDir, parsedID)

	var (
		firstErr error
		total    int
		parents  = map[string]bool{}
	)
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			w.Log.Warn("claudecode: glob session files", "session_id", w.sessionID, "pattern", pattern, "err", err)
			continue
		}
		for _, m := range matches {
			if err := os.RemoveAll(m); err != nil && firstErr == nil {
				firstErr = err
				w.Log.Warn("claudecode: failed to remove session file", "session_id", w.sessionID, "path", m, "err", err)
			} else {
				w.Log.Debug("claudecode: removed session file", "session_id", w.sessionID, "path", m)
				total++
				parents[filepath.Dir(m)] = true
			}
		}
	}
	// Best-effort cleanup of empty parent directories to prevent
	// future false positives from stale empty dirs.
	for dir := range parents {
		_ = os.Remove(dir)
	}
	if total > 0 {
		w.Log.Info("claudecode: deleted session files for reset", "count", total, "session_id", w.sessionID)
	}
	return firstErr
}

// ─── Internal ────────────────────────────────────────────────────────────────

func (w *Worker) readOutput(ctx context.Context) {
	// Capture current Conn at entry to avoid closing the NEW Conn after reset.
	// ResetContext.Start() replaces the Conn, but this goroutine's defer must
	// close the OLD Conn that it was actually reading from.
	entryConn := w.Conn()
	defer func() {
		if r := recover(); r != nil {
			w.BaseWorker.Log.Error("claudecode: readOutput panic",
				"session_id", w.sessionID, "panic", r, "stack", string(debug.Stack()))
		}
		if entryConn != nil {
			_ = entryConn.Close()
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
			w.BaseWorker.Log.Error("claudecode: read line", "session_id", w.sessionID, "err", err)
			return
		}

		if line == "" {
			continue
		}

		// Single parse — eliminates redundant []byte(string) conversions and
		// json.Unmarshal calls per line (previously 2-3 per line).
		var msg SDKMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			w.BaseWorker.Log.Warn("claudecode: parse line", "session_id", w.sessionID, "err", err, "line", line)
			continue
		}

		// Handle control_response — internal protocol, not a standard SDK event.
		if msg.Type == "control_response" {
			if len(msg.Response) > 0 && w.control != nil {
				var respMap map[string]any
				if json.Unmarshal(msg.Response, &respMap) == nil && respMap != nil {
					reqID, _ := respMap["request_id"].(string)
					if reqID != "" {
						payload := respMap
						if inner, ok := payload["response"].(map[string]any); ok {
							payload = inner
						}
						w.control.DeliverResponse(reqID, payload)
					}
				}
			}
			continue
		}

		workerEvents, err := w.parser.ParseMessage(&msg)
		if err != nil {
			w.BaseWorker.Log.Warn("claudecode: parse event", "session_id", w.sessionID, "err", err, "type", msg.Type)
			// Deny unparseable control_request to prevent Worker from blocking.
			if msg.Type == "control_request" && w.control != nil && msg.RequestID != "" {
				_ = w.control.SendPermissionResponse(msg.RequestID, false, "gateway: parse error, auto-denied")
			}
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
				w.BaseWorker.Log.Info("claudecode: received interrupt, terminating", "session_id", w.sessionID)
				_ = w.BaseWorker.Terminate(context.Background())
				return

			case EventControl:
				cr, ok := evt.Payload.(*ControlRequestPayload)
				if !ok {
					continue
				}
				switch cr.Subtype {
				case string(ControlCanUseTool):
					if cr.ToolName == "AskUserQuestion" {
						// AskUserQuestion → QuestionRequest event
						var questions []events.Question
						if len(cr.Input) > 0 {
							var input struct {
								Questions []events.Question `json:"questions"`
							}
							_ = json.Unmarshal(cr.Input, &input)
							questions = input.Questions
						}
						env := events.NewEnvelope(
							aep.NewID(),
							w.sessionID,
							w.nextSeq(),
							events.QuestionRequest,
							events.QuestionRequestData{
								ID:        cr.RequestID,
								ToolName:  cr.ToolName,
								Questions: questions,
							},
						)
						w.trySend(env)
					} else {
						// Check auto-approve list before forwarding to user
						if autoApproveTool(w.control, cr) {
							continue
						}
						// Other tools → PermissionRequest event
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
								InputRaw:    cr.Input,
							},
						)
						w.trySend(env)
					}
				case "elicitation":
					// MCP Elicitation → ElicitationRequest event
					var elData struct {
						MCPServerName   string         `json:"mcp_server_name"`
						Message         string         `json:"message"`
						Mode            string         `json:"mode,omitempty"`
						URL             string         `json:"url,omitempty"`
						ElicitationID   string         `json:"elicitation_id,omitempty"`
						RequestedSchema map[string]any `json:"requested_schema,omitempty"`
					}
					if evt.RawMessage != nil && len(evt.RawMessage.Response) > 0 {
						_ = json.Unmarshal(evt.RawMessage.Response, &elData)
					}
					env := events.NewEnvelope(
						aep.NewID(),
						w.sessionID,
						w.nextSeq(),
						events.ElicitationRequest,
						events.ElicitationRequestData{
							ID:              cr.RequestID,
							MCPServerName:   elData.MCPServerName,
							Message:         elData.Message,
							Mode:            elData.Mode,
							URL:             elData.URL,
							ElicitationID:   elData.ElicitationID,
							RequestedSchema: elData.RequestedSchema,
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
					w.BaseWorker.Log.Warn("claudecode: map event", "session_id", w.sessionID, "err", err)
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

// writeTempFile writes content to a temp file and tracks it for cleanup.
// Returns the absolute path to the file. The file is deleted in cleanupTempFiles
// which is called from Terminate.
func (w *Worker) writeTempFile(prefix, content string) (string, error) {
	ext := ".txt"
	if prefix == "mcp-config" {
		ext = ".json"
	}
	f, err := os.CreateTemp("", "hotplex-"+prefix+"-*"+ext)
	if err != nil {
		return "", fmt.Errorf("create temp file: %w", err)
	}
	path := f.Name()
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("write temp file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close temp file: %w", err)
	}
	w.tempFiles = append(w.tempFiles, path)
	return path, nil
}

// cleanupTempFiles removes all temp files created for this worker.
// On Windows, the child process may still hold a file handle briefly after
// termination, so we retry once after a short delay if deletion fails.
func (w *Worker) cleanupTempFiles() {
	for _, path := range w.tempFiles {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			// Retry once after a short delay for Windows file-lock stragglers.
			time.Sleep(200 * time.Millisecond)
			if err2 := os.Remove(path); err2 != nil && !os.IsNotExist(err2) {
				w.Log.Warn("claudecode: failed to remove temp file", "path", path, "err", err2)
			}
		}
	}
	w.tempFiles = nil
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
