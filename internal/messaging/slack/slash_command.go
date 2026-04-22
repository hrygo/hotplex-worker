package slack

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"

	"github.com/hotplex/hotplex-worker/pkg/aep"
	"github.com/hotplex/hotplex-worker/pkg/events"
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
	mu       sync.Mutex
	lastUsed map[string]time.Time
	done     chan struct{}
	wg       sync.WaitGroup
	stopOnce sync.Once
}

// NewSlashRateLimiter creates a new slash command rate limiter.
func NewSlashRateLimiter() *SlashRateLimiter {
	r := &SlashRateLimiter{
		lastUsed: make(map[string]time.Time),
		done:     make(chan struct{}),
	}
	r.Start()
	return r
}

// Allow reports whether a slash command from the given user is allowed.
// Subsequent calls within slashCooldown are rejected.
func (r *SlashRateLimiter) Allow(userID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if last, ok := r.lastUsed[userID]; ok && now.Sub(last) < slashCooldown {
		return false
	}
	r.lastUsed[userID] = now
	return true
}

// Start launches the background sweep goroutine.
func (r *SlashRateLimiter) Start() {
	r.wg.Add(1)
	go r.sweepLoop()
}

// sweepLoop periodically removes stale entries from lastUsed map.
func (r *SlashRateLimiter) sweepLoop() {
	defer r.wg.Done()
	ticker := time.NewTicker(slashSweepInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.done:
			return
		case <-ticker.C:
			r.mu.Lock()
			now := time.Now()
			for userID, last := range r.lastUsed {
				if now.Sub(last) > slashEntryTTL {
					delete(r.lastUsed, userID)
				}
			}
			r.mu.Unlock()
		}
	}
}

// Stop cleanly terminates the sweep goroutine.
func (r *SlashRateLimiter) Stop() {
	r.stopOnce.Do(func() {
		close(r.done)
	})
	r.wg.Wait()
}

func (a *Adapter) handleSlashCommandEvent(ctx context.Context, evt socketmode.Event) { //nolint:unused // wired in runSocketMode when slash commands enabled
	cmd, ok := evt.Data.(slack.SlashCommand)
	if !ok {
		a.log.Warn("slack: slash command event type assertion failed")
		return
	}

	a.log.Info("slack: slash command received",
		"command", cmd.Command,
		"user", cmd.UserID,
		"channel", cmd.ChannelID,
		"text", cmd.Text,
	)

	a.socketMode.Ack(*evt.Request) //nolint:errcheck // Ack must not block event processing

	if a.slashLimiter != nil && !a.slashLimiter.Allow(cmd.UserID) {
		a.log.Warn("slack: slash command rate limited", "user_id", cmd.UserID)
		a.sendEphemeralOrPost(ctx, cmd.ChannelID, "", cmd.UserID, "⚠️ Rate limit exceeded. Please wait a moment.")
		return
	}

	if a.gate != nil {
		result := a.gate.Check("channel", cmd.UserID, false)
		if !result.Allowed {
			a.log.Debug("slack: gate rejected slash command", "reason", result.Reason, "user", cmd.UserID)
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

func (a *Adapter) handleControlCommand(ctx context.Context, cmd slack.SlashCommand, action events.ControlAction, logPrefix, successMsg, errorMsg string) { //nolint:unused // wired via handleSlashCommandEvent
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
		a.log.Warn("slack: adapter closed, dropping slash command", "command", logPrefix)
		return
	}
	if err := a.bridge.Handle(ctx, env, conn); err != nil {
		a.log.Error("slack: control event failed", "command", logPrefix, "session_id", sessionID, "err", err)
		a.sendEphemeralOrPost(ctx, cmd.ChannelID, "", cmd.UserID, errorMsg)
		return
	}

	a.log.Info("slack: control sent", "command", logPrefix, "session_id", sessionID, "user", cmd.UserID)
	a.sendEphemeralOrPost(ctx, cmd.ChannelID, "", cmd.UserID, successMsg)
}

func (a *Adapter) sendEphemeralOrPost(ctx context.Context, channelID, threadTS, userID, text string) { //nolint:unused // wired via slash command handlers
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

func (a *Adapter) deriveSessionIDFromCommand(cmd slack.SlashCommand) string { //nolint:unused // wired via handleControlCommand
	envelope := a.bridge.MakeSlackEnvelope(cmd.TeamID, cmd.ChannelID, "", cmd.UserID, "")
	if envelope == nil {
		return ""
	}
	return envelope.SessionID
}
