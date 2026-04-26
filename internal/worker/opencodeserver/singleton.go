package opencodeserver

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/base"
	"github.com/hrygo/hotplex/internal/worker/proc"
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
	log    *slog.Logger
	client *http.Client
	cfg    config.OpenCodeServerConfig

	mu       sync.Mutex
	proc     *proc.Manager
	httpAddr string
	refs     int
	state    singletonState
	crashCh  chan struct{} // closed when process exits unexpectedly

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
	return &SingletonProcessManager{
		log:     log.With("component", "opencode-server-singleton"),
		client:  &http.Client{Timeout: cfg.HTTPTimeout},
		cfg:     cfg,
		crashCh: make(chan struct{}),
	}
}

// Acquire increments the reference count and starts the process if needed.
// Returns the server HTTP address, HTTP client, and a crash notification channel.
// The crash channel is closed when the process exits unexpectedly; workers should
// check it in their Wait() implementation to report the correct exit code.
func (s *SingletonProcessManager) Acquire(ctx context.Context) (httpAddr string, client *http.Client, crashCh <-chan struct{}, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.state == stateStopped {
		return "", nil, nil, fmt.Errorf("opencode-server-singleton: stopped")
	}

	// Cancel idle drain timer if one is pending.
	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}

	// Start process on first reference.
	if s.state == stateIdle {
		if err := s.startProcessLocked(ctx); err != nil {
			return "", nil, nil, err
		}
	}

	if s.state != stateRunning {
		return "", nil, nil, fmt.Errorf("opencode-server-singleton: unexpected state %d", s.state)
	}

	s.refs++
	s.log.Debug("opencode-server-singleton: acquire", "refs", s.refs)
	return s.httpAddr, s.client, s.crashCh, nil
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

// Shutdown forcefully terminates the process regardless of reference count.
// Called during gateway shutdown after all workers have been stopped.
func (s *SingletonProcessManager) Shutdown(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state = stateStopped

	if s.idleTimer != nil {
		s.idleTimer.Stop()
		s.idleTimer = nil
	}

	if s.proc != nil {
		s.log.Info("opencode-server-singleton: shutdown, killing process")
		_ = s.proc.Kill()
		s.proc = nil
		s.refs = 0
	}
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

	env := s.buildEnv()

	s.proc = proc.New(proc.Opts{Logger: s.log})

	stdin, stdout, _, err := s.proc.Start(context.Background(), "opencode", args, env, "")
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
		scanner := bufio.NewScanner(stdout)
		deadline := time.After(timeout)
		for scanner.Scan() {
			line := scanner.Text()
			s.log.Debug("opencode-server-singleton: stdout", "line", line)
			if m := portRegex.FindStringSubmatch(line); len(m) == 2 {
				p, err := strconv.Atoi(m[1])
				ch <- result{port: p, err: err}
				return
			}
			select {
			case <-deadline:
				ch <- result{err: fmt.Errorf("timeout waiting for port in stdout")}
				return
			default:
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

	// Notify crash subscribers if process died unexpectedly while sessions are active.
	if wasRunning && refs > 0 {
		s.log.Warn("opencode-server-singleton: process crashed", "exit_code", code, "refs", refs)
		close(s.crashCh)
		s.crashCh = make(chan struct{}) // new channel for next lifecycle
	} else {
		s.log.Info("opencode-server-singleton: process exited", "exit_code", code, "refs", refs)
	}
	s.mu.Unlock()
}

// startIdleDrainLocked starts a timer to kill the process when idle.
// Caller must hold s.mu.
func (s *SingletonProcessManager) startIdleDrainLocked() {
	s.log.Info("opencode-server-singleton: starting idle drain timer", "period", s.cfg.IdleDrainPeriod)
	s.idleTimer = time.AfterFunc(s.cfg.IdleDrainPeriod, func() {
		s.mu.Lock()
		defer s.mu.Unlock()

		if s.refs == 0 && s.state == stateRunning {
			s.log.Info("opencode-server-singleton: idle drain expired, killing process")
			_ = s.proc.Kill()
			// monitorProcess will set state=stateIdle and clean up.
		}
		s.idleTimer = nil
	})
}

// buildEnv creates the environment for the opencode serve process.
func (s *SingletonProcessManager) buildEnv() []string {
	return base.BuildEnv(worker.SessionInfo{}, openCodeSrvEnvWhitelist, "opencode-server")
}

// --- package-level singleton ---

var defaultSingleton *SingletonProcessManager

// InitSingleton initializes the global singleton process manager.
// Must be called during gateway startup before any sessions are created.
func InitSingleton(log *slog.Logger, cfg config.OpenCodeServerConfig) {
	defaultSingleton = NewSingletonProcessManager(log, cfg)
}

// ShutdownSingleton shuts down the global singleton process manager.
// Must be called during gateway shutdown after bridge.Shutdown().
func ShutdownSingleton(ctx context.Context) {
	if defaultSingleton != nil {
		defaultSingleton.Shutdown(ctx)
	}
}
