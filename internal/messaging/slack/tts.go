package slack

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"

	"golang.org/x/sync/semaphore"

	"github.com/hrygo/hotplex/internal/messaging/tts"

	slack "github.com/slack-go/slack"
)

// TTSPipeline processes AI responses into voice messages for Slack:
// full text → LLM summary → synthesizer → FFmpeg MP3 → Slack file upload.
// Slack natively supports MP3 inline playback.
type TTSPipeline struct {
	synthesizer tts.Synthesizer
	client      *slack.Client
	maxChars    int
	sem         *semaphore.Weighted
	log         *slog.Logger
}

func NewTTSPipeline(synthesizer tts.Synthesizer, client *slack.Client, maxChars int, log *slog.Logger) *TTSPipeline {
	if maxChars <= 0 {
		maxChars = 150
	}
	return &TTSPipeline{
		synthesizer: synthesizer,
		client:      client,
		maxChars:    maxChars,
		sem:         semaphore.NewWeighted(2),
		log:         log,
	}
}

// Process runs the full TTS pipeline. Call from a goroutine.
func (p *TTSPipeline) Process(ctx context.Context, fullText, channelID, threadTS string) {
	if !p.sem.TryAcquire(1) {
		p.log.Warn("tts: pipeline busy, dropping voice reply")
		return
	}
	defer p.sem.Release(1)

	summary, err := p.summarize(ctx, fullText)
	if err != nil {
		p.log.Warn("tts: summary failed, using truncated text", "err", err)
		summary = tts.SanitizeForSpeech(tts.TruncateText(fullText, p.maxChars))
	}
	if summary == "" {
		return
	}

	// Synthesizer may output MP3 (Edge) or WAV (MOSS) — normalize to MP3 for Slack.
	rawAudio, err := p.synthesizer.Synthesize(ctx, summary)
	if err != nil {
		p.log.Warn("tts: synthesis failed", "err", err)
		return
	}

	mp3Data, err := tts.ToMP3(ctx, rawAudio)
	if err != nil {
		p.log.Warn("tts: audio→mp3 conversion failed", "err", err)
		return
	}

	duration := tts.EstimateAudioDurationMs(len(mp3Data)) / 1000
	if err := p.uploadAndSend(ctx, channelID, threadTS, mp3Data); err != nil {
		p.log.Warn("tts: send audio failed", "err", err)
		return
	}
	p.log.Info("tts: voice reply sent", "summary_len", len(summary), "duration_s", duration)
}

func (p *TTSPipeline) summarize(ctx context.Context, fullText string) (string, error) {
	return tts.SummarizeForTTS(ctx, fullText, p.maxChars)
}

func (p *TTSPipeline) uploadAndSend(ctx context.Context, channelID, threadTS string, mp3Data []byte) error {
	params := slack.UploadFileParameters{
		Filename: "voice_reply.mp3",
		Title:    "Voice Reply",
		Reader:   bytes.NewReader(mp3Data),
		FileSize: len(mp3Data),
		Channel:  channelID,
	}
	if threadTS != "" {
		params.ThreadTimestamp = threadTS
	}

	_, err := p.client.UploadFileContext(ctx, params)
	if err != nil {
		return fmt.Errorf("slack upload audio: %w", err)
	}
	return nil
}
