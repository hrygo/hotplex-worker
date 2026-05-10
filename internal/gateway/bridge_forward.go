package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/hrygo/hotplex/internal/eventstore"
	"github.com/hrygo/hotplex/internal/messaging"
	"github.com/hrygo/hotplex/internal/metrics"
	"github.com/hrygo/hotplex/internal/worker"
	"github.com/hrygo/hotplex/pkg/events"
)

// forwardEvents proxies worker events to the hub with seq assignment.
// EVT-004: if msgStore is configured, it appends to the event log on done events.
// AEP-020: after the recv channel closes, calls Worker.Wait() to determine exit
// code and sets DoneData.Success accordingly (non-zero exit = crash = success=false).
func (b *Bridge) forwardEvents(w worker.Worker, sessionID string, opts forwardOpts) {
	defer func() {
		if r := recover(); r != nil {
			b.log.Error("bridge: panic in forwardEvents", "session_id", sessionID, "panic", r, "stack", string(debug.Stack()))
		}
	}()
	workerType := w.Type()
	b.log.Debug("bridge: forwardEvents goroutine started", "session_id", sessionID, "worker_type", workerType, "resumed", opts.resumed)

	// Cache session info for event capture (avoids per-turn DB lookup).
	var sessPlatform, sessOwner string
	if b.collector != nil && b.sm != nil {
		if si, err := b.sm.Get(context.Background(), sessionID); err == nil {
			sessPlatform = si.Platform
			sessOwner = si.OwnerID
		}
	}
	startTime := time.Now()
	turnStartTime := startTime
	firstEvent := true
	doneReceived := false

	// Capture reset generation at goroutine start. If a reset happens while
	// this goroutine is running, the generation will differ when we check
	// after the recv channel closes, and we exit cleanly without crash handling.
	var myGen int64
	if rg, ok := w.(resetGenerationer); ok {
		myGen = rg.LoadResetGeneration()
	}

	// LLM retry: accumulate turn text and error data for retry detection.
	var turnText strings.Builder
	var lastError *events.ErrorData
	var pendingError *events.Envelope // buffered error event; suppressed if retry triggers

	// Turn timeout: kill worker if a single turn exceeds the configured duration.
	// Timer resets on every received event; stops on done.
	var turnTimerFired atomic.Bool
	var turnTimer *time.Timer
	if b.turnTimeout > 0 {
		turnTimer = time.AfterFunc(b.turnTimeout, func() {
			if !turnTimerFired.CompareAndSwap(false, true) {
				return
			}
			b.log.Warn("bridge: turn timeout exceeded, terminating worker",
				"session_id", sessionID, "worker_type", workerType, "turn_timeout", b.turnTimeout)
			b.sendError(sessionID, events.ErrCodeTurnTimeout, "Turn exceeded %v time limit and was terminated.", b.turnTimeout)
			b.captureSyntheticEvent(sessionID, "turn_timeout", fmt.Sprintf("Turn exceeded %v time limit", b.turnTimeout), eventstore.SourceTimeout)
			_ = w.Terminate(context.Background())
		})
		defer turnTimer.Stop()
	}

	recvCh := w.Conn().Recv()

	for env := range recvCh {

		if env.Event.Type == events.Error {
			b.log.Warn("bridge: received error from worker", "session_id", sessionID, "worker_type", workerType, "data", env.Event.Data)
			if ed, ok := env.Event.Data.(events.ErrorData); ok {
				lastError = &ed
			}
			if b.retryCtrl != nil {
				cloned := events.Clone(env)
				cloned.SessionID = sessionID
				pendingError = cloned
				continue
			}
		} else if b.log.Enabled(context.Background(), slog.LevelDebug) {
			b.log.Debug("bridge: received event from worker", "session_id", sessionID, "worker_type", workerType, "event_type", env.Event.Type)
		}

		if firstEvent {
			b.persistWorkerSessionID(w, sessionID)
			turnStartTime = time.Now() // exclude worker cold-start from Turn 1
			firstEvent = false
		}

		if turnTimer != nil && !turnTimerFired.Load() {
			turnTimer.Reset(b.turnTimeout)
		}
		if turnTimerFired.Load() {
			continue
		}

		env = events.Clone(env)
		env.SessionID = sessionID
		env.OwnerID = sessOwner

		var capturedDeltaContent string
		if env.Event.Type == events.MessageDelta || env.Event.Type == events.Message {
			if content := extractMessageContent(env); content != "" {
				turnText.WriteString(content)
				if env.Event.Type == events.MessageDelta {
					capturedDeltaContent = content
				}
			}
		}

		// Stats accumulation: track tool calls and merge per-turn stats on done.
		switch env.Event.Type {
		case events.ToolCall:
			acc := b.getOrInitAccum(sessionID, "", startTime)
			acc.ToolCallCount++
			if tc, ok := asToolCallData(env.Event.Data); ok {
				if acc.ToolNames == nil {
					acc.ToolNames = make(map[string]int)
				}
				acc.ToolNames[tc.Name]++
			}
		case events.Done:
			if turnTimer != nil {
				turnTimer.Stop()
			}
			acc := b.getOrInitAccum(sessionID, opts.workDir, startTime)
			if dd, ok := asDoneData(env.Event.Data); ok {
				acc.mergePerTurnStats(dd)
			}
			acc.TurnCount++
			acc.TurnDurationMs = time.Since(turnStartTime).Milliseconds()
			acc.computePerTurnDeltas()

			// Query precise context usage from worker via control channel.
			// Silently falls back to aggregated Done stats on failure.
			if cr, ok := w.(ControlRequester); ok {
				fetchContextUsage(cr, acc)
			}

			b.injectSessionStats(env, acc)
			acc.resetPerTurn()
			if b.log.Enabled(context.Background(), slog.LevelDebug) {
				b.log.Debug("bridge: turn completed",
					"session_id", sessionID, "worker_type", workerType, "turn", acc.TurnCount,
					"duration", time.Since(turnStartTime).Round(time.Millisecond),
					"text_len", turnText.Len(), "tools", acc.ToolCallCount)
			}
		}

		// UI Reconciliation (Fallback full message if silent dropped)
		if env.Event.Type == events.Done {
			doneReceived = true
			b.resetCrashLoop(sessionID)
			if b.hub.GetAndClearDropped(sessionID) {
				b.log.Warn("bridge: handling dropped deltas before done", "session_id", sessionID, "worker_type", workerType)

				if dataMap, ok := env.Event.Data.(map[string]any); ok {
					if stats, ok := dataMap["stats"].(map[string]any); ok {
						stats["dropped"] = true
					} else {
						dataMap["stats"] = map[string]any{"dropped": true}
					}
				} else if doneData, ok := env.Event.Data.(events.DoneData); ok {
					doneData.Dropped = true
					env.Event.Data = doneData
				} else if doneDataPtr, ok := env.Event.Data.(*events.DoneData); ok {
					doneDataPtr.Dropped = true
					env.Event.Data = doneDataPtr
				}
			}
		}

		if err := b.hub.SendToSession(context.Background(), env); err != nil {
			b.log.Warn("bridge: forward event failed", "err", err, "session_id", sessionID, "worker_type", workerType, "event_type", env.Event.Type)
		}

		if capturedDeltaContent != "" && b.collector != nil {
			b.collector.CaptureDeltaString(sessionID, env.Seq, capturedDeltaContent)
		} else if env.Event.Type != events.MessageDelta {
			b.captureEvent(sessionID, env.Seq, env.Event.Type, env.Event.Data)
		}

		// Flush buffered error on non-Done events (no retry decision possible yet).
		if pendingError != nil && env.Event.Type != events.Done {
			if err := b.hub.SendToSession(context.Background(), pendingError); err != nil {
				b.log.Warn("bridge: forward buffered error failed", "err", err, "session_id", sessionID, "worker_type", workerType)
			}
			b.captureEvent(sessionID, pendingError.Seq, pendingError.Event.Type, pendingError.Event.Data)
			pendingError = nil
		}

		// LLM retry: check after Done is forwarded.
		if env.Event.Type == events.Done && b.retryCtrl != nil && (!opts.resumed || turnText.Len() > 0) {
			if shouldRetry, attempt := b.retryCtrl.ShouldRetry(sessionID, lastError); shouldRetry {
				pendingError = nil
				b.autoRetry(context.Background(), w, sessionID, attempt)
				turnText.Reset()
				if b.collector != nil {
					b.collector.ResetSession(sessionID)
				}
				lastError = nil
				continue
			}
			if pendingError != nil {
				if err := b.hub.SendToSession(context.Background(), pendingError); err != nil {
					b.log.Warn("bridge: forward buffered error failed", "err", err, "session_id", sessionID, "worker_type", workerType)
				}
				b.captureEvent(sessionID, pendingError.Seq, pendingError.Event.Type, pendingError.Event.Data)
				pendingError = nil
			}
			b.retryCtrl.RecordSuccess(sessionID)
			lastError = nil
		}

		if env.Event.Type == events.Done {
			turnText.Reset()
			turnStartTime = time.Now()
		}
	}

	// Flush buffered error that never reached a retry decision point.
	if pendingError != nil {
		if err := b.hub.SendToSession(context.Background(), pendingError); err != nil {
			b.log.Warn("bridge: flush pending error on exit failed", "session_id", sessionID, "err", err)
		}
		b.captureEvent(sessionID, pendingError.Seq, pendingError.Event.Type, pendingError.Event.Data)
		pendingError = nil
	}

	b.handleWorkerExit(w, workerExitParams{
		sessionID:      sessionID,
		workerType:     workerType,
		opts:           opts,
		startTime:      startTime,
		myGen:          myGen,
		doneReceived:   doneReceived,
		turnText:       turnText.String(),
		turnTextLen:    turnText.Len(),
		turnTimerFired: turnTimerFired.Load(),
		sessPlatform:   sessPlatform,
		sessOwner:      sessOwner,
	})
}

// workerExitParams carries the context needed by handleWorkerExit.
type workerExitParams struct {
	sessionID      string
	workerType     worker.WorkerType
	opts           forwardOpts
	startTime      time.Time
	myGen          int64
	doneReceived   bool
	turnText       string
	turnTextLen    int
	turnTimerFired bool
	sessPlatform   string
	sessOwner      string
}

// handleWorkerExit processes worker exit after the recv channel closes.
// It determines the exit code, attempts crash recovery, sends error events,
// and performs cleanup.
func (b *Bridge) handleWorkerExit(w worker.Worker, p workerExitParams) {
	workerType := p.workerType

	// Check reset generation: if a reset happened while this goroutine was
	// running, the generation counter will differ from our captured value.
	if rg, ok := w.(resetGenerationer); ok && rg.LoadResetGeneration() != p.myGen {
		b.log.Info("bridge: worker reset, old forwardEvents exiting", "session_id", p.sessionID, "worker_type", workerType, "my_gen", p.myGen, "cur_gen", rg.LoadResetGeneration())
		return
	}

	// AEP-020: Worker.Recv() closed — get exit code to determine crash vs normal exit.
	waitTimeout := 2 * time.Second
	if b.closed.Load() {
		waitTimeout = 10 * time.Second
	}
	var exitCode int
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		defer func() {
			if r := recover(); r != nil {
				b.log.Error("bridge: panic in waitWorker", "session_id", p.sessionID, "panic", r, "stack", string(debug.Stack()))
			}
		}()
		exitCode, _ = w.Wait()
	}()
	select {
	case <-ch:
	case <-time.After(waitTimeout):
		b.log.Warn("bridge: Wait() timed out, force-killing", "session_id", p.sessionID, "worker_type", workerType)
		_ = w.Kill()
		<-ch
	}

	// Resume retry: skip during shutdown and for SIGTERM (exit 143).
	fallbackAttempted := !b.closed.Load() && exitCode != 0 && exitCode != 143 && p.opts.resumed && p.opts.retryDepth < 2 && time.Since(p.startTime) < 15*time.Second
	if fallbackAttempted && p.turnTextLen == 0 && time.Since(p.startTime) < 5*time.Second {
		b.log.Info("bridge: session files missing after resume, skipping retry",
			"session_id", p.sessionID, "worker_type", workerType, "exit_code", exitCode)
		p.opts.retryDepth = 1
	}
	if fallbackAttempted {
		var lastInput string
		if conn := w.Conn(); conn != nil {
			if ir, ok := conn.(worker.InputRecoverer); ok {
				lastInput = sanitizeLastInput(ir.LastInput())
			}
		}
		if lastInput == "" {
			lastInput = p.opts.lastInput
		}
		if b.attemptResumeFallback(fallbackParams{
			sessionID:     p.sessionID,
			workDir:       p.opts.workDir,
			exitCode:      exitCode,
			retryDepth:    p.opts.retryDepth,
			workerType:    workerType,
			lastInput:     lastInput,
			crashedWorker: w,
		}) {
			return
		}
	}

	if b.closed.Load() {
		b.cleanupCrashedWorker(p.sessionID, w)
		return
	}

	if b.sm != nil {
		si, smErr := b.sm.Get(context.Background(), p.sessionID)
		if smErr == nil && si.State == events.StateTerminated {
			b.log.Debug("bridge: session already terminated, skipping error for handler-killed worker", "session_id", p.sessionID, "worker_type", workerType)
			if !fallbackAttempted {
				b.cleanupCrashedWorker(p.sessionID, w)
			}
			return
		}
	}

	// Suppress user-facing errors when:
	// 1. Session completed normally: "done" received with no pending turn text.
	// 2. Worker was intentionally terminated: SIGTERM (exit 143) is always
	//    bridge/handler/GC-initiated, never an unexpected crash.
	if p.doneReceived && p.turnTextLen == 0 {
		b.log.Info("bridge: worker exit clean (done received, no pending output)",
			"session_id", p.sessionID, "worker_type", workerType, "exit_code", exitCode)
	} else if exitCode == 143 {
		b.log.Info("bridge: worker exit intentional (SIGTERM)",
			"session_id", p.sessionID, "worker_type", workerType)
	} else if exitCode != 0 && exitCode != -1 {
		acc := b.getOrInitAccum(p.sessionID, "", p.startTime)
		b.log.Warn("bridge: worker exited with non-zero code, sending crash error",
			"session_id", p.sessionID, "worker_type", workerType, "exit_code", exitCode,
			"duration", time.Since(p.startTime).Round(time.Millisecond), "turn_count", acc.TurnCount)
		metrics.WorkerCrashesTotal.WithLabelValues(string(workerType), fmt.Sprintf("%d", exitCode)).Inc()
		b.sendError(p.sessionID, events.ErrCodeWorkerCrash, "worker crashed (exit code %d)", exitCode)
		b.captureSyntheticEvent(p.sessionID, "worker_crash", fmt.Sprintf("Worker crashed with exit code %d", exitCode), eventstore.SourceCrash)
	} else if exitCode == -1 {
		b.sendError(p.sessionID, events.ErrCodeSessionTerminated, "worker terminated (killed)")
	} else if !p.doneReceived {
		b.log.Debug("bridge: sending error for platform cleanup (no done received)", "session_id", p.sessionID, "worker_type", workerType)
		b.sendError(p.sessionID, events.ErrCodeWorkerCrash, "worker exited without sending done")
	}

	if !fallbackAttempted {
		b.cleanupCrashedWorker(p.sessionID, w)
	}
}

// captureEvent persists an outbound event for replay.
func (b *Bridge) captureEvent(sessionID string, seq int64, eventType events.Kind, data any) {
	b.captureDirected(sessionID, seq, eventType, data, "outbound")
}

// CaptureInbound persists an inbound (user→worker) event for replay.
func (b *Bridge) CaptureInbound(sessionID string, seq int64, eventType events.Kind, data any) {
	b.captureDirected(sessionID, seq, eventType, data, "inbound")
}

// captureDirected marshals event data and sends it to the collector with the given direction.
func (b *Bridge) captureDirected(sessionID string, seq int64, eventType events.Kind, data any, direction string) {
	if b.collector == nil {
		b.log.Debug("bridge: capture skipped, collector is nil", "session_id", sessionID, "type", eventType, "direction", direction)
		return
	}
	ed, err := json.Marshal(data)
	if err != nil {
		b.log.Warn("bridge: capture marshal failed", "session_id", sessionID, "type", eventType, "direction", direction, "err", err)
		return
	}
	if eventType == events.ToolResult {
		ed = truncateToolResultOutput(ed)
	}
	b.collector.Capture(sessionID, seq, eventType, ed, direction, eventstore.SourceNormal)
}

const maxToolResultOutputLen = 128

// truncateToolResultOutput truncates the output field in a tool_result JSON payload.
func truncateToolResultOutput(raw json.RawMessage) json.RawMessage {
	var v struct {
		ID     string `json:"id"`
		Output any    `json:"output"`
		Error  string `json:"error"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return raw
	}
	s, ok := v.Output.(string)
	if !ok || utf8.RuneCountInString(s) <= maxToolResultOutputLen {
		return raw
	}
	runes := []rune(s)
	v.Output = string(runes[:maxToolResultOutputLen])
	truncated, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return truncated
}

// captureSyntheticEvent writes a synthetic done-like event for crash/timeout/fresh_start scenarios.
// Allocates a real seq number to avoid colliding with the AEP "unassigned" convention (seq=0).
func (b *Bridge) captureSyntheticEvent(sessionID, reason, message, source string) {
	if b.collector == nil {
		return
	}
	data, err := json.Marshal(map[string]any{
		"success":   false,
		"reason":    reason,
		"message":   message,
		"synthetic": true,
	})
	if err != nil {
		return
	}
	seq := b.hub.NextSeq(sessionID)
	b.collector.Capture(sessionID, seq, events.Done, data, "outbound", source)
}

// extractMessageContent extracts text content from a message or message_delta event.
func extractMessageContent(env *events.Envelope) string {
	switch env.Event.Type {
	case events.Message, events.MessageDelta:
		if d, ok := env.Event.Data.(events.MessageDeltaData); ok {
			return d.Content
		}
		if m, ok := env.Event.Data.(map[string]any); ok {
			if content, ok := m["content"].(string); ok {
				return content
			}
		}
	}
	return ""
}

// getOrInitAccum returns the session accumulator, creating one if needed.
// gitBranchOf is called inside the lock only when the accumulator first
// receives a non-empty workDir — a one-time cost per session (up to 2s
// subprocess). After that, the branch is already set and skipped.
func (b *Bridge) getOrInitAccum(sessionID, workDir string, startTime time.Time) *sessionAccumulator {
	b.accumMu.Lock()
	defer b.accumMu.Unlock()
	if acc, ok := b.accum[sessionID]; ok {
		if workDir != "" && acc.WorkDir == "" {
			acc.WorkDir = workDir
			acc.GitBranch = gitBranchOf(workDir)
		}
		return acc
	}
	acc := &sessionAccumulator{StartedAt: startTime}
	if workDir != "" {
		acc.WorkDir = workDir
		acc.GitBranch = gitBranchOf(workDir)
	}
	b.accum[sessionID] = acc
	return acc
}

// injectSessionStats merges the accumulator snapshot into DoneData.Stats["_session"].
// Handles both typed DoneData and map[string]any (from events.Clone JSON round-tripping).
func (b *Bridge) injectSessionStats(env *events.Envelope, acc *sessionAccumulator) {
	dd, ok := asDoneData(env.Event.Data)
	if !ok {
		return
	}
	if dd.Stats == nil {
		dd.Stats = make(map[string]any)
	}
	dd.Stats["_session"] = acc.snapshot()

	// Write back: preserve the original representation (map stays map, struct stays struct).
	switch env.Event.Data.(type) {
	case map[string]any:
		raw, _ := json.Marshal(dd)
		_ = json.Unmarshal(raw, &env.Event.Data)
	default:
		env.Event.Data = dd
	}
}

// gitBranchOf delegates to messaging.GitBranchOf for branch resolution.
func gitBranchOf(dir string) string { return messaging.GitBranchOf(dir) }

// fetchContextUsage queries the worker for precise context usage via control channel.
// Errors are silently ignored; the caller falls back to aggregated Done stats.
func fetchContextUsage(cr ControlRequester, acc *sessionAccumulator) {
	ctrlCtx, ctrlCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer ctrlCancel()
	if resp, err := cr.SendControlRequest(ctrlCtx, "get_context_usage", nil); err == nil {
		if cu := events.MapContextUsageResponse(resp); cu.MaxTokens > 0 {
			acc.mergeContextUsage(cu)
		}
	}
}
