package tts

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
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

// --- Factory ---

// SynthesizerConfig holds parameters for building a Synthesizer.
type SynthesizerConfig struct {
	EdgeVoice       string
	MossModelDir    string
	MossVoice       string
	MossPort        int
	MossCpuThreads  int
	MossIdleTimeout time.Duration
}

// NewConfiguredSynthesizer creates an Edge-TTS + MOSS Fallback setup from config.
func NewConfiguredSynthesizer(cfg SynthesizerConfig, log *slog.Logger) Synthesizer {
	if cfg.EdgeVoice == "" {
		cfg.EdgeVoice = "zh-CN-XiaoxiaoNeural"
	}
	edge := NewEdgeSynthesizer(cfg.EdgeVoice, log)
	moss := NewMossSynthesizer(cfg.MossModelDir, cfg.MossVoice, cfg.MossPort, cfg.MossCpuThreads, cfg.MossIdleTimeout, log)
	return NewFallbackSynthesizer(edge, moss, log)
}

// Shared TTS Support
// ---------------------------------------------------------------------------

// SharedSynthesizer wraps a Synthesizer to support reference-counted shared
// ownership. Multiple messaging adapters can share a single TTS process
// (e.g. MOSS sidecar) when their configurations are identical.
type SharedSynthesizer struct {
	Synthesizer
	closer Closer
	mu     sync.Mutex
	refs   atomic.Int32
}

func NewSharedSynthesizer(s Synthesizer) *SharedSynthesizer {
	w := &SharedSynthesizer{
		Synthesizer: s,
	}
	w.refs.Store(1)
	if c, ok := s.(Closer); ok {
		w.closer = c
	}
	return w
}

func (w *SharedSynthesizer) Refs() int32 { return w.refs.Load() }

func (w *SharedSynthesizer) Acquire() *SharedSynthesizer {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.refs.Add(1)
	return w
}

func (w *SharedSynthesizer) Close(ctx context.Context) error {
	w.mu.Lock()
	if w.refs.Add(-1) > 0 {
		w.mu.Unlock()
		return nil
	}
	closer := w.closer
	w.mu.Unlock()

	if closer != nil {
		return closer.Close(ctx)
	}
	return nil
}
