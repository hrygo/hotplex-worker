package slack

import (
	"context"
	"fmt"

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
	store.Classify(ctx, string(messaging.PlatformSlack), event.Channel, a.botID, event.User, 0)

	// Welcome message disabled — users prefer silence on app_home_opened.

	inserted, err := store.Record(ctx, messaging.ChatAccessRecord{
		EventID:     eventID,
		Platform:    string(messaging.PlatformSlack),
		ChatID:      event.Channel,
		UserID:      event.User,
		BotID:       a.botID,
		WelcomeSent: false,
	})
	if err != nil {
		a.Log.Warn("slack: chat access record failed", "err", err)
	}
	if !inserted {
		a.Log.Debug("slack: duplicate app_home_opened event", "event_id", eventID)
	}
}

// chatAccessStore extracts the ChatAccessStore from the adapter extras.
func (a *Adapter) chatAccessStore() *messaging.ChatAccessStore {
	if a.Extras == nil {
		return nil
	}
	s, _ := a.Extras["chat_access_store"].(*messaging.ChatAccessStore)
	return s
}
