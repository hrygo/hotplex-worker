// Package base provides shared infrastructure for CLI-based worker adapters.
package base

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/internal/worker/proc"
)

// Compile-time interface compliance checks.
var (
	_ worker.SessionConn = (*Conn)(nil)
)

// Grace period for graceful worker shutdown.
const gracefulShutdownTimeout = 5 * time.Second

// BaseWorker provides shared lifecycle methods for CLI-based worker adapters.
// Embed this struct to get Terminate/Kill/Wait/Health/LastIO/Conn for free.
type BaseWorker struct {
	Log *slog.Logger
	// Proc is the process manager. Subclasses should initialize via w.Proc = proc.New(opts).
	Proc      *proc.Manager
	Cfg       *config.Config
	Cmd       *exec.Cmd // Reserved for future use; currently Proc manages the command
	StartTime time.Time
	lastIO    atomic.Int64 // unix nano, use LastIO() and SetLastIO() accessors
	Mu        sync.Mutex
	conn      *Conn // stdin-based conn, nil for HTTP-based adapters

	// resetGen is a monotonic counter incremented before each deliberate
	// Terminate+Start cycle (e.g. session reset). forwardEvents captures the
	// value at goroutine start and checks after the recv channel closes: if the
	// current generation differs from its captured value, another reset happened
	// and the OLD forwardEvents should exit cleanly without crash handling.
	// This replaces the previous boolean flag which suffered from a race where
	// ResetSession reset the flag to false before OLD forwardEvents could check it.
	resetGen atomic.Int64
}

// NewBaseWorker creates a new BaseWorker with the given logger and config.
func NewBaseWorker(log *slog.Logger, cfg *config.Config) *BaseWorker {
	if log == nil {
		log = slog.Default()
	}
	return &BaseWorker{
		Log: log,
		Cfg: cfg,
	}
}

// Terminate gracefully stops the worker process: SIGTERM → 5s grace → SIGKILL.
func (w *BaseWorker) Terminate(ctx context.Context) error {
	w.Mu.Lock()
	proc := w.Proc
	w.Mu.Unlock()

	if proc == nil {
		return nil
	}

	if err := proc.Terminate(ctx, gracefulShutdownTimeout); err != nil {
		return fmt.Errorf("base: terminate: %w", err)
	}

	w.Mu.Lock()
	w.Proc = nil
	w.Mu.Unlock()

	return nil
}

// Kill immediately terminates the worker process with SIGKILL.
func (w *BaseWorker) Kill() error {
	w.Mu.Lock()
	proc := w.Proc
	w.Mu.Unlock()

	if proc == nil {
		return nil
	}

	if err := proc.Kill(); err != nil {
		return fmt.Errorf("base: kill: %w", err)
	}

	w.Mu.Lock()
	w.Proc = nil
	w.Mu.Unlock()

	return nil
}

// Wait blocks until the worker process exits, returning the exit code.
func (w *BaseWorker) Wait() (int, error) {
	w.Mu.Lock()
	proc := w.Proc
	w.Mu.Unlock()

	if proc == nil {
		return -1, fmt.Errorf("base: not started")
	}

	code, err := proc.Wait()
	if err != nil {
		return code, fmt.Errorf("base: wait: %w", err)
	}

	w.Mu.Lock()
	w.Proc = nil
	w.Mu.Unlock()

	return code, nil
}

// Health returns a snapshot of the worker's runtime health.
func (w *BaseWorker) Health(typ worker.WorkerType) worker.WorkerHealth {
	w.Mu.Lock()
	defer w.Mu.Unlock()

	health := worker.WorkerHealth{
		Type:      typ,
		SessionID: "",
		Running:   false,
		Healthy:   true,
		Uptime:    "0s",
	}

	if w.conn != nil {
		health.SessionID = w.conn.SessionID()
	}

	if w.Proc == nil {
		return health
	}

	health.PID = w.Proc.PID()
	health.Running = w.Proc.IsRunning()

	if !w.StartTime.IsZero() {
		health.Uptime = time.Since(w.StartTime).Round(time.Second).String()
	}

	return health
}

// LastIO returns the time of the last I/O activity (input sent or output received).
func (w *BaseWorker) LastIO() time.Time {
	nano := w.lastIO.Load()
	if nano == 0 {
		return time.Time{}
	}
	return time.Unix(0, nano)
}

// SetLastIO atomically stores the last I/O time.
func (w *BaseWorker) SetLastIO(t time.Time) {
	w.lastIO.Store(t.UnixNano())
}

// SetConn sets the session connection after Start.
func (w *BaseWorker) SetConn(c *Conn) {
	w.Mu.Lock()
	defer w.Mu.Unlock()
	w.conn = c
}

// SetConnLocked sets the session connection without acquiring the mutex.
// Caller must hold w.Mu.
func (w *BaseWorker) SetConnLocked(c *Conn) {
	w.conn = c
}

// Conn returns the session connection, or nil if not started.
func (w *BaseWorker) Conn() worker.SessionConn {
	w.Mu.Lock()
	defer w.Mu.Unlock()
	if w.conn == nil {
		return nil
	}
	return w.conn
}

// IncResetGeneration increments the reset generation counter and returns the new value.
// Called by Bridge.ResetSession before the Terminate+Start cycle.
func (w *BaseWorker) IncResetGeneration() int64 { return w.resetGen.Add(1) }

// LoadResetGeneration returns the current reset generation counter.
// forwardEvents captures this at goroutine start to detect resets.
func (w *BaseWorker) LoadResetGeneration() int64 { return w.resetGen.Load() }
