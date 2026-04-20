package feishu

import (
	"bufio"
	"bytes"
	"context"
	crypto_rand "crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkspeech "github.com/larksuite/oapi-sdk-go/v3/service/speech_to_text/v1"
)

// Transcriber converts raw audio bytes to text.
// Implementations may use cloud APIs or local tools.
type Transcriber interface {
	Transcribe(ctx context.Context, audioData []byte) (string, error)
	// RequiresDisk returns true if the transcriber (or any of its fallbacks)
	// needs the audio file written to disk. Only pure cloud transcribers
	// can skip the disk write on success.
	RequiresDisk() bool
}

// ---------------------------------------------------------------------------
// FeishuSTT — cloud transcription via Feishu speech_to_text API
// ---------------------------------------------------------------------------

// FeishuSTT calls the Feishu speech_to_text file_recognize endpoint.
// Audio is converted to PCM in memory (no disk I/O).
type FeishuSTT struct {
	client *lark.Client
	log    *slog.Logger
}

func NewFeishuSTT(client *lark.Client, log *slog.Logger) *FeishuSTT {
	return &FeishuSTT{client: client, log: log}
}

func (s *FeishuSTT) RequiresDisk() bool { return false }

func (s *FeishuSTT) Transcribe(ctx context.Context, audioData []byte) (string, error) {
	pcmData, err := audioToPCM(ctx, audioData)
	if err != nil {
		return "", fmt.Errorf("feishu stt: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(pcmData)
	fileID := randomAlphaNum(16)

	speech := larkspeech.NewSpeechBuilder().Speech(encoded).Build()
	config := larkspeech.NewFileConfigBuilder().
		FileId(fileID).
		Format("pcm").
		EngineType("16k_auto").
		Build()

	// Use direct struct init for the body to avoid SDK builder flag bug.
	body := &larkspeech.FileRecognizeSpeechReqBody{
		Speech: speech,
		Config: config,
	}
	req := larkspeech.NewFileRecognizeSpeechReqBuilder().Body(body).Build()

	resp, err := s.client.SpeechToText.Speech.FileRecognize(ctx, req)
	if err != nil {
		return "", fmt.Errorf("feishu stt: api: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("feishu stt: code=%d msg=%s", resp.Code, resp.Msg)
	}
	if resp.Data == nil || resp.Data.RecognitionText == nil || *resp.Data.RecognitionText == "" {
		return "", nil
	}

	text := *resp.Data.RecognitionText
	s.log.Debug("feishu stt: transcribed", "text", text, "text_len", len(text))
	return text, nil
}

// ---------------------------------------------------------------------------
// LocalSTT — local transcription via external command
// ---------------------------------------------------------------------------

// LocalSTT executes an external command for transcription.
// The command template uses {file} as a placeholder for the audio file path.
// The first line of stdout is used as the transcription result.
type LocalSTT struct {
	cmdTemplate string
	log         *slog.Logger
}

func NewLocalSTT(cmdTemplate string, log *slog.Logger) *LocalSTT {
	return &LocalSTT{cmdTemplate: cmdTemplate, log: log}
}

func (s *LocalSTT) RequiresDisk() bool { return true }

func (s *LocalSTT) Transcribe(ctx context.Context, audioData []byte) (string, error) {
	// Local tools need a file on disk. Write to a temp location.
	tmpDir := "/tmp/hotplex/media/stt_tmp"
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return "", fmt.Errorf("local stt: mkdir: %w", err)
	}
	tmpPath := filepath.Join(tmpDir, fmt.Sprintf("stt_%d.opus", time.Now().UnixNano()))
	if err := os.WriteFile(tmpPath, audioData, 0o644); err != nil {
		return "", fmt.Errorf("local stt: write: %w", err)
	}
	defer func() { _ = os.Remove(tmpPath) }()

	cmdStr := strings.ReplaceAll(s.cmdTemplate, "{file}", tmpPath)
	parts := strings.Fields(cmdStr)
	if len(parts) == 0 {
		return "", fmt.Errorf("local stt: empty command")
	}

	out, err := exec.CommandContext(ctx, parts[0], parts[1:]...).Output()
	if err != nil {
		return "", fmt.Errorf("local stt: %s: %w", parts[0], err)
	}

	text := strings.TrimSpace(string(out))
	s.log.Debug("local stt: transcribed", "text", text, "text_len", len(text))
	return text, nil
}

// ---------------------------------------------------------------------------
// FallbackSTT — try primary, fall back to secondary
// ---------------------------------------------------------------------------

// FallbackSTT tries the primary transcriber, then falls back to the secondary.
type FallbackSTT struct {
	primary   Transcriber
	secondary Transcriber
	log       *slog.Logger
}

func NewFallbackSTT(primary, secondary Transcriber, log *slog.Logger) *FallbackSTT {
	return &FallbackSTT{primary: primary, secondary: secondary, log: log}
}

func (s *FallbackSTT) RequiresDisk() bool { return true }

func (s *FallbackSTT) Transcribe(ctx context.Context, audioData []byte) (string, error) {
	text, err := s.primary.Transcribe(ctx, audioData)
	if err == nil && text != "" {
		return text, nil
	}
	if err != nil {
		s.log.Warn("stt: primary failed, trying fallback", "err", err)
	}
	return s.secondary.Transcribe(ctx, audioData)
}

// ---------------------------------------------------------------------------
// PersistentSTT — long-lived subprocess for local transcription
// ---------------------------------------------------------------------------

// PersistentSTT manages a long-lived subprocess for transcription.
// Audio is written to a temp file; the path is sent to the subprocess via
// JSON-over-stdio. The subprocess keeps the model loaded in memory, avoiding
// per-request cold start overhead.
//
// Protocol:
//
//	Go → subprocess stdin:  {"audio_path": "/tmp/.../stt_123.opus"}\n
//	Subprocess → Go stdout: {"text": "转录结果", "error": ""}\n
//
// Resource cleanup:
//   - Temp audio files: removed after each Transcribe call (defer).
//   - Subprocess: PGID-isolated, layered SIGTERM → 5s → SIGKILL.
//   - Idle timeout: auto-shutdown after idleTTL (configurable).
//   - Crash recovery: detected on next Transcribe, subprocess auto-restarts.
//   - Gateway shutdown: Adapter.Close → PersistentSTT.Close.
type PersistentSTT struct {
	cmdParts []string
	idleTTL  time.Duration
	log      *slog.Logger

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdoutR *os.File
	scanner *bufio.Scanner
	pgid    int
	started bool

	lastUsed atomic.Int64 // unix nano of last successful request
	cancel   context.CancelFunc
	done     chan struct{} // signals idleMonitor exited
}

// NewPersistentSTT creates a persistent STT transcriber.
// cmdTemplate is the command to launch the subprocess (no {file} placeholder).
// idleTTL controls auto-shutdown after idle (0 = disabled).
func NewPersistentSTT(cmdTemplate string, idleTTL time.Duration, log *slog.Logger) *PersistentSTT {
	parts := strings.Fields(cmdTemplate)
	return &PersistentSTT{
		cmdParts: parts,
		idleTTL:  idleTTL,
		log:      log,
	}
}

func (s *PersistentSTT) RequiresDisk() bool { return true }

func (s *PersistentSTT) Transcribe(ctx context.Context, audioData []byte) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Lazy start: launch subprocess if not running.
	if !s.started || !s.isAlive() {
		if err := s.start(ctx); err != nil {
			return "", fmt.Errorf("persistent stt: %w", err)
		}
	}

	// Write audio to temp file.
	tmpDir := "/tmp/hotplex/media/stt_tmp"
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return "", fmt.Errorf("persistent stt: mkdir: %w", err)
	}
	tmpPath := filepath.Join(tmpDir, fmt.Sprintf("stt_%d.opus", time.Now().UnixNano()))
	if err := os.WriteFile(tmpPath, audioData, 0o644); err != nil {
		return "", fmt.Errorf("persistent stt: write: %w", err)
	}
	defer func() { _ = os.Remove(tmpPath) }()

	// Send JSON request via stdin.
	req, _ := json.Marshal(map[string]string{"audio_path": tmpPath})
	if _, err := s.stdin.Write(append(req, '\n')); err != nil {
		s.kill()
		return "", fmt.Errorf("persistent stt: write stdin: %w", err)
	}

	// Read JSON response from stdout.
	if !s.scanner.Scan() {
		s.kill()
		return "", fmt.Errorf("persistent stt: subprocess exited unexpectedly")
	}

	var resp struct {
		Text  string `json:"text"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal([]byte(s.scanner.Text()), &resp); err != nil {
		return "", fmt.Errorf("persistent stt: parse response: %w", err)
	}
	if resp.Error != "" {
		return "", fmt.Errorf("persistent stt: %s", resp.Error)
	}

	s.lastUsed.Store(time.Now().UnixNano())
	s.log.Debug("persistent stt: transcribed", "text", resp.Text, "text_len", len(resp.Text))
	return resp.Text, nil
}

// Close shuts down the subprocess with layered termination.
func (s *PersistentSTT) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.terminate(ctx)
	return nil
}

// start launches the subprocess with PGID isolation.
func (s *PersistentSTT) start(_ context.Context) error {
	cmd := exec.Command(s.cmdParts[0], s.cmdParts[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// Create pipes for stdio.
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("persistent stt: stdin pipe: %w", err)
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		_ = stdinR.Close()
		_ = stdinW.Close()
		return fmt.Errorf("persistent stt: stdout pipe: %w", err)
	}

	cmd.Stdin = stdinR
	cmd.Stdout = stdoutW

	if err := cmd.Start(); err != nil {
		_ = stdinR.Close()
		_ = stdinW.Close()
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		return fmt.Errorf("persistent stt: start: %w", err)
	}

	// Close parent's ends of subprocess pipes.
	_ = stdinR.Close()
	_ = stdoutW.Close()

	s.cmd = cmd
	s.stdin = stdinW
	s.stdoutR = stdoutR
	s.pgid = cmd.Process.Pid
	s.started = true

	// Set up line scanner for stdout (64KB init, 10MB cap).
	buf := make([]byte, 64*1024)
	s.scanner = bufio.NewScanner(stdoutR)
	s.scanner.Buffer(buf, 10*1024*1024)

	// Start idle monitor if TTL configured.
	if s.idleTTL > 0 {
		idleCtx, cancel := context.WithCancel(context.Background())
		s.cancel = cancel
		s.done = make(chan struct{})
		go s.idleMonitor(idleCtx)
	}

	s.log.Info("persistent stt: started", "pid", cmd.Process.Pid, "idle_ttl", s.idleTTL)
	return nil
}

// terminate sends SIGTERM, waits 5s, then SIGKILL. Cleans up all resources.
func (s *PersistentSTT) terminate(ctx context.Context) {
	if !s.started {
		return
	}

	// Stop idle monitor.
	if s.cancel != nil {
		s.cancel()
		<-s.done
		s.cancel = nil
	}

	// Close stdin to signal subprocess.
	_ = s.stdin.Close()

	// Send SIGTERM to process group.
	if s.pgid > 0 {
		_ = syscall.Kill(-s.pgid, syscall.SIGTERM)
		s.log.Info("persistent stt: sent SIGTERM", "pgid", s.pgid)
	}

	// Wait for graceful exit with deadline.
	done := make(chan struct{})
	go func() { _ = s.cmd.Wait(); close(done) }()

	select {
	case <-done:
		// Graceful exit.
	case <-time.After(5 * time.Second):
		s.log.Warn("persistent stt: graceful timeout, sending SIGKILL", "pgid", s.pgid)
		if s.pgid > 0 {
			_ = syscall.Kill(-s.pgid, syscall.SIGKILL)
		}
		<-done
	case <-ctx.Done():
		if s.pgid > 0 {
			_ = syscall.Kill(-s.pgid, syscall.SIGKILL)
		}
		<-done
	}

	_ = s.stdoutR.Close()
	s.started = false
	s.log.Info("persistent stt: stopped")
}

// kill sends SIGKILL immediately and cleans up.
func (s *PersistentSTT) kill() {
	if s.cancel != nil {
		s.cancel()
		if s.done != nil {
			<-s.done
		}
		s.cancel = nil
	}
	if s.pgid > 0 {
		_ = syscall.Kill(-s.pgid, syscall.SIGKILL)
	}
	if s.cmd != nil {
		_ = s.cmd.Wait()
	}
	_ = s.stdin.Close()
	_ = s.stdoutR.Close()
	s.started = false
}

// isAlive checks if subprocess is still running (non-blocking).
func (s *PersistentSTT) isAlive() bool {
	return s.cmd != nil && s.cmd.ProcessState == nil
}

// idleMonitor auto-shuts down the subprocess after idle TTL.
func (s *PersistentSTT) idleMonitor(ctx context.Context) {
	defer close(s.done)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			last := time.Unix(0, s.lastUsed.Load())
			if time.Since(last) > s.idleTTL {
				s.mu.Lock()
				s.log.Info("persistent stt: idle timeout, shutting down", "idle_ttl", s.idleTTL)
				s.terminate(ctx)
				s.mu.Unlock()
				return
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// audioToPCM converts audio bytes (any ffmpeg-supported format) to raw PCM:
// 16-bit signed little-endian, 16kHz, mono. All work is done in memory via
// ffmpeg pipe — no temporary files on disk.
func audioToPCM(ctx context.Context, audioData []byte) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-i", "pipe:0",
		"-f", "s16le",
		"-ar", "16000",
		"-ac", "1",
		"-hide_banner",
		"-loglevel", "error",
		"pipe:1",
	)
	cmd.Stdin = bytes.NewReader(audioData)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	out, err := cmd.Output()
	if err != nil {
		hint := stderr.String()
		if hint == "" {
			hint = err.Error()
		}
		return nil, fmt.Errorf("ffmpeg opus→pcm: %s", hint)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("ffmpeg opus→pcm: empty output")
	}
	return out, nil
}

// randomAlphaNum returns an n-character lowercase alphanumeric string.
func randomAlphaNum(n int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		var rb [1]byte
		_, _ = crypto_rand.Read(rb[:])
		b[i] = charset[int(rb[0])%len(charset)]
	}
	return string(b)
}
