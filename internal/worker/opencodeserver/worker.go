// Package opencodeserver implements the OpenCode Server worker adapter.
//
// OpenCode Server runs as a persistent HTTP server process (opencode serve) that
// manages multiple sessions. Unlike CLI-based workers that use stdio, this adapter
// communicates via HTTP REST API for commands and Server-Sent Events (SSE) for
// streaming responses.
//
// # Architecture Overview
//
//	Gateway (main process)
//	    ↓ starts subprocess
//	OpenCode Server Worker (this adapter)
//	    ↓ manages lifecycle
//	OpenCode Server Process (independent HTTP server on port 18789)
//	    ↕ HTTP POST /sessions + GET /events (SSE)
//	Worker ↔ Server communication
//
// # Key Features
//
//   - Resume Support: Can reconnect to existing server sessions
//   - Multi-session: Server process handles multiple sessions concurrently
//   - SSE Streaming: Real-time event stream via text/event-stream
//   - Process Isolation: PGID-based process group for clean termination
//
// # Protocol
//
// # AEP v1 (Agent Event Protocol) over NDJSON
//
// See docs/specs/Worker-OpenCode-Server-Spec.md for full specification.
package opencodeserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hotplex/hotplex-worker/internal/worker"
	"github.com/hotplex/hotplex-worker/internal/worker/base"
	"github.com/hotplex/hotplex-worker/internal/worker/proc"
	"github.com/hotplex/hotplex-worker/pkg/aep"
	"github.com/hotplex/hotplex-worker/pkg/events"
)

// Compile-time interface compliance checks.
var (
	_ worker.Worker      = (*Worker)(nil)
	_ worker.SessionConn = (*conn)(nil)
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
	// defaultServePort is the default port for opencode serve HTTP server.
	// Port 18789 is chosen to avoid conflicts with common development ports.
	defaultServePort = 18789

	// recvChannelSize is the buffer size for SSE event channel.
	// This provides backpressure handling: when full, new events are silently dropped
	// to prevent blocking the SSE reader goroutine.
	recvChannelSize = 256

	// serverReadyTimeout is the maximum time to wait for server startup.
	// OpenCode Server typically starts within 1-2 seconds.
	serverReadyTimeout = 10 * time.Second

	// serverReadyPollInterval is the interval between health check polls.
	serverReadyPollInterval = 100 * time.Millisecond

	// httpClientTimeout is the timeout for HTTP client operations.
	httpClientTimeout = 30 * time.Second
)

// Worker implements the OpenCode Server worker adapter.
//
// # Design Philosophy
//
// Unlike CLI-based workers that communicate via stdin/stdout, this adapter manages
// a persistent HTTP server process (opencode serve). The server runs independently
// and can handle multiple sessions concurrently.
//
// # Lifecycle
//
//  1. Start() launches `opencode serve` process
//  2. waitForServer() polls /health until ready
//  3. createSession() creates a new session via HTTP API
//  4. readSSE() goroutine subscribes to SSE event stream
//  5. Input() sends user messages via HTTP POST
//  6. Terminate() gracefully shuts down (SIGTERM → 5s → SIGKILL)
//
// # Concurrency Model
//
//   - Single owner: Worker is owned by one session.Manager
//   - Thread-safe: All public methods are safe for concurrent use
//   - Goroutines: readSSE runs in separate goroutine, writes to recvCh
//   - Backpressure: recvCh has 256 buffer, drops messages when full
//
// # Memory Safety
//
//   - No shared mutable state between goroutines except httpConn
//   - httpConn protected by embedded BaseWorker.Mu
//   - SSE reader goroutine exits when context cancelled or connection closes
type Worker struct {
	*base.BaseWorker

	httpConn *conn
	port     int
	httpAddr string
	client   *http.Client

	// workerSessionID atomically stores the worker-internal session ID.
	// This serves as a fallback when httpConn is not yet initialized,
	// ensuring SetWorkerSessionID is never a silent no-op.
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
//
// The worker is initialized but not started. Call Start() to launch the server.
func New() *Worker {
	return &Worker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
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

// Type returns the worker type identifier.
func (w *Worker) Type() worker.WorkerType { return worker.TypeOpenCodeSrv }

// SupportsResume indicates that OpenCode Server supports session resumption.
// The server process stays alive between sessions, allowing reconnection.
func (w *Worker) SupportsResume() bool { return true }

// SupportsStreaming indicates that this worker supports real-time streaming via SSE.
func (w *Worker) SupportsStreaming() bool { return true }

// SupportsTools indicates that this worker can execute tool commands.
func (w *Worker) SupportsTools() bool { return true }

// EnvWhitelist returns the list of environment variables allowed to pass through.
func (w *Worker) EnvWhitelist() []string { return openCodeSrvEnvWhitelist }

// SessionStoreDir returns empty string as server doesn't use local session storage.
// Session state is managed in-memory by the server process.
func (w *Worker) SessionStoreDir() string { return "" }

// MaxTurns returns 0 (unlimited) as turn limits are managed by the server.
func (w *Worker) MaxTurns() int { return 0 }

// Modalities returns the supported content modalities.
func (w *Worker) Modalities() []string { return []string{"text", "code"} }

// ─── Worker Lifecycle ─────────────────────────────────────────────────────────

// Start launches the OpenCode Server process and creates a new session.
//
// # Startup Sequence
//
//  1. Start `opencode serve --port 18789` subprocess with PGID isolation
//  2. Poll /health endpoint until server responds (timeout: 10s)
//  3. POST /sessions to create new session → get session_id
//  4. Initialize HTTP connection with 256-buffer recvCh for backpressure
//  5. Launch readSSE goroutine to subscribe to event stream
//
// # Error Handling
//
//   - If server fails to start: kill process, clean up, return error
//   - If health check times out: kill process, clean up, return error
//   - If session creation fails: kill process, clean up, return error
//
// # Process Isolation
//
// Uses PGID (Process Group ID) for clean termination.
// Ensures all child processes are terminated on shutdown.
// See internal/worker/proc for termination protocol (SIGTERM → 5s → SIGKILL).
//
// # Concurrency
//
//   - Sets httpConn under lock to prevent race with Conn()
//   - readSSE goroutine reads SSE and writes to recvCh
//   - Backpressure: recvCh buffer 256, drops messages when full (non-blocking send)
func (w *Worker) Start(ctx context.Context, session worker.SessionInfo) error {
	// Early validation: prevent double-start
	w.Mu.Lock()
	if w.Proc != nil {
		w.Mu.Unlock()
		return fmt.Errorf("opencodeserver: already started")
	}
	w.Mu.Unlock()

	// Launch opencode serve subprocess
	if err := w.startServerProcess(ctx, session); err != nil {
		return err
	}

	// Create new session via HTTP API
	sessionID, err := w.createSession(ctx, session.ProjectDir)
	if err != nil {
		w.terminateProcess()
		return fmt.Errorf("opencodeserver: create session: %w", err)
	}

	// Initialize HTTP connection with buffered channel for backpressure
	w.initHTTPConn(session.UserID, sessionID)

	// Record startup time
	w.Mu.Lock()
	w.StartTime = time.Now()
	w.SetLastIO(w.StartTime)
	w.Mu.Unlock()

	// Start SSE reader goroutine (reads from server, writes to recvCh)
	go w.readSSE(sessionID)

	return nil
}

// Input sends a user message to the OpenCode server.
//
// This method constructs an AEP envelope containing the input data and sends it
// via the HTTP connection. The server processes the input asynchronously and
// sends responses via the SSE event stream.
//
// # Thread Safety
//
// Safe to call concurrently. Takes lock to access httpConn.
func (w *Worker) Input(ctx context.Context, content string, metadata map[string]any) error {
	w.Mu.Lock()
	conn := w.httpConn
	w.Mu.Unlock()

	if conn == nil {
		return fmt.Errorf("opencodeserver: worker not started")
	}

	// Route interaction responses to OpenCode REST API endpoints.
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

	// Construct AEP envelope with input event
	msg := events.NewEnvelope(
		aep.NewID(),
		conn.sessionID,
		0, // seq will be set by gateway
		events.Input,
		events.InputData{
			Content:  content,
			Metadata: metadata,
		},
	)

	// Send via HTTP connection (POST /sessions/{id}/input)
	if err := conn.Send(ctx, msg); err != nil {
		return fmt.Errorf("opencodeserver: send input: %w", err)
	}

	// Update last I/O timestamp for health monitoring
	w.SetLastIO(time.Now())

	return nil
}

// Resume reconnects to an existing session on the OpenCode server.
//
// Unlike CLI workers that cannot resume, OpenCode Server maintains session state
// in-memory, allowing reconnection if the gateway restarts.
//
// # Resume Sequence
//
//  1. Start `opencode serve` process (same as Start)
//  2. Wait for server health check
//  3. Reuse existing sessionID (from session parameter)
//  4. Reconnect to SSE event stream
//
// # Limitations
//
//   - Server must have session in memory (restart loses sessions)
//   - Project directory must match original session
//   - No session migration between server instances
//
// # Thread Safety
//
// Safe to call concurrently. Takes lock to prevent double-resume.
func (w *Worker) Resume(ctx context.Context, session worker.SessionInfo) error {
	// Early validation: prevent double-resume
	w.Mu.Lock()
	if w.Proc != nil {
		w.Mu.Unlock()
		return fmt.Errorf("opencodeserver: already started")
	}
	w.Mu.Unlock()

	// Start server process (same as Start)
	if err := w.startServerProcess(ctx, session); err != nil {
		return err
	}

	// Reuse existing session ID (session.SessionID provided by caller)
	w.initHTTPConn(session.UserID, session.SessionID)

	// Record resume time
	w.Mu.Lock()
	w.StartTime = time.Now()
	w.SetLastIO(w.StartTime)
	w.Mu.Unlock()

	// Reconnect to SSE stream for existing session
	go w.readSSE(session.SessionID)

	return nil
}

// Conn returns the HTTP-based session connection for sending/receiving events.
//
// # Thread Safety
//
// Safe to call concurrently. Takes lock to access httpConn.
// Returns nil if worker hasn't started yet.
func (w *Worker) Conn() worker.SessionConn {
	w.Mu.Lock()
	defer w.Mu.Unlock()
	return w.httpConn
}

// Health returns a snapshot of the worker's runtime health.
//
// Extends base.Health with HTTP-connection-specific information (session ID).
//
// # Thread Safety
//
// Safe to call concurrently.
func (w *Worker) Health() worker.WorkerHealth {
	health := w.BaseWorker.Health(worker.TypeOpenCodeSrv)

	w.Mu.Lock()
	if w.httpConn != nil {
		health.SessionID = w.httpConn.sessionID
	}
	w.Mu.Unlock()

	return health
}

// LastIO returns the time of last I/O activity, or zero time if never started.
//
// LastIO is updated on:
//   - Successful input send
//   - SSE event received (even if dropped due to backpressure)
//
// # Thread Safety
//
// Safe to call concurrently.
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
// OpenCode Server supports in-place context clearing, so we send a reset request
// without terminating the server process. The Gateway layer has already called
// sm.ClearContext() to clear SessionInfo.Context.
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

// ─── Internal Methods ─────────────────────────────────────────────────────────

// startServerProcess launches the opencode serve subprocess.
func (w *Worker) startServerProcess(ctx context.Context, session worker.SessionInfo) error {
	// Build command arguments
	args := []string{
		"serve",
		"--port", fmt.Sprintf("%d", defaultServePort),
		"--dangerously-skip-permissions",
	}

	// Build environment using shared base.BuildEnv
	env := base.BuildEnv(session, openCodeSrvEnvWhitelist, "opencode-server")

	// Create process manager
	w.Proc = proc.New(proc.Opts{
		Logger:       w.Log,
		AllowedTools: session.AllowedTools,
	})

	// Start the opencode serve process
	bgCtx := context.Background()
	_, _, _, err := w.Proc.Start(bgCtx, "opencode", args, env, session.ProjectDir)
	if err != nil {
		w.Mu.Lock()
		w.Proc = nil
		w.Mu.Unlock()
		return fmt.Errorf("opencodeserver: start process: %w", err)
	}

	// Configure server address
	w.port = defaultServePort
	w.httpAddr = fmt.Sprintf("http://localhost:%d", w.port)

	// Wait for server to be ready
	if err := w.waitForServer(ctx); err != nil {
		w.terminateProcess()
		return fmt.Errorf("opencodeserver: wait for server: %w", err)
	}

	return nil
}

// waitForServer polls the /health endpoint until the server is ready.
func (w *Worker) waitForServer(ctx context.Context) error {
	ticker := time.NewTicker(serverReadyPollInterval)
	defer ticker.Stop()

	timeout := time.After(serverReadyTimeout)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("timeout waiting for server after %v", serverReadyTimeout)
		case <-ticker.C:
			// Poll /health endpoint
			req, err := http.NewRequestWithContext(ctx, "GET", w.httpAddr+"/health", http.NoBody)
			if err != nil {
				continue // Retry on request creation error
			}

			resp, err := w.client.Do(req)
			if err != nil {
				continue // Retry on connection error
			}
			_ = resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				return nil // Server is ready
			}
		}
	}
}

// terminateProcess gracefully terminates the server process.
func (w *Worker) terminateProcess() {
	if w.Proc != nil {
		_ = w.Proc.Kill()
		w.Mu.Lock()
		w.Proc = nil
		w.Mu.Unlock()
	}
}

// createSession creates a new session via HTTP API.
func (w *Worker) createSession(ctx context.Context, projectDir string) (string, error) {
	reqBody := strings.NewReader(fmt.Sprintf(`{"project_dir": %q}`, projectDir))
	req, err := http.NewRequestWithContext(ctx, "POST", w.httpAddr+"/sessions", reqBody)
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
		SessionID string `json:"session_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	return result.SessionID, nil
}

// initHTTPConn creates and assigns the HTTP connection for a session.
func (w *Worker) initHTTPConn(userID, sessionID string) {
	w.httpConn = &conn{
		userID:    userID,
		sessionID: sessionID,
		httpAddr:  w.httpAddr,
		client:    w.client,
		recvCh:    make(chan *events.Envelope, recvChannelSize),
		log:       w.Log,
	}
}

// readSSE subscribes to the Server-Sent Events stream for a session.
//
// This method runs in a goroutine and reads SSE events from the server.
// Events are decoded from AEP format and forwarded to recvCh.
//
// # Backpressure Handling
//
// When recvCh is full (256 buffer), new events are silently dropped.
// This prevents blocking the SSE reader and allows the system to degrade
// gracefully under load.
//
// # Goroutine Lifecycle
//
//   - Started by Start() and Resume()
//   - Exits on: HTTP response error, non-200 status, EOF, or conn closed
//   - Does NOT close recvCh — conn.Close() is responsible for cleanup (uses sync.Once)
func (w *Worker) readSSE(sessionID string) {
	// Note: recvCh is closed by conn.Close(), not here, to avoid double-close.
	// conn.Close uses sync.Once for thread-safe idempotent cleanup.

	// Build SSE URL
	url := fmt.Sprintf("%s/events?session_id=%s", w.httpAddr, sessionID)
	req, err := http.NewRequest("GET", url, http.NoBody)
	if err != nil {
		w.Log.Error("opencodeserver: create SSE request", "error", err)
		return
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	// Connect to SSE endpoint
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

	// Read SSE stream line by line
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
			continue // Skip empty lines
		}

		// Parse SSE format: "data: {json}"
		if !strings.HasPrefix(line, "data: ") {
			continue // Skip non-data lines (e.g., event type, id)
		}
		data := strings.TrimPrefix(line, "data: ")

		// Decode AEP envelope
		env, err := aep.DecodeLine([]byte(data))
		if err != nil {
			// Not AEP — try parsing as OpenCode bus event
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
			w.Log.Warn("opencodeserver: decode SSE data",
				"error", err,
				"data", data)
			continue // Non-fatal: continue reading
		}

		// Ensure session ID is set
		env.SessionID = sessionID

		// Update last I/O timestamp (SetLastIO uses atomic, no lock needed)
		w.SetLastIO(time.Now())

		// Get current connection and check if closed (single lock acquisition)
		w.Mu.Lock()
		conn := w.httpConn
		closed := conn == nil
		w.Mu.Unlock()

		if closed {
			w.Log.Debug("opencodeserver: connection closed, stopping SSE reader")
			return
		}

		// Forward event to recvCh with backpressure handling
		select {
		case conn.recvCh <- env:
			// Successfully sent
		default:
			// Channel full, drop message (backpressure)
			w.Log.Warn("opencodeserver: recv channel full, dropping message",
				"event_type", env.Event.Type,
				"event_id", env.ID)
		}
	}
}

// ─── OpenCode Bus Event Handlers ──────────────────────────────────────────────

// handlePermissionAsked converts a permission.asked bus event to an AEP
// PermissionRequest envelope and forwards it to the recv channel.
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
		},
	)
	w.trySend(env)
}

// handleQuestionAsked converts a question.asked bus event to an AEP
// QuestionRequest envelope and forwards it to the recv channel.
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

// httpPost sends a JSON POST request to the OpenCode server.
func (w *Worker) httpPost(ctx context.Context, path string, payload any) error {
	w.Mu.Lock()
	addr := w.httpAddr
	w.Mu.Unlock()

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("opencodeserver: marshal payload: %w", err)
	}

	url := addr + path
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(body)))
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

// answersToArrays converts a map[string]string to [][]string for OpenCode's
// question reply API which expects answer values only.
func answersToArrays(m map[string]string) [][]string {
	result := make([][]string, 0, len(m))
	for _, v := range m {
		result = append(result, []string{v})
	}
	return result
}

// ─── SessionConn Implementation ───────────────────────────────────────────────

// conn implements the worker.SessionConn interface for HTTP-based communication.
type conn struct {
	userID    string
	sessionID string
	httpAddr  string
	client    *http.Client
	recvCh    chan *events.Envelope // Buffered channel for SSE events
	log       *slog.Logger
	mu        sync.Mutex
	closed    bool
	closeOnce sync.Once // Prevent double-close panic on recvCh
}

// Send posts an input message to the OpenCode server.
//
// # Thread Safety
//
// Safe to call concurrently. Returns error if connection is closed.
func (c *conn) Send(ctx context.Context, msg *events.Envelope) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("opencodeserver: connection closed")
	}
	c.mu.Unlock()

	// Extract input data from envelope
	inputData := events.InputData{}
	if msg.Event.Data != nil {
		if data, ok := msg.Event.Data.(map[string]any); ok {
			if content, ok := data["content"].(string); ok {
				inputData.Content = content
			}
			if metadata, ok := data["metadata"].(map[string]any); ok {
				inputData.Metadata = metadata
			}
		}
	}

	// Marshal request body
	body, err := json.Marshal(inputData)
	if err != nil {
		return fmt.Errorf("opencodeserver: marshal input: %w", err)
	}

	// Create HTTP request
	url := fmt.Sprintf("%s/sessions/%s/input", c.httpAddr, c.sessionID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("opencodeserver: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// Send request
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("opencodeserver: send input: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Check response status
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("opencodeserver: input failed: status %d, body: %s",
			resp.StatusCode, string(respBody))
	}

	return nil
}

// Recv returns the receive channel for SSE events.
//
// The channel is buffered (256). When full, new events are silently dropped.
// The channel is closed when the connection is terminated or SSE stream ends.
func (c *conn) Recv() <-chan *events.Envelope {
	return c.recvCh
}

// Close closes the connection and releases resources.
//
// Uses sync.Once to ensure idempotent behavior — safe to call multiple times.
//
// # Thread Safety
//
// Safe to call concurrently from multiple goroutines.
func (c *conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.closeOnce.Do(func() {
		c.closed = true
		close(c.recvCh) // Safe: sync.Once ensures single close
	})

	return nil
}

// UserID returns the user ID associated with this connection.
func (c *conn) UserID() string { return c.userID }

// SessionID returns the session ID associated with this connection.
func (c *conn) SessionID() string { return c.sessionID }

// ─── Init ────────────────────────────────────────────────────────────────────

func init() {
	// Register worker factory for OpenCode Server
	worker.Register(worker.TypeOpenCodeSrv, func() (worker.Worker, error) {
		return &Worker{
			BaseWorker: base.NewBaseWorker(slog.Default(), nil),
			client:     newHTTPClient(),
		}, nil
	})
}
