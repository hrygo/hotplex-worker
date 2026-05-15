package slack

import (
	"context"
	"fmt"
	"strings"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"

	"github.com/hrygo/hotplex/internal/messaging"
)

// handleAppHomeOpened processes the app_home_opened event.
// When tab=="messages" it mirrors Feishu's bot_p2p_chat_entered_v1.
func (a *Adapter) handleAppHomeOpened(ctx context.Context, event *slackevents.AppHomeOpenedEvent) {
	if event.User == "" || event.Channel == "" {
		return
	}

	// Only trigger on the messages tab (the DM conversation view).
	if event.Tab != "messages" {
		return
	}

	store := a.chatAccessStore()
	if store == nil {
		return
	}

	eventID := fmt.Sprintf("app_home_opened_%s_%s_%s", event.User, event.Channel, event.EventTimeStamp)
	accessType := store.Classify(ctx, string(messaging.PlatformSlack), event.Channel, a.botID, event.User, 0)

	welcomeSent := false
	if accessType == messaging.ChatAccessNew || accessType == messaging.ChatAccessReturning {
		if err := a.sendWelcomeMessage(ctx, event.Channel, accessType); err != nil {
			a.Log.Warn("slack: welcome message send failed", "channel", event.Channel, "err", err)
		} else {
			welcomeSent = true
		}
	}

	inserted, err := store.Record(ctx, messaging.ChatAccessRecord{
		EventID:     eventID,
		Platform:    string(messaging.PlatformSlack),
		ChatID:      event.Channel,
		UserID:      event.User,
		BotID:       a.botID,
		WelcomeSent: welcomeSent,
	})
	if err != nil {
		a.Log.Warn("slack: chat access record failed", "err", err)
	}
	if !inserted {
		a.Log.Debug("slack: duplicate app_home_opened event", "event_id", eventID)
	}
}

// sendWelcomeMessage posts a Block Kit welcome message to the DM channel.
func (a *Adapter) sendWelcomeMessage(ctx context.Context, channelID string, accessType messaging.ChatAccessType) error {
	category := "welcome"
	if accessType == messaging.ChatAccessReturning {
		category = "welcome_back"
	}

	text := a.phrases.Random(category)
	if text == "" {
		text = "Hi，我是 {bot_name}，你的 AI 编程助手！"
	}
	botName := "HotPlex"
	text = strings.ReplaceAll(text, "{bot_name}", botName)

	body := fmt.Sprintf("%s\n\n我可以帮你：\n• 💻 编写、审查、调试代码\n• 📁 管理项目文件和目录\n• 🔍 搜索代码库和分析架构", text)

	blocks := []slack.Block{
		&slack.SectionBlock{
			Type: slack.MBTSection,
			Text: &slack.TextBlockObject{Type: "mrkdwn", Text: body},
		},
		slack.NewDividerBlock(),
		slack.NewContextBlock("", &slack.TextBlockObject{Type: "mrkdwn", Text: "快捷命令：`/help` `/reset` `/cd`  ·  直接发消息即可开始 ✨"}),
	}

	_, _, err := a.client.PostMessageContext(ctx, channelID,
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionText(text, false),
	)
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
