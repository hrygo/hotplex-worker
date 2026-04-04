package opencodeserver

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
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

// Env whitelist for OpenCode Server worker.
var openCodeSrvEnvWhitelist = []string{
	"HOME", "USER", "SHELL", "PATH", "TERM",
	"LANG", "LC_ALL", "PWD",
	"OPENAI_API_KEY", "OPENAI_BASE_URL",
	"OPENCODE_API_KEY", "OPENCODE_BASE_URL",
}

// Default port for opencode serve.
const defaultServePort = 18789

// Worker implements the OpenCode Server worker adapter.
type Worker struct {
	*base.BaseWorker // embedded shared lifecycle methods (Terminate/Kill/Wait/Health/LastIO)

	httpConn *conn // custom HTTP-based conn (NOT base.Conn)
	port     int
	httpAddr string
	client   *http.Client
}

// New creates a new OpenCode Server worker.
func New() *Worker {
	return &Worker{
		BaseWorker: base.NewBaseWorker(slog.Default(), nil),
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ─── Capabilities ─────────────────────────────────────────────────────────────

func (w *Worker) Type() worker.WorkerType { return worker.TypeOpenCodeSrv }

func (w *Worker) SupportsResume() bool    { return true } // process stays alive
func (w *Worker) SupportsStreaming() bool { return true }
func (w *Worker) SupportsTools() bool     { return true }
func (w *Worker) EnvWhitelist() []string  { return openCodeSrvEnvWhitelist }
func (w *Worker) SessionStoreDir() string { return "" }
func (w *Worker) MaxTurns() int           { return 0 }
func (w *Worker) Modalities() []string    { return []string{"text", "code"} }

// ─── Worker ─────────────────────────────────────────────────────────────────

func (w *Worker) Start(ctx context.Context, session worker.SessionInfo) error {
	w.Mu.Lock()
	if w.Proc != nil {
		w.Mu.Unlock()
		return fmt.Errorf("opencodeserver: already started")
	}
	w.Mu.Unlock()

	// Build command arguments: opencode serve (starts HTTP server)
	args := []string{
		"serve",
		"--port", fmt.Sprintf("%d", defaultServePort),
	}

	// Build environment using shared base.BuildEnv.
	env := base.BuildEnv(session, openCodeSrvEnvWhitelist, "opencode-server")

	// Create process manager.
	w.Proc = proc.New(proc.Opts{
		Logger:       w.Log,
		AllowedTools: session.AllowedTools,
	})

	// Start the opencode serve process.
	_, _, _, err := w.Proc.Start(ctx, "opencode", args, env, session.ProjectDir)
	if err != nil {
		w.Mu.Lock()
		w.Proc = nil
		w.Mu.Unlock()
		return fmt.Errorf("opencodeserver: start: %w", err)
	}

	// Wait for server to be ready.
	w.port = defaultServePort
	w.httpAddr = fmt.Sprintf("http://localhost:%d", w.port)

	if err := w.waitForServer(ctx); err != nil {
		_ = w.Proc.Kill()
		w.Mu.Lock()
		w.Proc = nil
		w.Mu.Unlock()
		return fmt.Errorf("opencodeserver: wait for server: %w", err)
	}

	// Create session via API.
	sessionID, err := w.createSession(ctx, session.ProjectDir)
	if err != nil {
		_ = w.Proc.Kill()
		w.Mu.Lock()
		w.Proc = nil
		w.Mu.Unlock()
		return fmt.Errorf("opencodeserver: create session: %w", err)
	}

	// Create session connection.
	w.httpConn = &conn{
		userID:    session.UserID,
		sessionID: sessionID,
		httpAddr:  w.httpAddr,
		client:    w.client,
		recvCh:    make(chan *events.Envelope, 256),
		log:       w.Log,
	}

	w.Mu.Lock()
	w.StartTime = time.Now()
	w.SetLastIO(w.StartTime)
	w.Mu.Unlock()

	// Start SSE reader goroutine.
	go w.readSSE(sessionID)

	return nil
}

func (w *Worker) Input(ctx context.Context, content string, metadata map[string]any) error {
	w.Mu.Lock()
	conn := w.httpConn
	w.Mu.Unlock()

	if conn == nil {
		return fmt.Errorf("opencodeserver: not started")
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
		return fmt.Errorf("opencodeserver: input: %w", err)
	}

	w.SetLastIO(time.Now())

	return nil
}

func (w *Worker) Resume(ctx context.Context, session worker.SessionInfo) error {
	w.Mu.Lock()
	if w.Proc != nil {
		w.Mu.Unlock()
		return fmt.Errorf("opencodeserver: already started")
	}
	w.Mu.Unlock()

	// Build command arguments.
	args := []string{
		"serve",
		"--port", fmt.Sprintf("%d", defaultServePort),
	}

	// Build environment using shared base.BuildEnv.
	env := base.BuildEnv(session, openCodeSrvEnvWhitelist, "opencode-server")

	// Create process manager.
	w.Proc = proc.New(proc.Opts{
		Logger:       w.Log,
		AllowedTools: session.AllowedTools,
	})

	// Start the process.
	_, _, _, err := w.Proc.Start(ctx, "opencode", args, env, session.ProjectDir)
	if err != nil {
		w.Mu.Lock()
		w.Proc = nil
		w.Mu.Unlock()
		return fmt.Errorf("opencodeserver: resume start: %w", err)
	}

	// Wait for server.
	w.port = defaultServePort
	w.httpAddr = fmt.Sprintf("http://localhost:%d", w.port)

	if err := w.waitForServer(ctx); err != nil {
		_ = w.Proc.Kill()
		w.Mu.Lock()
		w.Proc = nil
		w.Mu.Unlock()
		return fmt.Errorf("opencodeserver: wait for server: %w", err)
	}

	// Create session connection with existing session ID.
	w.httpConn = &conn{
		userID:    session.UserID,
		sessionID: session.SessionID,
		httpAddr:  w.httpAddr,
		client:    w.client,
		recvCh:    make(chan *events.Envelope, 256),
		log:       w.Log,
	}

	w.Mu.Lock()
	w.StartTime = time.Now()
	w.SetLastIO(w.StartTime)
	w.Mu.Unlock()

	// Start SSE reader goroutine.
	go w.readSSE(session.SessionID)

	return nil
}

// Terminate, Kill, Wait are provided by embedded BaseWorker.

// Conn returns the custom HTTP-based session connection.
func (w *Worker) Conn() worker.SessionConn {
	w.Mu.Lock()
	defer w.Mu.Unlock()
	return w.httpConn
}

// Health returns a snapshot of the worker's runtime health.
// Uses base.Health as foundation but adds HTTP conn sessionID.
func (w *Worker) Health() worker.WorkerHealth {
	health := w.BaseWorker.Health(worker.TypeOpenCodeSrv)

	w.Mu.Lock()
	if w.httpConn != nil {
		health.SessionID = w.httpConn.sessionID
	}
	w.Mu.Unlock()

	return health
}

// LastIO returns the time of last I/O, or zero time if never started.
func (w *Worker) LastIO() time.Time {
	w.Mu.Lock()
	started := w.httpConn != nil
	w.Mu.Unlock()
	if !started {
		return time.Time{}
	}
	return w.BaseWorker.LastIO()
}

// ─── Internal ────────────────────────────────────────────────────────────────

func (w *Worker) waitForServer(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	timeout := time.After(10 * time.Second)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("timeout waiting for server")
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, "GET", w.httpAddr+"/health", nil)
			if err != nil {
				continue
			}
			resp, err := w.client.Do(req)
			if err != nil {
				continue
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
	}
}

type createSessionResponse struct {
	SessionID string `json:"session_id"`
}

func (w *Worker) createSession(ctx context.Context, projectDir string) (string, error) {
	reqBody := strings.NewReader(fmt.Sprintf(`{"project_dir": %q}`, projectDir))
	req, err := http.NewRequestWithContext(ctx, "POST", w.httpAddr+"/sessions", reqBody)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := w.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("create session failed: %d %s", resp.StatusCode, string(body))
	}

	var result createSessionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	return result.SessionID, nil
}

func (w *Worker) readSSE(sessionID string) {
	defer func() {
		w.Mu.Lock()
		if w.httpConn != nil {
			close(w.httpConn.recvCh)
		}
		w.Mu.Unlock()
	}()

	url := fmt.Sprintf("%s/events?session_id=%s", w.httpAddr, sessionID)
	req, err := http.NewRequest("GET", url, nil)
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
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		w.Log.Error("opencodeserver: SSE status", "status", resp.StatusCode, "body", string(body))
		return
	}

	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return
			}
			w.Log.Error("opencodeserver: SSE read", "error", err)
			return
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// SSE format: "data: {json}"
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		data := strings.TrimPrefix(line, "data: ")

		env, err := aep.DecodeLine([]byte(data))
		if err != nil {
			w.Log.Warn("opencodeserver: decode SSE data", "error", err, "data", data)
			continue
		}

		env.SessionID = sessionID

		w.SetLastIO(time.Now())

		w.Mu.Lock()
		conn := w.httpConn
		w.Mu.Unlock()

		if conn == nil {
			return
		}

		select {
		case conn.recvCh <- env:
		default:
			w.Log.Warn("opencodeserver: recv channel full, dropping message")
		}
	}
}

// ─── SessionConn ─────────────────────────────────────────────────────────────

type conn struct {
	userID    string
	sessionID string
	httpAddr  string
	client    *http.Client
	recvCh    chan *events.Envelope
	log       *slog.Logger
	mu        sync.Mutex
	closed    bool
}

func (c *conn) Send(ctx context.Context, msg *events.Envelope) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("opencodeserver: connection closed")
	}
	c.mu.Unlock()

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

	body, err := json.Marshal(inputData)
	if err != nil {
		return fmt.Errorf("opencodeserver: marshal input: %w", err)
	}

	url := fmt.Sprintf("%s/sessions/%s/input", c.httpAddr, c.sessionID)
	req, err := http.NewRequestWithContext(ctx, "POST", url, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("opencodeserver: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("opencodeserver: send input: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("opencodeserver: input failed: %d %s", resp.StatusCode, string(respBody))
	}

	return nil
}

func (c *conn) Recv() <-chan *events.Envelope {
	return c.recvCh
}

func (c *conn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	close(c.recvCh)

	return nil
}

func (c *conn) UserID() string    { return c.userID }
func (c *conn) SessionID() string { return c.sessionID }

// ─── Init ────────────────────────────────────────────────────────────────────

func init() {
	worker.Register(worker.TypeOpenCodeSrv, func() (worker.Worker, error) {
		return &Worker{
			BaseWorker: base.NewBaseWorker(slog.Default(), nil),
			client: &http.Client{
				Timeout: 30 * time.Second,
			},
		}, nil
	})
}
