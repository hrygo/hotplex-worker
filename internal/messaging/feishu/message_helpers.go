package feishu

import (
	"context"
	"fmt"
	"time"

	lark "github.com/larksuite/oapi-sdk-go/v3"
	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

const (
	messageExpiry = 30 * time.Minute
)

// IsMessageExpired checks if a message's create time is beyond the expiry threshold.
func IsMessageExpired(createTimeMs int64) bool {
	if createTimeMs <= 0 {
		return false
	}
	return time.Since(time.UnixMilli(createTimeMs)) > messageExpiry
}

// larkCreateMessage sends a new message via Lark IM Create API and returns the message ID.
func larkCreateMessage(ctx context.Context, client *lark.Client, chatID, content string) (string, error) {
	body := larkim.NewCreateMessageReqBodyBuilder().
		ReceiveId(chatID).
		MsgType(larkim.MsgTypeInteractive).
		Content(content).
		Build()

	req := larkim.NewCreateMessageReqBuilder().
		ReceiveIdType(larkim.ReceiveIdTypeChatId).
		Body(body).
		Build()

	resp, err := client.Im.Message.Create(ctx, req)
	if err != nil {
		return "", fmt.Errorf("im message create: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("im message create failed: code=%d msg=%s", resp.Code, resp.Msg)
	}

	if resp.Data == nil || resp.Data.MessageId == nil {
		return "", nil
	}
	return *resp.Data.MessageId, nil
}

// larkReplyMessage replies to a message via Lark IM Reply API and returns the message ID.
func larkReplyMessage(ctx context.Context, client *lark.Client, messageID, content string) (string, error) {
	body := larkim.NewReplyMessageReqBodyBuilder().
		MsgType(larkim.MsgTypeInteractive).
		Content(content).
		Build()

	req := larkim.NewReplyMessageReqBuilder().
		MessageId(messageID).
		Body(body).
		Build()

	resp, err := client.Im.Message.Reply(ctx, req)
	if err != nil {
		return "", fmt.Errorf("im message reply: %w", err)
	}
	if !resp.Success() {
		return "", fmt.Errorf("im message reply failed: code=%d msg=%s", resp.Code, resp.Msg)
	}

	if resp.Data == nil || resp.Data.MessageId == nil {
		return "", nil
	}
	return *resp.Data.MessageId, nil
}
