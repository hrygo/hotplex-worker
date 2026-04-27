// Package opencodeserver implements the OpenCode Server worker adapter.
//
// OpenCode Server runs as a persistent HTTP server process (opencode serve) that
// manages multiple sessions. Unlike CLI-based workers that use stdio, this adapter
// communicates via HTTP REST API for commands and Server-Sent Events (SSE) for
// streaming responses.
//
// # Architecture
//
//	Gateway (main process)
//	    ↓ creates Worker instances (one per session)
//	OpenCode Server Worker (this adapter, thin session adapter)
//	    ↓ shares SingletonProcessManager (one process for all sessions)
//	OpenCode Server Process (independent HTTP server, lazy-started)
//	    ↕ HTTP REST API + SSE
//	Worker ↔ Server communication
//
// # Key Features
//
//   - Resume Support: Can reconnect to existing server sessions
//   - Multi-session: Shared singleton server process handles all sessions
//   - SSE Streaming: Real-time event stream via text/event-stream
//   - Process Isolation: PGID-based process group for clean termination
//   - Lazy Startup: Server process starts on first session, stops after idle drain
//
// # Protocol
//
// # AEP v1 (Agent Event Protocol) over NDJSON
//
// See docs/specs/Worker-OpenCode-Server-Spec.md for full specification.
package opencodeserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

// Compile-time interface compliance checks.
var (
	_ worker.Worker         = (*Worker)(nil)
	_ worker.SessionConn    = (*conn)(nil)
	_ worker.InPlaceReseter = (*Worker)(nil)
	_ ControlRequester      = (*Worker)(nil)
	_ WorkerCommander       = (*Worker)(nil)
)

// Environment variable whitelist for OpenCode Server worker.
// These variables are passed through to the opencode serve process.
// All other environment variables are stripped for security isolation.
var openCodeSrvEnvWhitelist = []string{
	// System environment
	"HOME", "USER", "SHELL", "PATH", "TERM",
	"LANG", "LC_ALL", "PWD",
	// OpenCode API configuration
	"OPENAI_API_KEY", "OPENAI_BASE_URL",
	"OPENCODE_API_KEY", "OPENCODE_BASE_URL",
	// External LLM API Keys
	"DASHSCOPE_API_KEY", "MINIMAX_API_KEY",
	"ZHIPU_API_KEY", "DEEPSEEK_API_KEY", "OPENROUTER_API_KEY",
}

const (
	// recvChannelSize is the buffer size for SSE event channel.
	recvChannelSize = 256

	// httpClientTimeout is the timeout for HTTP client operations.
	httpClientTimeout = 30 * time.Second
)

// Worker implements the OpenCode Server worker adapter.
//
// Each Worker instance is a thin session adapter that does NOT own a process.
// All Workers share a SingletonProcessManager that manages one `opencode serve`
// process lazily started on first use.
//
// # Lifecycle
//
//  1. Start() acquires a ref from singleton, creates HTTP session, starts SSE reader
//  2. Input() sends user messages via HTTP POST
//  3. Terminate()/Kill() releases the ref and closes the SSE connection (not the process)
//  4. Wait() reports exit code based on crash notification from singleton
//
// # Concurrency Model
//
//   - Single owner: Worker is owned by one session.Manager
//   - Thread-safe: All public methods are safe for concurrent use
//   - Goroutines: readSSE runs in separate goroutine, writes to recvCh
//   - Backpressure: recvCh has 256 buffer, drops messages when full
type Worker struct {
	*base.BaseWorker

	singleton *SingletonProcessManager
	httpConn  *conn
	httpAddr  string
	client    *http.Client
	cmd       *ServerCommander
	crashSub  <-chan struct{} // closed when singleton process crashes

	// releaseOnce ensures singleton.Release() is called exactly once,
	// regardless of which method triggers it (Terminate, Kill, or Wait).
	releaseOnce sync.Once

	// workerSessionID atomically stores the worker-internal session ID.
	workerSessionID atomic.Value // string
}

var _ worker.WorkerSessionIDHandler = (*Worker)(nil)

func (w *Worker) GetWorkerSessionID() string {
	if w.httpConn != nil {
		return w.httpConn.sessionID
	}
	if v := w.workerSessionID.Load(); v != nil {
		if sid, ok := v.(string); ok {
			return sid
		}
	}
	return ""
}

func (w *Worker) SetWorkerSessionID(id string) {
	w.workerSessionID.Store(id)
	if w.httpConn != nil {
		w.httpConn.sessionID = id
	}
}

// New creates a new OpenCode Server worker instance.
func New() *Worker {
	return &Worker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		singleton:  defaultSingleton,
		client:     newHTTPClient(),
	}
}

func newHTTPClient() *http.Client {
	return &http.Client{
		Timeout: httpClientTimeout,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
			IdleConnTimeout:     90 * time.Second,
		},
	}
}

// ─── Capabilities ─────────────────────────────────────────────────────────────

func (w *Worker) Type() worker.WorkerType { return worker.TypeOpenCodeSrv }
func (w *Worker) SupportsResume() bool    { return true }
func (w *Worker) SupportsStreaming() bool { return true }
func (w *Worker) SupportsTools() bool     { return true }
func (w *Worker) EnvWhitelist() []string  { return openCodeSrvEnvWhitelist }
func (w *Worker) SessionStoreDir() string { return "" }
func (w *Worker) MaxTurns() int           { return 0 }
func (w *Worker) Modalities() []string    { return []string{"text", "code"} }

// ─── Worker Lifecycle ─────────────────────────────────────────────────────────

// Start acquires the singleton server, creates a new HTTP session, and starts
// the SSE reader goroutine.
func (w *Worker) Start(ctx context.Context, session worker.SessionInfo) error {
	if w.singleton == nil {
		return fmt.Errorf("opencodeserver: singleton not initialized (call InitSingleton first)")
	}

	// Prevent double-start
	w.Mu.Lock()
	if w.httpConn != nil {
		w.Mu.Unlock()
		return fmt.Errorf("opencodeserver: already started")
	}
	w.Mu.Unlock()

	// Acquire ref from singleton (starts process if first session)
	httpAddr, client, crashSub, err := w.singleton.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("opencodeserver: acquire server: %w", err)
	}
	w.httpAddr = httpAddr
	w.client = client
	w.crashSub = crashSub

	// Create new session via HTTP API
	sessionID, err := w.createSession(ctx, session.ProjectDir)
	if err != nil {
		w.releaseOnce.Do(func() { w.singleton.Release() })
		return fmt.Errorf("opencodeserver: create session: %w", err)
	}

	w.initSessionConn(ctx, sessionID, session)

	go w.readSSE(sessionID)
	return nil
}

// Input sends a user message to the OpenCode server.
func (w *Worker) Input(ctx context.Context, content string, metadata map[string]any) error {
	w.Mu.Lock()
	conn := w.httpConn
	w.Mu.Unlock()

	if conn == nil {
		return fmt.Errorf("opencodeserver: worker not started")
	}

	if metadata != nil {
		if permResp, ok := metadata["permission_response"].(map[string]any); ok {
			reqID, _ := permResp["request_id"].(string)
			allowed, _ := permResp["allowed"].(bool)
			reply := "once"
			if !allowed {
				reply = "reject"
			}
			return w.httpPost(ctx, fmt.Sprintf("/permission/%s/reply", reqID),
				map[string]string{"reply": reply})
		}
		if qResp, ok := metadata["question_response"].(map[string]any); ok {
			reqID, _ := qResp["id"].(string)
			answers, _ := qResp["answers"].(map[string]string)
			return w.httpPost(ctx, fmt.Sprintf("/question/%s/reply", reqID),
				map[string][][]string{"answers": answersToArrays(answers)})
		}
	}

	msg := events.NewEnvelope(
		aep.NewID(),
		conn.sessionID,
		0,
		events.Input,
		events.InputData{
			Content:  content,
			Metadata: metadata,
		},
	)

	if err := conn.Send(ctx, msg); err != nil {
		return fmt.Errorf("opencodeserver: send input: %w", err)
	}

	w.SetLastIO(time.Now())
	return nil
}

// Resume reconnects to an existing session on the shared OpenCode server.
func (w *Worker) Resume(ctx context.Context, session worker.SessionInfo) error {
	if w.singleton == nil {
		return fmt.Errorf("opencodeserver: singleton not initialized (call InitSingleton first)")
	}

	w.Mu.Lock()
	if w.httpConn != nil {
		w.Mu.Unlock()
		return fmt.Errorf("opencodeserver: already started")
	}
	w.Mu.Unlock()

	httpAddr, client, crashSub, err := w.singleton.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("opencodeserver: acquire server: %w", err)
	}
	w.httpAddr = httpAddr
	w.client = client
	w.crashSub = crashSub

	w.initSessionConn(ctx, session.SessionID, session)

	go w.readSSE(session.SessionID)
	return nil
}

// Terminate closes the SSE connection and releases the singleton ref.
// Does NOT kill the shared server process.
func (w *Worker) Terminate(_ context.Context) error {
	w.releaseOnce.Do(func() {
		if w.singleton != nil {
			w.singleton.Release()
		}
	})

	w.Mu.Lock()
	conn := w.httpConn
	w.httpConn = nil
	w.Mu.Unlock()

	if conn != nil {
		_ = conn.Close()
	}
	return nil
}

// Kill closes the SSE connection and releases the singleton ref.
// Does NOT kill the shared server process.
func (w *Worker) Kill() error {
	w.releaseOnce.Do(func() {
		if w.singleton != nil {
			w.singleton.Release()
		}
	})

	w.Mu.Lock()
	conn := w.httpConn
	w.httpConn = nil
	w.Mu.Unlock()

	if conn != nil {
		_ = conn.Close()
	}
	return nil
}

// Wait reports exit code based on whether the singleton process crashed.
// 0 = normal session end, 1 = process crashed.
func (w *Worker) Wait() (int, error) {
	if w.singleton == nil {
		return 0, fmt.Errorf("opencodeserver: not started")
	}

	w.releaseOnce.Do(func() { w.singleton.Release() })

	select {
	case <-w.crashSub:
		return 1, nil // process crashed
	default:
		return 0, nil // normal session end
	}
}

// Conn returns the HTTP-based session connection.
func (w *Worker) Conn() worker.SessionConn {
	w.Mu.Lock()
	defer w.Mu.Unlock()
	return w.httpConn
}

// Health returns a snapshot of the worker's runtime health.
func (w *Worker) Health() worker.WorkerHealth {
	health := worker.WorkerHealth{
		Type:    worker.TypeOpenCodeSrv,
		Healthy: true,
	}
	if w.singleton != nil {
		health.Running = w.singleton.IsRunning()
		health.Healthy = w.singleton.IsRunning()
	}

	w.Mu.Lock()
	if w.httpConn != nil {
		health.SessionID = w.httpConn.sessionID
	}
	if !w.StartTime.IsZero() {
		health.Uptime = time.Since(w.StartTime).Round(time.Second).String()
	}
	w.Mu.Unlock()

	return health
}

// LastIO returns the time of last I/O activity.
func (w *Worker) LastIO() time.Time {
	w.Mu.Lock()
	started := w.httpConn != nil
	w.Mu.Unlock()
	if !started {
		return time.Time{}
	}
	return w.BaseWorker.LastIO()
}

// ResetContext clears the worker runtime context in-place via HTTP API.
func (w *Worker) ResetContext(ctx context.Context) error {
	w.Mu.Lock()
	sessionID := ""
	if w.httpConn != nil {
		sessionID = w.httpConn.sessionID
	}
	httpAddr := w.httpAddr
	client := w.client
	w.Mu.Unlock()

	if sessionID == "" || httpAddr == "" {
		return fmt.Errorf("opencodeserver: reset: worker not started")
	}

	req, err := http.NewRequestWithContext(ctx, "POST", httpAddr+"/session/"+sessionID+"/reset", http.NoBody)
	if err != nil {
		return fmt.Errorf("opencodeserver: reset: new request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("opencodeserver: reset: http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
		return fmt.Errorf("opencodeserver: reset: status %d: %s", resp.StatusCode, string(body))
	}
	return nil
}

// InPlaceReset indicates context reset is in-place via HTTP, no process restart.
func (w *Worker) InPlaceReset() bool { return true }

func (w *Worker) SendControlRequest(ctx context.Context, subtype string, body map[string]any) (map[string]any, error) {
	if w.cmd == nil {
		return nil, fmt.Errorf("opencode server: commander not initialized")
	}
	return w.cmd.SendControlRequest(ctx, subtype, body)
}

func (w *Worker) Compact(ctx context.Context, args map[string]any) error {
	if w.cmd == nil {
		return fmt.Errorf("opencode server: commander not initialized")
	}
	return w.cmd.Compact(ctx, args)
}

func (w *Worker) Clear(ctx context.Context) error {
	if w.cmd == nil {
		return fmt.Errorf("opencode server: commander not initialized")
	}
	return w.cmd.Clear(ctx)
}

func (w *Worker) Rewind(ctx context.Context, targetID string) error {
	if w.cmd == nil {
		return fmt.Errorf("opencode server: commander not initialized")
	}
	return w.cmd.Rewind(ctx, targetID)
}

// ─── Internal Methods ─────────────────────────────────────────────────────────

func (w *Worker) applyPermissions(ctx context.Context, _ worker.SessionInfo) error {
	w.Mu.Lock()
	cmd := w.cmd
	w.Mu.Unlock()

	if cmd == nil {
		return fmt.Errorf("commander not initialized")
	}

	_, err := cmd.SendControlRequest(ctx, "set_permission_mode", map[string]any{
		"mode": "bypassPermissions",
	})
	return err
}

func (w *Worker) createSession(ctx context.Context, projectDir string) (string, error) {
	reqBody := strings.NewReader(fmt.Sprintf(`{"project_dir": %q}`, projectDir))
	req, err := http.NewRequestWithContext(ctx, "POST", w.httpAddr+"/session", reqBody)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create session failed: status %d, body: %s", resp.StatusCode, string(body))
	}

	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	return result.ID, nil
}

func (w *Worker) initHTTPConn(userID, sessionID, systemPrompt string) {
	w.httpConn = &conn{
		userID:       userID,
		sessionID:    sessionID,
		httpAddr:     w.httpAddr,
		client:       w.client,
		recvCh:       make(chan *events.Envelope, recvChannelSize),
		log:          w.Log,
		systemPrompt: systemPrompt,
	}
}

func (w *Worker) initSessionConn(ctx context.Context, serverSessionID string, session worker.SessionInfo) {
	w.initHTTPConn(session.UserID, serverSessionID, session.SystemPrompt)
	w.cmd = &ServerCommander{
		client:    w.client,
		baseURL:   w.httpAddr,
		sessionID: serverSessionID,
	}
	if err := w.applyPermissions(ctx, session); err != nil {
		w.Log.Warn("opencodeserver: failed to set permissions", "error", err)
	}
	w.Mu.Lock()
	w.StartTime = time.Now()
	w.SetLastIO(w.StartTime)
	w.Mu.Unlock()
}

func (w *Worker) readSSE(sessionID string) {
	defer func() {
		if r := recover(); r != nil {
			w.Log.Error("opencodeserver: readSSE panic", "session_id", sessionID, "panic", r, "stack", string(debug.Stack()))
		}
	}()

	url := fmt.Sprintf("%s/events?session_id=%s", w.httpAddr, sessionID)
	req, err := http.NewRequest("GET", url, http.NoBody)
	if err != nil {
		w.Log.Error("opencodeserver: create SSE request", "error", err)
		return
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	resp, err := w.client.Do(req)
	if err != nil {
		w.Log.Error("opencodeserver: SSE connect", "error", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		w.Log.Error("opencodeserver: SSE status",
			"status", resp.StatusCode,
			"body", string(body))
		return
	}

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				w.Log.Debug("opencodeserver: SSE stream ended (EOF)")
				return
			}
			w.Log.Error("opencodeserver: SSE read", "error", err)
			return
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		env, err := aep.DecodeLine([]byte(data))
		if err != nil {
			var busEvent struct {
				Type       string          `json:"type"`
				Properties json.RawMessage `json:"properties"`
			}
			if jsonErr := json.Unmarshal([]byte(data), &busEvent); jsonErr == nil {
				switch busEvent.Type {
				case "permission.asked":
					w.handlePermissionAsked(sessionID, busEvent.Properties)
				case "question.asked":
					w.handleQuestionAsked(sessionID, busEvent.Properties)
				default:
					w.Log.Debug("opencodeserver: unhandled bus event", "type", busEvent.Type)
				}
				continue
			}
			w.Log.Warn("opencodeserver: decode SSE data", "error", err, "data", data)
			continue
		}

		env.SessionID = sessionID
		w.SetLastIO(time.Now())

		w.Mu.Lock()
		ch := w.httpConn
		var recvCh chan *events.Envelope
		if ch != nil {
			recvCh = ch.recvCh
		}
		w.Mu.Unlock()

		if recvCh == nil {
			w.Log.Debug("opencodeserver: connection closed, stopping SSE reader")
			return
		}

		select {
		case recvCh <- env:
		default:
			w.Log.Warn("opencodeserver: recv channel full, dropping message",
				"event_type", env.Event.Type, "event_id", env.ID)
		}
	}
}

// ─── OpenCode Bus Event Handlers ──────────────────────────────────────────────

func (w *Worker) handlePermissionAsked(sessionID string, props json.RawMessage) {
	var data struct {
		ID       string         `json:"id"`
		Metadata map[string]any `json:"metadata"`
	}
	if err := json.Unmarshal(props, &data); err != nil {
		w.Log.Warn("opencodeserver: parse permission.asked", "error", err)
		return
	}

	toolName, _ := data.Metadata["tool"].(string)
	args, _ := json.Marshal(data.Metadata)
	env := events.NewEnvelope(
		aep.NewID(), sessionID, 0,
		events.PermissionRequest,
		events.PermissionRequestData{
			ID:          data.ID,
			ToolName:    toolName,
			Description: toolName,
			Args:        []string{string(args)},
			InputRaw:    json.RawMessage(args),
		},
	)
	w.trySend(env)
}

func (w *Worker) handleQuestionAsked(sessionID string, props json.RawMessage) {
	var data struct {
		ID        string            `json:"id"`
		Questions []events.Question `json:"questions"`
	}
	if err := json.Unmarshal(props, &data); err != nil {
		w.Log.Warn("opencodeserver: parse question.asked", "error", err)
		return
	}

	env := events.NewEnvelope(
		aep.NewID(), sessionID, 0,
		events.QuestionRequest,
		events.QuestionRequestData{
			ID:        data.ID,
			Questions: data.Questions,
		},
	)
	w.trySend(env)
}

func (w *Worker) trySend(env *events.Envelope) {
	w.Mu.Lock()
	c := w.httpConn
	closed := c == nil
	w.Mu.Unlock()

	if closed {
		return
	}

	w.SetLastIO(time.Now())
	select {
	case c.recvCh <- env:
	default:
		w.Log.Warn("opencodeserver: recv channel full, dropping bus event",
			"event_type", env.Event.Type)
	}
}

func (w *Worker) httpPost(ctx context.Context, path string, payload any) error {
	w.Mu.Lock()
	addr := w.httpAddr
	w.Mu.Unlock()

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("opencodeserver: marshal payload: %w", err)
	}

	url := addr + path
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("opencodeserver: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("opencodeserver: post %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("opencodeserver: post %s failed: status %d, body: %s",
			path, resp.StatusCode, string(respBody))
	}

	return nil
}

func answersToArrays(m map[string]string) [][]string {
	result := make([][]string, 0, len(m))
	for _, v := range m {
		result = append(result, []string{v})
	}
	return result
}

// ─── SessionConn Implementation ───────────────────────────────────────────────

type conn struct {
	userID       string
	sessionID    string
	httpAddr     string
	client       *http.Client
	recvCh       chan *events.Envelope
	log          *slog.Logger
	systemPrompt string

	mu        sync.Mutex
	closed    bool
	closeOnce sync.Once
}

func (c *conn) Send(ctx context.Context, msg *events.Envelope) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("opencodeserver: connection closed")
	}
	c.mu.Unlock()

	var content string
	if msg.Event.Data != nil {
		if data, ok := msg.Event.Data.(map[string]any); ok {
			if v, ok := data["content"].(string); ok {
				content = v
			}
		}
	}

	body := map[string]any{
		"parts": []map[string]any{{"type": "text", "text": content}},
	}
	if c.systemPrompt != "" {
		body["system"] = c.systemPrompt
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("opencodeserver: marshal input: %w", err)
	}

	url := fmt.Sprintf("%s/session/%s/message", c.httpAddr, c.sessionID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(payload)))
	if err != nil {
		return fmt.Errorf("opencodeserver: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("opencodeserver: send input: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("opencodeserver: input failed: status %d, body: %s",
			resp.StatusCode, string(respBody))
	}

	return nil
}

func (c *conn) Recv() <-chan *events.Envelope { return c.recvCh }

func (c *conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closeOnce.Do(func() {
		c.closed = true
		close(c.recvCh)
	})

	return nil
}

func (c *conn) UserID() string    { return c.userID }
func (c *conn) SessionID() string { return c.sessionID }

// ─── Init ────────────────────────────────────────────────────────────────────

func init() {
	worker.Register(worker.TypeOpenCodeSrv, func() (worker.Worker, error) {
		return New(), nil
	})
}
