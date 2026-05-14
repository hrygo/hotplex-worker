package feishu

import (
	"context"
	"fmt"
	"strconv"

	larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"

	"github.com/hrygo/hotplex/internal/config"
	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/pkg/events"
)

func (a *Adapter) handleMessage(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
	if event.Event == nil || event.Event.Message == nil {
		return nil
	}

	msg := event.Event.Message

	// Step 2: Bot self-message defense.
	if event.Event.Sender != nil {
		senderType := ptrStr(event.Event.Sender.SenderType)
		if senderType == "app" {
			return nil
		}
	}

	// Step 3: Message expiry check (30 minutes).
	if msg.CreateTime != nil && *msg.CreateTime != "" {
		createTimeMs, err := strconv.ParseInt(*msg.CreateTime, 10, 64)
		if err == nil && IsMessageExpired(createTimeMs) {
			return nil
		}
	}

	// Step 4: Dedup.
	messageID := ptrStr(msg.MessageId)
	if messageID == "" {
		return nil
	}
	a.mu.RLock()
	dedup := a.Dedup
	a.mu.RUnlock()
	if dedup == nil {
		return nil // adapter is closing
	}
	if !dedup.TryRecord(messageID) {
		return nil
	}

	// Step 5: Message type conversion.
	msgType := ptrStr(msg.MessageType)
	text, ok, medias := ConvertMessage(msgType, ptrStr(msg.Content), msg.Mentions, a.botOpenID, messageID)
	if !ok || text == "" {
		return nil
	}
	text = messaging.SanitizeText(text)

	var hasVoice bool
	if len(medias) > 0 {
		var paths, transcriptions []string
		paths, transcriptions, hasVoice = a.processMediaAttachments(ctx, medias)
		if len(paths) > 0 || len(transcriptions) > 0 {
			text = BuildMediaPrompt(text, paths, medias, transcriptions)
		}
	}

	// Step 6: @Mention resolution is done inside ConvertMessage for text/post types.

	// Extract routing info.
	chatType := ptrStr(msg.ChatType)
	chatID := ptrStr(msg.ChatId)
	rootID := ptrStr(msg.RootId)
	parentID := ptrStr(msg.ParentId)
	threadKey := rootID
	if threadKey == "" {
		threadKey = ptrStr(msg.ThreadId)
	}
	userID := ""
	if event.Event.Sender != nil && event.Event.Sender.SenderId != nil {
		userID = ptrStr(event.Event.Sender.SenderId.OpenId)
	}

	// Step 7: Access control.
	botMentioned := isBotMentioned(msg.Mentions, a.botOpenID)
	if a.Gate != nil {
		if allowed, reason := a.Gate.Check(chatType == "p2p", userID, botMentioned); !allowed {
			a.Log.Debug("feishu: gate rejected", "reason", reason, "chat", chatID, "user", userID)
			return nil
		}
	}

	// Step 8: Abort fast-path.
	if messaging.IsAbortCommand(text) {
		a.chatQueue.Abort(chatID)
		return nil
	}

	// Step 9: All message processing (including control commands) goes through
	// chatQueue to serialize execution per chatID, preventing races between
	// reset's Terminate→Start and the next message's Input() call.
	replyToMsgID := parentID
	if replyToMsgID == "" {
		replyToMsgID = rootID
	}

	return a.chatQueue.Enqueue(chatID, func(qtx context.Context) error {
		cmd := messaging.DetectCommand(text)
		switch cmd.Action {
		case messaging.CmdHelp:
			_ = a.replyMessage(qtx, messageID, messaging.HelpText(), false)
			return nil
		case messaging.CmdControl:
			a.handleTextControlCommand(qtx, chatID, userID, threadKey, messageID, cmd.Control)
			return nil
		case messaging.CmdWorker:
			a.handleTextWorkerCommand(qtx, chatID, chatType, userID, threadKey, messageID, replyToMsgID, cmd.Worker)
			return nil
		}

		a.Log.Debug("feishu: handling message",
			"chat_type", chatType,
			"chat", chatID,
			"user", userID,
			"thread_key", threadKey,
			"text_len", len(text),
		)

		return a.handleTextMessage(qtx, messageID, chatID, chatType, userID, text, threadKey, replyToMsgID, hasVoice)
	})
}

func isBotMentioned(mentions []*larkim.MentionEvent, botOpenID string) bool {
	if botOpenID == "" {
		return false
	}
	for _, m := range mentions {
		if m.Id != nil && m.Id.OpenId != nil && *m.Id.OpenId == botOpenID {
			return true
		}
	}
	return false
}

func (a *Adapter) handleTextMessage(ctx context.Context, platformMsgID, channelID, chatType, userID, text, threadKey, replyToMsgID string, voiceTriggered bool) error {
	if a.Bridge() == nil {
		return nil
	}

	conn := a.GetOrCreateConn(channelID, threadKey)

	if voiceTriggered {
		conn.voiceTriggered.Store(true)
	}

	envelope := a.Bridge().MakeFeishuEnvelope(channelID, threadKey, userID, text, conn.WorkDir(), a.botOpenID)
	if envelope == nil {
		return fmt.Errorf("feishu: failed to build envelope")
	}

	if md, ok := envelope.Event.Data.(map[string]any); ok {
		md["platform_msg_id"] = platformMsgID
		md["reply_to_msg_id"] = replyToMsgID
	}

	// Check if this text is a response to a pending interaction.
	if a.checkPendingInteraction(ctx, text, userID, conn) {
		return nil // text consumed as interaction response
	}
	conn.mu.Lock()
	// Clean up stale reactions from previous message before switching platformMsgID.
	if conn.platformMsgID != "" && conn.platformMsgID != platformMsgID {
		if conn.typingRid != "" {
			_ = a.RemoveTypingIndicator(context.Background(), conn.platformMsgID, conn.typingRid)
			conn.typingRid = ""
		}
	}
	conn.replyToMsgID = replyToMsgID
	conn.platformMsgID = platformMsgID
	conn.chatType = chatType
	conn.mu.Unlock()

	// Typing indicator: add reaction to user's message (non-blocking, failure is non-fatal).
	if platformMsgID != "" {
		if rid, err := a.AddTypingIndicator(ctx, platformMsgID); err == nil && rid != "" {
			conn.SetTypingReactionID(rid)
		} else if err != nil {
			a.Log.Debug("feishu: typing indicator failed (non-fatal)", "err", err)
		}
	}

	// Start silence timer: fires THINKING reaction after 30s of no worker events.
	conn.resetSilenceTimer()

	// Prepare streaming controller (card is lazily created on first content).
	if a.larkClient != nil && a.rateLimiter != nil {
		// Check if streaming is already active — if so, skip placeholder to avoid
		// creating multiple concurrent streaming cards for the same conn.
		if ctrl := conn.GetStreamCtrl(); ctrl != nil && ctrl.IsCreated() {
			a.Log.Debug("feishu: skipping placeholder, streaming already active")
		} else {
			turnNum, model, branch, workDir := conn.turnHeaderMeta()
			ctrl := NewStreamingCardController(a.larkClient, a.rateLimiter, a.Log, a.resolveBotName(), turnNum+1, model, branch, workDir, a.phrases)
			conn.EnableStreaming(ctrl)

			// Send placeholder card immediately — same streaming card structure as real messages.
			// This eliminates the "black hole" effect where users see nothing while the worker processes.
			if err := ctrl.SendPlaceholder(ctx, channelID, chatType, replyToMsgID); err != nil {
				a.Log.Warn("feishu: placeholder card failed (non-fatal)", "err", err)
			}
		}
	}

	err := a.Bridge().Handle(ctx, envelope, conn)
	if err != nil && conn != nil {
		notifyErr := a.sendTextMessage(context.Background(), channelID,
			"抱歉，处理您的请求时遇到问题，请稍后重试。")
		if notifyErr != nil {
			a.Log.Warn("feishu: failed to send error notification",
				"chat", channelID, "original_err", err, "notify_err", notifyErr)
		}
	}
	return err
}

func (a *Adapter) HandleTextMessage(ctx context.Context, platformMsgID, channelID, teamID, threadTS, userID, text string) error {
	return a.handleTextMessage(ctx, platformMsgID, channelID, "p2p", userID, text, "", "", false)
}

func (a *Adapter) GetOrCreateConn(chatID, threadKey string) *FeishuConn {
	return a.BaseAdapter.GetOrCreateConn(chatID, threadKey)
}

func (a *Adapter) handleTextControlCommand(ctx context.Context, chatID, userID, threadKey, platformMsgID string, result *messaging.ControlCommandResult) {
	conn := a.GetOrCreateConn(chatID, threadKey)
	envelope := a.Bridge().MakeFeishuEnvelope(chatID, threadKey, userID, "", conn.WorkDir(), a.botOpenID)
	if envelope == nil {
		a.Log.Warn("feishu: text control command failed to derive session", "action", result.Label)
		return
	}

	ctrlEnv := messaging.BuildControlEnvelope(result, envelope.SessionID, userID)

	// CD sends progress feedback before execution; other actions send completion feedback after.
	if result.Action == events.ControlActionCD {
		_ = a.replyOrSend(ctx, platformMsgID, chatID, controlFeedbackMessageCN(result.Action))
	}

	if err := a.Bridge().Handle(ctx, ctrlEnv, conn); err != nil {
		a.Log.Warn("feishu: text control command failed", "action", result.Label, "err", err)
		// Provide user-friendly error message with details
		errMsg := fmt.Sprintf("❌ 执行 %s 失败：%s", result.Label, formatSecurityError(err))
		if replyErr := a.replyMessage(ctx, platformMsgID, errMsg, false); replyErr != nil {
			a.Log.Error("feishu: failed to send error message", "action", result.Label, "user", userID, "err", replyErr)
		}
		return
	}

	a.Log.Info("feishu: text control command sent", "action", result.Label, "user", userID, "session_id", envelope.SessionID)

	// After a successful CD, update conn's workDir so subsequent messages
	// derive the correct session ID for the target directory.
	if result.Action == events.ControlActionCD && result.Arg != "" {
		if expanded, err := config.ExpandAndAbs(result.Arg); err == nil {
			conn.SetWorkDir(expanded)
		}
	}

	// Reset/GC kills the worker without a guaranteed done event, so stale
	// pending interactions (permission/question/elicitation) may survive.
	// Cancel them now to prevent the next user message from being consumed
	// by checkPendingInteraction as a response to a dead interaction.
	if result.Action == events.ControlActionReset || result.Action == events.ControlActionGC {
		a.Interactions.CancelAll(envelope.SessionID)
		// Abort any active streaming card — GC/Reset kills the worker without a
		// done event, so the card would otherwise remain in streaming state.
		conn.mu.RLock()
		ctrl := conn.streamCtrl
		conn.mu.RUnlock()
		if ctrl != nil {
			_ = ctrl.Abort(ctx)
		}
		// Clear cached turn metadata so the next turn starts fresh.
		conn.mu.Lock()
		conn.turnCount = 0
		conn.lastModel = ""
		conn.lastBranch = ""
		conn.mu.Unlock()
	}

	// Completion feedback for non-CD actions (CD feedback was sent before execution).
	if result.Action != events.ControlActionCD {
		_ = a.replyOrSend(ctx, platformMsgID, chatID, controlFeedbackMessageCN(result.Action))
	}
}

func (a *Adapter) handleTextWorkerCommand(ctx context.Context, chatID, chatType, userID, threadKey, platformMsgID, replyToMsgID string, result *messaging.WorkerCommandResult) {
	conn := a.GetOrCreateConn(chatID, threadKey)
	envelope := a.Bridge().MakeFeishuEnvelope(chatID, threadKey, userID, "", conn.WorkDir(), a.botOpenID)
	if envelope == nil {
		a.Log.Warn("feishu: worker command failed to derive session", "command", result.Label)
		return
	}

	cmdEnv := messaging.BuildWorkerCommandEnvelope(result, envelope.SessionID, userID)

	// Set conn fields for async response delivery.
	conn.mu.Lock()
	conn.platformMsgID = platformMsgID
	conn.replyToMsgID = replyToMsgID
	conn.chatType = chatType
	conn.mu.Unlock()

	if err := a.Bridge().Handle(ctx, cmdEnv, conn); err != nil {
		a.Log.Warn("feishu: worker command failed", "command", result.Label, "err", err)
		_ = a.replyOrSend(ctx, platformMsgID, chatID, fmt.Sprintf("❌ 执行 %s 失败。", result.Label))
		return
	}

	a.Log.Info("feishu: worker command sent", "command", result.Label, "user", userID, "session_id", envelope.SessionID)
}
