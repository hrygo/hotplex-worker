---
type: spec
tags:
  - project/HotPlex
  - architecture/gateway
  - protocol/aep
  - refactor/async-init
date: 2026-04-04
status: draft
progress: 0
estimated_hours: 8
---

# Gateway Async Init — Session Start Asynchronization

## 0. Status & Motivation

**Status**: Draft

**Problem**: `performInit` calls `starter.StartSession()` synchronously inside the `ReadPump` goroutine. When the worker binary cold-starts, `ReadPump` is blocked for the entire duration (~300ms–10s), during which:

1. **No WebSocket message is processed** — including pong responses, reconnect attempts, and concurrent inputs
2. **Heartbeat detection is suspended** — client may appear unresponsive
3. **The `init_ack` state field is stale** — reports `CREATED` even after the session has transitioned to `RUNNING`, because `performInit` fetches session state before `StartSession` transitions it

**Goal**: Decompose `StartSession` into a synchronous setup phase + asynchronous worker launch phase, so `init_ack` is sent immediately while the worker boots in a background goroutine.

---

## 1. Non-Goals

- Async init for **reconnect/resume** paths (`StateTerminated → ResumeSession`) — these do not involve binary cold-start and are already fast
- Changing the protocol envelope format or `seq` allocation semantics
- Modifying `SessionManager` or the SQLite persistence layer

---

## 2. Current Behavior (Baseline)

### 2.1 Init Flow (Synchronous)

```
Client                     Gateway                          Worker
  │                            │                               │
  │──── init ────────────────▶│                               │
  │                            │ sm.CreateWithBot (DB write)   │
  │                            │ wf.NewWorker (memory alloc)   │
  │                            │ sm.AttachWorker                │
  │                            │ w.Start() ←───────────────────│ fork+exec
  │                            │ sm.Transition(RUNNING)         │   (cold start 300ms–10s)
  │                            │ go forwardEvents()              │
  │◀─── init_ack (state=CREATED)│                              │
  │                            │                               │
```

> [!NOTE]
> Current `init_ack` reports `state=CREATED` because `si` is fetched *before* `StartSession` is called. `StartSession` transitions the session to `RUNNING` internally, but `performInit` never re-reads the updated state.

### 2.2 Blocking Analysis

| Phase | Duration | Blocks ReadPump? |
|-------|----------|-----------------|
| `sm.CreateWithBot` | ~1ms | Yes |
| `wf.NewWorker` | ~0.5ms | Yes |
| `sm.AttachWorker` | ~1ms | Yes |
| `w.Start()` | **300ms–10s** | **Yes (primary bottleneck)** |
| `sm.Transition` | ~1ms | Yes |
| `go forwardEvents()` | — | No (async) |
| `send init_ack` | ~1ms | Yes |

Total ReadPump blockage: dominated by `w.Start()`, worst case ~10 seconds.

---

## 3. Proposed Design

### 3.1 Decomposition: Two-Phase Start

```
┌─────────────────────────────────────────────────────────┐
│ Phase 1: Synchronous (performInit, inside ReadPump)    │
│                                                          │
│  sm.CreateWithBot   ──▶ session record (CREATED)      │
│  wf.NewWorker        ──▶ worker object (not started)   │
│  sm.AttachWorker     ──▶ worker attached, no events yet  │
│  send init_ack       ──▶ client receives CREATED         │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼ goroutine
┌─────────────────────────────────────────────────────────┐
│ Phase 2: Asynchronous (asyncStart, background)         │
│                                                          │
│  w.Start()             ──▶ fork+exec worker binary    │
│  sm.Transition(RUNNING) ──▶ state event emitted        │
│  go forwardEvents()    ──▶ worker → hub → client       │
└─────────────────────────────────────────────────────────┘
```

Key invariant: **Client cannot send a valid `input` before receiving `state(running)`**, because `handleInput` rejects inputs for non-RUNNING sessions. This makes the race window safe.

### 3.2 Protocol Correctness

| Scenario | init_ack state | Actual state | Input accepted? |
|----------|----------------|--------------|----------------|
| Before async refactor | `CREATED` | `RUNNING` (after start) | ✅ (by design, input handled after ack) |
| After async refactor | `CREATED` | `RUNNING` (later via event) | ✅ (protocol unchanged) |

**`init_ack` reporting `CREATED` is not a protocol violation** — it is an accurate statement at the moment of sending. The state transition to `RUNNING` is guaranteed to follow via the `state(running)` event, which the client must receive before the session accepts input.

### 3.3 Error Handling

If Phase 2 fails, the client must be notified. Two error paths:

#### Path A: Worker Start Failed (Binary Not Found, Permission Denied)

```
goroutine:
  w.Start() returns error
  sm.Delete(sessionID)
  → bridge emits: error(WORKER_START_FAILED) + done(success=false)
  → client receives both events
```

Implementation: `asyncStart` calls `hub.SendToSession` with `PriorityControl` to guarantee delivery.

#### Path B: Worker Process Exited Immediately After Start

```
goroutine:
  w.Start() succeeds
  forwardEvents goroutine detects: conn.Recv() closed, exit code ≠ 0
  sm.Transition(TERMINATED)
  → bridge emits: error(WORKER_EXITED) + done(success=false)
```

This path already exists and is handled by `forwardEvents`. No change needed.

### 3.4 Concurrency Safety

#### Session Creation Atomicity

`CreateWithBot` and `AttachWorker` are called synchronously in Phase 1. Between Phase 1 and Phase 2:

- Session is `CREATED`, worker is attached but not started
- If a concurrent `init` for the same `session_id` arrives on a different connection: `Hub.JoinSession` disconnects the older connection (per "dedup by session_id" rule), so only one `performInit` runs
- If `sm.Delete` is called by admin between Phase 1 and Phase 2: `w.Start()` will still run, but `sm.Transition` will fail (already deleted) and `forwardEvents` will exit

#### State Machine Consistency

The state machine invariant: **only `RUNNING` sessions accept input**. During Phase 1 (CREATED), input is rejected with `SESSION_BUSY`. During Phase 2, input is also rejected because the session is still CREATED. When Phase 2 completes, `state(running)` is emitted and input is accepted.

```go
// handleInput (handler.go)
si, err := h.sm.Get(env.SessionID)
if !si.State.IsActive() {           // CREATED → rejected
    return ErrSessionNotActive
}
if si.State == events.StateIdle {   // IDLE → resume
    h.sm.TransitionWithInput(...)
}
w := h.sm.GetWorker(env.SessionID)
w.Input(ctx, content, nil)         // RUNNING → delivered
```

**No additional locking is required** — the existing `Manager.mu → per-session mutex` lock ordering protects concurrent access.

---

## 4. API Changes

### 4.1 `SessionStarter` Interface (conn.go)

```go
// SessionStarter initiates a worker session.
type SessionStarter interface {
    // SetupSession creates the DB record and prepares the worker without blocking.
    // Returns the prepared worker for async launch, The caller
    // (performInit) sends init_ack immediately after SetupSession returns.
    SetupSession(ctx context.Context, id, userID, botID string,
        wt worker.WorkerType, allowedTools []string) (worker.Worker, error)

    // LaunchSession starts a prepared worker asynchronously.
    // Must be called after SetupSession. Runs in a goroutine.
    // allowedTools is passed explicitly since LaunchSession only receives
    // the worker interface, not the session info.
    LaunchSession(ctx context.Context, w worker.Worker, id string, allowedTools []string)
}
```

> [!NOTE]
> Splitting `StartSession` into `SetupSession` + `LaunchSession` makes the interface cleaner and testable. `SetupSession` is fast (~5ms, synchronous); `LaunchSession` blocks on `w.Start()` and is always called as `go b.LaunchSession(...)`.

### 4.2 `Bridge` Implementation (bridge.go)

```go
// SetupSession creates the DB record and worker, attaches it, but does not start it.
// This is the synchronous part of session initialization.
func (b *Bridge) SetupSession(ctx context.Context, id, userID, botID string,
    wt worker.WorkerType, allowedTools []string) (worker.Worker, error) {

    _, err := b.sm.CreateWithBot(ctx, id, userID, botID, wt, allowedTools)
    if err != nil {
        return nil, fmt.Errorf("bridge: create session: %w", err)
    }

    w, err := b.wf.NewWorker(wt)
    if err != nil {
        return nil, fmt.Errorf("bridge: create worker: %w", err)
    }

    if err := b.sm.AttachWorker(id, w); err != nil {
        _ = b.sm.Delete(ctx, id)
        return nil, fmt.Errorf("bridge: attach worker: %w", err)
    }

    return w, nil
}

// LaunchSession starts the worker asynchronously and transitions the session to RUNNING.
func (b *Bridge) LaunchSession(ctx context.Context, w worker.Worker, id string, allowedTools []string) {
    workerInfo := worker.SessionInfo{
        SessionID:     id,
        AllowedTools:  allowedTools,
    }
    if err := w.Start(ctx, workerInfo); err != nil {
        b.sm.DetachWorker(id)
        _ = b.sm.Delete(ctx, id)
        // Notify client of failure via error + done.
        // Use PriorityControl to bypass backpressure queue.
        errEnv := events.NewEnvelope(aep.NewID(), id, b.hub.NextSeq(id),
            events.Error, events.ErrorData{
                Code:    events.ErrCodeWorkerStartFailed,
                Message: err.Error(),
            })
        errEnv.Priority = events.PriorityControl
        doneEnv := events.NewEnvelope(aep.NewID(), id, b.hub.NextSeq(id),
            events.Done, events.DoneData{Success: false})
        doneEnv.Priority = events.PriorityControl
        _ = b.hub.SendToSession(context.Background(), errEnv)
        _ = b.hub.SendToSession(context.Background(), doneEnv)
        metrics.GatewayErrorsTotal.WithLabelValues("worker_start_failed").Inc()
        return
    }

    // Transition to RUNNING. StateNotifier emits state(running) automatically.
    if err := b.sm.Transition(ctx, id, events.StateRunning); err != nil {
        b.log.Warn("bridge: transition to running failed", "id", id, "err", err)
    }

    go b.forwardEvents(w, id)
}
```

### 4.3 `performInit` Changes (conn.go)

```go
// In performInit, for new session creation:
if c.starter != nil {
    w, err := c.starter.SetupSession(context.Background(),
        sessionID, c.userID, c.botID, initData.WorkerType, initData.Config.AllowedTools)
    if err != nil {
        c.sendInitError(events.ErrCodeInternalError, "failed to create session")
        return fmt.Errorf("create session: %w", err)
    }
    c.log.Info("gateway: session created via init", "session_id", sessionID,
        "worker_type", initData.WorkerType)
    // Send init_ack immediately (session is CREATED, no state change event yet).
    ack := BuildInitAck(sessionID, events.StateCreated, initData.WorkerType)
    ack.Seq = c.hub.NextSeq(sessionID)
    if err := c.WriteCtx(context.Background(), ack); err != nil {
        return err
    }
    // Launch worker asynchronously — does NOT block ReadPump.
    go c.starter.LaunchSession(context.Background(), w, sessionID, initData.Config.AllowedTools)
} else {
    // Test mode: create session without worker.
    si, err = handler.sm.CreateWithBot(...)
    ...
    ack := BuildInitAck(sessionID, events.StateCreated, initData.WorkerType)
    ...
}
```

### 4.4 Session State in init_ack

After this refactor, `init_ack` always reports `state=CREATED` for new sessions (whether async or test mode). The `state(running)` event follows asynchronously. This is correct per AEP — `init_ack` confirms the session was created; state transitions are separate events.

---

## 5. Error Codes

| Code | When | Payload |
|------|-------|---------|
| `WORKER_START_FAILED` | Phase 2: `w.Start()` returned error | `{"code": "WORKER_START_FAILED", "message": "..."}` |
| `WORKER_EXITED` | Phase 2: worker exited immediately | `{"code": "WORKER_EXITED", "message": "exit code N"}` |

Existing codes (`SESSION_NOT_FOUND`, `SESSION_BUSY`, `INVALID_MESSAGE`, etc.) are unaffected.

---

## 6. Backward Compatibility

### Client Impact

- Client behavior **unchanged**: client sends `init`, receives `init_ack`, then waits for `state(running)` before sending `input`
- `init_ack` state field: currently reports `CREATED` even in synchronous mode (bug). After fix: still reports `CREATED` (correct). Clients that relied on `init_ack` state being `RUNNING` were already incorrect.

### Admin API (`/admin/sessions`)

`CreateSession` endpoint calls `bridge.StartSession(...)` — this is a full synchronous start. It is used for explicit session creation (not AEP init). No change needed; `StartSession` remains as a convenience method that calls `SetupSession + LaunchSession` synchronously:

```go
// StartSession is a convenience that launches synchronously (used by Admin API).
func (b *Bridge) StartSession(ctx context.Context, id, userID, botID string,
    wt worker.WorkerType, allowedTools []string) error {
    w, err := b.SetupSession(ctx, id, userID, botID, wt, allowedTools)
    if err != nil {
        return err
    }
    b.LaunchSession(ctx, w, id, allowedTools)
    return nil
}
```

### Resume Path (`Bridge.ResumeSession`)

Unchanged. Resume does not involve binary cold-start; the worker is already running. `ResumeSession` is fast and does not need async treatment.

---

## 7. Test Strategy

### Unit Tests

| Test | Subject | Validates |
|------|---------|-----------|
| `TestSetupSession_Success` | `Bridge.SetupSession` | DB record created, worker attached, worker not started |
| `TestSetupSession_CreateFails` | `Bridge.SetupSession` | Returns error, worker never created |
| `TestSetupSession_AttachFails` | `Bridge.SetupSession` | Cleans up DB record on attach failure |
| `TestLaunchSession_StartSucceeds` | `Bridge.LaunchSession` | Worker started, session transitioned, forwardEvents goroutine started |
| `TestLaunchSession_StartFails` | `Bridge.LaunchSession` | Error + done sent to client, session deleted |
| `TestPerformInit_NewSession_Async` | `performInit` | `init_ack` sent immediately, worker launched in goroutine |

### Integration Tests

| Test | Scenario |
|------|----------|
| Python gateway test | WebSocket init → init_ack (CREATED) → state (RUNNING) → input accepted |
| Worker crash during cold start | Error + done received by client |
| Concurrent init for same session_id | Old connection closed, new one proceeds |

---

## 8. Implementation Plan

**Phase 1 — Interface Split** (low risk, pure refactor)
1. Add `SetupSession` + `LaunchSession` to `SessionStarter` interface
2. Implement on `Bridge`
3. Add `StartSession` as convenience (calls both)
4. Update `conn.go performInit` to call `SetupSession` synchronously + `go LaunchSession`
5. Tests pass

**Phase 2 — Error Path** (medium risk)
6. Implement error notification in `LaunchSession` failure path
7. Verify `error(WORKER_START_FAILED)` is delivered to client

**Phase 3 — Admin API** (no change)
8. Admin `CreateSession` uses `StartSession` (already synchronous by design)
9. No protocol changes

**Estimated scope**: ~3 files changed (`conn.go`, `bridge.go`, `conn_test.go`), ~150 lines diff.

---

## 9. Open Questions

| # | Question | Recommendation |
|---|----------|----------------|
| 1 | Should `LaunchSession` retry on transient failures (e.g., ENOENT binary, then retry)? | No — fail fast. Admin intervention required. |
| 2 | Should there be a timeout for `w.Start()` beyond which the goroutine is killed? | Yes — add a `startTimeout` context (e.g., 60s). If exceeded, kill worker and emit error. |
| 3 | Does `hub.SendToSession` in the `LaunchSession` error path need special priority? | Yes — use `PriorityControl` to bypass backpressure queue, guarantee error delivery. |
