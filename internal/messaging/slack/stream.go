package slack

import (
	"bytes"
	"context"
	"fmt"
	"io"
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

// NativeStreamingWriter wraps Slack's three-phase streaming API
// into a standard io.WriteCloser. First Write() starts the stream,
// subsequent calls buffer content, Close() ends it with fallback.
type NativeStreamingWriter struct {
	ctx       context.Context
	client    *slack.Client
	channelID string
	threadTS  string

	mu          sync.Mutex
	started     bool
	closed      bool
	onComplete  func(string)
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
	rateLimiter *ChannelRateLimiter, onComplete func(string)) *NativeStreamingWriter {
	w := &NativeStreamingWriter{
		ctx:          ctx,
		client:       client,
		channelID:    channelID,
		threadTS:     threadTS,
		onComplete:   onComplete,
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
	if len(p) == 0 {
		return 0, nil
	}

	// Check TTL
	if !w.started && !w.streamStartTime.IsZero() {
		if time.Since(w.streamStartTime) > StreamTTL {
			w.streamExpired = true
			if !w.ttlWarningLogged {
				w.ttlWarningLogged = true
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
	if len(content) > maxAppendSize {
		chunks := splitChunks(content, maxAppendSize)
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
		time.Sleep(retryDelay)
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
	w.mu.Unlock()

	close(w.closeChan)
	w.wg.Wait()

	// 最后一次捕获状态 — buf still has rate-limited content after goroutine stops
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

	// 完整性校验
	integrityOK := len(failedChunks) == 0 && bytesWritten == bytesFlushed

	// 结束远端流
	_, _, _ = w.client.StopStreamContext(w.ctx, w.channelID, w.messageTS)

	if w.onComplete != nil {
		w.onComplete(w.messageTS)
	}

	// Fallback: 流失败时用普通消息补发未送达内容
	if !integrityOK {
		var fallbackText string
		if len(failedChunks) > 0 {
			fallbackText = "⚠️ *Stream interrupted, resending incomplete content:*\n\n"
			for _, chunk := range failedChunks {
				fallbackText += chunk
			}
		}
		if remainingBuf != "" {
			fallbackText += remainingBuf
		}
		if fallbackText != "" {
			_, _, _ = w.client.PostMessageContext(w.ctx, w.channelID, slack.MsgOptionText(fallbackText, false))
		}
	}
	return nil
}

// Compile-time check
var _ io.WriteCloser = (*NativeStreamingWriter)(nil)

// splitChunks splits a string into chunks of maxLen bytes at rune boundaries.
func splitChunks(s string, maxLen int) []string {
	var chunks []string
	runes := []rune(s)
	start := 0
	for start < len(runes) {
		end := start + maxLen
		if end > len(runes) {
			end = len(runes)
		}
		// Back up if we'd split in the middle of a multi-byte rune (shouldn't happen with []rune)
		chunks = append(chunks, string(runes[start:end]))
		start = end
	}
	return chunks
}
