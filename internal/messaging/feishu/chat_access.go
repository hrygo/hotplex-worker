package feishu

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/hrygo/hotplex/internal/messaging"
)

// handleChatEntered processes the bot_p2p_chat_entered_v1 event.
// It sends a welcome card to new/returning users and records analytics.
func (a *Adapter) handleChatEntered(ctx context.Context, event *larkim.P2ChatAccessEventBotP2pChatEnteredV1) error {
	if event.Event == nil {
		return nil
	}

	chatID := ptrStr(event.Event.ChatId)
	if chatID == "" {
		return nil
	}
	openID := ptrStr(event.Event.OperatorId.OpenId)
	if openID == "" {
		return nil
	}
	eventID := ""
	if event.EventV2Base != nil && event.EventV2Base.Header != nil {
		eventID = event.EventV2Base.Header.EventID
	}
	if eventID == "" {
		return nil
	}

	var lastMsgMs int64
	if event.Event.LastMessageCreateTime != nil && *event.Event.LastMessageCreateTime != "" {
		if v, err := strconv.ParseInt(*event.Event.LastMessageCreateTime, 10, 64); err == nil {
			lastMsgMs = v
		}
	}

	store := a.chatAccessStore()
	if store == nil {
		return nil
	}

	accessType := store.Classify(ctx, string(messaging.PlatformFeishu), chatID, a.botOpenID, openID, lastMsgMs)

	welcomeSent := false
	if accessType == messaging.ChatAccessNew || accessType == messaging.ChatAccessReturning {
		if err := a.sendWelcomeCard(ctx, chatID, accessType); err != nil {
			a.Log.Warn("feishu: welcome card send failed", "chat", chatID, "err", err)
		} else {
			welcomeSent = true
		}
	}

	inserted, err := store.Record(ctx, messaging.ChatAccessRecord{
		EventID:       eventID,
		Platform:      string(messaging.PlatformFeishu),
		ChatID:        chatID,
		UserID:        openID,
		BotID:         a.botOpenID,
		LastMessageAt: lastMsgMs,
		WelcomeSent:   welcomeSent,
	})
	if err != nil {
		return fmt.Errorf("feishu: chat access record: %w", err)
	}
	if !inserted {
		a.Log.Debug("feishu: duplicate chat_entered event", "event_id", eventID)
	}
	return nil
}

// sendWelcomeCard builds and sends a welcome card to the chat.
func (a *Adapter) sendWelcomeCard(ctx context.Context, chatID string, accessType messaging.ChatAccessType) error {
	category := "welcome"
	if accessType == messaging.ChatAccessReturning {
		category = "welcome_back"
	}

	text := a.phrases.Random(category)
	if text == "" {
		text = "Hi，我是 {bot_name}，你的 AI 编程助手！"
	}
	text = strings.ReplaceAll(text, "{bot_name}", a.resolveBotName())

	body := fmt.Sprintf("%s\n\n我可以帮你：\n• 💻 编写、审查、调试代码\n• 📁 管理项目文件和目录\n• 🔍 搜索代码库和分析架构\n\n快捷命令：/help /reset /cd\n直接发消息即可开始 ✨", text)

	cardJSON := buildCard(
		cardHeader{Title: a.resolveBotName(), Template: headerBlue},
		map[string]any{"wide_screen_mode": true},
		[]map[string]any{{"tag": "markdown", "content": body}},
	)

	_, err := larkCreateMessage(ctx, a.larkClient, chatID, cardJSON)
	return err
}

// chatAccessStore extracts the ChatAccessStore from the adapter extras.
func (a *Adapter) chatAccessStore() *messaging.ChatAccessStore {
	if a.Extras == nil {
		return nil
	}
	s, _ := a.Extras["chat_access_store"].(*messaging.ChatAccessStore)
	return s
}
