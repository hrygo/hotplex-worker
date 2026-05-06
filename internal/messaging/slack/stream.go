package slack

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/slack-go/slack"
)

const (
	flushInterval     = 150 * time.Millisecond
	flushSize         = 20 // rune count threshold for immediate flush
	maxAppendRetries  = 3
	retryDelay        = 50 * time.Millisecond
	maxAppendSize     = 3000 // Slack limit ~4000, safety margin
	StreamTTL         = 10 * time.Minute
	StreamRotationTTL = 8 * time.Minute // proactive rotation before server 10min limit
)

func isStreamStateError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return containsAny(errStr, []string{
		"message_not_in_streaming_state",
		"not_in_channel",
		"channel_not_found",
		"message_not_found",
	})
}

func isRateLimitError(err error) (bool, time.Duration) {
	if err == nil {
		return false, 0
	}

	var rateLimitErr *slack.RateLimitedError
	if errors.As(err, &rateLimitErr) {
		return true, rateLimitErr.RetryAfter
	}

	errStr := err.Error()
	if strings.Contains(errStr, "429") || strings.Contains(errStr, "rate_limit") {
		return true, time.Second
	}

	return false, 0
}

func isAuthError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return containsAny(errStr, []string{
		"invalid_auth",
		"missing_scope",
		"not_allowed",
		"account_inactive",
		"invalid_token",
		"token_revoked",
	})
}

func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	return !isStreamStateError(err) && !isAuthError(err)
}

func containsAny(str string, substrs []string) bool {
	for _, substr := range substrs {
		if strings.Contains(str, substr) {
			return true
		}
	}
	return false
}

// NativeStreamingWriter wraps Slack's three-phase streaming API
// into a standard io.WriteCloser. First Write() starts the stream,
// subsequent calls buffer content, Close() ends it with fallback.
type NativeStreamingWriter struct {
	// startCtx is the caller's context, used only for the synchronous
	// StartStream call in Write(). Background operations (flushLoop,
	// appendWithRetry) must NOT reference this — the caller (pcEntry.writeOne)
	// cancels its context as soon as Write() returns.
	startCtx  context.Context
	client    *slack.Client
	channelID string
	threadTS  string
	teamID    string
	log       *slog.Logger

	mu          sync.Mutex
	started     bool
	closed      bool
	onComplete  func(string)
	onRegister  func(*NativeStreamingWriter)
	messageTS   string
	rateLimiter *ChannelRateLimiter

	// 缓冲流控
	buf          bytes.Buffer
	flushTrigger chan struct{}
	closeChan    chan struct{}
	wg           sync.WaitGroup

	// 完整性校验
	bytesWritten      int64
	bytesFlushed      int64
	failedFlushChunks []string

	// Post-stream table upgrade: accumulates full content for table detection on Close.
	fullContent strings.Builder

	// TTL rotation
	streamStartTime time.Time
	streamExpired   bool
}

// NewNativeStreamingWriter creates a new streaming writer for Slack.
func NewNativeStreamingWriter(ctx context.Context, client *slack.Client, channelID, threadTS, teamID string,
	rateLimiter *ChannelRateLimiter, log *slog.Logger, onComplete func(string), onRegister func(*NativeStreamingWriter)) *NativeStreamingWriter {
	w := &NativeStreamingWriter{
		startCtx:     ctx,
		client:       client,
		channelID:    channelID,
		threadTS:     threadTS,
		teamID:       teamID,
		log:          log,
		onComplete:   onComplete,
		onRegister:   onRegister,
		rateLimiter:  rateLimiter,
		flushTrigger: make(chan struct{}, 1),
		closeChan:    make(chan struct{}),
	}
	w.wg.Add(1)
	go w.flushLoop()
	return w
}

// Write starts the stream on first call, buffers content on subsequent calls.
func (w *NativeStreamingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, fmt.Errorf("stream already closed")
	}
	if w.streamExpired {
		return 0, fmt.Errorf("stream expired")
	}
	if len(p) == 0 {
		return 0, nil
	}

	// 首次调用：用首个 payload 启动流（替代占位符，直接展示真实内容）
	if !w.started {
		initialContent := string(p)
		opts := []slack.MsgOption{slack.MsgOptionMarkdownText(initialContent)}
		if w.threadTS != "" {
			opts = append(opts, slack.MsgOptionTS(w.threadTS))
		}
		if w.teamID != "" {
			opts = append(opts, slack.MsgOptionRecipientTeamID(w.teamID))
		}
		_, streamTS, err := w.client.StartStreamContext(w.startCtx,
			w.channelID,
			opts...,
		)
		if err != nil {
			return 0, fmt.Errorf("start stream: %w", err)
		}
		w.messageTS = streamTS
		w.started = true
		w.streamStartTime = time.Now()
		w.fullContent.Write(p)
		w.bytesWritten += int64(len(p))
		w.bytesFlushed += int64(len(p))
		if w.onRegister != nil {
			w.onRegister(w)
		}
		return len(p), nil
	}

	w.buf.Write(p)
	w.fullContent.Write(p)
	w.bytesWritten += int64(len(p))

	// 超过阈值触发即时 flush
	if utf8.RuneCount(w.buf.Bytes()) >= flushSize {
		select {
		case w.flushTrigger <- struct{}{}:
		default:
		}
	}
	return len(p), nil
}

// Expired reports whether the stream has exceeded the rotation TTL.
// Used by writeWithStreaming to proactively replace the stream before
// Slack's server-side streaming limit kicks in.
func (w *NativeStreamingWriter) Expired() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.streamStartTime.IsZero() || !w.started || w.closed {
		return false
	}
	return time.Since(w.streamStartTime) > StreamRotationTTL
}

// flushLoop background goroutine: periodic + threshold-triggered flush.
// Uses closeChan for lifecycle control instead of w.startCtx, because
// startCtx is canceled by pcEntry.writeOne as soon as Write() returns.
func (w *NativeStreamingWriter) flushLoop() {
	defer w.wg.Done()
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.closeChan:
			w.flushBuffer()
			return
		case <-w.flushTrigger:
			w.flushBuffer()
		case <-ticker.C:
			w.flushBuffer()
		}
	}
}

// flushBuffer flushes the buffered content to Slack via AppendStream.
func (w *NativeStreamingWriter) flushBuffer() {
	w.mu.Lock()
	if w.buf.Len() == 0 || !w.started {
		w.mu.Unlock()
		return
	}
	content := w.buf.String()
	w.buf.Reset()
	w.mu.Unlock()

	// Rate limit check
	if w.rateLimiter != nil && !w.rateLimiter.Allow(w.channelID) {
		// Re-buffer if rate limited
		w.mu.Lock()
		w.buf.WriteString(content)
		w.mu.Unlock()
		return
	}

	// Chunk if too large
	if utf8.RuneCountInString(content) > maxAppendSize {
		chunks := ChunkContent(content, maxAppendSize)
		for _, chunk := range chunks {
			if err := w.appendWithRetry(chunk); err != nil {
				w.mu.Lock()
				w.failedFlushChunks = append(w.failedFlushChunks, chunk)
				w.mu.Unlock()
			}
		}
	} else {
		if err := w.appendWithRetry(content); err != nil {
			w.mu.Lock()
			w.failedFlushChunks = append(w.failedFlushChunks, content)
			w.mu.Unlock()
		}
	}
}

const appendTimeout = 10 * time.Second

func (w *NativeStreamingWriter) appendWithRetry(content string) error {
	var lastErr error
	for i := 0; i < maxAppendRetries; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), appendTimeout)
		_, _, err := w.client.AppendStreamContext(ctx, w.channelID, w.messageTS,
			slack.MsgOptionMarkdownText(content),
		)
		cancel()
		if err == nil {
			w.mu.Lock()
			w.bytesFlushed += int64(len(content))
			w.mu.Unlock()
			return nil
		}
		lastErr = err
		w.log.Debug("slack: appendStream attempt failed",
			"attempt", i+1, "channel", w.channelID, "err", err)

		if isStreamStateError(err) || isAuthError(err) {
			w.mu.Lock()
			w.streamExpired = true
			w.mu.Unlock()
			return err
		}

		if isRateLimited, retryAfter := isRateLimitError(err); isRateLimited {
			time.Sleep(retryAfter)
			continue
		}

		if i < maxAppendRetries-1 {
			time.Sleep(retryDelay)
		}
	}
	w.log.Warn("slack: appendStream failed after all retries",
		"channel", w.channelID, "max_retries", maxAppendRetries, "err", lastErr)
	return lastErr
}

// Close ends the stream, runs integrity check, and fallbacks if needed.
func (w *NativeStreamingWriter) Close() error {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil
	}
	w.closed = true
	streamExpired := w.streamExpired
	w.mu.Unlock()

	close(w.closeChan)
	w.wg.Wait()

	w.mu.Lock()
	started := w.started
	bytesWritten := w.bytesWritten
	bytesFlushed := w.bytesFlushed
	failedChunks := w.failedFlushChunks
	remainingBuf := w.buf.String()
	w.mu.Unlock()

	if !started {
		return nil
	}

	integrityOK := len(failedChunks) == 0 && bytesWritten == bytesFlushed
	duration := time.Since(w.streamStartTime).Round(time.Millisecond)

	if !integrityOK || streamExpired {
		w.log.Warn("slack: stream closed with issues",
			"channel", w.channelID, "thread", w.threadTS,
			"duration", duration,
			"bytes_written", bytesWritten, "bytes_flushed", bytesFlushed,
			"failed_chunks", len(failedChunks), "expired", streamExpired)
	} else {
		w.log.Debug("slack: stream closed",
			"channel", w.channelID, "thread", w.threadTS,
			"duration", duration,
			"bytes_written", bytesWritten)
	}

	// Use a fresh context for cleanup since startCtx may be cancelled
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Build table blocks before stopping stream so we can pass them atomically.
	// Passing blocks during stop avoids the block_mismatch error from chat.update
	// (rich_text blocks created by markdown_text cannot be replaced via chat.update).
	var stopOpts []slack.MsgOption
	if integrityOK && !streamExpired {
		stopOpts = w.buildTableStopOpts()
	}

	var stopErr error
	if len(stopOpts) > 0 {
		_, _, stopErr = w.client.StopStreamContext(cleanupCtx, w.channelID, w.messageTS, stopOpts...)
		if stopErr != nil {
			w.log.Debug("slack: stop stream with table blocks failed, retrying plain", "err", stopErr)
			_, _, stopErr = w.client.StopStreamContext(cleanupCtx, w.channelID, w.messageTS)
		}
	} else {
		_, _, stopErr = w.client.StopStreamContext(cleanupCtx, w.channelID, w.messageTS)
	}
	if stopErr != nil {
		w.log.Warn("slack: stop stream failed", "channel", w.channelID, "err", stopErr)
	}

	if w.onComplete != nil {
		w.onComplete(w.messageTS)
	}

	if !integrityOK || streamExpired {
		var fallbackText string
		if streamExpired {
			fallbackText = "⚠️ *Stream expired, sending complete content:*\n\n"
		} else if len(failedChunks) > 0 {
			fallbackText = "⚠️ *Stream interrupted, resending incomplete content:*\n\n"
			for _, chunk := range failedChunks {
				fallbackText += chunk
			}
		}
		if remainingBuf != "" {
			fallbackText += remainingBuf
		}
		if fallbackText != "" {
			opts := []slack.MsgOption{slack.MsgOptionText(fallbackText, false)}
			if w.threadTS != "" {
				opts = append(opts, slack.MsgOptionTS(w.threadTS))
			}
			if w.teamID != "" {
				opts = append(opts, slack.MsgOptionRecipientTeamID(w.teamID))
			}
			_, _, err := w.client.PostMessageContext(cleanupCtx, w.channelID, opts...)
			if err != nil {
				w.log.Error("slack: fallback PostMessage failed",
					"channel", w.channelID, "err", err)
			}
		}
	}
	return nil
}

// buildTableStopOpts constructs MsgOption slice with table Block Kit for chat.stopStream.
// Returns nil if no tables found or blocks cannot be built.
// Blocks are passed atomically during stream stop to avoid block_mismatch
// (rich_text blocks from markdown_text cannot be replaced via chat.update).
func (w *NativeStreamingWriter) buildTableStopOpts() []slack.MsgOption {
	w.mu.Lock()
	content := w.fullContent.String()
	w.mu.Unlock()

	if content == "" {
		return nil
	}

	segments, tables := ExtractTables(content)
	if len(tables) == 0 {
		return nil
	}

	blocks := BuildTableBlocks(content, segments, tables)
	if len(blocks) == 0 {
		return nil
	}

	if len(blocks) > 50 {
		w.log.Debug("slack: too many blocks for table upgrade, skipping")
		return nil
	}

	return []slack.MsgOption{
		slack.MsgOptionBlocks(blocks...),
		slack.MsgOptionText(content, false),
	}
}

// Compile-time check
var _ io.WriteCloser = (*NativeStreamingWriter)(nil)
