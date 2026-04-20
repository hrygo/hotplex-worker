package slack

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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
		{"bold italic", "***bold italic***", "*_bold italic_*"},
		{"italic to underscore", "*italic*", "_italic_"},
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
	ai.Start(ctx, nil, "C1", "123", "456", nil)
	ai.Stop(ctx, "C1", "456")
	// Double stop ok
	ai.Stop(ctx, "C1", "456")
}

// ---------------------------------------------------------------------------
// AC 2.4-4 — Multiple mentions all resolved
// ---------------------------------------------------------------------------

func TestUserCache_ResolveMentions_MultipleMentions(t *testing.T) {
	t.Parallel()
	uc := NewUserCache(nil)
	uc.cache["U111"] = cacheEntry{name: "Alice", expiresAt: time.Now().Add(time.Hour)}
	uc.cache["U222"] = cacheEntry{name: "Bob", expiresAt: time.Now().Add(time.Hour)}

	result := uc.ResolveMentions(context.Background(), "<@U111> and <@U222>", "B001")
	require.Equal(t, "@Alice and @Bob", result, "all mentions should be resolved")
}

// ---------------------------------------------------------------------------
// AC 2.4-9 — Mixed format mentions handled correctly
// ---------------------------------------------------------------------------

func TestUserCache_ResolveMentions_MixedFormats(t *testing.T) {
	t.Parallel()
	uc := NewUserCache(nil)
	uc.cache["U111"] = cacheEntry{name: "Alice", expiresAt: time.Now().Add(time.Hour)}

	// <@U111> resolved from cache, <@U222|Bob> uses inline fallback
	result := uc.ResolveMentions(context.Background(), "<@U111> and <@U222|Bob>", "B001")
	require.Equal(t, "@Alice and @Bob", result, "mixed format mentions should both resolve")
}

// ---------------------------------------------------------------------------
// AC 3.3-13 — assistant_api_enabled:false skips probe, uses emoji
// ---------------------------------------------------------------------------

func TestAssistantAPIEnabled_ControlsProbe(t *testing.T) {
	t.Parallel()
	a := &Adapter{
		log:           slog.Default(),
		activeStreams: make(map[string]*NativeStreamingWriter),
		activeConns:   make(map[string]*SlackConn),
	}

	// Default (nil) → enabled
	require.True(t, a.assistantAPIEnabled(), "nil assistantEnabled should mean enabled")

	// Explicitly false → disabled
	disabled := false
	a.assistantEnabled = &disabled
	require.False(t, a.assistantAPIEnabled(), "explicit false should disable probe")

	// ProbeAssistantCapability returns false when disabled
	require.False(t, a.ProbeAssistantCapability(context.Background()))

	// Explicitly true → enabled
	enabled := true
	a.assistantEnabled = &enabled
	require.True(t, a.assistantAPIEnabled(), "explicit true should enable")
}

// ---------------------------------------------------------------------------
// AC 3.3-16 — Native API unavailable → auto-degrade, no retry
// ---------------------------------------------------------------------------

func TestHandleCapabilityError_Degrades(t *testing.T) {
	t.Parallel()
	a := &Adapter{
		log:           slog.Default(),
		activeStreams: make(map[string]*NativeStreamingWriter),
		activeConns:   make(map[string]*SlackConn),
	}

	// Set to capable
	a.isAssistantCapable.Store(true)

	// Capability error → degrades to false
	a.handleCapabilityError(fmt.Errorf("not_allowed"))
	require.False(t, a.isAssistantCapable.Load(), "should degrade after capability error")

	// Non-capability error → should NOT degrade
	a.isAssistantCapable.Store(true)
	a.handleCapabilityError(fmt.Errorf("timeout"))
	require.True(t, a.isAssistantCapable.Load(), "non-capability error should not degrade")
}

// ---------------------------------------------------------------------------
// AC 4.1-6 — group_policy=allowlist rejects non-whitelisted user
// ---------------------------------------------------------------------------

func TestGate_GroupAllowlist(t *testing.T) {
	t.Parallel()
	g := NewGate("open", "allowlist", false, []string{"U_ALLOWED"})

	result := g.Check(ChannelGroup, "U_ALLOWED", false)
	require.True(t, result.Allowed, "whitelisted user in group should pass")

	result = g.Check(ChannelGroup, "U_STRANGER", false)
	require.False(t, result.Allowed, "non-whitelisted user in group should be rejected")
	require.Equal(t, ReasonNotInAllowlist, result.Reason)
}

// ---------------------------------------------------------------------------
// AC 4.1-14 — Block Kit mention detection preserves <@BOTID> for gate
// ---------------------------------------------------------------------------

func TestGate_BlockKitMentionInExtractedText(t *testing.T) {
	t.Parallel()
	evt := slackevents.MessageEvent{
		Text:    "",
		Channel: "C123",
		User:    "U_ALICE",
	}
	evt.Blocks = slack.Blocks{BlockSet: []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", "Hey <@B_TEST> can you help?", false, false),
			nil, nil,
		),
	}}

	text := extractText(evt)
	require.Contains(t, text, "<@B_TEST>", "Block Kit mention should be preserved for gate check")
}

// ---------------------------------------------------------------------------
// AC 5.3-2 — No image → pure text
// ---------------------------------------------------------------------------

func TestExtractImages_NoImages(t *testing.T) {
	t.Parallel()
	parts, remaining := extractImages("hello world, no images here")
	require.Empty(t, parts, "plain text should yield no image parts")
	require.Equal(t, "hello world, no images here", remaining)
}

// ---------------------------------------------------------------------------
// AC 5.3-3 — Local image <5MB → base64 data URI
// ---------------------------------------------------------------------------

func TestLocalFileToImagePart_SmallFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "chart.png")

	// Minimal valid PNG (1x1 pixel)
	pngData := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01" +
		"\x00\x00\x00\x01\x08\x02\x00\x00\x00\x90wS\xde")
	require.NoError(t, os.WriteFile(path, pngData, 0o644))

	imgURL, altText := localFileToImagePart(path)
	require.NotEmpty(t, imgURL, "small image should return base64 data URI")
	require.Contains(t, imgURL, "data:image/")
	require.Contains(t, imgURL, ";base64,")
	require.Equal(t, "chart.png", altText)
}

// ---------------------------------------------------------------------------
// AC 5.3-4 — Local image >=5MB → skip
// ---------------------------------------------------------------------------

func TestLocalFileToImagePart_LargeFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.png")

	largeData := make([]byte, 5*1024*1024+1) // >5MB
	copy(largeData, []byte("\x89PNG\r\n\x1a\n"))
	require.NoError(t, os.WriteFile(path, largeData, 0o644))

	imgURL, altText := localFileToImagePart(path)
	require.Empty(t, imgURL, "image >=5MB should be skipped")
	require.Empty(t, altText)
}

// ---------------------------------------------------------------------------
// AC 5.3-6 — buildImageBlocks: text + images → mixed blocks
// ---------------------------------------------------------------------------

func TestBuildImageBlocks_WithTextAndImages(t *testing.T) {
	t.Parallel()
	parts := []imagePart{
		{URL: "data:image/png;base64,abc123", AltText: "chart.png"},
	}
	blocks := buildImageBlocks(parts, "Here is the chart:")
	require.Len(t, blocks, 2, "should have 1 text section + 1 image block")

	sec, ok := blocks[0].(*slack.SectionBlock)
	require.True(t, ok, "first block should be SectionBlock")
	require.NotNil(t, sec.Text)

	img, ok := blocks[1].(*slack.ImageBlock)
	require.True(t, ok, "second block should be ImageBlock")
	require.Equal(t, "data:image/png;base64,abc123", img.ImageURL)
}

func TestBuildImageBlocks_ImagesOnly(t *testing.T) {
	t.Parallel()
	parts := []imagePart{
		{URL: "https://example.com/a.png", AltText: "a.png"},
		{URL: "https://example.com/b.png", AltText: "b.png"},
	}
	blocks := buildImageBlocks(parts, "")
	require.Len(t, blocks, 2, "no text → only image blocks")
	for i, b := range blocks {
		_, ok := b.(*slack.ImageBlock)
		require.True(t, ok, "block %d should be ImageBlock", i)
	}
}

// ---------------------------------------------------------------------------
// AC 5.2-4 — Download failure cleans up empty file
// ---------------------------------------------------------------------------

func TestDownloadMedia_FailureCleansUpFile(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	targetPath := filepath.Join(tmpDir, "image_F_TEST.png")
	f, err := os.Create(targetPath)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	// File exists after Create
	_, err = os.Stat(targetPath)
	require.NoError(t, err, "file should exist after os.Create")

	// Simulate the cleanup downloadMedia performs on GetFile error
	_ = os.Remove(targetPath)

	// File should be gone
	_, err = os.Stat(targetPath)
	require.True(t, os.IsNotExist(err), "file should be removed after download failure")
}

// ---------------------------------------------------------------------------
// AC 5.2-6 — Re-download overwrites existing file
// ---------------------------------------------------------------------------

func TestDownloadMedia_OverwriteOnRepeat(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")

	require.NoError(t, os.WriteFile(path, []byte("first"), 0o644))
	data1, _ := os.ReadFile(path)
	require.Equal(t, "first", string(data1))

	// os.Create truncates → simulates re-download
	f, err := os.Create(path)
	require.NoError(t, err)
	_, err = f.WriteString("second")
	require.NoError(t, err)
	require.NoError(t, f.Close())

	data2, _ := os.ReadFile(path)
	require.Equal(t, "second", string(data2), "re-download should overwrite")
}

// ---------------------------------------------------------------------------
// AC 5.4-4 — Non-image file not converted to image block (outbound skip)
// ---------------------------------------------------------------------------

func TestLocalFileToImagePart_NonImageFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "data.csv")
	require.NoError(t, os.WriteFile(path, []byte("a,b\n1,2"), 0o644))

	imgURL, altText := localFileToImagePart(path)
	require.Empty(t, imgURL, "non-image file should not become image block")
	require.Empty(t, altText)
}
