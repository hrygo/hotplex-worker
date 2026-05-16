package opencodeserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
	"github.com/hrygo/hotplex/internal/worker/proc"
	"github.com/hrygo/hotplex/pkg/events"
)

// SingletonProcessManager manages a single shared `opencode serve` process
// across all OpenCode Server sessions. The process is lazily started on first
// Acquire and shut down when the last session releases its reference.
//
// # Lifecycle
//
//	idle → starting → running → (crash → restarting → running) → stopped
//
// # Concurrency
//
// All methods are safe for concurrent use. Acquire serializes process startup
// via mutex so only the first caller starts the process.
type SingletonProcessManager struct {
	log       *slog.Logger
	client    *http.Client // with Timeout for API calls
	sseClient *http.Client // without Timeout for long-lived SSE streams
	cfg       config.OpenCodeServerConfig

	mu       sync.Mutex
	proc     *proc.Manager
	httpAddr string
	refs     int
	state    singletonState
	crashCh  chan struct{} // closed when process exits unexpectedly

	// EventBus dispatches events from the global SSE stream to individual sessions.
	busMu       sync.RWMutex
	subscribers map[string]chan *events.Envelope
	sseCancel   context.CancelFunc

	// Converter maps OCS BusEvents to AEP envelopes.
	converter *Converter

	idleTimer *time.Timer
}

type singletonState int

const (
	stateIdle     singletonState = iota // no process
	stateStarting                       // process launching, waiting for health
	stateRunning                        // process serving requests
	stateStopped                        // gateway shutdown
)

// portRegex matches "opencode server listening on http://127.0.0.1:PORT".
var portRegex = regexp.MustCompile(`listening on http://[\d.]+:(\d+)`)

// NewSingletonProcessManager creates a new singleton process manager.
func NewSingletonProcessManager(log *slog.Logger, cfg config.OpenCodeServerConfig) *SingletonProcessManager {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	}
	return &SingletonProcessManager{
		log:         log.With("component", "opencode-server-singleton"),
		client:      &http.Client{Timeout: cfg.HTTPTimeout, Transport: transport},
		sseClient:   &http.Client{Transport: transport}, // no Timeout for SSE
		cfg:         cfg,
		crashCh:     make(chan struct{}),
		subscribers: make(map[string]chan *events.Envelope),
		converter:   NewConverter(),
	}
}

// Acquire increments the reference count and starts the process if needed.
// Returns the server HTTP address, HTTP client for API calls, HTTP client for SSE (no timeout),
// and a crash notification channel.
// The crash channel is closed when the process exits unexpectedly; workers should
// check it in their Wait() implementation to report the correct exit code.
func (s *SingletonProcessManager) Acquire(ctx context.Context) (httpAddr string, client, sseClient *http.Client, crashCh <-chan struct{}, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state == stateStopped {
		return "", nil, nil, nil, fmt.Errorf("opencode-server-singleton: stopped")
	}

	// Cancel idle drain timer if one is pending.
	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}

	// Start process on first reference.
	if s.state == stateIdle {
		if err := s.startProcessLocked(ctx); err != nil {
			return "", nil, nil, nil, err
		}
	}

	if s.state != stateRunning {
		return "", nil, nil, nil, fmt.Errorf("opencode-server-singleton: unexpected state %d", s.state)
	}

	s.refs++
	s.log.Debug("opencode-server-singleton: acquire", "refs", s.refs)
	return s.httpAddr, s.client, s.sseClient, s.crashCh, nil
}

// Release decrements the reference count. When refs reach zero, an idle drain
// timer starts. If no new Acquire arrives within idleDrainPeriod, the process
// is killed.
func (s *SingletonProcessManager) Release() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.refs <= 0 {
		s.log.Warn("opencode-server-singleton: release with no active refs")
		return
	}

	s.refs--
	s.log.Debug("opencode-server-singleton: release", "refs", s.refs)

	if s.refs == 0 && s.state == stateRunning {
		s.startIdleDrainLocked()
	}
}

// Subscribe returns a channel that receives AEP events for the given session ID.
func (s *SingletonProcessManager) Subscribe(sessionID string) chan *events.Envelope {
	s.busMu.Lock()
	defer s.busMu.Unlock()

	if ch, ok := s.subscribers[sessionID]; ok {
		return ch
	}

	ch := make(chan *events.Envelope, 256)
	s.subscribers[sessionID] = ch
	s.log.Debug("opencode-server-singleton: subscribed", "session_id", sessionID)
	return ch
}

// Unsubscribe removes the subscription for the given session ID.
func (s *SingletonProcessManager) Unsubscribe(sessionID string) {
	s.busMu.Lock()
	defer s.busMu.Unlock()

	if ch, ok := s.subscribers[sessionID]; ok {
		delete(s.subscribers, sessionID)
		close(ch)
		s.log.Debug("opencode-server-singleton: unsubscribed", "session_id", sessionID)
	}
}

// Shutdown forcefully terminates the process regardless of reference count.
func (s *SingletonProcessManager) Shutdown(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state = stateStopped

	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}

	if s.sseCancel != nil {
		s.sseCancel()
	}

	if s.proc != nil {
		s.log.Info("opencode-server-singleton: shutdown, killing process")
		_ = s.proc.Kill()
		s.proc = nil
		s.refs = 0
	}

	// Close all active subscriptions.
	s.busMu.Lock()
	for id, ch := range s.subscribers {
		close(ch)
		delete(s.subscribers, id)
	}
	s.busMu.Unlock()
}

// IsRunning reports whether the singleton process is currently running.
func (s *SingletonProcessManager) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state == stateRunning
}

// PID returns the process ID, or 0 if not running.
func (s *SingletonProcessManager) PID() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.proc == nil {
		return 0
	}
	// proc.Manager doesn't expose PID directly; report 0 for now.
	// Health checks use IsRunning() instead.
	return 0
}

// --- internal ---

// startProcessLocked starts the opencode serve process. Caller must hold s.mu.
func (s *SingletonProcessManager) startProcessLocked(ctx context.Context) error {
	s.state = stateStarting
	s.log.Info("opencode-server-singleton: starting opencode serve process")

	// Allocate an ephemeral port.
	port, err := s.allocatePort()
	if err != nil {
		s.state = stateIdle
		return fmt.Errorf("opencode-server-singleton: allocate port: %w", err)
	}

	args := []string{
		"serve",
		"--port", strconv.Itoa(port),
	}

	parts := strings.Fields(s.cfg.Command)
	if len(parts) == 0 {
		parts = []string{"opencode"}
	}
	binary := parts[0]
	fullArgs := make([]string, 0, len(parts)-1+len(args))
	fullArgs = append(fullArgs, parts[1:]...)
	fullArgs = append(fullArgs, args...)

	env := s.buildEnv()
	s.proc = proc.New(proc.Opts{Logger: s.log})

	stdin, stdout, _, err := s.proc.Start(context.Background(), binary, fullArgs, env, "")
	if err != nil {
		s.proc = nil
		s.state = stateIdle
		return fmt.Errorf("opencode-server-singleton: start process: %w", err)
	}
	_ = stdin

	// Discover actual port from stdout (opencode serve prints it).
	actualPort, err := s.discoverPort(stdout, s.cfg.ReadyTimeout)
	if err != nil {
		_ = s.proc.Kill()
		s.proc = nil
		s.state = stateIdle
		return fmt.Errorf("opencode-server-singleton: discover port: %w", err)
	}

	s.httpAddr = fmt.Sprintf("http://127.0.0.1:%d", actualPort)
	s.log.Info("opencode-server-singleton: process started", "addr", s.httpAddr)

	// Wait for /health endpoint.
	if err := s.waitForHealth(ctx); err != nil {
		_ = s.proc.Kill()
		s.proc = nil
		s.state = stateIdle
		return fmt.Errorf("opencode-server-singleton: health check: %w", err)
	}

	s.state = stateRunning

	// Monitor process exit in background.
	go s.monitorProcess()

	// Start global SSE reader for all sessions.
	sseCtx, sseCancel := context.WithCancel(context.Background())
	s.sseCancel = sseCancel
	go s.readGlobalSSE(sseCtx)

	return nil
}

// allocatePort gets an OS-assigned ephemeral port by briefly opening a listener.
func (s *SingletonProcessManager) allocatePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	addr, ok := l.Addr().(*net.TCPAddr)
	if !ok {
		_ = l.Close()
		return 0, fmt.Errorf("unexpected listener address type: %T", l.Addr())
	}
	_ = l.Close()
	return addr.Port, nil
}

// discoverPort reads stdout until finding the listening address line.
// Closes stdout after discovery since OCS communicates via HTTP, not stdout.
func (s *SingletonProcessManager) discoverPort(stdout *os.File, timeout time.Duration) (int, error) {
	type result struct {
		port int
		err  error
	}
	ch := make(chan result, 1)

	go func() {
		defer func() { _ = stdout.Close() }()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			s.log.Debug("opencode-server-singleton: stdout", "line", line)
			if m := portRegex.FindStringSubmatch(line); len(m) == 2 {
				p, err := strconv.Atoi(m[1])
				ch <- result{port: p, err: err}
				return
			}
		}
		if err := scanner.Err(); err != nil {
			ch <- result{err: fmt.Errorf("stdout read: %w", err)}
		} else {
			ch <- result{err: fmt.Errorf("stdout closed without port announcement")}
		}
	}()

	select {
	case r := <-ch:
		return r.port, r.err
	case <-time.After(timeout):
		return 0, fmt.Errorf("timeout discovering port")
	}
}

// waitForHealth polls the /health endpoint until the server is ready.
func (s *SingletonProcessManager) waitForHealth(ctx context.Context) error {
	ticker := time.NewTicker(s.cfg.ReadyPollInterval)
	defer ticker.Stop()

	timeout := time.After(s.cfg.ReadyTimeout)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("timeout waiting for server health after %v", s.cfg.ReadyTimeout)
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, "GET", s.httpAddr+"/health", http.NoBody)
			if err != nil {
				continue
			}
			resp, err := s.client.Do(req)
			if err != nil {
				continue
			}
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
	}
}

// monitorProcess waits for the process to exit and notifies subscribers.
func (s *SingletonProcessManager) monitorProcess() {
	code, _ := s.proc.Wait()

	s.mu.Lock()
	wasRunning := s.state == stateRunning
	refs := s.refs
	s.state = stateIdle
	s.proc = nil

	// Cancel the global SSE reader so it doesn't leak into the next lifecycle.
	if s.sseCancel != nil {
		s.sseCancel()
		s.sseCancel = nil
	}

	// Notify crash subscribers if process died unexpectedly while sessions are active.
	if wasRunning && refs > 0 {
		s.log.Warn("opencode-server-singleton: process crashed", "exit_code", code, "refs", refs)
		close(s.crashCh)
		s.crashCh = make(chan struct{}) // new channel for next lifecycle
	} else {
		s.log.Info("opencode-server-singleton: process exited", "exit_code", code, "refs", refs)
	}
	s.mu.Unlock()

	// Close all subscriber channels outside s.mu to avoid lock nesting with busMu.
	if wasRunning {
		s.busMu.Lock()
		for id, ch := range s.subscribers {
			close(ch)
			delete(s.subscribers, id)
		}
		s.busMu.Unlock()
	}
}

// startIdleDrainLocked starts a timer to kill the process when idle.
// Caller must hold s.mu.
func (s *SingletonProcessManager) startIdleDrainLocked() {
	s.log.Info("opencode-server-singleton: starting idle drain timer", "period", s.cfg.IdleDrainPeriod)
	s.idleTimer = time.AfterFunc(s.cfg.IdleDrainPeriod, func() {
		s.mu.Lock()
		defer s.mu.Unlock()

		if s.refs == 0 && s.state == stateRunning && s.proc != nil {
			s.log.Info("opencode-server-singleton: idle drain expired, killing process")
			_ = s.proc.Kill()
			// monitorProcess will set state=stateIdle and clean up.
		}
		s.idleTimer = nil
	})
}

// buildEnv creates the environment for the opencode serve process.
func (s *SingletonProcessManager) buildEnv() []string {
	env := base.BuildEnv(worker.SessionInfo{}, openCodeSrvEnvBlocklist, "opencode-server")
	env = append(env, "OPENCODE_EXPERIMENTAL_EVENT_SYSTEM=true")
	return env
}

// readGlobalSSE connects to the OCS global event stream and dispatches events to session channels.
func (s *SingletonProcessManager) readGlobalSSE(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error("opencode-server-singleton: readGlobalSSE panic", "panic", r, "stack", string(debug.Stack()))
		}
	}()

	s.mu.Lock()
	sseURL := s.httpAddr + "/global/event"
	s.mu.Unlock()

	var attempts int
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if attempts >= sseMaxReconnects {
			s.log.Error("opencode-server-singleton: SSE max reconnects exceeded", "attempts", attempts)
			return
		}

		req, err := http.NewRequestWithContext(ctx, "GET", sseURL, http.NoBody)
		if err != nil {
			s.log.Error("opencode-server-singleton: create SSE request", "err", err)
			return
		}
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Cache-Control", "no-cache")

		resp, err := s.sseClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			attempts++
			s.log.Warn("opencode-server-singleton: SSE connect error, reconnecting",
				"attempt", attempts, "err", err)
			s.sseBackoffSleep(ctx, attempts)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			_ = resp.Body.Close()
			attempts++
			s.log.Warn("opencode-server-singleton: SSE non-200 status, reconnecting",
				"status", resp.StatusCode, "attempt", attempts, "body", string(body))
			s.sseBackoffSleep(ctx, attempts)
			continue
		}

		s.log.Debug("opencode-server-singleton: global SSE connected", "url", sseURL)

		gotData := false
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				_ = resp.Body.Close()
				if ctx.Err() != nil {
					return
				}
				if errors.Is(err, io.EOF) {
					if gotData {
						attempts = 0
						s.log.Debug("opencode-server-singleton: global SSE stream ended, reconnecting")
					} else {
						attempts++
						s.log.Debug("opencode-server-singleton: global SSE empty stream, reconnecting with backoff",
							"attempt", attempts)
						s.sseBackoffSleep(ctx, attempts)
					}
					break
				}
				s.log.Warn("opencode-server-singleton: global SSE read error, reconnecting", "err", err)
				break
			}

			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			gotData = true
			attempts = 0
			data := strings.TrimPrefix(line, "data: ")

			// Parse and dispatch.
			s.dispatchOCSEvent([]byte(data))
		}
	}
}

// dispatchOCSEvent parses a raw OCS event and forwards the converted AEP envelopes
// to the appropriate session channel.
func (s *SingletonProcessManager) dispatchOCSEvent(data []byte) {
	var evt ocsGlobalEvent
	if err := json.Unmarshal(data, &evt); err != nil {
		return
	}

	if evt.Payload.Type == "sync" || evt.Payload.Type == "server.connected" ||
		evt.Payload.Type == "server.heartbeat" || evt.Payload.Type == "global.disposed" {
		return
	}

	// session.error may have no sessionID — route via directory or skip.
	var props struct {
		SessionID string `json:"sessionID"`
	}
	if err := json.Unmarshal(evt.Payload.Properties, &props); err != nil {
		return
	}
	sessionID := props.SessionID
	if sessionID == "" {
		// session.error can have optional sessionID — dispatch to all subscribers.
		if evt.Payload.Type == "session.error" {
			s.dispatchToAllSubscribers(evt.Payload.Properties)
		}
		return
	}

	// Delegate to converter.
	envs := s.converter.Convert(sessionID, evt.Payload.Type, evt.Payload.Properties)
	for _, env := range envs {
		s.sendToSubscriber(sessionID, env)
	}
}

// sendToSubscriber delivers a single envelope to the session's channel.
func (s *SingletonProcessManager) sendToSubscriber(sessionID string, env *events.Envelope) {
	s.busMu.RLock()
	ch, ok := s.subscribers[sessionID]
	if !ok {
		s.busMu.RUnlock()
		return
	}
	select {
	case ch <- env:
	default:
		s.log.Warn("opencode-server-singleton: session channel full, dropping event",
			"session_id", sessionID, "type", env.Event.Type)
	}
	s.busMu.RUnlock()
}

// dispatchToAllSubscribers sends session.error to every active subscriber.
func (s *SingletonProcessManager) dispatchToAllSubscribers(props json.RawMessage) {
	s.busMu.RLock()
	defer s.busMu.RUnlock()
	for sessionID := range s.subscribers {
		envs := s.converter.Convert(sessionID, "session.error", props)
		ch := s.subscribers[sessionID]
		for _, env := range envs {
			select {
			case ch <- env:
			default:
				s.log.Warn("opencode-server-singleton: session channel full, dropping error event",
					"session_id", sessionID)
			}
		}
	}
}

func (s *SingletonProcessManager) sseBackoffSleep(ctx context.Context, attempt int) {
	dur := min(sseBackoffInitial*time.Duration(1<<min(attempt, 5)), sseBackoffMax)
	select {
	case <-ctx.Done():
	case <-time.After(dur):
	}
}

type ocsGlobalEvent struct {
	Directory string `json:"directory"`
	Payload   struct {
		Type       string          `json:"type"`
		Properties json.RawMessage `json:"properties"`
	} `json:"payload"`
}

var (
	sseMaxReconnects  = 50
	sseBackoffInitial = 100 * time.Millisecond
	sseBackoffMax     = 10 * time.Second
)

// --- package-level singleton ---

var singleton atomic.Pointer[SingletonProcessManager]

// InitSingleton initializes the global singleton process manager.
// Must be called during gateway startup before any sessions are created.
func InitSingleton(log *slog.Logger, cfg config.OpenCodeServerConfig) {
	mgr := NewSingletonProcessManager(log, cfg)
	singleton.Store(mgr)
}

// ShutdownSingleton shuts down the global singleton process manager.
// Must be called during gateway shutdown after bridge.Shutdown().
func ShutdownSingleton(ctx context.Context) {
	if m := singleton.Load(); m != nil {
		m.Shutdown(ctx)
		singleton.Store((*SingletonProcessManager)(nil))
	}
}
