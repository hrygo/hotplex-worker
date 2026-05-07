package slack

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"

	"golang.org/x/sync/semaphore"

	"github.com/hrygo/hotplex/internal/brain"
	"github.com/hrygo/hotplex/internal/messaging/tts"

	slack "github.com/slack-go/slack"
)

// TTSPipeline processes AI responses into voice messages for Slack:
// full text → LLM summary → Edge TTS → MP3 → FFmpeg Opus → Slack file upload.
type TTSPipeline struct {
	synthesizer tts.Synthesizer
	client      *slack.Client
	maxChars    int
	sem         *semaphore.Weighted
	log         *slog.Logger
}

func NewTTSPipeline(synthesizer tts.Synthesizer, client *slack.Client, maxChars int, log *slog.Logger) *TTSPipeline {
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
		summary = tts.TruncateText(fullText, p.maxChars)
	}
	if summary == "" {
		return
	}

	mp3Data, err := p.synthesizer.Synthesize(ctx, summary)
	if err != nil {
		p.log.Warn("tts: synthesis failed", "err", err)
		return
	}

	opusData, err := tts.MP3ToOpus(ctx, mp3Data)
	if err != nil {
		p.log.Warn("tts: mp3→opus conversion failed", "err", err)
		return
	}

	duration := tts.EstimateAudioDuration(len(opusData))
	if err := p.uploadAndSend(ctx, channelID, threadTS, opusData); err != nil {
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
	capped := tts.TruncateText(fullText, p.maxChars*5)
	prompt := fmt.Sprintf(ttsSummaryPrompt, p.maxChars, capped)
	result, err := b.Chat(ctx, prompt)
	if err != nil {
		return "", fmt.Errorf("brain chat: %w", err)
	}
	return strings.TrimSpace(result), nil
}

func (p *TTSPipeline) uploadAndSend(ctx context.Context, channelID, threadTS string, opusData []byte) error {
	params := slack.UploadFileParameters{
		Filename: "tts_reply.opus",
		Title:    "Voice Reply",
		Reader:   bytes.NewReader(opusData),
		FileSize: len(opusData),
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
