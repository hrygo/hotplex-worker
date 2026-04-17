// Package slack provides a Slack Socket Mode platform adapter.
package slack

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/hotplex/hotplex-worker/internal/messaging"
	"github.com/hotplex/hotplex-worker/pkg/events"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

func init() {
	messaging.Register(messaging.PlatformSlack, func(log *slog.Logger) messaging.PlatformAdapterInterface {
		return &Adapter{log: log}
	})
}

// Adapter implements messaging.PlatformAdapterInterface for Slack Socket Mode.
type Adapter struct {
	messaging.PlatformAdapter

	log        *slog.Logger
	botToken   string
	appToken   string
	client     *slack.Client
	socketMode *socketmode.Client
	botID      string
	bridge     *messaging.Bridge

	mu            sync.RWMutex
	rateLimiter   *ChannelRateLimiter
	ownership     *ThreadOwnershipTracker
	activeStreams map[string]*NativeStreamingWriter // messageTS -> writer
}

func (a *Adapter) Platform() messaging.PlatformType { return messaging.PlatformSlack }

// Configure sets tokens and bridge before Start.
func (a *Adapter) Configure(botToken, appToken string, bridge *messaging.Bridge) {
	a.botToken = botToken
	a.appToken = appToken
	a.bridge = bridge
}

// SetBridge stores the bridge for later use.
func (a *Adapter) SetBridge(b *messaging.Bridge) {
	a.bridge = b
}

func (a *Adapter) Start(ctx context.Context) error {
	if a.botToken == "" || a.appToken == "" {
		return fmt.Errorf("slack: botToken and appToken required")
	}

	a.client = slack.New(a.botToken, slack.OptionAppLevelToken(a.appToken))
	a.socketMode = socketmode.New(a.client)

	// Fetch bot identity
	authTest, err := a.client.AuthTestContext(ctx)
	if err != nil {
		return fmt.Errorf("slack: auth test: %w", err)
	}
	a.botID = authTest.UserID

	a.rateLimiter = NewChannelRateLimiter(ctx)
	a.ownership = NewThreadOwnershipTracker(ctx, a.botID, a.log)
	a.activeStreams = make(map[string]*NativeStreamingWriter)

	a.log.Info("slack: starting Socket Mode", "bot_id", a.botID)

	go a.runSocketMode(ctx)
	return nil
}

func (a *Adapter) runSocketMode(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-a.socketMode.Events:
			if !ok {
				return
			}
			switch evt.Type {
			case socketmode.EventTypeEventsAPI:
				eventsAPI, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}
				a.socketMode.Ack(*evt.Request)
				a.handleEventsAPI(ctx, eventsAPI)

			case socketmode.EventTypeConnecting:
				a.log.Info("slack: connecting to Slack API")

			case socketmode.EventTypeConnectionError:
				a.log.Warn("slack: connection error", "error", evt.Data)
			}
		}
	}
}

func (a *Adapter) handleEventsAPI(ctx context.Context, event slackevents.EventsAPIEvent) {
	msgEvent, ok := event.InnerEvent.Data.(*slackevents.MessageEvent)
	if !ok {
		return
	}

	// Skip own bot messages
	if msgEvent.BotID == a.botID {
		return
	}

	channelID := msgEvent.Channel
	threadTS := extractThreadTS(*msgEvent)
	userID := msgEvent.User
	text := extractText(*msgEvent)

	if text == "" {
		return
	}

	// Thread ownership check
	channelType := "channel"
	if len(channelID) > 0 && channelID[0] == 'D' {
		channelType = "im"
	}
	if !a.ownership.ShouldRespond(channelType, threadTS, text, userID) {
		return
	}

	// Dedup
	platformMsgID := msgEvent.ClientMsgID
	if platformMsgID == "" {
		platformMsgID = msgEvent.TimeStamp
	}

	a.log.Debug("slack: handling message",
		"channel", channelID,
		"thread", threadTS,
		"user", userID,
		"text_len", len(text),
	)

	if err := a.HandleTextMessage(ctx, platformMsgID, channelID, userID, text); err != nil {
		a.log.Error("slack: handle message failed", "error", err)
	}
}

func (a *Adapter) HandleTextMessage(ctx context.Context, platformMsgID, channelID, userID, text string) error {
	if a.bridge == nil {
		return nil
	}

	envelope := a.bridge.MakeSlackEnvelope("", channelID, "", userID, text)
	if envelope == nil {
		return fmt.Errorf("slack: failed to build envelope")
	}

	return a.bridge.Handle(ctx, envelope)
}

// NewStreamingWriter creates a streaming writer for the given channel/thread.
func (a *Adapter) NewStreamingWriter(ctx context.Context, channelID, threadTS string, onComplete func(string)) *NativeStreamingWriter {
	w := NewNativeStreamingWriter(ctx, a.client, channelID, threadTS, a.rateLimiter, func(ts string) {
		a.mu.Lock()
		delete(a.activeStreams, ts)
		a.mu.Unlock()
		if onComplete != nil {
			onComplete(ts)
		}
	})
	a.mu.Lock()
	a.activeStreams[w.messageTS] = w
	a.mu.Unlock()
	return w
}

// Close gracefully terminates the platform connection.
func (a *Adapter) Close(ctx context.Context) error {
	a.log.Info("slack: adapter closing")

	a.mu.Lock()
	for _, w := range a.activeStreams {
		_ = w.Close()
	}
	a.activeStreams = nil
	a.mu.Unlock()

	if a.rateLimiter != nil {
		a.rateLimiter.Stop()
	}
	if a.ownership != nil {
		a.ownership.Stop()
	}

	return nil
}

// SlackConn wraps the adapter with channel/thread routing info
// to satisfy messaging.PlatformConn for Hub.JoinPlatformSession.
type SlackConn struct {
	adapter   *Adapter
	channelID string
	threadTS  string
}

// NewSlackConn creates a platform connection bound to a channel/thread.
func NewSlackConn(adapter *Adapter, channelID, threadTS string) *SlackConn {
	return &SlackConn{adapter: adapter, channelID: channelID, threadTS: threadTS}
}

// WriteCtx sends an AEP envelope to the bound Slack channel/thread.
func (c *SlackConn) WriteCtx(ctx context.Context, env *events.Envelope) error {
	if env == nil {
		return fmt.Errorf("slack: nil envelope")
	}

	text, ok := extractResponseText(env)
	if !ok {
		return nil
	}

	opts := []slack.MsgOption{slack.MsgOptionText(text, false)}
	if c.threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(c.threadTS))
	}
	_, _, err := c.adapter.client.PostMessageContext(ctx, c.channelID, opts...)
	return err
}

// Close ends the streaming writer for this connection.
func (c *SlackConn) Close() error {
	return nil
}

// extractResponseText extracts text content from an AEP event for Slack output.
func extractResponseText(env *events.Envelope) (string, bool) {
	switch env.Event.Type {
	case "text", events.MessageDelta:
		// Try MessageDeltaData
		if d, ok := env.Event.Data.(events.MessageDeltaData); ok {
			return d.Content, d.Content != ""
		}
		// Try map[string]any (JSON-decoded)
		if m, ok := env.Event.Data.(map[string]any); ok {
			if text, ok := m["content"].(string); ok {
				return text, true
			}
			if text, ok := m["text"].(string); ok {
				return text, true
			}
		}
		// Try string data directly
		if text, ok := env.Event.Data.(string); ok {
			return text, true
		}
	case "done":
		return "", false
	case "raw":
		if d, ok := env.Event.Data.(events.RawData); ok {
			if m, ok := d.Raw.(map[string]any); ok {
				if text, ok := m["text"].(string); ok {
					return text, true
				}
			}
		}
	}
	return "", false
}

var _ messaging.PlatformConn = (*SlackConn)(nil)
