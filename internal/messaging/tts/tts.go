package tts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// ErrSynthesizerClosed is returned when Synthesize is called after Close.
var ErrSynthesizerClosed = errors.New("tts: synthesizer closed")

// Synthesizer converts text to audio bytes.
type Synthesizer interface {
	Synthesize(ctx context.Context, text string) ([]byte, error)
}

// Closer is an optional interface for synthesizers that manage long-lived resources.
type Closer interface {
	Close(ctx context.Context) error
}

// --- Fallback Mechanism ---

// FallbackSynthesizer tries multiple synthesizers in order.
type FallbackSynthesizer struct {
	primary   Synthesizer
	secondary Synthesizer
	log       *slog.Logger
}

func NewFallbackSynthesizer(primary, secondary Synthesizer, log *slog.Logger) *FallbackSynthesizer {
	return &FallbackSynthesizer{
		primary:   primary,
		secondary: secondary,
		log:       log,
	}
}

func (f *FallbackSynthesizer) Synthesize(ctx context.Context, text string) ([]byte, error) {
	data, err := f.primary.Synthesize(ctx, text)
	if err == nil {
		return data, nil
	}

	// Don't waste resources on fallback if context is already cancelled.
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	f.log.Warn("tts: primary synthesizer failed, falling back", "err", err)
	return f.secondary.Synthesize(ctx, text)
}

// Close closes both synthesizers if they implement the Closer interface.
func (f *FallbackSynthesizer) Close(ctx context.Context) error {
	var errs []error
	if c, ok := f.primary.(Closer); ok {
		if err := c.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("primary close: %w", err))
		}
	}
	if c, ok := f.secondary.(Closer); ok {
		if err := c.Close(ctx); err != nil {
			errs = append(errs, fmt.Errorf("secondary close: %w", err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("fallback close errors: %v", errs)
	}
	return nil
}

// --- Edge-TTS Implementation (Primary) ---

// EdgeSynthesizer uses Microsoft Edge TTS (free, no API key required).
// Implemented natively via WebSocket without third-party dependencies.
type EdgeSynthesizer struct {
	voice string
	log   *slog.Logger
}

func NewEdgeSynthesizer(voice string, log *slog.Logger) *EdgeSynthesizer {
	if voice == "" {
		voice = edgeDefaultVoice
	}
	return &EdgeSynthesizer{
		voice: voice,
		log:   log,
	}
}

func (s *EdgeSynthesizer) Synthesize(ctx context.Context, text string) ([]byte, error) {
	if text == "" {
		return nil, fmt.Errorf("tts: empty text")
	}

	data, err := synthesizeEdge(ctx, text, s.voice)
	if err != nil {
		return nil, err
	}

	s.log.Debug("tts: synthesized (edge)", "voice", s.voice, "text_len", len(text), "audio_len", len(data))
	return data, nil
}

// --- Kokoro Implementation (Fallback) ---

// KokoroSynthesizer uses local CPU with ONNX model.
// It implements a "temporary resident" pattern: loads the model on demand
// and unloads it after a configurable idle timeout (default 30m) to save memory.
type KokoroSynthesizer struct {
	modelPath string
	voice     string
	log       *slog.Logger

	mu             sync.Mutex
	lastUsed       time.Time
	idleTimeout    time.Duration
	idleTimer      *time.Timer
	closed         bool
	activeRequests sync.WaitGroup

	// session would be the actual ONNX session;
	// using a placeholder bool for now to represent the loaded state.
	isLoaded bool
}

func NewKokoroSynthesizer(modelPath, voice string, log *slog.Logger) *KokoroSynthesizer {
	return NewKokoroSynthesizerWithOptions(modelPath, voice, 30*time.Minute, log)
}

func NewKokoroSynthesizerWithOptions(modelPath, voice string, idleTimeout time.Duration, log *slog.Logger) *KokoroSynthesizer {
	if voice == "" {
		voice = "af_heart"
	}
	if idleTimeout <= 0 {
		idleTimeout = 30 * time.Minute
	}
	return &KokoroSynthesizer{
		modelPath:   modelPath,
		voice:       voice,
		log:         log,
		idleTimeout: idleTimeout,
	}
}

func (k *KokoroSynthesizer) Synthesize(ctx context.Context, text string) ([]byte, error) {
	if text == "" {
		return nil, fmt.Errorf("tts: empty text")
	}

	// Acquire lock only for state management, not for inference.
	k.mu.Lock()
	if k.closed {
		k.mu.Unlock()
		return nil, ErrSynthesizerClosed
	}

	// 1. Lazy load if not loaded
	if !k.isLoaded {
		k.load(ctx)
	}

	// 2. Update usage and reset/start idle timer
	k.lastUsed = time.Now()
	if k.idleTimer != nil {
		k.idleTimer.Stop()
	}
	k.idleTimer = time.AfterFunc(k.idleTimeout, k.unloadOnIdle)

	// 3. Track active request count, then release lock for inference.
	k.activeRequests.Add(1)
	k.mu.Unlock()

	defer k.activeRequests.Done()

	// 4. Perform synthesis outside the lock (allows concurrent inference).
	// TODO: Implement ONNX inference logic.
	k.log.Debug("tts: synthesized (kokoro-stub)", "voice", k.voice, "text_len", len(text))
	return nil, fmt.Errorf("tts kokoro: local CPU synthesizer not yet fully implemented")
}

func (k *KokoroSynthesizer) load(_ context.Context) {
	k.log.Info("tts: loading kokoro model into memory", "path", k.modelPath)
	// TODO: actual ONNX session initialization goes here.
	k.isLoaded = true
}

func (k *KokoroSynthesizer) unloadOnIdle() {
	k.mu.Lock()
	defer k.mu.Unlock()

	if !k.isLoaded || k.closed {
		return
	}

	// Double-check: has the model been used since the timer was set?
	if time.Since(k.lastUsed) < k.idleTimeout {
		return
	}

	k.log.Info("tts: kokoro model idle, unloading to release resources", "idle_timeout", k.idleTimeout)
	k.unload()
}

func (k *KokoroSynthesizer) unload() {
	// TODO: actual ONNX session closing goes here.
	k.isLoaded = false
	if k.idleTimer != nil {
		k.idleTimer.Stop()
		k.idleTimer = nil
	}
}

// Close terminates the synthesizer. It marks the instance as closed,
// prevents new requests, and waits for in-flight requests to complete.
func (k *KokoroSynthesizer) Close(_ context.Context) error {
	k.mu.Lock()
	k.closed = true
	k.unload()
	k.mu.Unlock()

	// Wait for any in-flight Synthesize calls to finish.
	k.activeRequests.Wait()
	return nil
}

// --- Factory ---

// SynthesizerConfig holds parameters for building a Synthesizer.
type SynthesizerConfig struct {
	EdgeVoice         string
	KokoroModelPath   string
	KokoroVoice       string
	KokoroIdleTimeout time.Duration
}

// NewConfiguredSynthesizer creates an Edge-TTS + Kokoro Fallback setup from config.
func NewConfiguredSynthesizer(cfg SynthesizerConfig, log *slog.Logger) Synthesizer {
	if cfg.EdgeVoice == "" {
		cfg.EdgeVoice = "zh-CN-XiaoxiaoNeural"
	}
	edge := NewEdgeSynthesizer(cfg.EdgeVoice, log)
	kokoro := NewKokoroSynthesizerWithOptions(cfg.KokoroModelPath, cfg.KokoroVoice, cfg.KokoroIdleTimeout, log)
	return NewFallbackSynthesizer(edge, kokoro, log)
}
