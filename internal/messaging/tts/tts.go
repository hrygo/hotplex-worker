package tts

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/lib-x/edgetts"
)

// Synthesizer converts text to audio bytes (Opus format).
type Synthesizer interface {
	Synthesize(ctx context.Context, text string) ([]byte, error)
}

// Closer is an optional interface for synthesizers that manage long-lived resources.
type Closer interface {
	Close(ctx context.Context) error
}

// EdgeSynthesizer uses Microsoft Edge TTS (free, no API key required).
type EdgeSynthesizer struct {
	client *edgetts.Client
	voice  string
	log    *slog.Logger
}

func NewEdgeSynthesizer(voice string, log *slog.Logger) *EdgeSynthesizer {
	if voice == "" {
		voice = "zh-CN-XiaoxiaoNeural"
	}
	return &EdgeSynthesizer{
		client: edgetts.New(edgetts.WithVoice(voice)),
		voice:  voice,
		log:    log,
	}
}

func (s *EdgeSynthesizer) Synthesize(ctx context.Context, text string) ([]byte, error) {
	if text == "" {
		return nil, fmt.Errorf("tts: empty text")
	}

	data, err := s.client.Bytes(ctx, text)
	if err != nil {
		return nil, fmt.Errorf("tts edge: %w", err)
	}

	s.log.Debug("tts: synthesized", "voice", s.voice, "text_len", len(text), "audio_len", len(data))
	return data, nil
}
