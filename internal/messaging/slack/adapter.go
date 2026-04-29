// Package slack provides a Slack Socket Mode platform adapter.
package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/messaging/stt"
	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"

	"runtime/debug"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

const (
	messageExpiry    = 30 * time.Minute
	dedupMaxEntries  = 5000
	dedupTTL         = 30 * time.Minute
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
		return &Adapter{log: log.With("channel", "slack")}
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
	dedup              *messaging.Dedup
	userCache          *UserCache
	statusMgr          *StatusManager
	isAssistantCapable atomic.Bool
	assistantEnabled   *bool
	gate               *messaging.Gate
	transcriber        stt.Transcriber
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

func (a *Adapter) ConfigureWith(config messaging.AdapterConfig) error {
	// Call base to set hub/sm/handler/bridge.
	_ = a.PlatformAdapter.ConfigureWith(config)

	// Slack-specific: tokens.
	a.botToken = config.ExtrasString("bot_token")
	a.appToken = config.ExtrasString("app_token")

	// Bridge reference and workdir.
	if config.Bridge != nil {
		a.bridge = config.Bridge
		SetWorkDir(config.Bridge.WorkDir())
	}

	// Access control.
	if config.Gate != nil {
		a.gate = config.Gate
	}

	// Platform-specific extras.
	if v := config.ExtrasBoolPtr("assistant_enabled"); v != nil {
		a.assistantEnabled = v
	}
	if bd := config.ExtrasDuration("reconnect_base_delay"); bd > 0 {
		a.backoffBaseDelay = bd
	}
	if md := config.ExtrasDuration("reconnect_max_delay"); md > 0 {
		a.backoffMaxDelay = md
	}
	if t, ok := config.Extras["transcriber"].(stt.Transcriber); ok && t != nil {
		a.transcriber = t
	}

	return nil
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
	a.dedup = messaging.NewDedup(dedupMaxEntries, dedupTTL)
	a.dedup.StartCleanup()
	a.userCache = NewUserCache(a.client)
	a.statusMgr = NewStatusManager(a, a.log)
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
			a.statusMgr.SetEmojiOnly(true)
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
	backoff := messaging.NewReconnectBackoff(baseDelay, maxDelay)

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

			case socketmode.EventTypeSlashCommand:
				go func() {
					defer func() {
						if r := recover(); r != nil {
							a.log.Error("slack: panic in slash command handler", "panic", r, "stack", string(debug.Stack()))
						}
					}()
					a.handleSlashCommandEvent(ctx, evt)
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
		result := a.gate.Check(channelType == ChannelIM, userID, botMentioned)
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
			// Audio + STT: transcribe voice messages to text.
			if m.Type == mediaTypeAudio && a.transcriber != nil {
				if audioText, audioErr := a.handleAudioMessage(ctx, m); audioErr != nil {
					text += fmt.Sprintf("\n[audio: %s]", m.Name)
				} else {
					text += audioText
				}
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

	// Help command - reply directly without involving the worker.
	if messaging.IsHelpCommand(text) {
		_ = a.SetStatus(ctx, channelID, threadTS, StatusThinking, "Loading help...")
		opts := []slack.MsgOption{
			slack.MsgOptionText(messaging.HelpText(), false),
		}
		if threadTS != "" {
			opts = append(opts, slack.MsgOptionTS(threadTS))
		}
		_, _, _ = a.client.PostMessageContext(ctx, channelID, opts...)
		_ = a.ClearStatus(ctx, channelID, threadTS)
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

	// Worker command detection (slash + $ natural language).
	// Only intercept structured commands (context, mcp, model, perm).
	// Passthrough commands (compact, clear, rewind, effort, commit)
	// fall through to normal input — they aren't supported in stream-json mode.
	if cmdResult := messaging.ParseWorkerCommand(text); cmdResult != nil && !cmdResult.Command.IsPassthrough() {
		conn := a.GetOrCreateConn(channelID, threadTS)
		if conn != nil {
			conn.messageTS = msgEvent.TimeStamp
			conn.handlerMu.Lock()
			defer conn.handlerMu.Unlock()
		}
		if a.isAssistantCapable.Load() && threadTS != "" {
			_ = a.SetAssistantStatus(ctx, channelID, threadTS, "Processing "+cmdResult.Label+"...")
		}
		a.handleTextWorkerCommand(ctx, teamID, channelID, threadTS, userID, cmdResult)
		return
	}

	// Set initial assistant status (native API for paid workspaces)
	if a.isAssistantCapable.Load() && threadTS != "" {
		_ = a.SetAssistantStatus(ctx, channelID, threadTS, "Initializing...")
	}

	if err := a.HandleTextMessage(ctx, platformMsgID, channelID, teamID, threadTS, userID, text); err != nil {
		a.log.Warn("slack: handle message failed", "err", err, "channel", channelID, "thread", threadTS, "user", userID)
	}
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
	w := NewNativeStreamingWriter(ctx, a.client, channelID, threadTS, a.rateLimiter, a.log, func(ts string) {
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

	if a.transcriber != nil {
		if closer, ok := a.transcriber.(stt.Closer); ok {
			_ = closer.Close(ctx)
		}
	}

	return nil
}

// handleAudioMessage downloads and transcribes a voice message, returning formatted text.
func (a *Adapter) handleAudioMessage(ctx context.Context, m *MediaInfo) (string, error) {
	audioData, err := a.downloadMediaBytes(ctx, m)
	if err != nil {
		a.log.Warn("slack: download audio failed", "file", m.Name, "err", err)
		return "", err
	}

	transcript, err := a.transcriber.Transcribe(ctx, audioData)
	if err != nil {
		a.log.Warn("slack: stt failed", "file", m.Name, "err", err)
		return "", err
	}

	var text string
	if transcript != "" {
		text = fmt.Sprintf("\n[voice message transcription]: %s", transcript)
	} else {
		text = fmt.Sprintf("\n[voice message: %s (empty transcription)]", m.Name)
	}

	if a.transcriber.RequiresDisk() {
		if path, err := a.saveMediaBytes(m, audioData); err == nil {
			text += "\n" + path
		}
	}
	return text, nil
}

// handleTextControlCommand sends a control event derived from a text message
// through the bridge, then sends ephemeral feedback to the user.
func (a *Adapter) handleTextControlCommand(ctx context.Context, teamID, channelID, threadTS, userID string, result *messaging.ControlCommandResult) {
	env := a.bridge.MakeSlackEnvelope(teamID, channelID, threadTS, userID, "")
	if env == nil {
		a.log.Warn("slack: text control command failed to derive session", "action", result.Label)
		return
	}

	ctrlData := events.ControlData{Action: result.Action}
	if result.Arg != "" {
		ctrlData.Details = map[string]any{"path": result.Arg}
	}

	ctrlEnv := &events.Envelope{
		Version:   events.Version,
		ID:        aep.NewID(),
		SessionID: env.SessionID,
		Event: events.Event{
			Type: events.Control,
			Data: ctrlData,
		},
		OwnerID: userID,
	}

	conn := a.GetOrCreateConn(channelID, threadTS)
	if conn == nil {
		a.log.Warn("slack: adapter closed, dropping control command", "action", result.Label)
		return
	}
	if err := a.bridge.Handle(ctx, ctrlEnv, conn); err != nil {
		a.log.Warn("slack: text control command failed", "action", result.Label, "err", err)
		a.sendEphemeralOrPost(ctx, channelID, threadTS, userID, fmt.Sprintf("❌ Failed to execute %s.", result.Label))
		return
	}

	a.log.Info("slack: text control command sent", "action", result.Label, "user", userID, "session_id", env.SessionID)

	// Reset/GC kills the worker without a guaranteed done event, so stale
	// pending interactions (permission/question/elicitation) may survive.
	// Cancel them now so stale interactive buttons don't route to the new worker.
	if result.Action == events.ControlActionReset || result.Action == events.ControlActionGC {
		a.interactions.CancelAll(env.SessionID)
		// Abort any active streaming writer — GC/Reset kills the worker without a
		// done event, so the writer would otherwise remain active until TTL expiry.
		if conn := a.GetOrCreateConn(channelID, threadTS); conn != nil {
			conn.closeStreamWriter()
		}
	}

	a.sendEphemeralOrPost(ctx, channelID, threadTS, userID, controlFeedbackMessage(result.Action))
}

func (a *Adapter) handleTextWorkerCommand(ctx context.Context, teamID, channelID, threadTS, userID string, result *messaging.WorkerCommandResult) {
	envelope := a.bridge.MakeSlackEnvelope(teamID, channelID, threadTS, userID, "")
	if envelope == nil {
		a.log.Warn("slack: worker command failed to derive session", "command", result.Label)
		return
	}

	cmdEnv := &events.Envelope{
		Version:   events.Version,
		ID:        aep.NewID(),
		SessionID: envelope.SessionID,
		Event: events.Event{
			Type: events.WorkerCmd,
			Data: events.WorkerCommandData{
				Command: result.Command,
				Args:    result.Args,
				Extra:   result.Extra,
			},
		},
		OwnerID: userID,
	}

	conn := a.GetOrCreateConn(channelID, threadTS)
	if conn == nil {
		a.log.Warn("slack: adapter closed, dropping worker command", "command", result.Label)
		return
	}

	if err := a.bridge.Handle(ctx, cmdEnv, conn); err != nil {
		a.log.Warn("slack: worker command failed", "command", result.Label, "err", err)
		a.sendEphemeralOrPost(ctx, channelID, threadTS, userID, fmt.Sprintf("❌ Failed to execute %s.", result.Label))
		return
	}

	a.log.Info("slack: worker command sent", "command", result.Label, "user", userID, "session_id", envelope.SessionID)
}

func controlFeedbackMessage(action events.ControlAction) string {
	switch action {
	case events.ControlActionGC:
		return "🗑️ Session parked. Send a message to resume."
	case events.ControlActionReset:
		return "🔄 Context reset."
	case events.ControlActionCD:
		return "📁 Switching work directory..."
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

// notifyStatus sets processing status (nil-safe for tests).
func (c *SlackConn) notifyStatus(ctx context.Context, text string) {
	if c.adapter != nil && c.adapter.statusMgr != nil {
		_ = c.adapter.statusMgr.Notify(ctx, c.channelID, c.threadTS, StatusThinking, text)
	}
}

// clearStatus clears processing status (nil-safe for tests).
func (c *SlackConn) clearStatus(ctx context.Context) {
	if c.adapter != nil && c.adapter.statusMgr != nil {
		c.adapter.statusMgr.Clear(ctx, c.channelID, c.threadTS)
	}
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
	// Log unregistered tool names for status formatter evolution
	if env.Event.Type == events.ToolCall {
		if name := toolNameFromEnvelope(env); name != "" {
			if _, ok := toolStatusFormatters[name]; !ok {
				c.adapter.statusMgr.LogOnceUnregistered(name)
			}
		}
	}

	// Clear status indicator on done/error
	switch env.Event.Type {
	case events.Done, events.Error:
		c.clearStatus(ctx)
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
		c.notifyStatus(ctx, "Permission request...")
		err := c.sendPermissionRequest(ctx, env)
		c.clearStatus(ctx)
		return err
	case events.QuestionRequest:
		c.notifyStatus(ctx, "Awaiting response...")
		qErr := c.sendQuestionRequest(ctx, env)
		c.clearStatus(ctx)
		return qErr
	case events.ElicitationRequest:
		c.notifyStatus(ctx, "Gathering input...")
		eErr := c.sendElicitationRequest(ctx, env)
		c.clearStatus(ctx)
		return eErr
	case events.ContextUsage:
		c.notifyStatus(ctx, "Loading context usage...")
		cErr := c.sendContextUsage(ctx, env)
		c.clearStatus(ctx)
		return cErr
	case events.MCPStatus:
		c.notifyStatus(ctx, "Loading MCP status...")
		mErr := c.sendMCPStatus(ctx, env)
		c.clearStatus(ctx)
		return mErr
	case events.SkillsList:
		c.notifyStatus(ctx, "Loading skills...")
		slErr := c.sendSkillsList(ctx, env)
		c.clearStatus(ctx)
		if slErr == nil || !strings.Contains(slErr.Error(), "invalid_blocks") {
			return slErr
		}
		c.adapter.log.Warn("slack: skills blocks rejected, falling back to plain text", "err", slErr)
		return c.postSkillsMessageFallback(ctx, env)
	}

	text, ok := extractResponseText(env)
	if !ok {
		return nil
	}
	text = messaging.SanitizeText(text)

	// Try file upload for document paths (.pdf, .csv) generated by AI
	if env.Event.Type != events.MessageDelta {
		if uploaded := c.tryFileUpload(ctx, text); uploaded {
			return nil
		}
	}

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
// Extracts local image paths and renders them as Block Kit Image Blocks.
func (c *SlackConn) writeWithPostMessage(ctx context.Context, text string, isDelta bool) error {
	if c.adapter == nil || c.adapter.client == nil {
		return fmt.Errorf("slack: client not initialized")
	}
	if isDelta && text != "" {
		text += "\n\n"
	}

	// Try image block rendering for non-delta messages
	if !isDelta {
		if err := c.tryImageBlocks(ctx, text); err == nil {
			return nil
		}
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

// tryImageBlocks attempts to send text with images as Block Kit.
// Returns error if no images found or block send fails (caller falls back to text).
func (c *SlackConn) tryImageBlocks(ctx context.Context, text string) error {
	parts, remaining := extractImages(text)
	if len(parts) == 0 {
		return fmt.Errorf("no images")
	}

	blocks := buildImageBlocks(parts, remaining)
	opts := []slack.MsgOption{
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionText(FormatMrkdwn(remaining), false),
	}
	if c.threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(c.threadTS))
	}
	_, _, err := c.adapter.client.PostMessageContext(ctx, c.channelID, opts...)
	if err != nil {
		c.adapter.log.Warn("slack: image blocks failed, falling back to text", "err", err)
	}
	return err
}

// postFile uploads a file to Slack and posts a reference in the thread.
func (a *Adapter) postFile(ctx context.Context, channelID, threadTS, filePath, title string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("read file: %w", err)
	}
	if len(data) > mediaMaxSize {
		return "", fmt.Errorf("file too large: %d bytes", len(data))
	}

	params := slack.UploadFileParameters{
		Filename:        filepath.Base(filePath),
		Title:           title,
		Reader:          strings.NewReader(string(data)),
		FileSize:        len(data),
		Channel:         channelID,
		ThreadTimestamp: threadTS,
	}

	file, err := a.client.UploadFileContext(ctx, params)
	if err != nil {
		return "", fmt.Errorf("upload file: %w", err)
	}

	return file.ID, nil
}

// uploadableExtensions are file types that should be uploaded rather than sent as text.
var uploadableExtensions = []string{".pdf", ".csv", ".xlsx", ".docx"}

// tryFileUpload checks if text contains a local file path that should be uploaded.
// Returns true if a file was successfully uploaded.
func (c *SlackConn) tryFileUpload(ctx context.Context, text string) bool {
	if c.adapter == nil || c.adapter.client == nil {
		return false
	}
	trimmed := strings.TrimSpace(text)
	for _, ext := range uploadableExtensions {
		if !strings.HasSuffix(trimmed, ext) {
			continue
		}
		// Check if the trimmed text is or ends with a file path
		lines := strings.Split(trimmed, "\n")
		lastLine := strings.TrimSpace(lines[len(lines)-1])
		if _, err := os.Stat(lastLine); err != nil {
			continue
		}
		fileID, err := c.adapter.postFile(ctx, c.channelID, c.threadTS, lastLine, filepath.Base(lastLine))
		if err != nil {
			c.adapter.log.Warn("slack: file upload failed, falling back to text", "path", lastLine, "err", err)
			return false
		}
		// Send any preceding text along with upload confirmation
		prefix := strings.Join(lines[:len(lines)-1], "\n")
		prefix = strings.TrimSpace(prefix)
		msg := fmt.Sprintf("📎 Uploaded: %s", filepath.Base(lastLine))
		if prefix != "" {
			msg = FormatMrkdwn(prefix) + "\n" + msg
		}
		opts := []slack.MsgOption{slack.MsgOptionText(msg, false)}
		if c.threadTS != "" {
			opts = append(opts, slack.MsgOptionTS(c.threadTS))
		}
		_, _, _ = c.adapter.client.PostMessageContext(ctx, c.channelID, opts...)
		_ = fileID
		return true
	}
	return false
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

	c.clearStatus(context.Background())

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
			a.cleanupMediaInDir(MediaPathPrefix)
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

func (c *SlackConn) sendContextUsage(ctx context.Context, env *events.Envelope) error {
	if c.adapter == nil || c.adapter.client == nil {
		return fmt.Errorf("slack: client not initialized")
	}

	var d events.ContextUsageData
	switch v := env.Event.Data.(type) {
	case events.ContextUsageData:
		d = v
	case map[string]any:
		raw, _ := json.Marshal(v)
		_ = json.Unmarshal(raw, &d)
	default:
		return nil
	}

	plainText := fmt.Sprintf("📊 Context Usage — %d%% (%d / %d)", d.Percentage, d.TotalTokens, d.MaxTokens)
	if d.Model != "" {
		plainText += fmt.Sprintf("\n🤖 Model: %s", d.Model)
	}
	catParts := formatCategoryParts(d.Categories)
	if len(catParts) > 0 {
		plainText += "\n📂 " + strings.Join(catParts, " · ")
	}

	// Primary: TableBlock (may be rejected by workspaces without the beta feature)
	blocks := c.buildContextUsageTable(d, catParts)
	opts := []slack.MsgOption{
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionText(plainText, false),
	}
	if c.threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(c.threadTS))
	}
	_, _, err := c.adapter.client.PostMessageContext(ctx, c.channelID, opts...)
	if err == nil {
		return nil
	}
	if !strings.Contains(err.Error(), "invalid_blocks") {
		return err
	}

	// Fallback: ContextBlock (universally supported)
	c.adapter.log.Warn("slack: context usage TableBlock rejected, falling back to ContextBlock", "err", err)
	fbBlocks := c.buildContextUsageFallback(d, catParts)
	fbOpts := []slack.MsgOption{
		slack.MsgOptionBlocks(fbBlocks...),
		slack.MsgOptionText(plainText, false),
	}
	if c.threadTS != "" {
		fbOpts = append(fbOpts, slack.MsgOptionTS(c.threadTS))
	}
	_, _, fbErr := c.adapter.client.PostMessageContext(ctx, c.channelID, fbOpts...)
	return fbErr
}

// buildContextUsageTable builds a TableBlock for context usage (primary format).
func (c *SlackConn) buildContextUsageTable(d events.ContextUsageData, catParts []string) []slack.Block {
	table := slack.NewTableBlock("context_usage")
	table = table.WithColumnSettings(
		slack.ColumnSetting{Align: slack.ColumnAlignmentLeft, IsWrapped: false},
		slack.ColumnSetting{Align: slack.ColumnAlignmentLeft, IsWrapped: true},
	)

	table.AddRow(richTextCell("📊 Usage"), richTextCell(fmt.Sprintf("%d%% (%d / %d)", d.Percentage, d.TotalTokens, d.MaxTokens)))
	if d.Model != "" {
		table.AddRow(richTextCell("🤖 Model"), richTextCell(d.Model))
	}
	if len(catParts) > 0 {
		table.AddRow(richTextCell("📂 Context"), richTextCell(strings.Join(catParts, " · ")))
	}
	if d.MemoryFiles > 0 {
		table.AddRow(richTextCell("📁 Memory"), richTextCell(fmt.Sprintf("%d files", d.MemoryFiles)))
	}
	if d.MCPTools > 0 {
		table.AddRow(richTextCell("🔧 MCP"), richTextCell(fmt.Sprintf("%d tools", d.MCPTools)))
	}
	if d.Agents > 0 {
		table.AddRow(richTextCell("🤖 Agents"), richTextCell(fmt.Sprintf("%d", d.Agents)))
	}
	if d.Skills.Total > 0 {
		table.AddRow(richTextCell("⚡ Skills"), richTextCell(fmt.Sprintf("%d (%d included, %d tokens)", d.Skills.Total, d.Skills.Included, d.Skills.Tokens)))
		if len(d.Skills.Names) > 0 {
			table.AddRow(richTextCell("📜 Skill List"), richTextCell(strings.Join(d.Skills.Names, ", ")))
		}
	}
	return []slack.Block{table}
}

// buildContextUsageFallback builds ContextBlock fallback when TableBlock is rejected.
func (c *SlackConn) buildContextUsageFallback(d events.ContextUsageData, catParts []string) []slack.Block {
	parts := []string{fmt.Sprintf("📊 *Context Usage* — %d%% (%d / %d)", d.Percentage, d.TotalTokens, d.MaxTokens)}
	if d.Model != "" {
		parts = append(parts, fmt.Sprintf("🤖 Model: %s", d.Model))
	}
	if len(catParts) > 0 {
		parts = append(parts, "📂 "+strings.Join(catParts, " · "))
	}
	if len(d.Skills.Names) > 0 {
		parts = append(parts, "📜 *Skills*: "+strings.Join(d.Skills.Names, ", "))
	}
	text := slack.NewTextBlockObject("mrkdwn", strings.Join(parts, "\n"), false, false)
	return []slack.Block{slack.NewContextBlock("", text)}
}

// richTextCell creates a RichTextBlock cell for use in TableBlock rows.
func richTextCell(text string) *slack.RichTextBlock {
	section := slack.NewRichTextSection(
		slack.NewRichTextSectionTextElement(text, nil),
	)
	return slack.NewRichTextBlock("", section)
}

// formatCategoryParts formats context categories as "Name: Tokens" pairs.
func formatCategoryParts(categories []events.ContextCategory) []string {
	parts := make([]string, 0, len(categories))
	for _, cat := range categories {
		parts = append(parts, fmt.Sprintf("%s: %d", cat.Name, cat.Tokens))
	}
	return parts
}

// mcpServerIcon returns the status icon for an MCP server.
func mcpServerIcon(status string) string {
	if status == "connected" || status == "ok" {
		return "✅"
	}
	return "❌"
}

func (c *SlackConn) sendMCPStatus(ctx context.Context, env *events.Envelope) error {
	if c.adapter == nil || c.adapter.client == nil {
		return fmt.Errorf("slack: client not initialized")
	}

	var d events.MCPStatusData
	switch v := env.Event.Data.(type) {
	case events.MCPStatusData:
		d = v
	case map[string]any:
		raw, _ := json.Marshal(v)
		_ = json.Unmarshal(raw, &d)
	default:
		return nil
	}

	var sb strings.Builder
	sb.WriteString("🔌 MCP Server Status\n")
	for _, s := range d.Servers {
		fmt.Fprintf(&sb, "%s %s — %s\n", mcpServerIcon(s.Status), s.Name, s.Status)
	}
	plainText := sb.String()

	table := slack.NewTableBlock("mcp_status")
	table = table.WithColumnSettings(
		slack.ColumnSetting{Align: slack.ColumnAlignmentLeft, IsWrapped: false},
		slack.ColumnSetting{Align: slack.ColumnAlignmentLeft, IsWrapped: true},
	)
	table.AddRow(richTextCell("🔌 MCP Status"), richTextCell(fmt.Sprintf("%d servers", len(d.Servers))))
	for _, s := range d.Servers {
		table.AddRow(richTextCell(mcpServerIcon(s.Status)+" "+s.Name), richTextCell(s.Status))
	}

	blocks := []slack.Block{table}
	opts := []slack.MsgOption{
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionText(plainText, false),
	}
	if c.threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(c.threadTS))
	}
	_, _, err := c.adapter.client.PostMessageContext(ctx, c.channelID, opts...)
	if err != nil {
		if strings.Contains(err.Error(), "invalid_blocks") {
			c.adapter.log.Warn("slack: MCP status TableBlock rejected, falling back to plain text", "err", err)
			fbOpts := []slack.MsgOption{slack.MsgOptionText(plainText, false)}
			if c.threadTS != "" {
				fbOpts = append(fbOpts, slack.MsgOptionTS(c.threadTS))
			}
			_, _, fbErr := c.adapter.client.PostMessageContext(ctx, c.channelID, fbOpts...)
			return fbErr
		}
	}
	return err
}

var _ messaging.PlatformConn = (*SlackConn)(nil)
