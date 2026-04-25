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
	flushInterval    = 150 * time.Millisecond
	flushSize        = 20 // rune count threshold for immediate flush
	maxAppendRetries = 3
	retryDelay       = 50 * time.Millisecond
	maxAppendSize    = 3000 // Slack limit ~4000, safety margin
	StreamTTL        = 10 * time.Minute
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
	// ctx is stored because the writer needs it for the lifecycle of the stream,
	// and the goroutines spawned by Write/flushBuffer need access to it.
	ctx       context.Context
	client    *slack.Client
	channelID string
	threadTS  string
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

	// TTL 监控
	streamStartTime  time.Time
	streamExpired    bool
	ttlWarningLogged bool
}

// NewNativeStreamingWriter creates a new streaming writer for Slack.
func NewNativeStreamingWriter(ctx context.Context, client *slack.Client, channelID, threadTS string,
	rateLimiter *ChannelRateLimiter, log *slog.Logger, onComplete func(string), onRegister func(*NativeStreamingWriter)) *NativeStreamingWriter {
	w := &NativeStreamingWriter{
		ctx:          ctx,
		client:       client,
		channelID:    channelID,
		threadTS:     threadTS,
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

	// Check TTL
	if !w.started && !w.streamStartTime.IsZero() {
		if time.Since(w.streamStartTime) > StreamTTL {
			w.streamExpired = true
			if !w.ttlWarningLogged {
				w.ttlWarningLogged = true
				w.log.Warn("slack: stream TTL exceeded before start",
					"channel", w.channelID, "thread", w.threadTS,
					"ttl", StreamTTL, "elapsed", time.Since(w.streamStartTime).Round(time.Second))
			}
			return 0, fmt.Errorf("stream expired after %v", StreamTTL)
		}
	}

	// 首次调用：同步启动流
	if !w.started {
		_, streamTS, err := w.client.StartStreamContext(w.ctx,
			w.channelID,
			slack.MsgOptionMarkdownText(":thought_balloon: Thinking..."),
		)
		if err != nil {
			return 0, fmt.Errorf("start stream: %w", err)
		}
		w.messageTS = streamTS
		w.started = true
		w.streamStartTime = time.Now()
		if w.onRegister != nil {
			w.onRegister(w)
		}
	}

	w.buf.Write(p)
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

// flushLoop background goroutine: periodic + threshold-triggered flush
func (w *NativeStreamingWriter) flushLoop() {
	defer w.wg.Done()
	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			w.flushBuffer()
			return
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
	if w.buf.Len() == 0 || w.closed || !w.started {
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

func (w *NativeStreamingWriter) appendWithRetry(content string) error {
	var lastErr error
	for i := 0; i < maxAppendRetries; i++ {
		_, _, err := w.client.AppendStreamContext(w.ctx, w.channelID, w.messageTS,
			slack.MsgOptionMarkdownText(content),
		)
		if err == nil {
			w.mu.Lock()
			w.bytesFlushed += int64(len(content))
			w.mu.Unlock()
			return nil
		}
		lastErr = err

		if isStreamStateError(err) || isAuthError(err) {
			w.mu.Lock()
			w.streamExpired = true
			w.mu.Unlock()
			return err
		}

		if isRateLimited, retryAfter := isRateLimitError(err); isRateLimited {
			timer := time.NewTimer(retryAfter)
			select {
			case <-timer.C:
				continue
			case <-w.ctx.Done():
				timer.Stop()
				return w.ctx.Err()
			}
		}

		if i < maxAppendRetries-1 {
			select {
			case <-time.After(retryDelay):
			case <-w.ctx.Done():
				return w.ctx.Err()
			}
		}
	}
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

	// Use a fresh context for cleanup since w.ctx may be cancelled
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, _, _ = w.client.StopStreamContext(cleanupCtx, w.channelID, w.messageTS)

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
			_, _, _ = w.client.PostMessageContext(cleanupCtx, w.channelID, slack.MsgOptionText(fallbackText, false))
		}
	}
	return nil
}

// Compile-time check
var _ io.WriteCloser = (*NativeStreamingWriter)(nil)
