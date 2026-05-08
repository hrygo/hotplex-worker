package tts

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// MossSynthesizer implements Synthesizer by delegating to a MOSS-TTS-Nano
// sidecar process managed by MossProcess. It returns WAV audio bytes (48kHz stereo).
type MossSynthesizer struct {
	process *MossProcess
	voice   string
	log     *slog.Logger
}

func NewMossSynthesizer(modelDir, voice string, port, cpuThreads int, idleTTL time.Duration, log *slog.Logger) *MossSynthesizer {
	if voice == "" {
		voice = mossDefaultVoice
	}
	return &MossSynthesizer{
		process: NewMossProcess(modelDir, port, cpuThreads, idleTTL, log),
		voice:   voice,
		log:     log,
	}
}

func (m *MossSynthesizer) Synthesize(ctx context.Context, text string) ([]byte, error) {
	if text == "" {
		return nil, fmt.Errorf("tts: empty text")
	}
	return m.process.Synthesize(ctx, text, m.voice)
}

func (m *MossSynthesizer) Close(ctx context.Context) error {
	return m.process.Close(ctx)
}
