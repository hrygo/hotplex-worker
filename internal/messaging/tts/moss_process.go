package tts

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hrygo/hotplex/internal/worker/proc"
)

const (
	mossDefaultPort       = 18083
	mossDefaultVoice      = "Xiaoyu"
	mossDefaultIdleTTL    = 30 * time.Minute
	mossDefaultCpuThreads = 0
	mossReadyTimeout      = 60 * time.Second
	mossReadyInterval     = 500 * time.Millisecond
	mossSynthTimeout      = 60 * time.Second
)

// MossProcess manages the MOSS-TTS-Nano FastAPI server as a child process.
// It follows the PersistentSTT pattern: lazy start, idle timer, layered termination.
type MossProcess struct {
	modelDir   string
	port       int
	cpuThreads int
	idleTTL    time.Duration
	pidKey     string
	log        *slog.Logger

	mu      sync.Mutex
	cmd     *exec.Cmd
	pgid    int
	started bool
	closed  bool
	baseURL string
	client  *http.Client

	lastUsed    atomic.Int64
	activeCount atomic.Int32 // tracks in-flight Synthesize calls for idleMonitor
	cancel      context.CancelFunc
	done        chan struct{}
	activeWg    sync.WaitGroup
}

func NewMossProcess(modelDir string, port, cpuThreads int, idleTTL time.Duration, log *slog.Logger) *MossProcess {
	if port <= 0 {
		port = mossDefaultPort
	}
	if cpuThreads <= 0 {
		cpuThreads = mossDefaultCpuThreads
	}
	if idleTTL <= 0 {
		idleTTL = mossDefaultIdleTTL
	}
	if log == nil {
		log = slog.Default()
	}
	return &MossProcess{
		modelDir:   modelDir,
		port:       port,
		cpuThreads: cpuThreads,
		idleTTL:    idleTTL,
		pidKey:     "moss-tts",
		log:        log,
		baseURL:    fmt.Sprintf("http://127.0.0.1:%d", port),
		client: &http.Client{
			Timeout: mossSynthTimeout,
		},
	}
}

// Synthesize sends text to the MOSS sidecar and returns WAV audio bytes.
func (p *MossProcess) Synthesize(ctx context.Context, text, voice string) ([]byte, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, ErrSynthesizerClosed
	}
	if err := p.ensureRunningLocked(ctx); err != nil {
		p.mu.Unlock()
		return nil, fmt.Errorf("tts moss: %w", err)
	}
	p.activeWg.Add(1)
	p.activeCount.Add(1)
	p.mu.Unlock()
	defer func() {
		p.activeCount.Add(-1)
		p.activeWg.Done()
	}()

	if voice == "" {
		voice = mossDefaultVoice
	}

	form := url.Values{}
	form.Set("text", text)
	form.Set("voice", voice)
	form.Set("max_new_frames", "150")
	form.Set("enable_wetext", "false")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/api/generate", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("tts moss request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.client.Do(req)
	if err != nil {
		p.maybeRestart(err)
		return nil, fmt.Errorf("tts moss call: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		p.maybeRestart(fmt.Errorf("status %d: %s", resp.StatusCode, string(body)))
		return nil, fmt.Errorf("tts moss: status %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		AudioBase64 string  `json:"audio_base64"`
		SampleRate  int     `json:"sample_rate"`
		Error       string  `json:"error"`
		Voice       string  `json:"voice"`
		Elapsed     float64 `json:"elapsed_seconds"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("tts moss decode: %w", err)
	}
	if result.Error != "" {
		return nil, fmt.Errorf("tts moss: %s", result.Error)
	}
	if result.AudioBase64 == "" {
		return nil, fmt.Errorf("tts moss: empty audio response")
	}

	wav, err := base64.StdEncoding.DecodeString(result.AudioBase64)
	if err != nil {
		return nil, fmt.Errorf("tts moss base64: %w", err)
	}

	p.lastUsed.Store(time.Now().UnixNano())
	p.log.Debug("tts: synthesized (moss)", "voice", result.Voice, "sr", result.SampleRate, "elapsed", fmt.Sprintf("%.1fs", result.Elapsed), "wav_len", len(wav))
	return wav, nil
}

// Close shuts down the sidecar process with layered termination.
func (p *MossProcess) Close(ctx context.Context) error {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()

	p.activeWg.Wait()

	p.mu.Lock()
	p.terminate()
	p.mu.Unlock()
	return nil
}

// ensureRunning lazily starts the sidecar on first call or restarts on crash.
// Caller must hold p.mu.
func (p *MossProcess) ensureRunningLocked(ctx context.Context) error {
	if p.closed {
		return ErrSynthesizerClosed
	}
	if p.started && p.isAlive() {
		return nil
	}
	return p.start(ctx)
}

func (p *MossProcess) start(ctx context.Context) error {
	appPath := p.modelDir + "/app_onnx.py"
	args := []string{
		appPath,
		"--host", "127.0.0.1",
		"--port", fmt.Sprintf("%d", p.port),
		"--model-dir", p.modelDir,
	}

	p.log.Info("tts: starting moss sidecar", "port", p.port, "model_dir", p.modelDir)

	cmd := exec.Command("python3", args...)
	proc.SetSysProcAttr(cmd)

	// Redirect sidecar stdout/stderr to gateway logs.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start moss sidecar: %w", err)
	}

	p.cmd = cmd
	p.pgid = cmd.Process.Pid
	p.started = true

	// Track PID for orphan cleanup.
	if tracker := proc.GlobalTracker(); tracker != nil {
		if err := tracker.Write(p.pidKey, p.pgid); err != nil {
			p.log.Warn("tts moss: pidfile write", "err", err)
		}
	}

	// Wait for health check.
	if err := p.waitForReady(ctx); err != nil {
		p.terminate()
		return fmt.Errorf("moss sidecar warmup: %w", err)
	}

	// Start idle monitor.
	if p.idleTTL > 0 {
		idleCtx, cancel := context.WithCancel(context.Background())
		p.cancel = cancel
		done := make(chan struct{})
		p.done = done
		go p.idleMonitor(idleCtx, done)
	}

	p.log.Info("tts: moss sidecar ready", "pid", cmd.Process.Pid, "port", p.port)
	return nil
}

func (p *MossProcess) waitForReady(ctx context.Context) error {
	client := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(mossReadyTimeout)
	checkURL := p.baseURL + "/api/warmup-status"

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		resp, err := client.Get(checkURL)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		// Check if process exited.
		if !p.isAlive() {
			return fmt.Errorf("moss sidecar exited during warmup")
		}
		time.Sleep(mossReadyInterval)
	}
	return fmt.Errorf("moss sidecar warmup timed out after %v", mossReadyTimeout)
}

// terminate cancels the idle monitor and shuts down the subprocess.
// Caller must hold p.mu.
func (p *MossProcess) terminate() {
	if p.cancel != nil {
		p.cancel()
		// Do NOT wait for <-p.done — idleMonitor might be blocked
		// on mu held by our caller, causing deadlock. It exits on its own.
		p.cancel = nil
	}
	p.shutdownProcess()
}

// shutdownProcess terminates the sidecar subprocess and cleans up resources.
// Caller must hold p.mu. Does NOT stop the idle monitor.
func (p *MossProcess) shutdownProcess() {
	if !p.started {
		return
	}

	if p.pgid > 0 {
		_ = proc.GracefulTerminate(p.pgid)
		p.log.Info("tts moss: sent graceful termination", "pgid", p.pgid)
	}

	// Wait up to 5s for graceful exit.
	if p.cmd != nil && p.cmd.Process != nil {
		waitDone := make(chan struct{})
		go func() {
			_ = p.cmd.Wait()
			close(waitDone)
		}()
		select {
		case <-waitDone:
		case <-time.After(5 * time.Second):
			_ = proc.ForceKill(p.pgid)
			p.log.Warn("tts moss: force killed after timeout", "pgid", p.pgid)
			<-waitDone // ensure cmd.Wait goroutine completes
		}
	}

	// Clean up PID file.
	if tracker := proc.GlobalTracker(); tracker != nil {
		_ = tracker.Remove(p.pidKey)
	}

	p.started = false
	p.cmd = nil
	p.pgid = 0
}

func (p *MossProcess) isAlive() bool {
	if p.cmd == nil || p.cmd.Process == nil {
		return false
	}
	return proc.IsProcessAlive(p.cmd.Process.Pid) == nil
}

func (p *MossProcess) maybeRestart(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.isAlive() {
		p.log.Warn("tts moss: sidecar process died, will restart on next request", "err", err)
		p.started = false
	}
}

// idleMonitor auto-shuts down the sidecar after idle TTL.
// done chan is captured by parameter to avoid racing with start() reassignment.
func (p *MossProcess) idleMonitor(ctx context.Context, done chan struct{}) {
	defer func() {
		if r := recover(); r != nil {
			p.log.Error("tts moss: panic in idleMonitor", "panic", r)
		}
	}()
	defer close(done)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		if p.activeCount.Load() > 0 {
			continue // Active request in progress, skip.
		}

		last := time.Unix(0, p.lastUsed.Load())
		if time.Since(last) < p.idleTTL {
			continue
		}

		p.mu.Lock()
		if p.started && p.isAlive() {
			p.log.Info("tts moss: idle timeout, shutting down sidecar", "idle_ttl", p.idleTTL)
			p.shutdownProcess()
		}
		p.mu.Unlock()
		return
	}
}
