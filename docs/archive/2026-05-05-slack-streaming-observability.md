# Slack Streaming Observability & Silent Drop Fix

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix Slack no-reply bug by adding observability to silent event drops (Hub) and error visibility (Slack streaming), plus dead pcEntry detection to prevent message loss.

**Architecture:** Four focused changes: (1) new Prometheus counter for silently dropped events, (2) Hub `routeMessage`/`sendControlToSession` log+metric when conns empty, (3) Hub `JoinPlatformSession` dead pcEntry replacement, (4) Slack stream retry logging + Close() error handling.

**Tech Stack:** Go 1.26, Prometheus client_golang, slog, testify/require, prometheus/testutil

**PR:** Fork-PR workflow — branch `fix/slack-streaming-observability` from `origin/main`, push to `fork` remote.

**Issue:** https://github.com/hrygo/hotplex/issues/180

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/metrics/metrics.go:~98` | Modify | Add `GatewayEventsSilentDropped` CounterVec |
| `internal/gateway/hub.go:314,417` | Modify | Add log+metric on empty conns in `sendControlToSession` and `routeMessage` |
| `internal/gateway/hub.go:234-238` | Modify | Detect dead pcEntry in `JoinPlatformSession` dedup |
| `internal/gateway/hub_test.go` | Modify | Add metric verification tests |
| `internal/messaging/slack/stream.go:291,319,380,383,404` | Modify | Add retry logging + fix error swallowing |

---

### Task 1: Add Prometheus Metric for Silent Event Drops

**Files:**
- Modify: `internal/metrics/metrics.go:98`

- [ ] **Step 1: Add metric definition**

Insert after `GatewayPlatformDroppedTotal` (after line 98):

```go
// GatewayEventsSilentDropped tracks events silently dropped due to no subscribed connections.
GatewayEventsSilentDropped = promauto.NewCounterVec(prometheus.CounterOpts{
    Namespace: "hotplex",
    Name:      "gateway_events_silent_dropped_total",
    Help:      "Total events silently dropped due to no subscribed connections",
}, []string{"event_type"})
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/metrics/`
Expected: no errors

- [ ] **Step 3: Commit**

```bash
git add internal/metrics/metrics.go
git commit -m "feat(metrics): add GatewayEventsSilentDropped counter for silent event drops"
```

---

### Task 2: Add Observability to Hub routeMessage

**Files:**
- Modify: `internal/gateway/hub.go:417-419`
- Modify: `internal/gateway/hub_test.go` (add test near line 400)

- [ ] **Step 1: Write the failing test**

Append after `TestHub_RouteMessage_NoConnections` (after line 400):

```go
func TestHub_RouteMessage_SilentDropMetric(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)

	before := testutil.ToFloat64(metrics.GatewayEventsSilentDropped.WithLabelValues(string(events.State)))

	h.routeMessage(&EnvelopeWithConn{
		Env:  events.NewEnvelope(aep.NewID(), "orphan", 1, events.State, events.StateData{State: events.StateIdle}),
		Conn: nil,
	})

	after := testutil.ToFloat64(metrics.GatewayEventsSilentDropped.WithLabelValues(string(events.State)))
	require.Equal(t, before+1, after, "metric should increment when events are dropped with no connections")
}
```

Add imports to hub_test.go:

```go
"github.com/prometheus/client_golang/prometheus/testutil"
"github.com/hrygo/hotplex/internal/metrics"
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gateway/ -run TestHub_RouteMessage_SilentDropMetric -v`
Expected: FAIL — metric not incremented (before == after)

- [ ] **Step 3: Implement routeMessage observability**

Replace `hub.go` lines 417-419:

```go
	if len(conns) == 0 {
		return
	}
```

With:

```go
	if len(conns) == 0 {
		metrics.GatewayEventsSilentDropped.WithLabelValues(string(msg.Env.Event.Type)).Inc()
		h.log.Warn("gateway: event dropped, no connections",
			"session_id", msg.Env.SessionID, "event_type", msg.Env.Event.Type)
		return
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/gateway/ -run TestHub_RouteMessage_SilentDropMetric -v`
Expected: PASS

- [ ] **Step 5: Run existing test to verify no regression**

Run: `go test ./internal/gateway/ -run TestHub_RouteMessage_NoConnections -v`
Expected: PASS (still no panic, now also increments metric)

- [ ] **Step 6: Commit**

```bash
git add internal/gateway/hub.go internal/gateway/hub_test.go
git commit -m "feat(gateway): add log + metric when routeMessage drops events with no connections"
```

---

### Task 3: Add Observability to Hub sendControlToSession

**Files:**
- Modify: `internal/gateway/hub.go:314-316`
- Modify: `internal/gateway/hub_test.go` (extend test near line 402)

- [ ] **Step 1: Write the failing test**

Replace `TestHub_sendControlToSession_NoConns` (line 402-407) with:

```go
func TestHub_sendControlToSession_NoConns(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)

	before := testutil.ToFloat64(metrics.GatewayEventsSilentDropped.WithLabelValues(string(events.Control)))

	env := events.NewEnvelope(aep.NewID(), "no_conns", 1, events.Control, nil)
	h.sendControlToSession(context.Background(), env)

	after := testutil.ToFloat64(metrics.GatewayEventsSilentDropped.WithLabelValues(string(events.Control)))
	require.Equal(t, before+1, after, "metric should increment when control events are dropped")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gateway/ -run TestHub_sendControlToSession_NoConns -v`
Expected: FAIL — metric not incremented

- [ ] **Step 3: Implement sendControlToSession observability**

Replace `hub.go` lines 314-316:

```go
	if len(conns) == 0 {
		return
	}
```

With:

```go
	if len(conns) == 0 {
		metrics.GatewayEventsSilentDropped.WithLabelValues(string(env.Event.Type)).Inc()
		h.log.Warn("gateway: control event dropped, no connections",
			"session_id", env.SessionID, "event_type", env.Event.Type)
		return
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/gateway/ -run TestHub_sendControlToSession_NoConns -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/hub.go internal/gateway/hub_test.go
git commit -m "feat(gateway): add log + metric when sendControlToSession drops events"
```

---

### Task 4: Detect and Replace Dead pcEntry in JoinPlatformSession

**Files:**
- Modify: `internal/gateway/hub.go:234-238`
- Modify: `internal/gateway/hub_test.go` (add test after line 884)

- [ ] **Step 1: Write the failing test**

Append after `TestPCEntry_JoinPlatformSession_Dedup` (after line 884):

```go
func TestHub_JoinPlatformSession_DeadEntryReplaced(t *testing.T) {
	t.Parallel()
	h := newTestHub(t)
	pc := &mockPlatformConn{}

	// First join — creates pcEntry
	h.JoinPlatformSession("s1", pc)

	// Find and close the pcEntry to simulate writeLoop death
	h.mu.RLock()
	var oldEntry *pcEntry
	for sw := range h.sessions["s1"] {
		if pce, ok := sw.(*pcEntry); ok {
			oldEntry = pce
		}
	}
	h.mu.RUnlock()
	require.NotNil(t, oldEntry)

	_ = oldEntry.Close() // kills writeLoop, closes done channel

	// Wait for done to be signaled
	require.Eventually(t, func() bool {
		select {
		case <-oldEntry.done:
			return true
		default:
			return false
		}
	}, time.Second, 10*time.Millisecond)

	// Re-join with same PlatformConn — should replace dead entry
	h.JoinPlatformSession("s1", pc)

	h.mu.RLock()
	count := len(h.sessions["s1"])
	var newEntry *pcEntry
	for sw := range h.sessions["s1"] {
		if pce, ok := sw.(*pcEntry); ok {
			newEntry = pce
		}
	}
	h.mu.RUnlock()

	require.Equal(t, 1, count, "should have exactly 1 entry after replacing dead one")
	require.NotNil(t, newEntry, "new pcEntry should exist")
	require.NotEqual(t, fmt.Sprintf("%p", oldEntry), fmt.Sprintf("%p", newEntry),
		"dead entry should have been replaced with a fresh one")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gateway/ -run TestHub_JoinPlatformSession_DeadEntryReplaced -v`
Expected: FAIL — dead entry not replaced, count == 1 but entry is still the old dead one (or count == 1 with old entry if dedup returns early)

- [ ] **Step 3: Implement dead pcEntry detection**

Replace `hub.go` lines 234-238:

```go
	for sw := range h.sessions[sessionID] {
		if pce, ok := sw.(*pcEntry); ok && pce.pc == pc {
			return
		}
	}
```

With:

```go
	for sw := range h.sessions[sessionID] {
		if pce, ok := sw.(*pcEntry); ok && pce.pc == pc {
			select {
			case <-pce.done:
				// writeLoop exited; remove stale entry and create fresh one
				delete(h.sessions[sessionID], sw)
				h.log.Info("gateway: replaced dead platform conn entry",
					"session_id", sessionID)
			default:
				return // alive, dedup
			}
		}
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/gateway/ -run TestHub_JoinPlatformSession_DeadEntryReplaced -v`
Expected: PASS

- [ ] **Step 5: Run dedup test to verify no regression**

Run: `go test ./internal/gateway/ -run TestPCEntry_JoinPlatformSession_Dedup -v`
Expected: PASS — alive entries still dedup correctly

- [ ] **Step 6: Commit**

```bash
git add internal/gateway/hub.go internal/gateway/hub_test.go
git commit -m "fix(gateway): detect and replace dead pcEntry in JoinPlatformSession"
```

---

### Task 5: Add Retry Logging to Slack appendWithRetry

**Files:**
- Modify: `internal/messaging/slack/stream.go:291,319`

- [ ] **Step 1: Add per-retry logging**

In `stream.go`, after line 291 (`lastErr = err`), insert:

```go
			w.log.Debug("slack: appendStream attempt failed",
				"attempt", i+1, "channel", w.channelID, "err", err)
```

- [ ] **Step 2: Add final failure logging**

In `stream.go`, before line 319 (`return lastErr`), insert:

```go
	w.log.Warn("slack: appendStream failed after all retries",
		"channel", w.channelID, "max_retries", maxAppendRetries, "err", lastErr)
```

- [ ] **Step 3: Verify it compiles and existing tests pass**

Run: `go test ./internal/messaging/slack/ -v -count=1`
Expected: all tests PASS

- [ ] **Step 4: Commit**

```bash
git add internal/messaging/slack/stream.go
git commit -m "feat(slack): add retry logging to appendWithRetry for streaming failures"
```

---

### Task 6: Fix Silent Error Swallowing in Slack Close()

**Files:**
- Modify: `internal/messaging/slack/stream.go:377-384,404`

- [ ] **Step 1: Fix StopStream error handling (table-block retry path)**

Replace lines 377-381:

```go
		_, _, err := w.client.StopStreamContext(cleanupCtx, w.channelID, w.messageTS, stopOpts...)
		if err != nil {
			w.log.Debug("slack: stop stream with table blocks failed, retrying plain", "err", err)
			_, _, _ = w.client.StopStreamContext(cleanupCtx, w.channelID, w.messageTS)
		}
```

With:

```go
		_, _, err := w.client.StopStreamContext(cleanupCtx, w.channelID, w.messageTS, stopOpts...)
		if err != nil {
			w.log.Debug("slack: stop stream with table blocks failed, retrying plain", "err", err)
			_, _, err = w.client.StopStreamContext(cleanupCtx, w.channelID, w.messageTS)
			if err != nil {
				w.log.Warn("slack: stop stream failed", "channel", w.channelID, "err", err)
			}
		}
```

- [ ] **Step 2: Fix StopStream error handling (plain path)**

Replace lines 382-384:

```go
	} else {
		_, _, _ = w.client.StopStreamContext(cleanupCtx, w.channelID, w.messageTS)
	}
```

With:

```go
	} else {
		_, _, err := w.client.StopStreamContext(cleanupCtx, w.channelID, w.messageTS)
		if err != nil {
			w.log.Warn("slack: stop stream failed", "channel", w.channelID, "err", err)
		}
	}
```

- [ ] **Step 3: Fix fallback PostMessage error handling**

Replace line 404:

```go
				_, _, _ = w.client.PostMessageContext(cleanupCtx, w.channelID, slack.MsgOptionText(fallbackText, false))
```

With:

```go
				_, _, err := w.client.PostMessageContext(cleanupCtx, w.channelID, slack.MsgOptionText(fallbackText, false))
				if err != nil {
					w.log.Error("slack: fallback PostMessage failed",
						"channel", w.channelID, "err", err)
				}
```

- [ ] **Step 4: Verify it compiles and existing tests pass**

Run: `go test ./internal/messaging/slack/ -v -count=1`
Expected: all tests PASS

- [ ] **Step 5: Commit**

```bash
git add internal/messaging/slack/stream.go
git commit -m "fix(slack): log errors instead of silently swallowing in Close()"
```

---

### Task 7: Full Test Suite + Lint + Build

- [ ] **Step 1: Run gateway tests with race detector**

Run: `go test -race ./internal/gateway/ -v -count=1`
Expected: all PASS, no data races

- [ ] **Step 2: Run Slack adapter tests with race detector**

Run: `go test -race ./internal/messaging/slack/ -v -count=1`
Expected: all PASS, no data races

- [ ] **Step 3: Run full lint check**

Run: `make lint`
Expected: no new warnings

- [ ] **Step 4: Run full build**

Run: `make build`
Expected: successful build

- [ ] **Step 5: Run make check (full CI)**

Run: `make check`
Expected: all quality gates pass

---

### Task 8: Fork-PR — Push and Create Pull Request

- [ ] **Step 1: Create feature branch from origin/main**

```bash
git stash
git checkout -b fix/slack-streaming-observability origin/main
git stash pop
```

If stash is empty (clean working tree from commits), just:
```bash
git checkout -b fix/slack-streaming-observability origin/main
```

Then cherry-pick all commits from the working branch onto this new branch.

- [ ] **Step 2: Push to fork remote**

```bash
git push -u fork fix/slack-streaming-observability
```

- [ ] **Step 3: Create PR**

```bash
gh pr create --base main --head aaronwong1989:fix/slack-streaming-observability \
  --title "fix(messaging): streaming observability and silent event drop fixes" \
  --body "$(cat <<'EOF'
## Summary

Fixes #180 — Slack messages get no reply due to two compounding issues: streaming API failures with silent error swallowing, and Hub silently dropping events when no platform connections are registered.

### Changes

**Hub observability (gateway):**
- Add `GatewayEventsSilentDropped` Prometheus CounterVec (label: `event_type`)
- Log warning + increment metric when `routeMessage` drops events due to empty connections
- Log warning + increment metric when `sendControlToSession` drops control events
- Detect and replace dead `pcEntry` in `JoinPlatformSession` (checks `done` channel)

**Slack streaming error visibility:**
- Log each `appendStream` retry failure at DEBUG level
- Log final failure after all retries exhausted at WARN level
- Replace `_, _, _` error swallowing in `StopStreamContext` with actual error logging
- Replace `_, _, _` error swallowing in fallback `PostMessageContext` with ERROR-level logging

## Test plan

- [x] `TestHub_RouteMessage_SilentDropMetric` — verifies metric increments on empty conns
- [x] `TestHub_sendControlToSession_NoConns` — verifies control event drop metric
- [x] `TestHub_JoinPlatformSession_DeadEntryReplaced` — verifies dead entry detection
- [x] All existing tests pass (no regression)
- [x] `make check` passes (lint + test + build)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 4: Verify CI**

Run: `gh pr checks` on the new PR
Expected: all CI checks pass
