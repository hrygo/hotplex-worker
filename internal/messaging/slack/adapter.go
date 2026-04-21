// Package slack provides a Slack Socket Mode platform adapter.
package slack

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hotplex/hotplex-worker/internal/messaging"
	"github.com/hotplex/hotplex-worker/pkg/aep"
	"github.com/hotplex/hotplex-worker/pkg/events"

	"runtime/debug"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

const (
	messageExpiry    = 30 * time.Minute
	dedupMaxEntries  = 5000
	dedupTTL         = 30 * time.Minute
	mediaPathPrefix  = "/tmp/hotplex/media/slack"
	mediaCleanupInt  = 6 * time.Hour
	mediaTTL         = 24 * time.Hour
	maxMessageLength = 3800            // Slack limit is ~4000
	errPrefix        = "\u26a0\ufe0f " // ⚠️
)

// Subtypes that should never be processed.
var blockedSubtypes = map[string]bool{
	"message_changed": true, "message_deleted": true,
	"channel_join": true, "channel_leave": true,
	"group_join": true, "group_leave": true,
	"channel_topic": true, "channel_purpose": true,
}

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
	started            atomic.Bool
	backoffBaseDelay   time.Duration
	backoffMaxDelay    time.Duration

	mu            sync.RWMutex
	rateLimiter   *ChannelRateLimiter
	slashLimiter  *SlashRateLimiter
	activeStreams map[string]*NativeStreamingWriter // messageTS -> writer
	activeConns   map[string]*SlackConn             // "channelID#threadTS" -> conn
	interactions  *messaging.InteractionManager
	closed        atomic.Bool
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

// SetReconnectDelays configures the exponential backoff delays for reconnection.
func (a *Adapter) SetReconnectDelays(baseDelay, maxDelay time.Duration) {
	a.backoffBaseDelay = baseDelay
	a.backoffMaxDelay = maxDelay
}

func (a *Adapter) Start(ctx context.Context) error {
	if !a.started.CompareAndSwap(false, true) {
		a.log.Warn("slack: adapter already started, skipping")
		return nil
	}
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
	a.slashLimiter = NewSlashRateLimiter()
	a.dedup = NewDedup(dedupMaxEntries, dedupTTL)
	a.userCache = NewUserCache(a.client)
	a.statusMgr = NewStatusManager(a, a.log)
	a.activeIndicators = NewActiveIndicators()
	a.typingStages = a.resolveTypingStages()
	a.interactions = messaging.NewInteractionManager(a.log)
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
	go a.cleanupMedia(ctx)
	return nil
}

func (a *Adapter) runSocketMode(ctx context.Context) {
	baseDelay := a.backoffBaseDelay
	if baseDelay <= 0 {
		baseDelay = 1 * time.Second
	}
	maxDelay := a.backoffMaxDelay
	if maxDelay <= 0 {
		maxDelay = 60 * time.Second
	}
	backoff := newReconnectBackoff(baseDelay, maxDelay)

	// Run() blocks until the WebSocket closes. Wrap it in a loop so that
	// connection errors trigger automatic reconnect instead of silently exiting.
	go func() {
		attempt := 1
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			a.log.Info("slack: starting socket mode", "attempt", attempt)
			if err := a.socketMode.Run(); err != nil {
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff.Next()):
					a.log.Warn("slack: socket mode error, will retry", "err", err, "attempt", attempt)
					attempt++
					continue
				}
			}
			// Run() returned without error (clean close); reset attempt counter.
			attempt = 1
			a.log.Info("slack: socket closed cleanly, reconnecting")
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff.Next()):
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-a.socketMode.Events:
			if !ok {
				// Channel closed — Run() exited. The reconnect goroutine above will
				// detect this and restart the connection.
				a.log.Warn("slack: events channel closed, waiting for reconnect")
				continue
			}
			switch evt.Type {
			case socketmode.EventTypeEventsAPI:
				eventsAPI, ok := evt.Data.(slackevents.EventsAPIEvent)
				if !ok {
					continue
				}
				a.socketMode.Ack(*evt.Request) //nolint:errcheck // Ack must not block event processing
				go func() {
					defer func() {
						if r := recover(); r != nil {
							a.log.Error("slack: panic in event handler", "panic", r, "stack", string(debug.Stack()))
						}
					}()
					a.handleEventsAPI(ctx, eventsAPI)
				}()

			case socketmode.EventTypeConnecting:
				a.log.Info("slack: websocket handshake in progress")
			case socketmode.EventTypeConnected:
				a.log.Info("slack: websocket established, ready to receive events")
				backoff.Reset()

			case socketmode.EventTypeDisconnect:
				a.log.Info("slack: disconnected by Slack server, reconnecting...")

			case socketmode.EventTypeConnectionError:
				a.log.Warn("slack: websocket connection error, retrying...", "err", evt.Data)

			case socketmode.EventTypeInteractive:
				go func() {
					defer func() {
						if r := recover(); r != nil {
							a.log.Error("slack: panic in interaction handler", "panic", r, "stack", string(debug.Stack()))
						}
					}()
					a.handleInteractionEvent(ctx, evt)
				}()
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

	if blockedSubtypes[msgEvent.SubType] {
		return
	}

	if msgEvent.TimeStamp != "" {
		if ts, err := parseSlackTS(msgEvent.TimeStamp); err == nil {
			if time.Since(ts) > messageExpiry {
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
	text = messaging.SanitizeText(text)
	teamID := event.TeamID

	channelType := ChannelGroup
	if channelID != "" && channelID[0] == 'D' {
		channelType = ChannelIM
	}

	// Access control gate (must run before ResolveMentions which strips <@BOTID>)
	if a.gate != nil {
		botMentioned := strings.Contains(text, "<@"+a.botID+">")
		result := a.gate.Check(channelType, userID, botMentioned)
		if !result.Allowed {
			a.log.Debug("slack: gate rejected", "reason", result.Reason, "user", userID)
			return
		}
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
				a.log.Warn("slack: download media failed", "file", m.Name, "err", err)
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

	// Control command detection (natural language + /command in text).
	if result := messaging.ParseControlCommand(text); result != nil {
		conn := a.GetOrCreateConn(channelID, threadTS)
		if conn != nil {
			conn.handlerMu.Lock()
			defer conn.handlerMu.Unlock()
		}
		a.handleTextControlCommand(ctx, teamID, channelID, threadTS, userID, result)
		return
	}

	// Start typing indicator (emoji fallback for free workspaces)
	a.activeIndicators.Start(ctx, a, channelID, threadTS, msgEvent.TimeStamp, a.typingStages)

	// Set initial assistant status (native API for paid workspaces)
	if a.isAssistantCapable.Load() && threadTS != "" {
		_ = a.SetAssistantStatus(ctx, channelID, threadTS, "Initializing...")
	}

	if err := a.HandleTextMessage(ctx, platformMsgID, channelID, teamID, threadTS, userID, text); err != nil {
		a.log.Error("slack: handle message failed", "err", err)
	}
}

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

// GetOrCreateConn returns an existing SlackConn for the channel/thread pair,
// or creates and registers a new one. This ensures the same conn is reused
// across multiple messages in the same thread, so Hub.Shutdown can close it.
func (a *Adapter) GetOrCreateConn(channelID, threadTS string) *SlackConn {
	key := channelID + "#" + threadTS
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed.Load() {
		return nil
	}
	if c, ok := a.activeConns[key]; ok {
		return c
	}
	c := NewSlackConn(a, channelID, threadTS)
	a.activeConns[key] = c
	return c
}

func (a *Adapter) HandleTextMessage(ctx context.Context, platformMsgID, channelID, teamID, threadTS, userID, text string) error {
	if a.bridge == nil {
		a.log.Warn("slack: bridge not configured, dropping message", "channel", channelID, "user", userID)
		return nil
	}

	envelope := a.bridge.MakeSlackEnvelope(teamID, channelID, threadTS, userID, text)
	if envelope == nil {
		return fmt.Errorf("slack: failed to build envelope")
	}

	conn := a.GetOrCreateConn(channelID, threadTS)
	if conn == nil {
		return fmt.Errorf("slack: adapter closed, dropping message for channel %s", channelID)
	}

	conn.handlerMu.Lock()
	defer conn.handlerMu.Unlock()
	return a.bridge.Handle(ctx, envelope, conn)
}

// NewStreamingWriter creates a streaming writer for the given channel/thread.
func (a *Adapter) NewStreamingWriter(ctx context.Context, channelID, threadTS string, onComplete func(string)) *NativeStreamingWriter {
	w := NewNativeStreamingWriter(ctx, a.client, channelID, threadTS, a.rateLimiter, func(ts string) {
		if !a.closed.Load() {
			a.mu.Lock()
			delete(a.activeStreams, ts)
			a.mu.Unlock()
		}
		if onComplete != nil {
			onComplete(ts)
		}
	}, func(w *NativeStreamingWriter) {
		if !a.closed.Load() {
			a.mu.Lock()
			if w.messageTS != "" {
				a.activeStreams[w.messageTS] = w
			}
			a.mu.Unlock()
		}
	})
	return w
}

// Close gracefully terminates the platform connection. Safe to call multiple times.
func (a *Adapter) Close(ctx context.Context) error {
	a.log.Info("slack: adapter closing")
	a.closed.Store(true)

	a.mu.Lock()
	for _, w := range a.activeStreams {
		_ = w.Close()
	}
	for k := range a.activeStreams {
		delete(a.activeStreams, k)
	}
	conns := make([]*SlackConn, 0, len(a.activeConns))
	for _, c := range a.activeConns {
		conns = append(conns, c)
	}
	for k := range a.activeConns {
		delete(a.activeConns, k)
	}
	a.mu.Unlock()

	for _, c := range conns {
		_ = c.Close()
	}

	if a.activeIndicators != nil {
		a.activeIndicators.CloseAll(ctx)
	}

	a.mu.Lock()
	if a.rateLimiter != nil {
		a.rateLimiter.Stop()
		a.rateLimiter = nil
	}
	if a.slashLimiter != nil {
		a.slashLimiter.Stop()
		a.slashLimiter = nil
	}
	a.mu.Unlock()
	if a.dedup != nil {
		a.dedup.Close()
		a.dedup = nil
	}
	if a.userCache != nil {
		a.userCache.Close()
	}

	return nil
}

// handleTextControlCommand sends a control event derived from a text message
// through the bridge, then sends ephemeral feedback to the user.
func (a *Adapter) handleTextControlCommand(ctx context.Context, teamID, channelID, threadTS, userID string, result *messaging.ControlCommandResult) {
	env := a.bridge.MakeSlackEnvelope(teamID, channelID, threadTS, userID, "")
	if env == nil {
		a.log.Error("slack: text control command failed to derive session", "action", result.Label)
		return
	}

	ctrlEnv := &events.Envelope{
		Version:   events.Version,
		ID:        aep.NewID(),
		SessionID: env.SessionID,
		Event: events.Event{
			Type: events.Control,
			Data: events.ControlData{Action: result.Action},
		},
		OwnerID: userID,
	}

	conn := a.GetOrCreateConn(channelID, threadTS)
	if conn == nil {
		a.log.Warn("slack: adapter closed, dropping control command", "action", result.Label)
		return
	}
	if err := a.bridge.Handle(ctx, ctrlEnv, conn); err != nil {
		a.log.Error("slack: text control command failed", "action", result.Label, "err", err)
		a.sendEphemeralOrPost(ctx, channelID, userID, fmt.Sprintf("❌ Failed to execute %s.", result.Label))
		return
	}

	a.log.Info("slack: text control command sent", "action", result.Label, "user", userID, "session_id", env.SessionID)

	// Reset/GC kills the worker without a guaranteed done event, so stale
	// pending interactions (permission/question/elicitation) may survive.
	// Cancel them now so stale interactive buttons don't route to the new worker.
	if result.Action == events.ControlActionReset || result.Action == events.ControlActionGC {
		a.interactions.CancelAll(env.SessionID)
	}

	a.sendEphemeralOrPost(ctx, channelID, userID, controlFeedbackMessage(result.Action))
}

func controlFeedbackMessage(action events.ControlAction) string {
	switch action {
	case events.ControlActionGC:
		return "🗑️ Session parked. Send a message to resume."
	case events.ControlActionReset:
		return "🔄 Context reset."
	default:
		return "✅ Done."
	}
}

// SlackConn wraps the adapter with channel/thread routing info
// to satisfy messaging.PlatformConn for Hub.JoinPlatformSession.
type SlackConn struct {
	adapter   *Adapter
	channelID string
	threadTS  string
	messageTS string // anchor message for typing indicator cleanup

	handlerMu      sync.Mutex // serializes control commands and message handling per thread
	streamWriter   *NativeStreamingWriter
	streamWriterMu sync.Mutex
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

	// Clear status indicator on done/error
	switch env.Event.Type {
	case events.Done, events.Error:
		c.adapter.statusMgr.Clear(ctx, c.channelID, c.threadTS)
		c.adapter.activeIndicators.Stop(ctx, c.channelID, c.messageTS)
		c.adapter.interactions.CancelAll(env.SessionID)
		c.closeStreamWriter()
		if env.Event.Type == events.Error {
			if errMsg := extractErrorMessage(env); errMsg != "" {
				// Async: PostMessage is synchronous HTTP and must not block Hub broadcast.
				go func() { _ = c.writeWithPostMessage(ctx, FormatMrkdwn(errPrefix+errMsg), false) }()
			}
		}
		return nil
	case events.PermissionRequest:
		return c.sendPermissionRequest(ctx, env)
	case events.QuestionRequest:
		return c.sendQuestionRequest(ctx, env)
	case events.ElicitationRequest:
		return c.sendElicitationRequest(ctx, env)
	}

	text, ok := extractResponseText(env)
	if !ok {
		return nil
	}
	text = messaging.SanitizeText(text)

	// Try streaming for delta/text events
	if env.Event.Type == events.MessageDelta || env.Event.Type == "text" {
		if err := c.writeWithStreaming(ctx, text); err != nil {
			// Fall back to PostMessage on streaming failure
			return c.writeWithPostMessage(ctx, text, env.Event.Type == events.MessageDelta)
		}
		return nil
	}

	// Default: use PostMessage for other event types
	return c.writeWithPostMessage(ctx, text, false)
}

// writeWithStreaming attempts to write using the streaming API.
func (c *SlackConn) writeWithStreaming(ctx context.Context, text string) error {
	if text == "" {
		return nil
	}

	if c.adapter == nil {
		return fmt.Errorf("slack: adapter is nil")
	}

	c.streamWriterMu.Lock()
	defer c.streamWriterMu.Unlock()

	// Create new streaming writer if needed
	if c.streamWriter == nil {
		writer := c.adapter.NewStreamingWriter(ctx, c.channelID, c.threadTS, func(ts string) {
			c.streamWriterMu.Lock()
			if c.threadTS == "" && ts != "" {
				c.threadTS = ts
			}
			c.streamWriterMu.Unlock()
		})
		if writer == nil {
			return fmt.Errorf("failed to create streaming writer")
		}
		c.streamWriter = writer
	}

	_, err := c.streamWriter.Write([]byte(text))
	return err
}

// writeWithPostMessage falls back to PostMessageContext.
// Handles long messages by chunking them into multiple calls.
func (c *SlackConn) writeWithPostMessage(ctx context.Context, text string, isDelta bool) error {
	if c.adapter == nil || c.adapter.client == nil {
		return fmt.Errorf("slack: client not initialized")
	}
	if isDelta && text != "" {
		text += "\n\n"
	}

	chunks := ChunkContent(text, maxMessageLength)
	for _, chunk := range chunks {
		opts := []slack.MsgOption{slack.MsgOptionText(FormatMrkdwn(chunk), false)}
		if c.threadTS != "" {
			opts = append(opts, slack.MsgOptionTS(c.threadTS))
		}
		_, _, err := c.adapter.client.PostMessageContext(ctx, c.channelID, opts...)
		if err != nil {
			return err
		}
	}
	return nil
}

// closeStreamWriter closes and clears the stream writer.
func (c *SlackConn) closeStreamWriter() {
	c.streamWriterMu.Lock()
	defer c.streamWriterMu.Unlock()

	if c.streamWriter != nil {
		_ = c.streamWriter.Close()
		c.streamWriter = nil
	}
}

// Close removes the conn from the adapter registry and cleans up the stream writer.
func (c *SlackConn) Close() error {
	key := c.channelID + "#" + c.threadTS
	c.adapter.mu.Lock()
	delete(c.adapter.activeConns, key)
	c.adapter.mu.Unlock()

	c.closeStreamWriter()

	// Clean up typing indicator + status emoji (same as done/error path in WriteCtx).
	ctx := context.Background()
	c.adapter.activeIndicators.Stop(ctx, c.channelID, c.messageTS)
	c.adapter.statusMgr.Clear(ctx, c.channelID, c.threadTS)

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

// extractErrorMessage tries ErrorData then map[string]any fallback.
func extractErrorMessage(env *events.Envelope) string {
	if d, ok := env.Event.Data.(events.ErrorData); ok {
		return d.Message
	}
	if m, ok := env.Event.Data.(map[string]any); ok {
		if msg, ok := m["message"].(string); ok {
			return msg
		}
	}
	return ""
}

func (a *Adapter) cleanupMedia(ctx context.Context) {
	ticker := time.NewTicker(mediaCleanupInt)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.cleanupMediaInDir(mediaPathPrefix)
		}
	}
}

func (a *Adapter) cleanupMediaInDir(dir string) {
	a.log.Debug("slack: cleaning up media files", "dir", dir)
	_ = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && time.Since(info.ModTime()) > mediaTTL {
			if err := os.Remove(path); err != nil {
				a.log.Warn("slack: failed to remove old media file", "path", path, "err", err)
			}
		}
		return nil
	})
}

var _ messaging.PlatformConn = (*SlackConn)(nil)
