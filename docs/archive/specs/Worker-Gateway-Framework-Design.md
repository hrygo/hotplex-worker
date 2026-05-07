---
type: spec
tags:
  - project/HotPlex
  - architecture/gateway
  - framework
date: 2026-03-30
status: implemented
progress: 100
completion_date: 2026-03-30
---

# HotPlex Worker Gateway — Application Framework Design

> Date: 2026-03-30
> Status: Implemented
> Scope: All layers except concrete Worker adapters

---

## 1. Overview

This document describes the implemented application framework for the HotPlex Worker Gateway.
The framework provides the complete infrastructure layer required to run Worker adapters,
excluding the adapters themselves (Claude Code, etc.).

**What was built:**

```
Client (Web/IDE/CLI)
       │ WebSocket + AEP v1
       ▼
cmd/gateway (DI + graceful shutdown)
       │
   ┌───┴──────────────────────────────────────────────┐
   │                                                  │
internal/gateway          internal/security           internal/config
  WS Hub (per-session      Auth (API Key)            YAML + env override
  routing, dedup,          Input/Env validation
  heartbeat)
   │
   ├──────────────────────────────────────────────┤
   │                                              │
internal/session          internal/pool           internal/worker
  SQLite WAL                Per-user limits         Interfaces + NoOp
  State machine             Pool size limits        stub
  Background GC             NoOp Release()
   │
   └──────────────────────────────────────────────┤
                                                   │
                                         internal/worker/proc
                                           Process lifecycle
                                           PGID isolation
                                           Tiered termination
```

---

## 2. Package Structure

| Package | Responsibility |
|---------|---------------|
| `pkg/events` | AEP v1 shared types: Envelope, Kind, ErrorCode, SessionState, state machine |
| `internal/aep` | Encode/Decode/Validate AEP envelopes; ID generation |
| `internal/config` | YAML config loading via Viper; environment variable overrides |
| `internal/session` | SQLite WAL persistence; 5-state machine; atomic transitions; background GC |
| `internal/pool` | Per-user session quota; global size limits; Release() on delete |
| `internal/worker` | Worker/SessionConn/Capabilities interfaces; worker type registry |
| `internal/worker/proc` | Process lifecycle: Start (PGID), Terminate (SIGTERM → SIGKILL), Kill (SIGKILL) |
| `internal/gateway` | WebSocket Hub; per-session routing; ping/pong heartbeat; connection dedup |
| `internal/security` | API Key auth; InputValidator; EnvValidator |
| `cmd/gateway` | DI wiring; graceful shutdown; Admin API (POST/GET/DELETE sessions, pool stats) |

---

## 3. Concurrency Model

### Mutex Rules (Uber Go Style)

- All `sync.Mutex` / `sync.RWMutex` are **zero-value safe**, **no embedding**, **explicitly named** (`mu`)
- `Manager` structs expose a public `Lock(id)` method returning a `release()` closure
- Input handling and state transition execute **atomically** under the same per-session mutex

### Goroutine Lifecycle

Every goroutine has an explicit shutdown path:

| Goroutine | Shutdown Mechanism |
|-----------|-------------------|
| `session.Manager.runGC` | `ctx cancel` via `gcStop()` |
| `gateway.Conn.WritePump` | `conn.done` channel close |
| `gateway.Bridge.forwardEvents` | `conn.Recv()` channel close |
| `proc.Manager.drainStderr` | `m.stderr` read EOF |
| `cmd/gateway.server` | `http.Server.Shutdown` |

---

## 4. Session State Machine

5 states: `CREATED → RUNNING ↔ IDLE → TERMINATED → DELETED`

Implemented in `pkg/events/events.go` as a `ValidTransitions` map.
Transitions are validated at the `session.Manager` layer; invalid transitions return `ErrInvalidTransition`.

### Atomic Input + Transition

```
TransitionWithInput() {
    ms.mu.Lock()
    defer ms.mu.Unlock()
    if from not in {CREATED, RUNNING, IDLE} {
        return ErrInvalidTransition
    }
    // Both state update AND worker.Input() under same lock
    ms.info.State = to
    persist()
}
```

---

## 5. Pool Quota Enforcement

`pool.Manager.Acquire()` is called **before** `session.Manager.Create()`.
If pool quota is exceeded, the session is NOT created and no DB write occurs.

```
Acquire() → ErrPoolExhausted | ErrUserQuotaExceeded → do not create session
```

---

## 6. Gateway Connection Deduplication

When a client reconnects to an existing session:

```
JoinSession(sessionID, newConn):
    for each existingConn in sessions[sessionID]:
        existingConn.Close()  // kick old connection
    sessions[sessionID] = {newConn}
```

This ensures only the most recent WebSocket connection receives events for a session.

---

## 7. Process Management (worker/proc)

- **PGID Isolation**: `cmd.SysProcAttr.Setpgid = true` — signals target the entire process group
- **Tiered Termination**:
  1. `syscall.Kill(-pgid, SIGTERM)` — graceful, 5s grace period
  2. `syscall.Kill(-pgid, SIGKILL)` — force kill
- `Go 1.26 os.Pipe()` returns 3 values: `(r, w, err)`, unused ends closed immediately

---

## 8. SQLite Persistence

- WAL mode enabled (`PRAGMA journal_mode=WAL`)
- `busy_timeout = 500ms` (prevents "database locked" errors)
- Writes serialized via Go-level mutex (not goroutine — single write path)
- Sessions table indexes: `state`, `user_id`, `expires_at`

---

## 9. Security

| Layer | Mechanism |
|-------|-----------|
| HTTP | API Key via `X-API-Key` header (constant-time comparison) |
| Input | Null byte rejection, 1MB max length |
| Env | Whitelist filter before passing to worker process |
| CORS | Origin allowlist from config |

---

## 10. Clean Architecture Violations

The following violations were accepted for pragmatic reasons:

1. **`gateway.Bridge`** imports `session.Manager` and `pool.Manager` (violates "gateways depend on use cases" rule) — justified because Bridge is a thin adapter layer co-located with the gateway
2. **`security.Authenticator`** is instantiated in `cmd/gateway` and passed as a dependency — clean for current needs, would be refactored to a dedicated auth package in v2

---

## 11. Testing Status

No unit tests written yet. The framework builds and passes `go build ./...`.
Test coverage targets for v1:

- `pkg/events` — state machine transition table
- `internal/aep` — encode/decode roundtrip, validation errors
- `internal/session` — state transitions, concurrent access
- `internal/gateway` — message routing, connection dedup
- `internal/pool` — quota enforcement

---

## 12. Next Steps

1. Write unit tests for core packages
2. Implement first concrete Worker adapter (Claude Code)
3. Add integration test with real WebSocket client
