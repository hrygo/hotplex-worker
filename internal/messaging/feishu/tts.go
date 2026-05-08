package feishu

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"

	"golang.org/x/sync/semaphore"

	"github.com/hrygo/hotplex/internal/messaging/tts"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// TTSPipeline processes AI responses into voice messages:
// full text → LLM summary → synthesizer → FFmpeg Opus → Feishu audio message.
type TTSPipeline struct {
	synthesizer tts.Synthesizer
	client      *lark.Client
	maxChars    int
	sem         *semaphore.Weighted
	log         *slog.Logger
}

func NewTTSPipeline(synthesizer tts.Synthesizer, client *lark.Client, maxChars int, log *slog.Logger) *TTSPipeline {
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
// Limits concurrency to avoid overwhelming TTS/LLM resources.
func (p *TTSPipeline) Process(ctx context.Context, fullText, chatID, replyToMsgID string) {
	if !p.sem.TryAcquire(1) {
		p.log.Warn("tts: pipeline busy, dropping voice reply")
		return
	}
	defer p.sem.Release(1)

	// 1. LLM summary via Brain
	summary, err := p.summarize(ctx, fullText)
	if err != nil {
		p.log.Warn("tts: summary failed, using truncated text", "err", err)
		summary = tts.SanitizeForSpeech(tts.TruncateText(fullText, p.maxChars))
	}

	if summary == "" {
		return
	}

	// 2. Synthesize (Edge→MP3 or MOSS→WAV)
	rawAudio, err := p.synthesizer.Synthesize(ctx, summary)
	if err != nil {
		p.log.Warn("tts: synthesis failed", "err", err)
		return
	}

	// 3. FFmpeg → Opus
	opusData, err := tts.ToOpus(ctx, rawAudio)
	if err != nil {
		p.log.Warn("tts: audio→opus conversion failed", "err", err)
		return
	}

	// 4. Upload to Feishu + send audio message
	duration := tts.EstimateAudioDuration(len(opusData))
	if err := p.sendAudio(ctx, chatID, replyToMsgID, opusData); err != nil {
		p.log.Warn("tts: send audio failed", "err", err)
		return
	}
	p.log.Info("tts: voice reply sent", "summary_len", len(summary), "duration_s", duration)
}

func (p *TTSPipeline) summarize(ctx context.Context, fullText string) (string, error) {
	return tts.SummarizeForTTS(ctx, fullText, p.maxChars)
}

func (p *TTSPipeline) sendAudio(ctx context.Context, chatID, replyToMsgID string, opusData []byte) error {
	fileKey, err := p.uploadAudio(ctx, opusData)
	if err != nil {
		return fmt.Errorf("upload audio: %w", err)
	}

	msgContent := fmt.Sprintf(`{"file_key":%q,"duration":%d}`, fileKey, tts.EstimateAudioDurationMs(len(opusData)))
	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType("chat_id").
		Body(larkim.NewCreateMessageReqBodyBuilder().
			ReceiveId(chatID).
			MsgType("audio").
			Content(msgContent).
			Build()).
		Build()

	if replyToMsgID != "" {
		replyReq := larkim.NewReplyMessageReqBuilder().
			MessageId(replyToMsgID).
			Body(larkim.NewReplyMessageReqBodyBuilder().
				Content(msgContent).
				MsgType("audio").
				Build()).
			Build()
		resp, err := p.client.Im.Message.Reply(ctx, replyReq)
		if err != nil {
			return fmt.Errorf("reply audio message: %w", err)
		}
		if resp == nil {
			return fmt.Errorf("reply audio message: empty response")
		}
		return nil
	}

	resp, err := p.client.Im.Message.Create(ctx, req)
	if err != nil {
		return fmt.Errorf("create audio message: %w", err)
	}
	if resp == nil || resp.Data == nil {
		return fmt.Errorf("create audio message: empty response")
	}
	return nil
}

func (p *TTSPipeline) uploadAudio(ctx context.Context, opusData []byte) (string, error) {
	req := larkim.NewCreateFileReqBuilder().
		Body(larkim.NewCreateFileReqBodyBuilder().
			FileType("opus").
			FileName("tts_reply.opus").
			File(io.NopCloser(bytes.NewReader(opusData))).
			Build()).
		Build()

	resp, err := p.client.Im.File.Create(ctx, req)
	if err != nil {
		return "", fmt.Errorf("feishu file create: %w", err)
	}
	if resp == nil || resp.Data == nil || resp.Data.FileKey == nil {
		return "", fmt.Errorf("feishu file create: empty response")
	}
	return *resp.Data.FileKey, nil
}
