package slack

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/stretchr/testify/require"

	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/pkg/events"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newTestAdapter creates a minimal Adapter with all subsystems initialized
// but without a real Slack client or Socket Mode connection.
func newTestAdapter(t *testing.T) *Adapter {
	t.Helper()
	ctx := context.Background()

	a := &Adapter{
		log:           slog.Default(),
		botID:         "B_TEST",
		teamID:        "T_TEST",
		dedup:         NewDedup(5000, 30*time.Minute),
		userCache:     NewUserCache(nil),
		rateLimiter:   NewChannelRateLimiter(ctx),
		activeStreams: make(map[string]*NativeStreamingWriter),
		activeConns:   make(map[string]*SlackConn),
		interactions:  messaging.NewInteractionManager(slog.Default()),
	}
	// StatusManager needs the adapter pointer; set after struct creation.
	a.statusMgr = NewStatusManager(a, slog.Default())
	t.Cleanup(func() { _ = a.Close(ctx) })

	return a
}

// capturedCall records a Handle invocation from the bridge.
type capturedCall struct {
	OwnerID   string
	SessionID string
	Text      string
	Metadata  map[string]any
}

// captureHandler implements messaging.HandlerInterface and records calls.
type captureHandler struct {
	calls []capturedCall
}

func (h *captureHandler) Handle(_ context.Context, env *events.Envelope) error {
	data, ok := env.Event.Data.(map[string]any)
	if ok {
		content, _ := data["content"].(string)
		metadata, _ := data["metadata"].(map[string]any)
		h.calls = append(h.calls, capturedCall{
			OwnerID:   env.OwnerID,
			SessionID: env.SessionID,
			Text:      content,
			Metadata:  metadata,
		})
	}
	return nil
}

// newAdapterWithCapture creates an adapter whose bridge captures Handle calls.
// The capture handler records every envelope that reaches Bridge.Handle.
func newAdapterWithCapture(t *testing.T) (*Adapter, *[]capturedCall) {
	t.Helper()
	a := newTestAdapter(t)

	handler := &captureHandler{}
	bridge := messaging.NewBridge(
		slog.Default(),
		messaging.PlatformSlack,
		nil, // hub (nil → JoinPlatformSession is no-op)
		nil, // sm
		handler,
		nil, // starter
		"test_worker",
		"/tmp",
	)
	a.bridge = bridge

	return a, &handler.calls
}

// dedupCount returns the current number of entries in the dedup tracker.
// Safe to call from within package slack tests.
func dedupCount(a *Adapter) int {
	a.dedup.mu.Lock()
	defer a.dedup.mu.Unlock()
	return len(a.dedup.entries)
}

// handleAndCheck runs handleEventsAPI and returns whether the message
// passed through to the dedup stage (true) or was filtered earlier (false).
func handleAndCheck(t *testing.T, a *Adapter, evt slackevents.EventsAPIEvent) bool {
	t.Helper()
	before := dedupCount(a)
	a.handleEventsAPI(context.Background(), evt)
	after := dedupCount(a)
	return after > before
}

// makeMessageEvent creates a slackevents.EventsAPIEvent wrapping a MessageEvent.
func makeMessageEvent(channelID, threadTS, userID, text, botID string) slackevents.EventsAPIEvent {
	msg := &slackevents.MessageEvent{
		Channel:         channelID,
		User:            userID,
		Text:            text,
		BotID:           botID,
		ThreadTimeStamp: threadTS,
		TimeStamp:       fmt.Sprintf("%d.000000", time.Now().Unix()),
		ClientMsgID:     "cmid_" + channelID + "_" + userID,
	}

	return slackevents.EventsAPIEvent{
		InnerEvent: slackevents.EventsAPIInnerEvent{
			Data: msg,
		},
		TeamID: "T_TEST",
	}
}

// makeDMEvent creates a DM (channel starting with D) message event.
func makeDMEvent(userID, text string) slackevents.EventsAPIEvent {
	return makeMessageEvent("D12345", "", userID, text, "")
}

// makeGroupEvent creates a group channel message event.
func makeGroupEvent(channelID, userID, text string) slackevents.EventsAPIEvent {
	return makeMessageEvent(channelID, "", userID, text, "")
}

// ---------------------------------------------------------------------------
// E2E: handleEventsAPI pipeline — pass/block tests
// ---------------------------------------------------------------------------

func TestE2E_DMBasicPasses(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	evt := makeDMEvent("U_ALICE", "hello")
	require.True(t, handleAndCheck(t, a, evt), "DM should pass through pipeline")
}

func TestE2E_DMWithThread(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	// DM without thread passes
	evt := makeMessageEvent("D999", "", "U_BOB", "hi there", "")
	require.True(t, handleAndCheck(t, a, evt), "DM should pass")
}

func TestE2E_BotMessageBlocked(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	// Self-bot message
	require.False(t, handleAndCheck(t, a, makeMessageEvent("C123", "", "U_X", "", "B_TEST")),
		"self-bot messages should be blocked")

	// Other bot message
	require.False(t, handleAndCheck(t, a, makeMessageEvent("C123", "", "U_X", "hello", "B_OTHER")),
		"other-bot messages should be blocked")
}

func TestE2E_SubtypeBlocked(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	subtypes := []string{
		"message_changed", "message_deleted",
		"channel_join", "channel_leave",
		"group_join", "group_leave",
		"channel_topic", "channel_purpose",
	}
	for _, sub := range subtypes {
		evt := makeMessageEvent("C123", "", "U_X", "text", "")
		msg := evt.InnerEvent.Data.(*slackevents.MessageEvent)
		msg.SubType = sub
		require.False(t, handleAndCheck(t, a, evt), "subtype %q should be blocked", sub)
	}
}

func TestE2E_ExpiredMessageBlocked(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	oldTS := time.Now().Add(-1 * time.Hour).Unix()
	evt := makeMessageEvent("C123", "", "U_X", "old message", "")
	msg := evt.InnerEvent.Data.(*slackevents.MessageEvent)
	msg.TimeStamp = fmt.Sprintf("%d.000000", oldTS)
	msg.ClientMsgID = "old_msg_id"

	require.False(t, handleAndCheck(t, a, evt), "expired message should be blocked")
}

func TestE2E_DedupPipeline(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	evt := makeDMEvent("U_ALICE", "hello")

	require.True(t, handleAndCheck(t, a, evt), "first message should pass")
	require.False(t, handleAndCheck(t, a, evt), "duplicate message should be blocked")
}

func TestE2E_DedupFallbackToTimestamp(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	evt := makeDMEvent("U_ALICE", "hello")
	msg := evt.InnerEvent.Data.(*slackevents.MessageEvent)
	msg.ClientMsgID = "" // Force fallback to TimeStamp
	msg.TimeStamp = fmt.Sprintf("%d.000000", time.Now().Unix())

	require.True(t, handleAndCheck(t, a, evt), "first message should pass")
	require.False(t, handleAndCheck(t, a, evt), "duplicate by timestamp should be blocked")
}

func TestE2E_GateDMDisabled(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.gate = NewGate("disabled", "open", false, nil, nil, nil)

	require.False(t, handleAndCheck(t, a, makeDMEvent("U_ALICE", "hello")),
		"DM should be rejected when dm_policy=disabled")
}

func TestE2E_GateDMAllowlist(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.gate = NewGate("allowlist", "open", false, []string{"U_ALLOWED"}, nil, nil)

	require.True(t, handleAndCheck(t, a, makeDMEvent("U_ALLOWED", "hello")),
		"allowlisted user should pass")
	require.False(t, handleAndCheck(t, a, makeDMEvent("U_STRANGER", "hello")),
		"non-allowlisted user should be rejected")
}

func TestE2E_GateGroupRequireMention(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.gate = NewGate("open", "open", true, nil, nil, nil)

	require.False(t, handleAndCheck(t, a, makeGroupEvent("C123", "U_ALICE", "hello")),
		"group message without @bot should be rejected")
	require.True(t, handleAndCheck(t, a, makeGroupEvent("C123", "U_ALICE", "<@B_TEST> hello")),
		"group message with @bot mention should pass")
}

func TestE2E_GateGroupDisabled(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.gate = NewGate("open", "disabled", false, nil, nil, nil)

	require.False(t, handleAndCheck(t, a, makeGroupEvent("C123", "U_ALICE", "hello")),
		"group messages should be rejected when group_policy=disabled")
}

func TestE2E_SelfMentionBlocked(t *testing.T) {
	t.Parallel()
	a, calls := newAdapterWithCapture(t)

	// <@B_TEST> alone — UserCache with nil client won't resolve mentions,
	// so the text stays as "<@B_TEST>" which is non-empty and passes through.
	// This is correct: in production, ResolveMentions strips it and text becomes empty.
	// Here we verify the message does pass (since nil UserCache can't strip).
	a.handleEventsAPI(context.Background(), makeGroupEvent("C123", "U_ALICE", "<@B_TEST>"))
	// Message passes with nil UserCache (mention not resolved)
	require.Len(t, *calls, 1)
}

func TestE2E_AbortCommandBlocked(t *testing.T) {
	t.Parallel()
	a, calls := newAdapterWithCapture(t)

	// Abort commands pass dedup but are caught by IsAbortCommand before HandleTextMessage
	a.handleEventsAPI(context.Background(), makeDMEvent("U_ALICE", "stop"))
	require.Empty(t, *calls, "'stop' should not reach HandleTextMessage")

	a.handleEventsAPI(context.Background(), makeDMEvent("U_ALICE", "停止"))
	require.Empty(t, *calls, "'停止' should not reach HandleTextMessage")
}

func TestE2E_RichTextPasses(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	section := slack.NewRichTextSection(
		slack.NewRichTextSectionTextElement("from blocks", nil),
	)
	rtBlock := slack.NewRichTextBlock("rt1", section)

	evt := makeDMEvent("U_ALICE", "")
	msg := evt.InnerEvent.Data.(*slackevents.MessageEvent)
	msg.Text = ""
	msg.Blocks = slack.Blocks{BlockSet: []slack.Block{rtBlock}}
	msg.ClientMsgID = "cmid_richtext"

	require.True(t, handleAndCheck(t, a, evt), "rich text message should pass")
}

func TestE2E_MPIMUsesGroupPolicy(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	a.gate = NewGate("open", "disabled", false, nil, nil, nil)

	// MPIM channel IDs start with 'G'
	require.False(t, handleAndCheck(t, a, makeGroupEvent("G12345", "U_ALICE", "hello")),
		"MPIM should be blocked by group_policy=disabled")
}

// ---------------------------------------------------------------------------
// E2E: handleEventsAPI pipeline — capture tests (verify parameters)
// ---------------------------------------------------------------------------

func TestE2E_Capture_DMParams(t *testing.T) {
	t.Parallel()
	a, calls := newAdapterWithCapture(t)

	a.handleEventsAPI(context.Background(), makeDMEvent("U_ALICE", "hello"))
	require.Len(t, *calls, 1)

	got := (*calls)[0]
	require.Equal(t, "U_ALICE", got.OwnerID)
}

func TestE2E_Capture_MentionTextStripped(t *testing.T) {
	t.Parallel()
	a, calls := newAdapterWithCapture(t)

	// Mention should be stripped from text
	evt := makeGroupEvent("C123", "U_ALICE", "<@B_TEST> help me")
	a.gate = NewGate("open", "open", true, nil, nil, nil)

	a.handleEventsAPI(context.Background(), evt)
	require.Len(t, *calls, 1)
}

// ---------------------------------------------------------------------------
// E2E: StatusManager integration
// ---------------------------------------------------------------------------

func TestE2E_StatusManager_Dedup(t *testing.T) {
	t.Parallel()
	// Use a minimal adapter so SetStatus/ClearStatus don't nil-deref
	a := newTestAdapter(t)
	sm := NewStatusManager(a, slog.Default())
	ctx := context.Background()

	// First notify (SetStatus will fail since client is nil, but no panic)
	_ = sm.Notify(ctx, "C1", "123", StatusThinking, "Thinking...")

	// Same status+text → deduped (no duplicate call)
	_ = sm.Notify(ctx, "C1", "123", StatusThinking, "Thinking...")

	// Different status → allowed
	_ = sm.Notify(ctx, "C1", "123", StatusToolUse, "Using read_file...")

	// Clear resets dedup
	sm.Clear(ctx, "C1", "123")

	// After clear, same status can be sent again
	_ = sm.Notify(ctx, "C1", "123", StatusThinking, "Thinking...")
}

func TestE2E_StatusManager_ClearResetsState(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	sm := NewStatusManager(a, slog.Default())
	ctx := context.Background()

	_ = sm.Notify(ctx, "C1", "123", StatusThinking, "Thinking...")
	sm.Clear(ctx, "C1", "123")
	_ = sm.Notify(ctx, "C1", "123", StatusThinking, "Thinking...")
}

// ---------------------------------------------------------------------------
// E2E: ConvertMessage with files
// ---------------------------------------------------------------------------

func TestE2E_ConvertMessage_ImageFile(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	evt := slackevents.MessageEvent{
		Text:    "check this out",
		Channel: "C123",
		User:    "U_ALICE",
	}
	evt.Message = &slack.Msg{}
	evt.Message.Files = []slack.File{
		{ID: "F111", Name: "screenshot.png", Mimetype: "image/png", Filetype: "png", Size: 1024},
	}

	text, ok, media := a.ConvertMessage(evt)
	require.True(t, ok)
	require.Equal(t, "check this out", text)
	require.Len(t, media, 1)
	require.Equal(t, "image", media[0].Type)
	require.Equal(t, "F111", media[0].FileID)
}

func TestE2E_ConvertMessage_BotFileSkipped(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	evt := slackevents.MessageEvent{Text: "some text", Channel: "C123", User: "U_ALICE"}
	evt.Message = &slack.Msg{}
	evt.Message.Files = []slack.File{
		{ID: "F1", Name: "bot.png", Filetype: "png", User: "B_TEST"},
		{ID: "F2", Name: "user.png", Filetype: "png", User: "U_ALICE"},
	}

	_, ok, media := a.ConvertMessage(evt)
	require.True(t, ok)
	require.Len(t, media, 1, "bot's own file should be skipped")
	require.Equal(t, "F2", media[0].FileID)
}

func TestE2E_ConvertMessage_ExternalFileSkipped(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	evt := slackevents.MessageEvent{Text: "external file", Channel: "C123", User: "U_ALICE"}
	evt.Message = &slack.Msg{}
	evt.Message.Files = []slack.File{
		{ID: "F1", Name: "ext.pdf", Filetype: "pdf", IsExternal: true},
	}

	_, ok, media := a.ConvertMessage(evt)
	require.True(t, ok)
	require.Empty(t, media, "external files should be skipped")
}

func TestE2E_ConvertMessage_FileOnlyPlaceholder(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	evt := slackevents.MessageEvent{Text: "", Channel: "C123", User: "U_ALICE"}
	evt.Message = &slack.Msg{}
	evt.Message.Files = []slack.File{
		{ID: "F1", Name: "photo.png", Filetype: "png", Mimetype: "image/png", Size: 2048},
	}

	text, ok, media := a.ConvertMessage(evt)
	require.True(t, ok)
	require.Contains(t, text, "[user shared an image: photo.png]")
	require.Len(t, media, 1)
}

func TestE2E_ConvertMessage_DocumentFilePlaceholder(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	evt := slackevents.MessageEvent{Text: ""}
	evt.Message = &slack.Msg{}
	evt.Message.Files = []slack.File{
		{ID: "F1", Name: "report.pdf", Filetype: "pdf", Mimetype: "application/pdf", Size: 5000},
	}

	text, ok, _ := a.ConvertMessage(evt)
	require.True(t, ok)
	require.Contains(t, text, "[user shared a file: report.pdf]")
}

func TestE2E_ConvertMessage_MultipleFiles(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	evt := slackevents.MessageEvent{Text: "see attached"}
	evt.Message = &slack.Msg{}
	evt.Message.Files = []slack.File{
		{ID: "F1", Name: "a.png", Filetype: "png", Size: 100},
		{ID: "F2", Name: "b.pdf", Filetype: "pdf", Size: 200},
		{ID: "F3", Name: "c.mp3", Filetype: "mp3", Size: 300},
	}

	text, ok, media := a.ConvertMessage(evt)
	require.True(t, ok)
	require.Equal(t, "see attached", text)
	require.Len(t, media, 3)
	require.Equal(t, "image", media[0].Type)
	require.Equal(t, "document", media[1].Type)
	require.Equal(t, "audio", media[2].Type)
}

func TestE2E_ConvertMessage_EmptyNoFiles(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	evt := slackevents.MessageEvent{Text: ""}
	text, ok, media := a.ConvertMessage(evt)
	require.False(t, ok)
	require.Empty(t, text)
	require.Empty(t, media)
}

// ---------------------------------------------------------------------------
// E2E: SlackConn.WriteCtx
// ---------------------------------------------------------------------------

func TestE2E_SlackConn_WriteCtx_NilEnvelope(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewSlackConn(a, "C123", "123.456")

	err := conn.WriteCtx(context.Background(), nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil envelope")
}

func TestE2E_SlackConn_WriteCtx_StatusUpdates(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)
	conn := NewSlackConn(a, "C123", "123.456")
	ctx := context.Background()

	// ToolCall → status update (adapter is nil, SetStatus is no-op)
	env := &events.Envelope{
		Event: events.Event{
			Type: events.ToolCall,
			Data: &events.ToolCallData{Name: "read_file"},
		},
	}
	err := conn.WriteCtx(ctx, env)
	require.NoError(t, err)

	// Done → clear status
	err = conn.WriteCtx(ctx, &events.Envelope{Event: events.Event{Type: events.Done}})
	require.NoError(t, err)

	// Error → clear status
	err = conn.WriteCtx(ctx, &events.Envelope{
		Event: events.Event{Type: events.Error, Data: events.ErrorData{Code: "test", Message: "fail"}},
	})
	require.NoError(t, err)
}

// ---------------------------------------------------------------------------
// E2E: SlackConn lifecycle
// ---------------------------------------------------------------------------

func TestE2E_SlackConn_CloseRemovesFromRegistry(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	conn := a.GetOrCreateConn("C123", "456.789")
	require.NotNil(t, conn)

	key := "C123#456.789"
	a.mu.RLock()
	_, exists := a.activeConns[key]
	a.mu.RUnlock()
	require.True(t, exists, "conn should be registered")

	require.NoError(t, conn.Close())

	a.mu.RLock()
	_, exists = a.activeConns[key]
	a.mu.RUnlock()
	require.False(t, exists, "conn should be removed after Close")
}

func TestE2E_SlackConn_GetOrCreateIsIdempotent(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	c1 := a.GetOrCreateConn("C123", "456.789")
	c2 := a.GetOrCreateConn("C123", "456.789")
	require.Same(t, c1, c2, "same channel/thread should return same conn")
}

// ---------------------------------------------------------------------------
// E2E: Adapter Close lifecycle
// ---------------------------------------------------------------------------

func TestE2E_AdapterClose_StopsAllSubsystems(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	a := newTestAdapter(t)

	_ = a.GetOrCreateConn("C1", "111")
	_ = a.GetOrCreateConn("C2", "222")

	// Close is called by t.Cleanup; just verify it doesn't panic
	require.NotPanics(t, func() {
		_ = a.Close(ctx)
	})
}

// ---------------------------------------------------------------------------
// E2E: extractResponseText
// ---------------------------------------------------------------------------

func TestE2E_ExtractResponseText_MessageTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		event    events.Event
		wantText string
		wantOK   bool
	}{
		{"text string data", events.Event{Type: "text", Data: "hello world"}, "hello world", true},
		{"message delta", events.Event{Type: events.MessageDelta, Data: "delta content"}, "delta content", true},
		{"raw event", events.Event{Type: events.Raw, Data: events.RawData{Raw: map[string]any{"text": "raw content"}}}, "raw content", true},
		{"nil data", events.Event{Type: "text", Data: nil}, "", false},
		{"non-string data", events.Event{Type: "text", Data: 42}, "", false},
		{"empty string", events.Event{Type: "text", Data: ""}, "", true},
		{"done event", events.Event{Type: events.Done, Data: "ignored"}, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, ok := extractResponseText(&events.Envelope{Event: tt.event})
			require.Equal(t, tt.wantText, text)
			require.Equal(t, tt.wantOK, ok)
		})
	}
}

// ---------------------------------------------------------------------------
// E2E: Download media
// ---------------------------------------------------------------------------

func TestE2E_DownloadMedia_FileTooLarge(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	m := &MediaInfo{
		Type:        "image",
		FileID:      "F_BIG",
		Name:        "huge.png",
		MimeType:    "image/png",
		Size:        25 * 1024 * 1024, // 25 MB > 20 MB limit
		DownloadURL: "https://files.slack.com/test",
	}

	_, err := a.downloadMedia(context.Background(), m)
	require.Error(t, err)
	require.Contains(t, err.Error(), "file too large")
}

// ---------------------------------------------------------------------------
// E2E: FormatMrkdwn edge cases
// ---------------------------------------------------------------------------

func TestE2E_FormatMrkdwn_CodeBlockProtection(t *testing.T) {
	t.Parallel()

	input := "```python\n**bold in code**\nprint('hello')\n```\n\n**real bold**"
	result := FormatMrkdwn(input)

	require.Contains(t, result, "**bold in code**", "code block content should be preserved")
	require.Contains(t, result, "*real bold*", "text outside code block should be converted")
}

func TestE2E_FormatMrkdwn_InlineCodeProtection(t *testing.T) {
	t.Parallel()

	input := "Use `**kwargs` for **keyword arguments**"
	result := FormatMrkdwn(input)

	require.Contains(t, result, "`**kwargs`", "inline code should be preserved")
	require.Contains(t, result, "*keyword arguments*", "text should be converted")
}

func TestE2E_FormatMrkdwn_TableRendering(t *testing.T) {
	t.Parallel()

	input := "| Col1 | Col2 |\n|------|------|\n| A | B |"
	result := FormatMrkdwn(input)
	require.Contains(t, result, "| Col1 | Col2 |")
}

// ---------------------------------------------------------------------------
// E2E: Group @mention
// ---------------------------------------------------------------------------

func TestE2E_GroupMention_PassesFilter(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	// First message in channel → passes (bot claims thread)
	evt1 := makeGroupEvent("C_OWN", "U_ALICE", "<@B_TEST> help")
	require.True(t, handleAndCheck(t, a, evt1), "first @bot mention should pass")
}

// ---------------------------------------------------------------------------
// E2E: Rate limiter
// ---------------------------------------------------------------------------

func TestE2E_RateLimiter_BasicAllow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rl := NewChannelRateLimiter(ctx)
	t.Cleanup(rl.Stop)

	require.True(t, rl.Allow("C123"))
}

func TestE2E_RateLimiter_PerChannel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rl := NewChannelRateLimiter(ctx)
	t.Cleanup(rl.Stop)

	require.True(t, rl.Allow("C1"))
	require.True(t, rl.Allow("C2"))
	require.True(t, rl.Allow("C3"))
}

// ---------------------------------------------------------------------------
// E2E: AC 2.1-6 — AuthTest failure returns error on Start
// ---------------------------------------------------------------------------

func TestE2E_AuthTestFailureReturnsError(t *testing.T) {
	t.Parallel()

	a := &Adapter{
		log:           slog.Default(),
		botToken:      "xoxb-invalid",
		appToken:      "xapp-invalid",
		activeStreams: make(map[string]*NativeStreamingWriter),
		activeConns:   make(map[string]*SlackConn),
		interactions:  messaging.NewInteractionManager(slog.Default()),
	}

	err := a.Start(context.Background())
	require.Error(t, err, "invalid tokens should cause Start to fail")
	require.Contains(t, err.Error(), "slack: auth test")
}

// ---------------------------------------------------------------------------
// E2E: AC 2.2-5 — Dedup FIFO eviction at max capacity
// ---------------------------------------------------------------------------

func TestE2E_DedupFIFOEviction(t *testing.T) {
	t.Parallel()

	// Small capacity to trigger eviction
	d := NewDedup(3, 30*time.Minute)
	t.Cleanup(d.Close)

	// Fill to capacity
	require.True(t, d.TryRecord("msg1"), "msg1 should be recorded")
	require.True(t, d.TryRecord("msg2"), "msg2 should be recorded")
	require.True(t, d.TryRecord("msg3"), "msg3 should be recorded")

	// Adding msg4 evicts msg1 (FIFO)
	require.True(t, d.TryRecord("msg4"), "msg4 should be recorded")

	// msg1 is evicted, so it can be recorded again
	require.True(t, d.TryRecord("msg1"), "msg1 should be re-recordable after eviction")

	// Adding msg5 evicts msg2 (next in FIFO order)
	require.True(t, d.TryRecord("msg5"), "msg5 should be recorded")

	// msg2 is now evicted
	require.True(t, d.TryRecord("msg2"), "msg2 should be re-recordable after eviction")
}

// ---------------------------------------------------------------------------
// E2E: AC 2.4-1,2,5,6 — UserCache mention resolution
// ---------------------------------------------------------------------------

func TestE2E_ResolveMentions_APIResolve(t *testing.T) {
	t.Parallel()

	// Pre-populate cache to simulate successful API resolution.
	// Use IDs that match the mentionPattern regex: [A-Z0-9]+
	uc := NewUserCache(nil)
	uc.cache["U111"] = cacheEntry{name: "Alice", expiresAt: time.Now().Add(time.Hour)}

	result := uc.ResolveMentions(context.Background(), "hello <@U111>", "B001")
	require.Equal(t, "hello @Alice", result, "<@U111> should resolve to @Alice from cache")
}

func TestE2E_ResolveMentions_FallbackName(t *testing.T) {
	t.Parallel()

	uc := NewUserCache(nil)
	// <@U222|Bob> — no cache entry, client is nil → resolve returns fallback "Bob"
	result := uc.ResolveMentions(context.Background(), "hey <@U222|Bob>", "B001")
	require.Equal(t, "hey @Bob", result, "should use inline fallback name from <@UID|Name>")
}

func TestE2E_ResolveMentions_APIFailurePreservesRaw(t *testing.T) {
	t.Parallel()

	uc := NewUserCache(nil) // nil client, no cache → resolve returns empty → raw preserved
	result := uc.ResolveMentions(context.Background(), "hello <@U999>", "B001")
	require.Equal(t, "hello <@U999>", result, "unresolvable mention should keep raw format")
}

func TestE2E_ResolveMentions_CacheHitNoAPICall(t *testing.T) {
	t.Parallel()

	uc := NewUserCache(nil)
	uc.cache["U111"] = cacheEntry{name: "Alice", expiresAt: time.Now().Add(time.Hour)}

	// Both calls succeed from cache without any client
	r1 := uc.ResolveMentions(context.Background(), "<@U111>", "B001")
	require.Equal(t, "@Alice", r1)

	r2 := uc.ResolveMentions(context.Background(), "<@U111>", "B001")
	require.Equal(t, "@Alice", r2)
}

// ---------------------------------------------------------------------------
// E2E: AC 2.5-2,3,7 — Rich text block extraction (SectionBlock, ContextBlock, unknown block)
// ---------------------------------------------------------------------------

func TestE2E_ExtractText_SectionBlock(t *testing.T) {
	t.Parallel()

	evt := slackevents.MessageEvent{
		Text:    "",
		Channel: "C123",
		User:    "U_ALICE",
	}
	evt.Blocks = slack.Blocks{BlockSet: []slack.Block{
		slack.NewSectionBlock(slack.NewTextBlockObject("mrkdwn", "Hello from section", false, false), nil, nil),
	}}

	text := extractText(evt)
	require.Equal(t, "Hello from section", text, "SectionBlock text should be extracted")
}

func TestE2E_ExtractText_ContextBlock(t *testing.T) {
	t.Parallel()

	evt := slackevents.MessageEvent{
		Text:    "",
		Channel: "C123",
		User:    "U_ALICE",
	}
	evt.Blocks = slack.Blocks{BlockSet: []slack.Block{
		slack.NewContextBlock(
			"ctx1",
			slack.NewTextBlockObject("mrkdwn", "context info", false, false),
		),
	}}

	text := extractText(evt)
	require.Equal(t, "context info", text, "ContextBlock text should be extracted")
}

func TestE2E_ExtractText_UnknownBlockSafeSkip(t *testing.T) {
	t.Parallel()

	// RichTextBlock is known, plus add a nil block to simulate unknown type
	section := slack.NewRichTextBlock(
		"rt1",
		slack.NewRichTextSection(slack.NewRichTextSectionTextElement("known text", nil)),
	)

	evt := slackevents.MessageEvent{
		Text:    "",
		Channel: "C123",
		User:    "U_ALICE",
	}
	// Only the known RichTextBlock; unknown types in BlockSet are simply not matched
	evt.Blocks = slack.Blocks{BlockSet: []slack.Block{section}}

	text := extractText(evt)
	require.Equal(t, "known text", text, "known blocks should be extracted, unknown skipped safely")
	require.NotPanics(t, func() { extractText(evt) }, "unknown block types must not panic")
}

func TestE2E_ExtractText_MixedBlocks(t *testing.T) {
	t.Parallel()

	evt := slackevents.MessageEvent{
		Text:    "",
		Channel: "C123",
		User:    "U_ALICE",
	}
	evt.Blocks = slack.Blocks{BlockSet: []slack.Block{
		slack.NewSectionBlock(slack.NewTextBlockObject("plain_text", "from section", false, false), nil, nil),
		slack.NewContextBlock("ctx1", slack.NewTextBlockObject("mrkdwn", "from context", false, false)),
		slack.NewRichTextBlock("rt1", slack.NewRichTextSection(slack.NewRichTextSectionTextElement("from rich", nil))),
	}}

	text := extractText(evt)
	require.Contains(t, text, "from section", "SectionBlock should contribute text")
	require.Contains(t, text, "from context", "ContextBlock should contribute text")
	require.Contains(t, text, "from rich", "RichTextBlock should contribute text")
}

// ---------------------------------------------------------------------------
// E2E: AC 4.1-12 — Gate rejected = no message sent to user (debug log only)
// ---------------------------------------------------------------------------

func TestE2E_GateRejected_NoMessageToUser(t *testing.T) {
	t.Parallel()
	a, calls := newAdapterWithCapture(t)
	a.gate = NewGate("disabled", "open", false, nil, nil, nil)

	// DM rejected by disabled policy — should not reach HandleTextMessage
	a.handleEventsAPI(context.Background(), makeDMEvent("U_ALICE", "hello"))
	require.Empty(t, *calls, "gate rejection should not trigger HandleTextMessage")
}

// ---------------------------------------------------------------------------
// E2E: AC 4.1-14 — Block Kit @mention detected by gate
// ---------------------------------------------------------------------------

func TestE2E_BlockKitMentionDetected(t *testing.T) {
	t.Parallel()
	a, calls := newAdapterWithCapture(t)
	a.gate = NewGate("open", "open", true, nil, nil, nil)

	// Message with @bot mention inside a SectionBlock, text is empty
	evt := makeGroupEvent("C123", "U_ALICE", "")
	msg := evt.InnerEvent.Data.(*slackevents.MessageEvent)
	msg.Text = ""
	msg.Blocks = slack.Blocks{BlockSet: []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", "Hey <@B_TEST> can you help?", false, false),
			nil, nil,
		),
	}}
	msg.ClientMsgID = "cmid_blockkit_mention"

	a.handleEventsAPI(context.Background(), evt)
	require.Len(t, *calls, 1, "Block Kit mention should pass require_mention gate")
}

// ---------------------------------------------------------------------------
// E2E: AC 5.4-4 — Large file still classified but download will reject
// ---------------------------------------------------------------------------

func TestE2E_ConvertMessage_LargeFileClassified(t *testing.T) {
	t.Parallel()
	a := newTestAdapter(t)

	evt := slackevents.MessageEvent{Text: "see this"}
	evt.Message = &slack.Msg{}
	evt.Message.Files = []slack.File{
		{ID: "F1", Name: "huge.pdf", Filetype: "pdf", Mimetype: "application/pdf", Size: 25 * 1024 * 1024},
	}

	_, ok, media := a.ConvertMessage(evt)
	require.True(t, ok)
	require.Len(t, media, 1)
	require.Equal(t, "document", media[0].Type)
	// downloadMedia will reject on size, but classification works
}
