// Package proc implements process lifecycle management for worker runtimes.
package proc

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
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
}

// Opts configures a process manager.
type Opts struct {
	Logger *slog.Logger
}

// New creates a new process manager.
func New(opts Opts) *Manager {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Manager{
		log: opts.Logger,
	}
}

// Start launches a new process with the given command and arguments.
// It sets up a new process group (PGID) so that signals can be delivered
// to the entire subtree without affecting the gateway process.
func (m *Manager) Start(ctx context.Context, name string, args []string, env []string, dir string) (stdin, stdout, stderr *os.File, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.started {
		return nil, nil, nil, fmt.Errorf("proc: already started")
	}

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // create new process group
	}

	// Create pipes for stdio.
	// os.Pipe returns (r, w, err) where r=read-end, w=write-end.
	var stdinR, stdoutW, stderrW *os.File
	if stdinR, m.stdin, err = os.Pipe(); err != nil {
		return nil, nil, nil, fmt.Errorf("proc: stdin pipe: %w", err)
	}
	if m.stdout, stdoutW, err = os.Pipe(); err != nil {
		stdinR.Close()
		m.stdin.Close()
		return nil, nil, nil, fmt.Errorf("proc: stdout pipe: %w", err)
	}
	if m.stderr, stderrW, err = os.Pipe(); err != nil {
		stdinR.Close()
		m.stdin.Close()
		m.stdout.Close()
		return nil, nil, nil, fmt.Errorf("proc: stderr pipe: %w", err)
	}

	// stdinR, stdoutW, stderrW are only needed for SetStdout/SetStderr; close unused ends.
	stdinR.Close()
	stdoutW.Close()
	stderrW.Close()

	cmd.Stdin = m.stdin
	cmd.Stdout = m.stdout
	cmd.Stderr = m.stderr

	if err := cmd.Start(); err != nil {
		m.stdin.Close()
		m.stdout.Close()
		m.stderr.Close()
		return nil, nil, nil, fmt.Errorf("proc: start %s: %w", name, err)
	}

	m.cmd = cmd
	m.started = true

	// Record PGID.
	if cmd.Process != nil {
		m.pgid = cmd.Process.Pid
	}

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
	m.mu.Unlock()

	// Send signal to the entire process group.
	if pgid > 0 {
		_ = syscall.Kill(-pgid, sig)
		m.log.Info("proc: sent SIGTERM", "pgid", pgid)
	}

	// Wait for exit with deadline.
	done := make(chan struct{})
	go func() {
		m.cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
		m.captureExitCode()
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

	m.cmd.Wait()
	m.captureExitCodeLocked()
	return nil
}

// Wait waits for the process to exit and returns the exit code.
func (m *Manager) Wait() (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.started {
		return -1, fmt.Errorf("proc: not started")
	}

	m.cmd.Wait()
	m.captureExitCodeLocked()
	return m.exitCode, nil
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
	} else {
		m.exitCode = -1
	}
	m.log.Info("proc: exited", "exit_code", m.exitCode)
}

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
