package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkcardkit "github.com/larksuite/oapi-sdk-go/v3/service/cardkit/v1"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

type CardPhase int32

const (
	PhaseIdle CardPhase = iota
	PhaseCreating
	PhaseStreaming
	PhaseCompleted
	PhaseAborted
	PhaseTerminated
	PhaseCreationFailed
)

var phaseTransitions = map[CardPhase]map[CardPhase]bool{
	PhaseIdle:           {PhaseCreating: true},
	PhaseCreating:       {PhaseStreaming: true, PhaseCreationFailed: true, PhaseTerminated: true},
	PhaseStreaming:      {PhaseCompleted: true, PhaseAborted: true, PhaseTerminated: true},
	PhaseCompleted:      {},
	PhaseAborted:        {},
	PhaseTerminated:     {},
	PhaseCreationFailed: {},
}

func (p CardPhase) String() string {
	switch p {
	case PhaseIdle:
		return "idle"
	case PhaseCreating:
		return "creating"
	case PhaseStreaming:
		return "streaming"
	case PhaseCompleted:
		return "completed"
	case PhaseAborted:
		return "aborted"
	case PhaseTerminated:
		return "terminated"
	case PhaseCreationFailed:
		return "creation_failed"
	default:
		return fmt.Sprintf("unknown(%d)", p)
	}
}

type StreamingCardController struct {
	phase     atomic.Int32
	cardID    string
	elementID string
	msgID     string
	sequence  atomic.Int64

	mu          sync.Mutex
	buf         strings.Builder
	lastFlushed string
	cardKitOK   bool

	limiter *FeishuRateLimiter
	client  *lark.Client
	log     *slog.Logger
}

const streamingElementID = "streaming_content"

func NewStreamingCardController(client *lark.Client, limiter *FeishuRateLimiter, log *slog.Logger) *StreamingCardController {
	var p atomic.Int32
	p.Store(int32(PhaseIdle))
	return &StreamingCardController{
		limiter:   limiter,
		client:    client,
		log:       log,
		cardKitOK: true,
		elementID: streamingElementID,
	}
}

func (c *StreamingCardController) getPhase() CardPhase {
	return CardPhase(c.phase.Load())
}

func (c *StreamingCardController) transition(to CardPhase) bool {
	for {
		current := CardPhase(c.phase.Load())
		allowed, exists := phaseTransitions[current][to]
		if !exists || !allowed {
			return false
		}
		if c.phase.CompareAndSwap(int32(current), int32(to)) {
			c.log.Debug("feishu: streaming card phase transition",
				"from", current, "to", to)
			return true
		}
	}
}

func (c *StreamingCardController) EnsureCard(ctx context.Context, chatID string) error {
	if !c.transition(PhaseCreating) {
		return fmt.Errorf("feishu: cannot transition from %s to creating", c.getPhase())
	}

	var cardID string
	var err error

	cardID, err = c.createCard(ctx)
	if err != nil {
		c.log.Warn("feishu: cardkit create failed, degrading to static",
			"error", err)
		if c.transition(PhaseCreationFailed) {
			return fmt.Errorf("feishu: cardkit create failed: %w", err)
		}
		return err
	}

	c.mu.Lock()
	c.cardID = cardID
	c.mu.Unlock()

	msgID, err := c.sendCardMessage(ctx, chatID, cardID)
	if err != nil {
		c.log.Warn("feishu: send card message failed, degrading to static",
			"error", err)
		c.cardKitOK = false
		if c.transition(PhaseCreationFailed) {
			return fmt.Errorf("feishu: send card message failed: %w", err)
		}
		return err
	}

	c.mu.Lock()
	c.msgID = msgID
	c.mu.Unlock()

	if err := c.enableStreaming(ctx); err != nil {
		c.log.Warn("feishu: enable streaming failed, using IM patch fallback",
			"error", err)
		c.cardKitOK = false
	}

	if !c.transition(PhaseStreaming) {
		return fmt.Errorf("feishu: cannot transition to streaming")
	}
	return nil
}

func (c *StreamingCardController) Write(text string) error {
	c.mu.Lock()
	c.buf.WriteString(text)
	c.mu.Unlock()
	return nil
}

func (c *StreamingCardController) Flush(ctx context.Context) error {
	c.mu.Lock()
	content := c.buf.String()
	c.mu.Unlock()

	if content == c.lastFlushed {
		return nil
	}

	seq := int(c.sequence.Add(1))

	if c.cardKitOK && c.limiter.AllowCardKit(c.cardID) {
		if err := c.flushCardKit(ctx, content, seq); err != nil {
			if isCardRateLimitError(err) {
				c.log.Debug("feishu: cardkit rate limited, skipping frame")
				return nil
			}
			if isCardTableLimitError(err) {
				c.log.Warn("feishu: cardkit table limit exceeded, disabling streaming")
				c.cardKitOK = false
				return nil
			}
			c.log.Warn("feishu: cardkit flush failed, falling back to IM patch",
				"error", err)
			c.cardKitOK = false
		} else {
			c.mu.Lock()
			c.lastFlushed = content
			c.mu.Unlock()
			return nil
		}
	}

	if c.msgID != "" && c.limiter.AllowPatch(c.msgID) {
		if err := c.flushIMPatch(ctx, content); err != nil {
			c.log.Warn("feishu: IM patch flush failed", "error", err)
			return err
		}
		c.mu.Lock()
		c.lastFlushed = content
		c.mu.Unlock()
	}

	return nil
}

func (c *StreamingCardController) Close(ctx context.Context) error {
	c.mu.Lock()
	content := c.buf.String()
	c.mu.Unlock()

	if c.cardKitOK && c.cardID != "" {
		seq := int(c.sequence.Add(1))

		if err := c.flushCardKit(ctx, content, seq); err != nil {
			c.log.Warn("feishu: final cardkit flush failed", "error", err)
		}

		if err := c.disableStreaming(ctx); err != nil {
			c.log.Warn("feishu: disable streaming failed", "error", err)
		}
	}

	c.transition(PhaseCompleted)
	return nil
}

func (c *StreamingCardController) Abort(ctx context.Context) error {
	if !c.transition(PhaseAborted) {
		return nil
	}

	c.mu.Lock()
	cardID := c.cardID
	msgID := c.msgID
	c.mu.Unlock()

	if cardID != "" && c.cardKitOK {
		_ = c.disableStreaming(ctx)
	}

	if msgID != "" {
		c.sendAbortMessage(ctx, msgID)
	}

	return nil
}

func (c *StreamingCardController) createCard(ctx context.Context) (string, error) {
	cardData := map[string]any{
		"config": map[string]any{
			"streaming_mode": true,
		},
		"elements": []any{
			map[string]any{
				"tag":        "markdown",
				"element_id": streamingElementID,
				"content":    "Thinking...",
			},
		},
	}
	dataJSON, _ := json.Marshal(cardData)
	dataStr := string(dataJSON)

	body := larkcardkit.NewCreateCardReqBodyBuilder().
		Type("card_json").
		Data(dataStr).
		Build()

	req := larkcardkit.NewCreateCardReqBuilder().
		Body(body).
		Build()

	resp, err := c.client.Cardkit.V1.Card.Create(ctx, req)
	if err != nil {
		return "", fmt.Errorf("cardkit create: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("cardkit create failed: code=%d msg=%s", resp.Code, resp.Msg)
	}

	if resp.Data == nil || resp.Data.CardId == nil {
		return "", fmt.Errorf("cardkit create: missing card_id in response")
	}
	return *resp.Data.CardId, nil
}

func (c *StreamingCardController) sendCardMessage(ctx context.Context, chatID, cardID string) (string, error) {
	cardContent := map[string]any{
		"config": map[string]any{
			"streaming_mode": true,
		},
		"elements": []any{
			map[string]any{
				"tag":        "markdown",
				"element_id": streamingElementID,
				"content":    "Thinking...",
			},
		},
	}
	contentJSON, _ := json.Marshal(cardContent)

	body := larkim.NewCreateMessageReqBodyBuilder().
		ReceiveId(chatID).
		MsgType("interactive").
		Content(string(contentJSON)).
		Build()

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(body).
		Build()

	resp, err := c.client.Im.V1.Message.Create(ctx, req)
	if err != nil {
		return "", fmt.Errorf("im message create: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("im message create failed: code=%d msg=%s", resp.Code, resp.Msg)
	}

	if resp.Data == nil || resp.Data.MessageId == nil {
		return "", fmt.Errorf("im message create: missing message_id in response")
	}
	return *resp.Data.MessageId, nil
}

func (c *StreamingCardController) enableStreaming(ctx context.Context) error {
	settingsJSON, _ := json.Marshal(map[string]any{
		"streaming_mode": true,
	})

	body := larkcardkit.NewSettingsCardReqBodyBuilder().
		Settings(string(settingsJSON)).
		Sequence(int(c.sequence.Add(1))).
		Build()

	req := larkcardkit.NewSettingsCardReqBuilder().
		CardId(c.cardID).
		Body(body).
		Build()

	resp, err := c.client.Cardkit.V1.Card.Settings(ctx, req)
	if err != nil {
		return fmt.Errorf("cardkit settings enable streaming: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("cardkit settings enable streaming failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

func (c *StreamingCardController) disableStreaming(ctx context.Context) error {
	settingsJSON, _ := json.Marshal(map[string]any{
		"streaming_mode": false,
	})

	body := larkcardkit.NewSettingsCardReqBodyBuilder().
		Settings(string(settingsJSON)).
		Sequence(int(c.sequence.Add(1))).
		Build()

	req := larkcardkit.NewSettingsCardReqBuilder().
		CardId(c.cardID).
		Body(body).
		Build()

	resp, err := c.client.Cardkit.V1.Card.Settings(ctx, req)
	if err != nil {
		return fmt.Errorf("cardkit settings disable streaming: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("cardkit settings disable streaming failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

func (c *StreamingCardController) flushCardKit(ctx context.Context, content string, seq int) error {
	uuid := fmt.Sprintf("feishu-stream-%d", time.Now().UnixNano())

	body := larkcardkit.NewContentCardElementReqBodyBuilder().
		Content(content).
		Sequence(seq).
		Uuid(uuid).
		Build()

	req := larkcardkit.NewContentCardElementReqBuilder().
		CardId(c.cardID).
		ElementId(c.elementID).
		Body(body).
		Build()

	resp, err := c.client.Cardkit.V1.CardElement.Content(ctx, req)
	if err != nil {
		return fmt.Errorf("cardkit element content: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("cardkit element content failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

func (c *StreamingCardController) flushIMPatch(ctx context.Context, content string) error {
	cardContent := map[string]any{
		"config": map[string]any{},
		"elements": []any{
			map[string]any{
				"tag":     "markdown",
				"content": content,
			},
		},
	}
	contentJSON, _ := json.Marshal(cardContent)

	body := larkim.NewPatchMessageReqBodyBuilder().
		Content(string(contentJSON)).
		Build()

	req := larkim.NewPatchMessageReqBuilder().
		MessageId(c.msgID).
		Body(body).
		Build()

	resp, err := c.client.Im.V1.Message.Patch(ctx, req)
	if err != nil {
		return fmt.Errorf("im message patch: %w", err)
	}
	if !resp.Success() {
		return fmt.Errorf("im message patch failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

func (c *StreamingCardController) sendAbortMessage(ctx context.Context, msgID string) {
	_ = c.adapterReplyMessage(ctx, msgID, "_Aborted._")
}

func (c *StreamingCardController) adapterReplyMessage(ctx context.Context, msgID, text string) error {
	textJSON, _ := json.Marshal(map[string]string{"text": text})
	body := larkim.NewReplyMessageReqBodyBuilder().
		MsgType(larkim.MsgTypeText).
		Content(string(textJSON)).
		Build()
	req := larkim.NewReplyMessageReqBuilder().
		MessageId(msgID).
		Body(body).
		Build()
	resp, err := c.client.Im.V1.Message.Reply(ctx, req)
	if err != nil {
		return err
	}
	if !resp.Success() {
		return fmt.Errorf("reply failed: code=%d msg=%s", resp.Code, resp.Msg)
	}
	return nil
}

func isCardRateLimitError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "230020")
}

func isCardTableLimitError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "230099") || strings.Contains(err.Error(), "11310")
}
