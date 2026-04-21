package feishu

import (
	"context"
	"fmt"
	"time"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

const (
	typingEmoji = "Typing"
)

// timelineEmojis maps elapsed duration thresholds to Feishu emoji_type values.
// The emoji reflects how long the bot has been processing, giving users a
// visual sense of progress without reading logs.
var timelineEmojis = []struct {
	threshold time.Duration
	emoji     string
}{
	{0, "YEAH"},                       // 耶
	{10 * time.Second, "SMILE"},       // 呲牙
	{30 * time.Second, "THINKING"},    // 思考
	{1 * time.Minute, "SMUG"},         // 得意
	{5 * time.Minute, "STRIVE"},       // 奋斗
	{10 * time.Minute, "BLACKFACE"},   // 黑线
	{15 * time.Minute, "NOSEPICK"},    // 抠鼻
	{20 * time.Minute, "EMBARRASSED"}, // 尬笑
	{25 * time.Minute, "WAIL"},        // 泪奔
	{30 * time.Minute, "DIZZY"},       // 晕
}

// timelineEmoji returns the emoji_type for the given elapsed duration.
func timelineEmoji(elapsed time.Duration) string {
	result := ""
	for _, t := range timelineEmojis {
		if elapsed >= t.threshold {
			result = t.emoji
		}
	}
	// No match means < 0 (impossible), but guard anyway.
	if result == "" {
		return timelineEmojis[0].emoji
	}
	return result
}

// addReaction adds an emoji reaction to a message. Returns the reaction ID.
func (a *Adapter) addReaction(ctx context.Context, messageID, emoji string) (string, error) {
	if a.larkClient == nil {
		return "", fmt.Errorf("feishu: lark client not initialized")
	}

	body := larkim.NewCreateMessageReactionReqBodyBuilder().
		ReactionType(larkim.NewEmojiBuilder().EmojiType(emoji).Build()).
		Build()

	req := larkim.NewCreateMessageReactionReqBuilder().
		MessageId(messageID).
		Body(body).
		Build()

	resp, err := a.larkClient.Im.V1.MessageReaction.Create(ctx, req)
	if err != nil {
		return "", fmt.Errorf("feishu: add reaction %s: %w", emoji, err)
	}

	if !resp.Success() {
		return "", fmt.Errorf("feishu: add reaction %s failed: code=%d msg=%s", emoji, resp.Code, resp.Msg)
	}

	a.log.Debug("feishu: added reaction", "emoji", emoji, "msg", messageID)

	reactionID := ""
	if resp.Data != nil && resp.Data.ReactionId != nil {
		reactionID = *resp.Data.ReactionId
	}
	return reactionID, nil
}

// removeReaction removes an emoji reaction from a message.
func (a *Adapter) removeReaction(ctx context.Context, messageID, reactionID string) error {
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
		return fmt.Errorf("feishu: remove reaction: %w", err)
	}

	if !resp.Success() {
		a.log.Warn("feishu: remove reaction failed",
			"msg", messageID, "code", resp.Code, "msg", resp.Msg)
	}

	a.log.Debug("feishu: removed reaction", "msg", messageID)
	return nil
}

// AddTypingIndicator adds a Typing emoji reaction to indicate the bot is processing.
func (a *Adapter) AddTypingIndicator(ctx context.Context, messageID string) (string, error) {
	return a.addReaction(ctx, messageID, typingEmoji)
}

// RemoveTypingIndicator removes a previously added Typing reaction.
func (a *Adapter) RemoveTypingIndicator(ctx context.Context, messageID, reactionID string) error {
	return a.removeReaction(ctx, messageID, reactionID)
}
