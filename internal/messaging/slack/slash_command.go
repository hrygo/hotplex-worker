package slack

import (
	"context"
	"fmt"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"

	"github.com/hrygo/hotplex/pkg/aep"
	"github.com/hrygo/hotplex/pkg/events"
)

const (
	CommandReset      = "/reset"
	CommandDisconnect = "/dc"

	slashCooldown      = 5 * time.Second
	slashSweepInterval = 5 * time.Minute
	slashEntryTTL      = 10 * time.Minute
)

// SlashRateLimiter provides per-user cooldown for slash commands.
type SlashRateLimiter struct {
	cache *TTLCache[string, time.Time]
}

// NewSlashRateLimiter creates a new slash command rate limiter.
func NewSlashRateLimiter() *SlashRateLimiter {
	return &SlashRateLimiter{
		cache: NewTTLCache[string, time.Time](slashEntryTTL, slashSweepInterval),
	}
}

// Allow reports whether a slash command from the given user is allowed.
// Subsequent calls within slashCooldown are rejected.
func (r *SlashRateLimiter) Allow(userID string) bool {
	var allowed bool
	r.cache.Do(func(items map[string]ttlEntry[time.Time]) {
		now := time.Now()
		e, ok := items[userID]
		if ok && now.Sub(e.Value) < slashCooldown {
			return
		}
		items[userID] = ttlEntry[time.Time]{
			Value:  now,
			Expiry: now.Add(slashEntryTTL),
		}
		allowed = true
	})
	return allowed
}

// Stop cleanly terminates the sweep goroutine.
func (r *SlashRateLimiter) Stop() {
	r.cache.Stop()
}

func (a *Adapter) handleSlashCommandEvent(ctx context.Context, evt socketmode.Event) {
	cmd, ok := evt.Data.(slack.SlashCommand)
	if !ok {
		a.Log.Warn("slack: slash command event type assertion failed")
		return
	}

	a.Log.Info("slack: slash command received",
		"command", cmd.Command,
		"user", cmd.UserID,
		"channel", cmd.ChannelID,
		"text_len", len(cmd.Text),
	)
	a.Log.Debug("slack: slash command detail", "text", cmd.Text)

	a.socketMode.Ack(*evt.Request) //nolint:errcheck // Ack must not block event processing

	if a.slashLimiter != nil && !a.slashLimiter.Allow(cmd.UserID) {
		a.Log.Warn("slack: slash command rate limited", "user_id", cmd.UserID)
		a.sendEphemeralOrPost(ctx, cmd.ChannelID, "", cmd.UserID, "⚠️ Rate limit exceeded. Please wait a moment.")
		return
	}

	if a.Gate != nil {
		if allowed, reason := a.Gate.Check(false, cmd.UserID, false); !allowed {
			a.Log.Debug("slack: gate rejected slash command", "reason", reason, "user", cmd.UserID)
			a.sendEphemeralOrPost(ctx, cmd.ChannelID, "", cmd.UserID, "🚫 You are not authorized to use this command.")
			return
		}
	}

	switch cmd.Command {
	case CommandReset:
		a.handleControlCommand(ctx, cmd, events.ControlActionReset,
			"/reset", "🔄 Resetting context...", "❌ Failed to reset. No active conversation found.")
	case CommandDisconnect:
		a.handleControlCommand(ctx, cmd, events.ControlActionTerminate,
			"/dc", "👋 Disconnecting. Context preserved for next message.", "❌ Failed to disconnect. No active conversation.")
	default:
		a.sendEphemeralOrPost(ctx, cmd.ChannelID, "", cmd.UserID, fmt.Sprintf("Unknown command: %s", cmd.Command))
	}
}

func (a *Adapter) handleControlCommand(ctx context.Context, cmd slack.SlashCommand, action events.ControlAction, logPrefix, successMsg, errorMsg string) {
	sessionID := a.deriveSessionIDFromCommand(cmd)

	env := &events.Envelope{
		Version:   events.Version,
		ID:        aep.NewID(),
		SessionID: sessionID,
		Event: events.Event{
			Type: events.Control,
			Data: events.ControlData{Action: action},
		},
		OwnerID: cmd.UserID,
	}

	conn := a.GetOrCreateConn(cmd.ChannelID, "")
	if conn == nil {
		a.Log.Warn("slack: adapter closed, dropping slash command", "command", logPrefix)
		return
	}
	if err := a.Bridge().Handle(ctx, env, conn); err != nil {
		a.Log.Error("slack: control event failed", "command", logPrefix, "session_id", sessionID, "err", err)
		a.sendEphemeralOrPost(ctx, cmd.ChannelID, "", cmd.UserID, errorMsg)
		return
	}

	a.Log.Info("slack: control sent", "command", logPrefix, "session_id", sessionID, "user", cmd.UserID)
	a.sendEphemeralOrPost(ctx, cmd.ChannelID, "", cmd.UserID, successMsg)
}

func (a *Adapter) sendEphemeralOrPost(ctx context.Context, channelID, threadTS, userID, text string) {
	if userID != "" && channelID != "" && channelID[0] != 'D' {
		opts := []slack.MsgOption{slack.MsgOptionText(text, false)}
		if threadTS != "" {
			opts = append(opts, slack.MsgOptionTS(threadTS))
		}
		if _, err := a.client.PostEphemeralContext(ctx, channelID, userID, opts...); err == nil {
			return
		}
	}
	opts := []slack.MsgOption{slack.MsgOptionText(text, false)}
	if threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(threadTS))
	}
	_, _, _ = a.client.PostMessageContext(ctx, channelID, opts...)
}

func (a *Adapter) deriveSessionIDFromCommand(cmd slack.SlashCommand) string {
	conn := a.GetOrCreateConn(cmd.ChannelID, "")
	workDir := ""
	if conn != nil {
		workDir = conn.WorkDir()
	}
	envelope := a.Bridge().MakeSlackEnvelope(cmd.TeamID, cmd.ChannelID, "", cmd.UserID, "", workDir, a.botID)
	if envelope == nil {
		return ""
	}
	return envelope.SessionID
}
