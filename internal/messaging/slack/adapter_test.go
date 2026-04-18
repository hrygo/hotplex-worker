package slack

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/stretchr/testify/require"

	"github.com/hotplex/hotplex-worker/pkg/events"
)

// --- Phase 1.2: Dedup ---

func TestDedup_TryRecord(t *testing.T) {
	t.Parallel()
	d := NewDedup(100, 5*time.Minute)
	t.Cleanup(d.Close)

	// First record succeeds
	require.True(t, d.TryRecord("msg1"))
	// Duplicate rejected
	require.False(t, d.TryRecord("msg1"))
	// Different message succeeds
	require.True(t, d.TryRecord("msg2"))
}

func TestDedup_FIFOEvection(t *testing.T) {
	t.Parallel()
	d := NewDedup(2, 5*time.Minute)
	t.Cleanup(d.Close)

	require.True(t, d.TryRecord("msg1"))
	require.True(t, d.TryRecord("msg2"))
	// Over capacity: msg1 evicted
	require.True(t, d.TryRecord("msg3"))
	// msg1 should be re-recordable
	require.True(t, d.TryRecord("msg1"))
}

func TestDedup_Close(t *testing.T) {
	t.Parallel()
	d := NewDedup(100, 5*time.Minute)
	d.Close()
	// No panic after close
}

// --- Phase 1.3: Bot defense (isBotMessage) ---

func TestIsBotMessage_AllBots(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		event slackevents.MessageEvent
		isBot bool
	}{
		{"bot via BotID", slackevents.MessageEvent{BotID: "B123"}, true},
		{"bot via subtype", slackevents.MessageEvent{SubType: "bot_message"}, true},
		{"user message", slackevents.MessageEvent{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.isBot, isBotMessage(tt.event))
		})
	}
}

// --- Phase 1.5: Rich Text Block extraction ---

func TestExtractText_ContextBlock(t *testing.T) {
	t.Parallel()

	event := slackevents.MessageEvent{
		Blocks: slack.Blocks{BlockSet: []slack.Block{
			slack.NewContextBlock("ctx1", []slack.MixedElement{
				slack.NewTextBlockObject(slack.PlainTextType, "context text", false, false),
			}...),
		}},
	}
	require.Equal(t, "context text", extractText(event))
}

func TestExtractText_RichTextBlock(t *testing.T) {
	t.Parallel()

	section := slack.NewRichTextSection(
		slack.NewRichTextSectionTextElement("hello ", nil),
		slack.NewRichTextSectionTextElement("world", nil),
	)
	rtBlock := slack.NewRichTextBlock("rt1", section)

	event := slackevents.MessageEvent{
		Blocks: slack.Blocks{BlockSet: []slack.Block{rtBlock}},
	}
	require.Equal(t, "hello world", extractText(event))
}

func TestExtractText_EmptyBlocks(t *testing.T) {
	t.Parallel()

	event := slackevents.MessageEvent{
		Blocks: slack.Blocks{BlockSet: []slack.Block{}},
	}
	require.Equal(t, "", extractText(event))
}

// --- Phase 2.1: mrkdwn formatting ---

func TestFormatMrkdwn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"bold", "**bold**", "*bold*"},
		{"heading", "## H2", "*H2*"},
		{"strikethrough", "~~strike~~", "~strike~"},
		{"link", "[text](url)", "<url|text>"},
		{"list item", "- item", "• item"},
		{"code block preserved", "```**bold**```", "```**bold**```"},
		{"inline code preserved", "`**bold**`", "`**bold**`"},
		{"empty", "", ""},
		{"plain text", "hello world", "hello world"},
		{"mixed", "**bold** and `**code**`", "*bold* and `**code**`"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, FormatMrkdwn(tt.input))
		})
	}
}

func TestFormatMrkdwn_Multiline(t *testing.T) {
	t.Parallel()

	input := "## Title\n\n**bold text** and [link](url)\n\n- item 1\n- item 2"
	result := FormatMrkdwn(input)
	require.Contains(t, result, "*Title*")
	require.Contains(t, result, "*bold text*")
	require.Contains(t, result, "<url|link>")
	require.Contains(t, result, "• item 1")
	require.Contains(t, result, "• item 2")
}

// --- Phase 2.2: Abort detection ---

func TestIsAbortCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"stop", "stop", true},
		{"Chinese stop", "停止", true},
		{"stop with period", "Stop.", true},
		{"please stop", "please stop", true},
		{"hello", "hello", false},
		{"stop it", "stop it", false},
		{"empty", "", false},
		{"STOP uppercase", "STOP", true},
		{"Chinese comma", "停止，", true},
		{"cancel", "cancel", true},
		{"abort", "abort", true},
		{"别说了", "别说了", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, IsAbortCommand(tt.input))
		})
	}
}

// --- Phase 2.3: Status ---

func TestAepEventToStatus_ToolCall(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Event: events.Event{
			Type: events.ToolCall,
			Data: &events.ToolCallData{Name: "read_file"},
		},
	}
	status, text := aepEventToStatus(env)
	require.Equal(t, StatusToolUse, status)
	require.Equal(t, "Using read_file...", text)
}

func TestAepEventToStatus_ToolResult(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Event: events.Event{
			Type: events.ToolResult,
			Data: &events.ToolResultData{},
		},
	}
	status, text := aepEventToStatus(env)
	require.Equal(t, StatusToolResult, status)
	require.Equal(t, "Tool completed", text)
}

func TestAepEventToStatus_MessageDelta(t *testing.T) {
	t.Parallel()

	env := &events.Envelope{
		Event: events.Event{
			Type: events.MessageDelta,
			Data: events.MessageDeltaData{Content: "hello"},
		},
	}
	status, text := aepEventToStatus(env)
	require.Equal(t, StatusAnswering, status)
	require.Equal(t, "Composing response...", text)
}

func TestExtractToolName(t *testing.T) {
	t.Parallel()

	// Typed data
	env := &events.Envelope{
		Event: events.Event{
			Type: events.ToolCall,
			Data: &events.ToolCallData{Name: "search_web"},
		},
	}
	require.Equal(t, "search_web", extractToolName(env))

	// Map data
	env2 := &events.Envelope{
		Event: events.Event{
			Type: events.ToolCall,
			Data: map[string]any{"name": "write_file"},
		},
	}
	require.Equal(t, "write_file", extractToolName(env2))

	// Nil data
	env3 := &events.Envelope{
		Event: events.Event{Type: events.ToolCall},
	}
	require.Equal(t, "tool", extractToolName(env3))
}

func TestIsAssistantCapabilityError(t *testing.T) {
	t.Parallel()

	require.True(t, isAssistantCapabilityError(errFake("not_allowed")))
	require.True(t, isAssistantCapabilityError(errFake("not_allowed_token_type")))
	require.False(t, isAssistantCapabilityError(errFake("timeout")))
	require.False(t, isAssistantCapabilityError(nil))
}

type errFake string

func (e errFake) Error() string { return string(e) }

// --- Phase 3.1: Gate ---

func TestGate_DMOpen(t *testing.T) {
	t.Parallel()
	g := NewGate("open", "open", false, nil)
	r := g.Check("im", "U1", false)
	require.True(t, r.Allowed)
}

func TestGate_DMDisabled(t *testing.T) {
	t.Parallel()
	g := NewGate("disabled", "open", false, nil)
	r := g.Check("im", "U1", false)
	require.False(t, r.Allowed)
	require.Equal(t, "dm_disabled", r.Reason)
}

func TestGate_DMAllowlist(t *testing.T) {
	t.Parallel()
	g := NewGate("allowlist", "open", false, []string{"U1"})
	r := g.Check("im", "U1", false)
	require.True(t, r.Allowed)

	r2 := g.Check("im", "U2", false)
	require.False(t, r2.Allowed)
	require.Equal(t, "not_in_allowlist", r2.Reason)
}

func TestGate_GroupDisabled(t *testing.T) {
	t.Parallel()
	g := NewGate("open", "disabled", false, nil)
	r := g.Check("channel", "U1", false)
	require.False(t, r.Allowed)
	require.Equal(t, "group_disabled", r.Reason)
}

func TestGate_RequireMention(t *testing.T) {
	t.Parallel()
	g := NewGate("open", "open", true, nil)

	r := g.Check("channel", "U1", false)
	require.False(t, r.Allowed)
	require.Equal(t, "no_mention", r.Reason)

	r2 := g.Check("channel", "U1", true)
	require.True(t, r2.Allowed)
}

func TestGate_DMNotRequireMention(t *testing.T) {
	t.Parallel()
	g := NewGate("open", "open", true, nil)
	// DM should not require mention
	r := g.Check("im", "U1", false)
	require.True(t, r.Allowed)
}

func TestGate_DefaultOpen(t *testing.T) {
	t.Parallel()
	g := NewGate("", "", false, nil)
	require.True(t, g.Check("im", "U1", false).Allowed)
	require.True(t, g.Check("channel", "U1", false).Allowed)
	require.True(t, g.Check("mpim", "U1", false).Allowed)
}

// --- Phase 3.2: Message expiry ---

func TestParseSlackTS(t *testing.T) {
	t.Parallel()

	ts, err := parseSlackTS("1234567890.123456")
	require.NoError(t, err)
	require.Equal(t, int64(1234567890), ts.Unix())

	_, err = parseSlackTS("")
	require.Error(t, err)

	_, err = parseSlackTS("invalid")
	require.Error(t, err)
}

func TestParseSlackTS_ExpiredMessage(t *testing.T) {
	t.Parallel()

	// A timestamp 1 hour ago
	oldTS := time.Now().Add(-1 * time.Hour).Unix()
	ts, err := parseSlackTS(fmt.Sprintf("%d.000000", oldTS))
	require.NoError(t, err)
	require.True(t, time.Since(ts) > 30*time.Minute)
}

// --- Phase 4: Converter ---

func TestFileCategory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		filetype string
		want     string
	}{
		{"png", "image"},
		{"jpg", "image"},
		{"gif", "image"},
		{"mp4", "video"},
		{"mp3", "audio"},
		{"pdf", "document"},
		{"txt", "document"},
		{"zip", "file"},
	}
	for _, tt := range tests {
		t.Run(tt.filetype, func(t *testing.T) {
			f := slack.File{Filetype: tt.filetype}
			require.Equal(t, tt.want, fileCategory(f))
		})
	}
}

func TestMimeExt(t *testing.T) {
	t.Parallel()

	require.Equal(t, ".jpg", mimeExt("image/jpeg"))
	require.Equal(t, ".png", mimeExt("image/png"))
	require.Equal(t, ".pdf", mimeExt("application/pdf"))
	require.Equal(t, "", mimeExt("unknown/unknown"))
}

// --- Phase 1.4: Mention resolution ---

func TestUserCache_ResolveMentions_SelfMention(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	uc := NewUserCache(nil) // no client needed for self-mention removal

	result := uc.ResolveMentions(ctx, "<@BOT1> hello", "BOT1")
	require.Equal(t, " hello", result)
}

func TestUserCache_ResolveMentions_InlineName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	uc := NewUserCache(nil) // no client, uses inline name fallback

	// <@U111|Bob> should use "Bob" as fallback when no client
	result := uc.ResolveMentions(ctx, "<@U111|Bob> hello", "BOT1")
	require.Equal(t, "@Bob hello", result)
}

func TestUserCache_ResolveMentions_NoMentions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	uc := NewUserCache(nil)

	result := uc.ResolveMentions(ctx, "hello world", "BOT1")
	require.Equal(t, "hello world", result)
}

func TestUserCache_ResolveMentions_UnknownUID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	uc := NewUserCache(nil) // no client, no fallback name

	result := uc.ResolveMentions(ctx, "<@U999> hello", "BOT1")
	require.Equal(t, "<@U999> hello", result)
}

// --- TypingIndicator ---

func TestTypingIndicator_StopIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	ti := NewTypingIndicator(nil, "C1", "123", "456", DefaultStages)
	// Multiple stops should not panic
	ti.Stop(ctx)
	ti.Stop(ctx)
	ti.Stop(ctx)
}

func TestActiveIndicators_StartStop(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	ai := NewActiveIndicators()
	// Start with nil adapter (no reactions added, but no panic)
	ai.Start(ctx, nil, "C1", "123", "456")
	ai.Stop(ctx, "C1", "456")
	// Double stop ok
	ai.Stop(ctx, "C1", "456")
}
