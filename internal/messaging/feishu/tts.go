package feishu

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"golang.org/x/sync/semaphore"

	"github.com/hrygo/hotplex/internal/brain"
	"github.com/hrygo/hotplex/internal/messaging/tts"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// TTSPipeline processes AI responses into voice messages:
// full text → LLM summary → Edge TTS → MP3 → FFmpeg Opus → Feishu audio message.
type TTSPipeline struct {
	synthesizer tts.Synthesizer
	client      *lark.Client
	maxChars    int
	sem         *semaphore.Weighted
	log         *slog.Logger
}

func NewTTSPipeline(synthesizer tts.Synthesizer, client *lark.Client, maxChars int, log *slog.Logger) *TTSPipeline {
	if maxChars <= 0 {
		maxChars = 2000
	}
	return &TTSPipeline{
		synthesizer: synthesizer,
		client:      client,
		maxChars:    maxChars,
		sem:         semaphore.NewWeighted(2),
		log:         log,
	}
}

const ttsSummaryPrompt = `将以下 AI 助手的回复转换为适合语音播报的自然语言。
规则：
- 跳过所有代码块和技术细节，用简短描述替代（如"已提供代码实现"）
- 跳过表格，概括为文字描述
- 跳过 URL 链接
- 保留核心结论和关键信息
- 使用口语化表达，避免书面语
- 控制在 %d 字符以内

AI 回复：
%s`

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
		summary = tts.TruncateText(fullText, p.maxChars)
	}
	if summary == "" {
		return
	}

	// 2. Edge TTS → MP3
	mp3Data, err := p.synthesizer.Synthesize(ctx, summary)
	if err != nil {
		p.log.Warn("tts: synthesis failed", "err", err)
		return
	}

	// 3. FFmpeg MP3 → Opus
	opusData, err := tts.MP3ToOpus(ctx, mp3Data)
	if err != nil {
		p.log.Warn("tts: mp3→opus conversion failed", "err", err)
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
	b := brain.Global()
	if b == nil {
		return "", fmt.Errorf("brain not initialized")
	}
	// Cap input to avoid blowing up the prompt — 5x maxChars gives the LLM enough context.
	capped := tts.TruncateText(fullText, p.maxChars*5)
	prompt := fmt.Sprintf(ttsSummaryPrompt, p.maxChars, capped)
	result, err := b.Chat(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("brain chat: %w", err)
	}
	return strings.TrimSpace(result), nil
}

func (p *TTSPipeline) sendAudio(ctx context.Context, chatID, replyToMsgID string, opusData []byte) error {
	fileKey, err := p.uploadAudio(ctx, opusData)
	if err != nil {
		return fmt.Errorf("upload audio: %w", err)
	}

	msgContent := fmt.Sprintf(`{"file_key":%q}`, fileKey)
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
