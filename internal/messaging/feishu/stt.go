package feishu

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkspeech "github.com/larksuite/oapi-sdk-go/v3/service/speech_to_text/v1"

	"github.com/hrygo/hotplex/internal/messaging/stt"
)

// Transcriber converts raw audio bytes to text.
// Implementations may use cloud APIs or local tools.
type Transcriber = stt.Transcriber

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
	pcmData, err := stt.AudioToPCM(ctx, audioData)
	if err != nil {
		return "", fmt.Errorf("feishu stt: %w", err)
	}

	encoded := base64.StdEncoding.EncodeToString(pcmData)
	fileID := stt.RandomAlphaNum(16)

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
