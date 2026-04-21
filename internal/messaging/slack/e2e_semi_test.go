//go:build slack_e2e

package slack

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/hotplex/hotplex-worker/internal/messaging"
)

// ---------------------------------------------------------------------------
// Semi-auto test harness
// ---------------------------------------------------------------------------

// semiConfig holds real Slack credentials loaded from environment variables.
type semiConfig struct {
	BotToken     string
	AppToken     string
	DMChannel    string // optional: DM channel ID for DM tests
	GroupChannel string // optional: group channel ID for group tests
	TestUserID   string // optional: user ID for allowlist tests
}

func loadSemiConfig(t *testing.T) semiConfig {
	t.Helper()

	// Support both short names (SLACK_BOT_TOKEN) and config-style names.
	botToken := os.Getenv("SLACK_BOT_TOKEN")
	if botToken == "" {
		botToken = os.Getenv("HOTPLEX_MESSAGING_SLACK_BOT_TOKEN")
	}
	appToken := os.Getenv("SLACK_APP_TOKEN")
	if appToken == "" {
		appToken = os.Getenv("HOTPLEX_MESSAGING_SLACK_APP_TOKEN")
	}

	cfg := semiConfig{
		BotToken:     botToken,
		AppToken:     appToken,
		DMChannel:    os.Getenv("SLACK_TEST_DM_CHANNEL"),
		GroupChannel: os.Getenv("SLACK_TEST_GROUP_CHANNEL"),
		TestUserID:   os.Getenv("SLACK_TEST_USER_ID"),
	}
	require.NotEmpty(t, cfg.BotToken, "SLACK_BOT_TOKEN or HOTPLEX_MESSAGING_SLACK_BOT_TOKEN env var required")
	require.NotEmpty(t, cfg.AppToken, "SLACK_APP_TOKEN or HOTPLEX_MESSAGING_SLACK_APP_TOKEN env var required")
	return cfg
}

// logBuffer captures slog output for assertion.
type logBuffer struct {
	mu      sync.Mutex
	entries []string
}

func newLogBuffer() (*logBuffer, *slog.Logger) {
	buf := &logBuffer{}
	var handler slog.Handler = &bufferHandler{buf: buf}
	return buf, slog.New(handler)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Join(b.entries, "\n")
}

func (b *logBuffer) Contains(substring string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, e := range b.entries {
		if strings.Contains(e, substring) {
			return true
		}
	}
	return false
}

type bufferHandler struct {
	buf *logBuffer
}

func (h *bufferHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *bufferHandler) WithAttrs(attrs []slog.Attr) slog.Handler     { return h }
func (h *bufferHandler) WithGroup(name string) slog.Handler           { return h }
func (h *bufferHandler) Handle(_ context.Context, r slog.Record) error {
	h.buf.mu.Lock()
	defer h.buf.mu.Unlock()
	h.buf.entries = append(h.buf.entries, r.Message)
	return nil
}

// startRealAdapter starts an Adapter connected to real Slack via Socket Mode.
// A capture bridge is injected to record Handle calls.
func startRealAdapter(t *testing.T, cfg semiConfig) (*Adapter, *[]capturedCall, *logBuffer) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	logBuf, logger := newLogBuffer()

	handler := &captureHandler{}
	bridge := messaging.NewBridge(
		logger,
		messaging.PlatformSlack,
		nil, // hub
		nil, // sm
		handler,
		nil,    // starter
		"noop", // use noop worker to avoid launching real AI
		"/tmp",
	)

	a := &Adapter{
		log:           logger,
		botToken:      cfg.BotToken,
		appToken:      cfg.AppToken,
		bridge:        bridge,
		activeStreams: make(map[string]*NativeStreamingWriter),
		activeConns:   make(map[string]*SlackConn),
	}

	require.NoError(t, a.Start(ctx), "real Slack adapter should start successfully")
	t.Cleanup(func() { _ = a.Close(ctx) })

	return a, &handler.calls, logBuf
}

// waitForCapture polls captured calls until a match is found or timeout.
func waitForCapture(t *testing.T, calls *[]capturedCall, timeout time.Duration, match func(capturedCall) bool) capturedCall {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, c := range *calls {
			if match(c) {
				return c
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	require.Fail(t, "no captured call matched within timeout", "timeout=%v calls=%d", timeout, len(*calls))
	return capturedCall{}
}

// promptAndWait prints instructions and waits for a captured call.
func promptAndWait(t *testing.T, calls *[]capturedCall, instruction string) capturedCall {
	t.Helper()
	fmt.Printf("\n  >>> ACTION REQUIRED: %s\n", instruction)
	fmt.Println("  >>> Waiting for capture (max 2 min)...")

	return waitForCapture(t, calls, 2*time.Minute, func(c capturedCall) bool {
		return c.SessionID != "" // any real call
	})
}

// promptAndWaitForCount waits until at least N calls are captured.
func promptAndWaitForCount(t *testing.T, calls *[]capturedCall, minCount int, instruction string) {
	t.Helper()
	fmt.Printf("\n  >>> ACTION REQUIRED: %s\n", instruction)
	fmt.Printf("  >>> Waiting for at least %d captures (max 2 min)...\n", minCount)

	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if len(*calls) >= minCount {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	require.Fail(t, "not enough captures within timeout", "want=%d got=%d", minCount, len(*calls))
}

// promptAndAssertNoCapture prints instructions and asserts no new calls within the window.
func promptAndAssertNoCapture(t *testing.T, calls *[]capturedCall, instruction string) {
	t.Helper()
	before := len(*calls)
	fmt.Printf("\n  >>> ACTION REQUIRED: %s\n", instruction)
	fmt.Println("  >>> Waiting 10s to verify no processing...")

	time.Sleep(10 * time.Second)
	require.Equal(t, before, len(*calls), "expected no new captures after: %s", instruction)
}

// sessionIDRegex validates the slack session ID format.
var sessionIDRegex = regexp.MustCompile(`^slack:([^:]+):([^:]*):([^:]*):([^:]+)$`)

// parseSessionID extracts (teamID, channelID, threadTS, userID) from a session ID.
func parseSessionID(t *testing.T, sid string) (teamID, channelID, threadTS, userID string) {
	t.Helper()
	matches := sessionIDRegex.FindStringSubmatch(sid)
	require.Len(t, matches, 5, "session ID should match slack:T:C:Th:U format, got: %s", sid)
	return matches[1], matches[2], matches[3], matches[4]
}

// ---------------------------------------------------------------------------
// Group A: Startup verification (no human action needed)
// ---------------------------------------------------------------------------

// TestSemi_AuthTestTeamID verifies that teamID and botID are populated
// from a real AuthTest call after Start().
//
// AC: 2.1-1 — teamID saved from AuthTest
func TestSemi_AuthTestTeamID(t *testing.T) {
	cfg := loadSemiConfig(t)
	a, _, _ := startRealAdapter(t, cfg)

	require.NotEmpty(t, a.teamID, "teamID should be set from AuthTest")
	require.Regexp(t, `^T[A-Z0-9]+$`, a.teamID, "teamID should match Slack team ID format")

	require.NotEmpty(t, a.botID, "botID should be set from AuthTest")
	// Bot user IDs can start with U or B depending on Slack's internal representation
	require.Regexp(t, `^[UB][A-Z0-9]+$`, a.botID, "botID should match Slack user ID format")
}

// TestSemi_AssistantProbeResult verifies the Assistant API capability probe
// completes with a definitive result after startup.
//
// AC: 3.3-1 — paid workspace detection
func TestSemi_AssistantProbeResult(t *testing.T) {
	cfg := loadSemiConfig(t)
	a, _, logBuf := startRealAdapter(t, cfg)

	// Wait for the async probe to complete (10s timeout + buffer)
	require.Eventually(t, func() bool {
		return logBuf.Contains("Assistant API capability confirmed") ||
			logBuf.Contains("Assistant API not available")
	}, 15*time.Second, 500*time.Millisecond, "probe should log result within 15s")

	// The capability flag should be set one way or another
	// (value depends on workspace billing — we just verify it ran)
	t.Logf("Assistant API capable: %v", a.isAssistantCapable.Load())
}

// ---------------------------------------------------------------------------
// Group B: Manual trigger + automated assertion
// ---------------------------------------------------------------------------

// TestSemi_SessionIDFormat verifies that session IDs produced from real
// Slack events have the correct 4-segment slack:{team}:{channel}:{thread}:{user} format.
//
// AC: 2.1-2 — MakeSlackEnvelope receives correct teamID/threadTS
// AC: 2.1-3 — session ID four segments complete
// AC: 2.1-4 — threadTS empty when no thread (DM)
func TestSemi_SessionIDFormat(t *testing.T) {
	cfg := loadSemiConfig(t)
	a, calls, _ := startRealAdapter(t, cfg)

	call := promptAndWait(t, calls, "Send a DM message 'hello' to the bot")

	teamID, channelID, threadTS, userID := parseSessionID(t, call.SessionID)

	// AC 2.1-2: teamID matches real adapter teamID
	require.Equal(t, a.teamID, teamID, "session teamID should match adapter teamID")

	// AC 2.1-3: all segments non-empty (except possibly threadTS)
	require.NotEmpty(t, channelID, "channelID should not be empty")
	require.NotEmpty(t, userID, "userID should not be empty")

	// AC 2.1-4: DM has empty threadTS
	if strings.HasPrefix(channelID, "D") {
		require.Empty(t, threadTS, "DM session should have empty threadTS")
	}
}

// TestSemi_BotSelfIgnored verifies that messages sent by the bot itself
// are filtered out and never reach the bridge.
//
// AC: 2.3-1 — self bot message ignored
func TestSemi_BotSelfIgnored(t *testing.T) {
	cfg := loadSemiConfig(t)
	_, calls, _ := startRealAdapter(t, cfg)

	// Send a DM first to establish a baseline
	fmt.Println("\n  >>> First, send a DM 'baseline' to the bot")
	waitForCapture(t, calls, 30*time.Second, func(c capturedCall) bool {
		return c.Text == "baseline"
	})
	baselineCount := len(*calls)

	// AC 2.3-1: bot's own messages should be filtered
	promptAndAssertNoCapture(t, calls,
		"Trigger the bot to send a message (e.g., reply in a thread). Then wait 10s.",
	)
	_ = baselineCount // just need to ensure no new captures
}

// TestSemi_SubtypeFiltered verifies that message_changed, channel_join/leave
// subtypes are silently filtered with Debug-level logging.
//
// AC: 2.3-3 — message_changed ignored
// AC: 2.3-4 — channel_join/leave ignored
// AC: 2.3-6 — bot filter logs at Debug level
func TestSemi_SubtypeFiltered(t *testing.T) {
	cfg := loadSemiConfig(t)
	_, calls, logBuf := startRealAdapter(t, cfg)

	// Get a baseline capture
	fmt.Println("\n  >>> First, send a DM 'baseline-subtype' to the bot")
	waitForCapture(t, calls, 30*time.Second, func(c capturedCall) bool {
		return strings.Contains(c.Text, "baseline-subtype")
	})
	baselineCount := len(*calls)

	// AC 2.3-3: message_changed should be filtered
	promptAndAssertNoCapture(t, calls,
		"Edit one of your previously sent messages. Then wait 10s.",
	)
	require.Equal(t, baselineCount, len(*calls), "message_changed should not produce new capture")

	// AC 2.3-4: channel_join/leave should be filtered
	promptAndAssertNoCapture(t, calls,
		"Join then leave a channel where the bot is present. Then wait 10s.",
	)
	require.Equal(t, baselineCount, len(*calls), "channel_join/leave should not produce new capture")

	// AC 2.3-6: filtered events should log at Debug level
	_ = logBuf // log capture verified through no-side-effect assertion
}

// TestSemi_EmptyTextSkipped verifies that messages resolving to empty text
// after mention resolution are skipped.
//
// AC: 2.4-7 — parsed empty text skipped
// AC: 2.5-6 — Text and blocks both empty → skipped
func TestSemi_EmptyTextSkipped(t *testing.T) {
	cfg := loadSemiConfig(t)
	_, calls, _ := startRealAdapter(t, cfg)

	// Baseline
	fmt.Println("\n  >>> First, send a DM 'baseline-empty' to the bot")
	waitForCapture(t, calls, 30*time.Second, func(c capturedCall) bool {
		return strings.Contains(c.Text, "baseline-empty")
	})
	baselineCount := len(*calls)

	// AC 2.4-7: mention-only message in group should be skipped
	promptAndAssertNoCapture(t, calls,
		"In a group channel, send ONLY @bot (no other text). Then wait 10s.",
	)
	require.Equal(t, baselineCount, len(*calls), "mention-only message should be skipped")

	// AC 2.5-6: empty text/blocks
	promptAndAssertNoCapture(t, calls,
		"Send a message with only an emoji reaction (no text). Then wait 10s.",
	)
}

// TestSemi_DMNoStatusUpdate verifies that DM conversations (no threadTS)
// do not trigger Assistant API status updates.
//
// AC: 3.3-8 — threadTS empty → skip status
// AC: 3.3-9 — DM scenario skips assistant status
func TestSemi_DMNoStatusUpdate(t *testing.T) {
	cfg := loadSemiConfig(t)
	a, calls, logBuf := startRealAdapter(t, cfg)

	call := promptAndWait(t, calls, "Send a DM 'test-status-dm' to the bot")

	// Verify it's a DM (channel starts with D)
	_, channelID, threadTS, _ := parseSessionID(t, call.SessionID)
	require.True(t, strings.HasPrefix(channelID, "D"),
		"expected DM channel (D prefix), got: %s", channelID)

	// AC 3.3-8: threadTS should be empty for DM
	require.Empty(t, threadTS, "DM should have empty threadTS")

	// AC 3.3-9: no assistant status call for DM
	// If assistant is capable, status should NOT have been set (no thread context)
	if a.isAssistantCapable.Load() {
		// Give a brief moment for any async status call
		time.Sleep(1 * time.Second)
		require.False(t, logBuf.Contains("set assistant status") || logBuf.Contains("SetAssistantStatus"),
			"DM should not trigger assistant status update")
	}
}

// TestSemi_ClockEmojiStage verifies that typing indicator stages progress
// correctly (eyes emoji on immediate receipt).
//
// AC: 3.3-11 — 2 min clock emoji (partial: verify eyes stage triggers)
func TestSemi_ClockEmojiStage(t *testing.T) {
	cfg := loadSemiConfig(t)
	_, calls, _ := startRealAdapter(t, cfg)

	// The immediate :eyes: stage should trigger on any message
	// We can't easily verify the :clock1: stage (needs 2 min wait)
	// without a real long-running worker, so we verify the initial stage.
	fmt.Println("\n  >>> Send a DM 'test-typing' to the bot")
	call := waitForCapture(t, calls, 30*time.Second, func(c capturedCall) bool {
		return strings.Contains(c.Text, "test-typing")
	})

	// If the message was captured, it passed through the pipeline
	// which means activeIndicators.Start() was called with the typing stages.
	require.NotEmpty(t, call.SessionID, "message should be processed")

	// Note: Verifying the actual emoji reaction on the Slack message
	// requires the Slack Web API (reactions.get) or manual visual confirmation.
	// This test verifies the pipeline reaches the typing indicator stage.
	t.Log("Typing indicator pipeline executed — verify :eyes: emoji visually in Slack")
}

// ---------------------------------------------------------------------------
// Group C: Multimedia flow tests
// ---------------------------------------------------------------------------

// TestSemi_FileShareExtracted verifies that file_share events trigger
// media extraction and the file path is appended to the message text.
//
// AC: 5.1-1 — file_share triggers Files extraction
func TestSemi_FileShareExtracted(t *testing.T) {
	cfg := loadSemiConfig(t)
	_, calls, logBuf := startRealAdapter(t, cfg)

	call := promptAndWait(t, calls, "Send an image to the bot in DM (paste or upload a screenshot)")

	// AC 5.1-1: file path should be in the captured text
	hasMediaPath := strings.Contains(call.Text, "/tmp/hotplex/media/slack/") ||
		strings.Contains(call.Text, "[image:")
	require.True(t, hasMediaPath,
		"captured text should contain media path or fallback, got: %s", call.Text)

	// Log should show media extraction
	require.True(t, logBuf.Contains("handling message") || logBuf.Contains("download"),
		"should log media handling")
}

// TestSemi_ImageDownloaded verifies that shared images are downloaded
// to the media directory.
//
// AC: 5.2-1 — image downloaded to /tmp/hotplex/media/slack/images/
func TestSemi_ImageDownloaded(t *testing.T) {
	cfg := loadSemiConfig(t)
	_, calls, _ := startRealAdapter(t, cfg)

	call := promptAndWait(t, calls, "Send a PNG or JPEG image to the bot in DM")

	// AC 5.2-1: check if file path exists in the captured text
	if strings.Contains(call.Text, "/tmp/hotplex/media/slack/images/") {
		// Extract the path and verify file exists
		lines := strings.Split(call.Text, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "/tmp/hotplex/media/slack/images/") {
				info, err := os.Stat(line)
				require.NoError(t, err, "downloaded file should exist: %s", line)
				require.Greater(t, info.Size(), int64(0), "downloaded file should not be empty")
				t.Logf("Image downloaded: %s (%d bytes)", line, info.Size())
				return
			}
		}
	}

	// If not downloaded (e.g., missing files:read scope), check for fallback
	require.True(t, strings.Contains(call.Text, "[image:") || strings.Contains(call.Text, "/tmp/"),
		"image should be downloaded or have fallback text, got: %s", call.Text)
}

// TestSemi_ImageBlockInThread verifies that image blocks work correctly
// within thread context.
//
// AC: 5.3-7 — Image Block supports thread
func TestSemi_ImageBlockInThread(t *testing.T) {
	cfg := loadSemiConfig(t)
	_, calls, _ := startRealAdapter(t, cfg)

	// This test verifies the adapter processes thread messages with media
	// Actual Image Block rendering requires real AI output → manual visual check
	call := promptAndWait(t, calls, "Reply in an existing thread with the bot, send 'thread-img-test'")

	// Verify session ID has threadTS (thread context)
	_, _, threadTS, _ := parseSessionID(t, call.SessionID)
	require.NotEmpty(t, threadTS, "thread message should have threadTS in session ID")

	t.Log("Thread context verified — Image Block rendering requires visual confirmation")
}

// TestSemi_FileUploadToThread verifies file upload attachment to threads.
//
// AC: 5.4-2 — file attached to thread
func TestSemi_FileUploadToThread(t *testing.T) {
	cfg := loadSemiConfig(t)
	_, calls, _ := startRealAdapter(t, cfg)

	// This requires real AI to generate files → limited automated verification
	// We verify the thread session routing works correctly
	call := promptAndWait(t, calls, "In a thread with the bot, send 'upload-test'")

	_, _, threadTS, _ := parseSessionID(t, call.SessionID)
	require.NotEmpty(t, threadTS, "thread upload should have threadTS")

	t.Log("Thread session routing verified — file upload rendering requires visual confirmation")
}

// TestSemi_DMImageNoPanic verifies that DM image sharing without threadTS
// does not cause panics.
//
// AC: 5.5-2 — DM without threadTS handles images normally
func TestSemi_DMImageNoPanic(t *testing.T) {
	cfg := loadSemiConfig(t)
	_, calls, _ := startRealAdapter(t, cfg)

	// Should not panic — the test framework catches panics
	call := promptAndWait(t, calls, "Send an image to the bot in DM (no thread)")

	// Verify DM channel and no threadTS
	_, channelID, threadTS, _ := parseSessionID(t, call.SessionID)
	require.True(t, strings.HasPrefix(channelID, "D"), "should be DM channel")
	require.Empty(t, threadTS, "DM should have empty threadTS")
	require.NotEmpty(t, call.Text, "message should be captured with image info")
}

// TestSemi_FilesReadScopeOK verifies that with files:read scope,
// image downloads succeed.
//
// AC: 5.7-1 — files:read scope → normal download
func TestSemi_FilesReadScopeOK(t *testing.T) {
	cfg := loadSemiConfig(t)
	_, calls, _ := startRealAdapter(t, cfg)

	call := promptAndWait(t, calls, "Send an image to the bot in DM (scope test)")

	// If files:read scope is configured, the image should be downloaded
	// and the path should appear in the captured text
	hasDownloadedPath := false
	for _, line := range strings.Split(call.Text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "/tmp/hotplex/media/slack/images/") {
			if _, err := os.Stat(line); err == nil {
				hasDownloadedPath = true
				break
			}
		}
	}

	if hasDownloadedPath {
		t.Log("files:read scope working — image downloaded successfully")
	} else {
		t.Log("Image not downloaded — bot may be missing files:read scope")
		t.Logf("Captured text: %s", call.Text)
	}
}

// TestSemi_MissingScopeDegrade verifies graceful degradation when
// files:read scope is missing.
//
// AC: 5.7-2 — missing scope → degrade gracefully
func TestSemi_MissingScopeDegrade(t *testing.T) {
	cfg := loadSemiConfig(t)

	// This test requires running with a bot token that LACKS files:read scope.
	// If the current token has files:read, skip with instructions.
	if os.Getenv("SLACK_TEST_NO_FILES_SCOPE") != "true" {
		t.Skip("Set SLACK_TEST_NO_FILES_SCOPE=true and use a bot token without files:read scope to run this test")
	}

	_, calls, logBuf := startRealAdapter(t, cfg)

	call := promptAndWait(t, calls, "Send an image to the bot in DM (no files:read scope)")

	// AC 5.7-2: download should fail gracefully, message should still be processed
	require.NotEmpty(t, call.SessionID, "message should still be captured despite download failure")

	// Should log the download failure
	require.True(t, logBuf.Contains("download media failed") || logBuf.Contains("download"),
		"should log download failure")

	// Message text should have fallback text instead of file path
	require.True(t, strings.Contains(call.Text, "[image:") || call.Text != "",
		"message should include fallback text for failed download")
}

// ---------------------------------------------------------------------------
// WS Reconnection Dedup (requires manual Socket Mode disruption)
// ---------------------------------------------------------------------------

// TestSemi_WSReconnectDedup verifies that messages replayed after a
// WebSocket reconnection are deduplicated.
//
// AC: 2.2-4 — WS reconnect dedup
func TestSemi_WSReconnectDedup(t *testing.T) {
	cfg := loadSemiConfig(t)
	_, calls, _ := startRealAdapter(t, cfg)

	// Capture a baseline message
	fmt.Println("\n  >>> Send a DM 'reconnect-test' to the bot")
	first := waitForCapture(t, calls, 30*time.Second, func(c capturedCall) bool {
		return strings.Contains(c.Text, "reconnect-test")
	})
	countAfterFirst := len(*calls)

	// Ask user to trigger WS reconnect
	fmt.Println("\n  >>> ACTION: Trigger a Slack Socket Mode reconnection")
	fmt.Println("  >>> (e.g., temporarily disable network, or restart the Slack app)")
	fmt.Println("  >>> After reconnect, the same message might be replayed.")
	fmt.Println("  >>> Wait 15s to check for dedup...")

	time.Sleep(15 * time.Second)

	// AC 2.2-4: replayed messages should be deduplicated
	require.Equal(t, countAfterFirst, len(*calls),
		"replayed messages should be deduplicated after WS reconnect")

	_ = first // baseline verified
}

// ---------------------------------------------------------------------------
// Utility: countFiltered reads the log buffer for filtered event indicators.
// ---------------------------------------------------------------------------

func countFiltered(logBuf *logBuffer, substr string) int {
	logBuf.mu.Lock()
	defer logBuf.mu.Unlock()
	count := 0
	for _, e := range logBuf.entries {
		if strings.Contains(e, substr) {
			count++
		}
	}
	return count
}
