package slack

import (
	"context"
	"testing"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/stretchr/testify/require"
)

func TestChannelRateLimiter_Allow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rl := NewChannelRateLimiter(ctx)
	t.Cleanup(rl.Stop)

	// First calls should be allowed (burst = 3, starts full)
	require.True(t, rl.Allow("C1"))
	require.True(t, rl.Allow("C1"))
	require.True(t, rl.Allow("C1"))
}

func TestChannelRateLimiter_DifferentChannels(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rl := NewChannelRateLimiter(ctx)
	t.Cleanup(rl.Stop)

	require.True(t, rl.Allow("C1"))
	require.True(t, rl.Allow("C2"))
	require.True(t, rl.Allow("C3"))
}

func TestThreadOwnershipTracker_NoThread(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tr := NewThreadOwnershipTracker(ctx, "BOT1", nil)
	t.Cleanup(tr.Stop)

	// DM or main channel (no thread): always respond
	require.True(t, tr.ShouldRespond("channel", "", "hello", "U1"))
	require.True(t, tr.ShouldRespond("im", "", "hello", "U1"))
}

func TestThreadOwnershipTracker_FirstMessage(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tr := NewThreadOwnershipTracker(ctx, "BOT1", nil)
	t.Cleanup(tr.Stop)

	// First message in thread, not mentioned → don't respond (R1)
	require.False(t, tr.ShouldRespond("channel", "123.456", "hello", "U1"))
}

func TestThreadOwnershipTracker_Mentioned(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tr := NewThreadOwnershipTracker(ctx, "BOT1", nil)
	t.Cleanup(tr.Stop)

	// Mentioned in thread → claim ownership
	require.True(t, tr.ShouldRespond("channel", "123.456", "<@BOT1> hello", "U1"))

	// Subsequent non-@ message → owner responds (R2)
	require.True(t, tr.ShouldRespond("channel", "123.456", "follow up", "U1"))
}

func TestThreadOwnershipTracker_OtherBot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	tr := NewThreadOwnershipTracker(ctx, "BOT1", nil)
	t.Cleanup(tr.Stop)

	// Other bot mentioned, not us → release (R5)
	require.False(t, tr.ShouldRespond("channel", "123.456", "<@BOT2> help", "U1"))
}

func TestSplitChunks(t *testing.T) {
	t.Parallel()

	// ASCII
	chunks := splitChunks("hello world", 5)
	require.Len(t, chunks, 3)
	require.Equal(t, "hello", chunks[0])
	require.Equal(t, " worl", chunks[1])
	require.Equal(t, "d", chunks[2])

	// Unicode (CJK) — splitChunks works on runes, maxLen is rune count
	chunks = splitChunks("你好世界", 2) // 4 runes, split at 2
	require.Len(t, chunks, 2)
	require.Equal(t, "你好", chunks[0])
	require.Equal(t, "世界", chunks[1])

	// Empty
	chunks = splitChunks("", 10)
	require.Empty(t, chunks)

	// Smaller than chunk size
	chunks = splitChunks("hi", 10)
	require.Len(t, chunks, 1)
	require.Equal(t, "hi", chunks[0])
}

func TestNativeStreamingWriter_Expired(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	w := NewNativeStreamingWriter(ctx, nil, "C1", "123", nil, nil)
	// Simulate expired stream
	w.started = false
	w.streamStartTime = time.Now().Add(-20 * time.Minute)

	_, err := w.Write([]byte("test"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "stream expired")
}

func TestNativeStreamingWriter_DoubleClose(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	w := NewNativeStreamingWriter(ctx, nil, "C1", "123", nil, nil)

	// First close
	require.NoError(t, w.Close())
	// Second close should be no-op
	require.NoError(t, w.Close())
}

func TestNativeStreamingWriter_WriteAfterClose(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	w := NewNativeStreamingWriter(ctx, nil, "C1", "123", nil, nil)
	require.NoError(t, w.Close())

	_, err := w.Write([]byte("test"))
	require.Error(t, err)
	require.Contains(t, err.Error(), "stream already closed")
}

func TestNativeStreamingWriter_EmptyWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	w := NewNativeStreamingWriter(ctx, nil, "C1", "123", nil, nil)

	// Empty write should not error and not start stream
	n, err := w.Write([]byte{})
	require.NoError(t, err)
	require.Equal(t, 0, n)
	require.False(t, w.started)
}

func TestExtractText(t *testing.T) {
	t.Parallel()

	// Plain text
	event := slackevents.MessageEvent{Text: "hello world"}
	require.Equal(t, "hello world", extractText(event))

	// Empty text
	event = slackevents.MessageEvent{Text: ""}
	require.Equal(t, "", extractText(event))

	// From blocks (markdown)
	event = slackevents.MessageEvent{
		Blocks: slack.Blocks{BlockSet: []slack.Block{
			slack.NewSectionBlock(
				slack.NewTextBlockObject(slack.MarkdownType, "*bold* hello", false, false),
				nil, nil,
			),
		}},
	}
	require.Equal(t, "bold hello", extractText(event))
}

func TestExtractThreadTS(t *testing.T) {
	t.Parallel()

	event := slackevents.MessageEvent{ThreadTimeStamp: "1234567890.123456"}
	require.Equal(t, "1234567890.123456", extractThreadTS(event))
}

func TestIsBotMessage(t *testing.T) {
	t.Parallel()

	// Bot message via BotID
	event := slackevents.MessageEvent{Text: "hello", BotID: "B123"}
	require.True(t, isBotMessage(event))

	// Bot message via SubType
	event = slackevents.MessageEvent{Text: "hello", SubType: "bot_message"}
	require.True(t, isBotMessage(event))

	// Regular user message
	event = slackevents.MessageEvent{Text: "hello"}
	require.False(t, isBotMessage(event))
}
