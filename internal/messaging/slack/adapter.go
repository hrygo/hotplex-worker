// Package slack provides a Slack Socket Mode platform adapter.
package slack

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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

	log                *slog.Logger
	botToken           string
	appToken           string
	client             *slack.Client
	socketMode         *socketmode.Client
	botID              string
	teamID             string
	bridge             *messaging.Bridge
	dedup              *Dedup
	userCache          *UserCache
	statusMgr          *StatusManager
	activeIndicators   *ActiveIndicators
	typingStages       []TypingStage
	isAssistantCapable atomic.Bool
	assistantEnabled   *bool
	gate               *Gate

	mu            sync.RWMutex
	rateLimiter   *ChannelRateLimiter
	ownership     *ThreadOwnershipTracker
	activeStreams map[string]*NativeStreamingWriter // messageTS -> writer
	activeConns   map[string]*SlackConn             // "channelID#threadTS" -> conn
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

// SetGate sets the access control gate.
func (a *Adapter) SetGate(g *Gate) {
	a.gate = g
}

// SetAssistantEnabled controls whether to attempt native Assistant API.
func (a *Adapter) SetAssistantEnabled(enabled *bool) {
	a.assistantEnabled = enabled
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
	a.teamID = authTest.TeamID

	a.rateLimiter = NewChannelRateLimiter(ctx)
	a.ownership = NewThreadOwnershipTracker(ctx, a.botID, a.log)
	a.dedup = NewDedup(5000, 30*time.Minute)
	a.userCache = NewUserCache(a.client)
	a.statusMgr = NewStatusManager(a, a.log)
	a.activeIndicators = NewActiveIndicators()
	a.typingStages = a.resolveTypingStages()
	a.activeStreams = make(map[string]*NativeStreamingWriter)
	a.activeConns = make(map[string]*SlackConn)

	a.log.Info("slack: starting Socket Mode", "bot_id", a.botID)

	// Async probe for Assistant API capability
	go func() {
		probeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		capable := a.ProbeAssistantCapability(probeCtx)
		a.isAssistantCapable.Store(capable)
		if capable {
			a.log.Info("slack: Assistant API capability confirmed (paid workspace)")
		} else {
			a.log.Info("slack: Assistant API not available, using emoji reaction fallback")
		}
	}()

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
				a.socketMode.Ack(*evt.Request) //nolint:errcheck // Ack must not block event processing
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

	if msgEvent.BotID != "" {
		a.log.Debug("slack: skipping bot message", "bot_id", msgEvent.BotID)
		return
	}

	switch msgEvent.SubType {
	case "message_changed", "message_deleted", "channel_join",
		"channel_leave", "group_join", "group_leave",
		"channel_topic", "channel_purpose":
		return
	}

	if msgEvent.TimeStamp != "" {
		if ts, err := parseSlackTS(msgEvent.TimeStamp); err == nil {
			if time.Since(ts) > 30*time.Minute {
				a.log.Debug("slack: skipping expired message", "ts", msgEvent.TimeStamp)
				return
			}
		}
	}

	channelID := msgEvent.Channel
	threadTS := extractThreadTS(*msgEvent)
	userID := msgEvent.User
	text, ok, media := a.ConvertMessage(*msgEvent)
	if !ok {
		return
	}
	teamID := event.TeamID

	channelType := "channel"
	if channelID != "" && channelID[0] == 'D' {
		channelType = "im"
	}

	// Access control gate (before ownership check; must be before ResolveMentions which strips <@BOTID>)
	if a.gate != nil {
		botMentioned := strings.Contains(text, "<@"+a.botID+">")
		result := a.gate.Check(channelType, userID, botMentioned)
		if !result.Allowed {
			a.log.Debug("slack: gate rejected", "reason", result.Reason, "user", userID)
			return
		}
	}

	// Thread ownership check
	if !a.ownership.ShouldRespond(channelType, threadTS, text, userID) {
		return
	}

	// Dedup
	platformMsgID := msgEvent.ClientMsgID
	if platformMsgID == "" {
		platformMsgID = msgEvent.TimeStamp
	}
	if !a.dedup.TryRecord(platformMsgID) {
		return
	}

	// Resolve user mentions: <@UID> → @DisplayName, remove bot self-mentions
	text = a.userCache.ResolveMentions(ctx, text, a.botID)
	text = strings.TrimSpace(text)

	if text == "" && len(media) == 0 {
		return
	}

	// Download media files and append paths to text
	if len(media) > 0 {
		for _, m := range media {
			if m.DownloadURL == "" {
				continue
			}
			path, err := a.downloadMedia(ctx, m)
			if err == nil {
				text += "\n" + path
			} else {
				a.log.Warn("slack: download media failed", "file", m.Name, "error", err)
				text += fmt.Sprintf("\n[%s: %s]", m.Type, m.Name)
			}
		}
	}

	a.log.Debug("slack: handling message",
		"channel", channelID,
		"thread", threadTS,
		"user", userID,
		"team", teamID,
		"text_len", len(text),
	)

	if IsAbortCommand(text) {
		a.log.Info("slack: abort command received", "channel", channelID)
		return
	}

	// Start typing indicator (emoji fallback for free workspaces)
	a.activeIndicators.Start(ctx, a, channelID, threadTS, msgEvent.TimeStamp, a.typingStages)

	// Set initial assistant status (native API for paid workspaces)
	if a.isAssistantCapable.Load() && threadTS != "" {
		_ = a.SetAssistantStatus(ctx, channelID, threadTS, "Initializing...")
	}

	if err := a.HandleTextMessage(ctx, platformMsgID, channelID, teamID, threadTS, userID, text); err != nil {
		a.log.Error("slack: handle message failed", "error", err)
	}
}

// GetOrCreateConn returns an existing SlackConn for the channel/thread pair,
// or creates and registers a new one. This ensures the same conn is reused
// across multiple messages in the same thread, so Hub.Shutdown can close it.
// resolveTypingStages converts config TypingStageConfig to TypingStage,
// falling back to DefaultStages when no custom config is provided.
func (a *Adapter) resolveTypingStages() []TypingStage {
	if len(a.typingStages) == 0 {
		return DefaultStages
	}
	return a.typingStages
}

// SetTypingStages sets custom emoji progress stages from config.
func (a *Adapter) SetTypingStages(stages []TypingStage) {
	a.typingStages = stages
}

func (a *Adapter) GetOrCreateConn(channelID, threadTS string) *SlackConn {
	key := channelID + "#" + threadTS
	a.mu.Lock()
	defer a.mu.Unlock()
	if c, ok := a.activeConns[key]; ok {
		return c
	}
	c := NewSlackConn(a, channelID, threadTS)
	a.activeConns[key] = c
	return c
}

func (a *Adapter) HandleTextMessage(ctx context.Context, platformMsgID, channelID, teamID, threadTS, userID, text string) error {
	if a.bridge == nil {
		return nil
	}

	envelope := a.bridge.MakeSlackEnvelope(teamID, channelID, threadTS, userID, text)
	if envelope == nil {
		return fmt.Errorf("slack: failed to build envelope")
	}

	conn := a.GetOrCreateConn(channelID, threadTS)
	return a.bridge.Handle(ctx, envelope, conn)
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
	for _, c := range a.activeConns {
		_ = c.Close()
	}
	a.activeConns = nil
	a.mu.Unlock()

	if a.rateLimiter != nil {
		a.rateLimiter.Stop()
	}
	if a.ownership != nil {
		a.ownership.Stop()
	}
	if a.dedup != nil {
		a.dedup.Close()
	}

	return nil
}

// SlackConn wraps the adapter with channel/thread routing info
// to satisfy messaging.PlatformConn for Hub.JoinPlatformSession.
type SlackConn struct {
	adapter   *Adapter
	channelID string
	threadTS  string
	messageTS string // anchor message for typing indicator cleanup
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

	// Status update: map AEP event to status indicator
	if status, text := aepEventToStatus(env); text != "" {
		_ = c.adapter.statusMgr.Notify(ctx, c.channelID, c.threadTS, status, text)
	}

	// Clear status on done/error
	switch env.Event.Type {
	case events.Done, events.Error:
		_ = c.adapter.statusMgr.Clear(ctx, c.channelID, c.threadTS)
		c.adapter.activeIndicators.Stop(ctx, c.channelID, c.messageTS)
		return nil
	}

	text, ok := extractResponseText(env)
	if !ok {
		return nil
	}

	opts := []slack.MsgOption{slack.MsgOptionText(FormatMrkdwn(text), false)}
	if c.threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(c.threadTS))
	}
	_, _, err := c.adapter.client.PostMessageContext(ctx, c.channelID, opts...)
	return err
}

// Close removes the conn from the adapter registry. Streaming writers are closed
// separately by the adapter's Close method.
func (c *SlackConn) Close() error {
	key := c.channelID + "#" + c.threadTS
	c.adapter.mu.Lock()
	delete(c.adapter.activeConns, key)
	c.adapter.mu.Unlock()
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
