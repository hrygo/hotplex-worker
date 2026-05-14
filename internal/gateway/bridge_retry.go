package gateway

import (
	"context"
	"time"

	"github.com/hrygo/hotplex/internal/metrics"
	"github.com/hrygo/hotplex/internal/worker"
)

// CancelRetry cancels any pending auto-retry for a session.
// Called by handler when a user sends a new input.
func (b *Bridge) CancelRetry(sessionID string) {
	b.retryCancelMu.Lock()
	defer b.retryCancelMu.Unlock()
	if ch, ok := b.retryCancel[sessionID]; ok {
		close(ch)
		delete(b.retryCancel, sessionID)
	}
}

// autoRetry performs exponential backoff then sends the retry input to the worker.
func (b *Bridge) autoRetry(ctx context.Context, w worker.Worker, sessionID string, attempt int) {
	delay := b.retryCtrl.Delay(attempt)

	// Notify user if enabled.
	if b.retryCtrl.ShouldNotify() {
		msg := b.retryCtrl.NotifyMessage(attempt)
		notifyEnv := buildNotifyEnvelope(sessionID, msg, b.hub.NextSeq(sessionID))
		_ = b.hub.SendToSession(ctx, notifyEnv)
	}

	// Register cancel channel.
	cancelCh := make(chan struct{})
	b.retryCancelMu.Lock()
	b.retryCancel[sessionID] = cancelCh
	b.retryCancelMu.Unlock()
	defer func() {
		b.retryCancelMu.Lock()
		delete(b.retryCancel, sessionID)
		b.retryCancelMu.Unlock()
	}()

	// Wait with backoff, respecting cancellation.
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-cancelCh:
		b.log.Info("bridge: auto-retry cancelled by user input", "session_id", sessionID)
		return
	case <-timer.C:
	}

	// Send retry input to worker.
	b.log.Info("bridge: auto-retry sending input", "session_id", sessionID, "attempt", attempt)
	metrics.RetryAttemptsTotal.WithLabelValues("llm_error").Inc()
	if err := w.Input(ctx, b.retryCtrl.RetryInput(), nil); err != nil {
		b.log.Warn("bridge: auto-retry input failed", "session_id", sessionID, "err", err)
	}
}
