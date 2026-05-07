---
type: review
tags:
  - project/HotPlex
  - spec-review
  - gateway/async-init
date: 2026-04-04
status: completed
review_result: PASSED
---

# Spec Review Report

**Document**: `docs/specs/Gateway-Async-Init-Spec.md`
**Date**: 2026-04-04
**Status**: ✅ PASSED (all issues fixed)

---

## Critical Issues Fixed

### 1. **Type Error: SetupSession Return Type** ✅ FIXED
**Location**: Section 4.2
**Issue**: Returned `*worker.Worker` (pointer to interface) instead of `worker.Worker` (interface value)
**Fix**: Changed to `worker.Worker` — interfaces should not be pointers in Go

```go
// Before (WRONG)
func (b *Bridge) SetupSession(...) (*worker.Worker, error)

// After (CORRECT)
func (b *Bridge) SetupSession(...) (worker.Worker, error)
```

**Root Cause**: Misunderstanding of Go interface semantics. `worker.Worker` is already an interface type.

---

### 2. **API Error: PriorityControl Usage** ✅ FIXED
**Location**: Section 4.2, `LaunchSession` error path
**Issue**: Passed `events.PriorityControl` as parameter to `SendToSession()`, but `SendToSession` doesn't accept priority parameters

**Fix**: Set priority on the envelope, not as function parameter

```go
// Before (WRONG)
_ = b.hub.SendToSession(ctx, errEnv, events.PriorityControl)

// After (CORRECT)
errEnv.Priority = events.PriorityControl
_ = b.hub.SendToSession(ctx, errEnv)
```

**Root Cause**: Misread `SendToSession` signature: `SendToSession(ctx, env, afterDrain ...func())`, priority is set on envelope.

---

### 3. **Unused Variable** ✅ FIXED
**Location**: Section 4.2, `SetupSession`
**Issue**: `si, err := b.sm.CreateWithBot(...)` but `si` never used

**Fix**: Changed to `_, err := b.sm.CreateWithBot(...)`

---

### 4. **Sequence Diagram Error** ✅ FIXED
**Location**: Section 2.1, Baseline Init Flow
**Issue**: Incorrect order — showed `w.Start()` before `sm.AttachWorker`

**Fix**: Corrected order to:
1. `sm.CreateWithBot` (DB write)
2. `wf.NewWorker` (memory alloc)
3. `sm.AttachWorker` ← moved before Start
4. `w.Start()` (blocks 300ms–10s)
5. `sm.Transition(RUNNING)`

---

### 5. **Blocking Analysis Table Error** ✅ FIXED
**Location**: Section 2.2
**Issue**: Same sequence error — `AttachWorker` listed after `w.Start()`

**Fix**: Reordered rows to match actual execution sequence

---

## Validation Checklist

### ✅ Interface Signatures
- [x] `SetupSession` returns `worker.Worker` (interface value)
- [x] `LaunchSession` accepts `worker.Worker` (interface value)
- [x] Parameter types match actual codebase: `worker.WorkerType`, `events.SessionState`
- [x] Compile-time check: `var _ SessionStarter = (*Bridge)(nil)`

### ✅ API Usage
- [x] `SendToSession` signature: `SendToSession(ctx, env, afterDrain ...func())`
- [x] Priority set via `env.Priority = events.PriorityControl`
- [x] `events.PriorityControl` constant exists in `pkg/events/events.go:40`
- [x] `forwardEvents` signature: `forwardEvents(w worker.Worker, sessionID string)` — call matches

### ✅ Error Handling
- [x] Worker start failure: DetachWorker → Delete → send error+done
- [x] Metrics tracking: `metrics.GatewayErrorsTotal.WithLabelValues("worker_start_failed").Inc()`
- [x] Control messages bypass backpressure via priority
- [x] Client receives both `error` and `done(success=false)` events

### ✅ Concurrency Safety
- [x] Lock ordering preserved: `Manager.mu` → `managedSession.mu`
- [x] State invariant: only RUNNING sessions accept input
- [x] Race condition analysis: client blocked by `state(running)` requirement
- [x] Goroutine cleanup: `forwardEvents` has clear shutdown path

### ✅ Protocol Correctness
- [x] `init_ack` reports `state=CREATED` (accur at send time)
- [x] `state(running)` event follows via StateNotifier
- [x] Client cannot send input before session is RUNNING
- [x] AEP v1 seq allocation: `hub.NextSeq(sessionID)` before send

### ✅ Implementation Details
- [x] `worker.SessionInfo` struct usage: `{SessionID, AllowedTools}`
- [x] `allowedTools` passed through call chain: `SetupSession` → `LaunchSession` → `worker.Start`
- [x] Session persistence: `CreateWithBot` writes to DB
- [x] Worker attachment: `AttachWorker` before `Start`

### ✅ Documentation Quality
- [x] Sequence diagrams accurate
- [x] Error path documented with code examples
- [x] Concurrency invariants explained
- [x] Backward compatibility analyzed
- [x] Test strategy complete

---

## Additional Notes

### Design Correctness
1. **Two-phase decomposition is sound**: SetupSession (sync, ~5ms) + LaunchSession (async, 300ms–10s)
2. **Race window is safe**: Client blocked by `state(running)` requirement
3. **Error notification guaranteed**: PriorityControl bypasses backpressure
4. **No interface changes**: `StartSession` convenience method preserved for Admin API

### Go Best Practices
1. ✅ Interfaces returned by value, not pointer
2. ✅ Error wrapping with `fmt.Errorf("context: %w", err)`
3. ✅ Context propagation through call chain
4. ✅ Goroutine launched with `go` keyword explicitly
5. ✅ Metrics tracking for observability

### Project Conventions
1. ✅ Conventional Commits format
2. ✅ Envelope priority set before send
3. ✅ SessionManager interface methods: `CreateWithBot`, `AttachWorker`, `Transition`, `Delete`
4. ✅ Worker interface methods: `Start`, `Input`, `Resume`, `Terminate`, `Kill`, `Wait`
5. ✅ Event types: `Error`, `Done`, `State` from `pkg/events`

---

## Conclusion

**All critical errors fixed**. The spec now accurately reflects:
- Correct Go interface semantics (no pointer to interface)
- Correct `SendToSession` API usage (priority on envelope)
- Correct execution sequence (AttachWorker before Start)
- Correct error handling path (DetachWorker → Delete → error+done)

The implementation is ready to proceed.
