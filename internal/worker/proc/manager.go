// Package proc implements process lifecycle management for worker runtimes.
package proc

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/hotplex/hotplex-worker/internal/security"
)

// Default output buffer limits for bufio.Scanner.
const (
	scannerInitSize = 64 * 1024        // 64 KB initial capacity
	scannerMaxSize  = 10 * 1024 * 1024 // 10 MB hard cap — scanner panics bufio.ErrTooLong beyond this
)

// Manager oversees the lifecycle of a single worker process.
type Manager struct {
	log *slog.Logger

	cmd    *exec.Cmd
	stdin  *os.File
	stdout *os.File
	stderr *os.File

	mu       sync.Mutex
	pgid     int
	started  bool
	exited   bool
	exitCode int

	// scanner reads stdout line-by-line with a 10MB per-line cap.
	// Created in Start(); safe to call ReadLine() concurrently from one goroutine.
	scanner      *bufio.Scanner
	outputLimit  int
	allowedTools []string
	pidKey       string
}

// Opts configures a process manager.
type Opts struct {
	Logger       *slog.Logger
	AllowedTools []string // tools allowed for this worker process
}

// New creates a new process manager.
func New(opts Opts) *Manager {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Manager{
		log:          opts.Logger,
		allowedTools: opts.AllowedTools,
	}
}

// Start launches a new process with the given command and arguments.
// It sets up a new process group (PGID) so that signals can be delivered
// to the entire subtree without affecting the gateway process.
func (m *Manager) Start(ctx context.Context, name string, args, env []string, dir string) (stdin, stdout, stderr *os.File, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil, nil, nil, fmt.Errorf("proc: already started")
	}

	// Append allowed-tools arguments if configured.
	if len(m.allowedTools) > 0 {
		toolsArgs := security.BuildAllowedToolsArgs(m.allowedTools)
		args = append(args, toolsArgs...)
	}

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = security.StripNestedAgent(env)

	// Ensure work dir exists; create if missing.
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, nil, nil, fmt.Errorf("proc: mkdir workdir %s: %w", dir, err)
		}
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // create new process group
	}

	// Create pipes for stdio.
	// os.Pipe returns (r, w, err) where r=read-end, w=write-end.
	// Parent reads from r, writes to w. Subprocess reads from r, writes to w.
	var stdinR, stdoutW, stderrW *os.File
	if stdinR, m.stdin, err = os.Pipe(); err != nil {
		return nil, nil, nil, fmt.Errorf("proc: stdin pipe: %w", err)
	}
	if m.stdout, stdoutW, err = os.Pipe(); err != nil {
		_ = stdinR.Close()
		_ = m.stdin.Close()
		return nil, nil, nil, fmt.Errorf("proc: stdout pipe: %w", err)
	}
	if m.stderr, stderrW, err = os.Pipe(); err != nil {
		_ = stdinR.Close()
		_ = m.stdin.Close()
		_ = m.stdout.Close()
		return nil, nil, nil, fmt.Errorf("proc: stderr pipe: %w", err)
	}

	cmd.Stdin = stdinR
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW

	if err := cmd.Start(); err != nil {
		_ = stdinR.Close()
		_ = stdoutW.Close()
		_ = stderrW.Close()
		_ = m.stdin.Close()
		_ = m.stdout.Close()
		_ = m.stderr.Close()
		return nil, nil, nil, fmt.Errorf("proc: start %s: %w", name, err)
	}

	// Close parent's ends of subprocess stdin/stdout/stderr - subprocess inherited copies.
	_ = stdinR.Close()
	_ = stdoutW.Close()
	_ = stderrW.Close()

	m.cmd = cmd
	m.started = true

	// Record PGID.
	if cmd.Process != nil {
		m.pgid = cmd.Process.Pid
	}

	// Write PID file if tracker is configured.
	m.trackPID()

	// Limit process virtual address space (RLIMIT_AS) to 512 MB.
	// This prevents runaway worker processes from exhausting the gateway's memory.
	// P3: RLIMIT_AS is not reliably supported on macOS — skip silently.
	if runtime.GOOS != "darwin" && cmd.Process != nil {
		const memLimit = 512 * 1024 * 1024 // 512 MB
		if err := syscall.Setrlimit(syscall.RLIMIT_AS, &syscall.Rlimit{
			Cur: memLimit,
			Max: memLimit,
		}); err != nil {
			m.log.Warn("proc: setrlimit RLIMIT_AS failed", "err", err)
			// Non-fatal: log and continue.
		}
	}

	// Set up bufio.Scanner for line-by-line stdout parsing.
	// Initial buffer 64 KB; hard cap 10 MB per line (AEP-008).
	buf := make([]byte, scannerInitSize)
	m.scanner = bufio.NewScanner(m.stdout)
	m.scanner.Buffer(buf, scannerMaxSize)
	m.scanner.Split(bufio.ScanLines)
	m.outputLimit = scannerMaxSize

	m.log.Info("proc: started",
		"pid", cmd.Process.Pid,
		"pgid", m.pgid,
		"dir", dir,
	)

	// Drain stderr in background.
	go m.drainStderr()

	return m.stdin, m.stdout, m.stderr, nil
}

// Terminate sends SIGTERM to the process group and waits for graceful shutdown.
// After the grace period, it escalates to SIGKILL.
func (m *Manager) Terminate(ctx context.Context, sig syscall.Signal, gracePeriod time.Duration) error {
	m.mu.Lock()
	if !m.started || m.exited {
		m.mu.Unlock()
		return nil
	}
	pgid := m.pgid
	pidKey := m.pidKey
	m.mu.Unlock()

	// Send signal to the entire process group.
	if pgid > 0 {
		_ = syscall.Kill(-pgid, sig)
		m.log.Info("proc: sent SIGTERM", "pgid", pgid)
	}

	// Wait for exit with deadline.
	done := make(chan struct{})
	go func() {
		_ = m.cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		m.captureExitCode()
		m.untrackPID(pidKey)
		return nil
	case <-time.After(gracePeriod):
		m.log.Warn("proc: graceful shutdown timeout, sending SIGKILL", "pgid", pgid)
		return m.Kill()
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Kill sends SIGKILL to the entire process group.
func (m *Manager) Kill() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started || m.exited {
		return nil
	}

	if m.pgid > 0 {
		_ = syscall.Kill(-m.pgid, syscall.SIGKILL)
		m.log.Info("proc: sent SIGKILL", "pgid", m.pgid)
	}
	_ = m.cmd.Wait()
	m.captureExitCodeLocked()
	m.untrackPID(m.pidKey)
	return nil
}

// Wait waits for the process to exit and returns the exit code.
func (m *Manager) Wait() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return -1, fmt.Errorf("proc: not started")
	}

	err := m.cmd.Wait()
	m.captureExitCodeLocked()
	return m.exitCode, err
}

// PID returns the process ID, or -1 if not started.
func (m *Manager) PID() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd != nil && m.cmd.Process != nil {
		return m.cmd.Process.Pid
	}
	return -1
}

// PGID returns the process group ID, or -1 if not started.
func (m *Manager) PGID() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pgid
}

// SetPIDKey sets the PID file tracking key for this process.
func (m *Manager) SetPIDKey(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pidKey = key
}

// trackPID writes the current PGID to the tracker. Must be called after Start()
// when m.pgid is set. Safe to call even if tracker is nil.
func (m *Manager) trackPID() {
	if t := GlobalTracker(); t != nil && m.pidKey != "" {
		if err := t.Write(m.pidKey, m.pgid); err != nil {
			m.log.Warn("proc: pidfile write", "key", m.pidKey, "err", err)
		}
	}
}

// untrackPID removes the PID file for key. Safe to call even if tracker is nil.
func (m *Manager) untrackPID(key string) {
	if t := GlobalTracker(); t != nil && key != "" {
		_ = t.Remove(key)
	}
}

// IsRunning returns true if the process has been started and has not exited.
func (m *Manager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.started && !m.exited
}

func (m *Manager) captureExitCode() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.captureExitCodeLocked()
}

func (m *Manager) captureExitCodeLocked() {
	if m.cmd == nil || m.cmd.ProcessState == nil {
		return
	}
	m.exited = true
	if ws, ok := m.cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
		m.exitCode = ws.ExitStatus()
		if ws.Signaled() {
			m.log.Info("proc: exited", "exit_code", m.exitCode, "signal", ws.Signal(), "signaled", true)
		} else {
			m.log.Info("proc: exited", "exit_code", m.exitCode)
		}
	} else {
		m.exitCode = -1
		m.log.Info("proc: exited", "exit_code", m.exitCode, "note", "non-POSIX wait status")
	}
}

// ReadLine reads the next line from the worker process stdout.
// It returns ("", io.EOF) when the scanner reaches end-of-file, or
// ("", ErrCodeWorkerOutputLimit) when a line exceeds the 10 MB buffer limit.
//
// ReadLine is NOT safe for concurrent calls; callers must serialize access
// (typically a single read goroutine per session). It does NOT hold m.mu
// during the blocking Scan call to avoid stalling Terminate/Kill.
func (m *Manager) ReadLine() (string, error) {
	// Fast path: check if scanner is available without locking.
	m.mu.Lock()
	scanner := m.scanner
	m.mu.Unlock()

	if scanner == nil {
		return "", io.EOF
	}

	// Defer a panic-recover to catch bufio.ErrTooLong (scanner panics when
	// a token exceeds the configured max buffer size).
	var line string
	var scanErr error

	func() {
		defer func() {
			if p := recover(); p != nil {
				if e, ok := p.(error); ok && errors.Is(e, bufio.ErrTooLong) {
					scanErr = fmt.Errorf("worker output limit exceeded (10 MB line)")
				} else {
					panic(p)
				}
			}
		}()
		if !scanner.Scan() {
			scanErr = scanner.Err()
			if scanErr == nil {
				scanErr = io.EOF
			}
			return
		}
		line = scanner.Text()
	}()

	if scanErr != nil {
		return "", scanErr
	}
	return line, nil
}

// drainStderr drains the stderr pipe in the background.
func (m *Manager) drainStderr() {
	buf := make([]byte, 4096)
	for {
		n, err := m.stderr.Read(buf)
		if n > 0 {
			m.log.Info("proc: stderr", "msg", string(buf[:n]))
		}
		if err != nil {
			break
		}
	}
}

// Close releases all pipe file descriptors.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []error
	if m.stdin != nil {
		if err := m.stdin.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if m.stdout != nil {
		if err := m.stdout.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if m.stderr != nil {
		if err := m.stderr.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("proc: close: %v", errs)
	}
	return nil
}
