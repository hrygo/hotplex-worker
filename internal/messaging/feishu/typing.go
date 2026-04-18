package feishu

import (
	"context"
	"fmt"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

const typingEmoji = "Typing"

func (a *Adapter) AddTypingIndicator(ctx context.Context, messageID string) (string, error) {
	if a.larkClient == nil {
		return "", fmt.Errorf("feishu: lark client not initialized")
	}

	body := larkim.NewCreateMessageReactionReqBodyBuilder().
		ReactionType(larkim.NewEmojiBuilder().EmojiType(typingEmoji).Build()).
		Build()

	req := larkim.NewCreateMessageReactionReqBuilder().
		MessageId(messageID).
		Body(body).
		Build()

	resp, err := a.larkClient.Im.V1.MessageReaction.Create(ctx, req)
	if err != nil {
		return "", fmt.Errorf("feishu: add typing indicator: %w", err)
	}

	if !resp.Success() {
		return "", fmt.Errorf("feishu: add typing indicator failed: code=%d msg=%s", resp.Code, resp.Msg)
	}

	a.log.Debug("feishu: added typing indicator", "msg", messageID)

	reactionID := ""
	if resp.Data != nil && resp.Data.ReactionId != nil {
		reactionID = *resp.Data.ReactionId
	}
	return reactionID, nil
}

func (a *Adapter) RemoveTypingIndicator(ctx context.Context, messageID, reactionID string) error {
	if a.larkClient == nil {
		return fmt.Errorf("feishu: lark client not initialized")
	}
	if reactionID == "" {
		return nil
	}

	req := larkim.NewDeleteMessageReactionReqBuilder().
		MessageId(messageID).
		ReactionId(reactionID).
		Build()

	resp, err := a.larkClient.Im.V1.MessageReaction.Delete(ctx, req)
	if err != nil {
		return fmt.Errorf("feishu: remove typing indicator: %w", err)
	}

	if !resp.Success() {
		a.log.Warn("feishu: remove typing indicator failed",
			"msg", messageID, "code", resp.Code, "msg", resp.Msg)
	}

	a.log.Debug("feishu: removed typing indicator", "msg", messageID)
	return nil
}
