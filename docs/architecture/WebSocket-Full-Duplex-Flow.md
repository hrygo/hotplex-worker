---
title: WebSocket Full-Duplex Communication Flow
type: architecture
tags:
  - project/HotPlex
  - architecture/gateway
  - architecture/websocket
---

# WebSocket Full-Duplex Communication Flow

> 描述客户端（Web / WeChat / Mobile / IDE）通过 HotPlex Worker Gateway 访问 Claude Code 的全双工 WebSocket 通信流程。

---

## 1. Architecture Overview

```
┌──────────────────────────────────────────────────────────────────────────────────────┐
│                              Client → HotPlex Worker → Claude Code                    │
│                                 Full-Duplex WebSocket Communication                    │
└──────────────────────────────────────────────────────────────────────────────────────┘

                                      ┌─────────────┐
                                      │   Client    │
                                      │(Web/WeChat/ │
                                      │ Mobile/IDE) │
                                      └──────┬──────┘
                                             │
                                             │ 1️⃣ WebSocket Upgrade
                                             │    GET /ws?session_id=xxx
                                             │    Authorization: Bearer <JWT>
                                             ▼
┌──────────────────────────────────────────────────────────────────────────────────────┐
│                          HotPlex Worker Gateway (Go)                                  │
│                                                                                       │
│  ┌────────────────────────────────────────────────────────────────────────────────┐  │
│  │                          🌐 WebSocket Layer                                    │  │
│  │                                                                                │  │
│  │   ┌──────────┐    ┌──────────┐    ┌──────────┐    ┌──────────┐               │  │
│  │   │ Conn #1  │    │ Conn #2  │    │ Conn #3  │    │ Conn #N  │  ...          │  │
│  │   │ (Web)    │    │ (WeChat) │    │ (Mobile) │    │ (API)    │               │  │
│  │   └────┬─────┘    └────┬─────┘    └────┬─────┘    └────┬─────┘               │  │
│  │        └───────────────┴───────────────┴───────────────┘                      │  │
│  │                               │                                                 │  │
│  │                    ┌──────────▼──────────┐                                    │  │
│  │                    │    🧠 Hub (Broadcast)   │                                    │  │
│  │                    │  - Conn Register/Unregister                               │  │
│  │                    │  - Sequence Number Generation                              │  │
│  │                    │  - Session Routing                                        │  │
│  │                    └──────────┬──────────┘                                    │  │
│  └────────────────────────────────┼────────────────────────────────────────────────┘  │
│                                   │                                                      │
│  ┌────────────────────────────────┼────────────────────────────────────────────────┐  │
│  │                         🔄 AEP Protocol Layer                                  │  │
│  │                                                                                │  │
│  │   ┌─────────────────────────────────────────────────────────────────────┐     │  │
│  │   │                    AEP v1 Envelope (NDJSON)                            │     │  │
│  │   │                                                                      │     │  │
│  │   │   Client → Server:          Server → Client:                         │     │  │
│  │   │   ┌─────────────────┐       ┌─────────────────┐                     │     │  │
│  │   │   │ {"id":"msg-1",  │       │ {"id":"msg-1",  │                     │     │  │
│  │   │   │  "version":     │       │  "version":     │                     │     │  │
│  │   │   │   "aep/v1",     │       │   "aep/v1",     │                     │     │  │
│  │   │   │  "seq":1,       │       │  "seq":2,       │                     │     │  │
│  │   │   │  "session_id":   │       │  "session_id":  │                     │     │  │
│  │   │   │   "s1",         │       │   "s1",         │                     │     │  │
│  │   │   │  "event":{      │       │  "event":{      │                     │     │  │
│  │   │   │   "type":"init",│       │   "type":"state",│                     │     │  │
│  │   │   │   "data":{...}  │       │   "data":{...}  │                     │     │  │
│  │   │   │  }}             │       │  }}             │                     │     │  │
│  │   │   │ }               │       │ }               │                     │     │  │
│  │   │   └─────────────────┘       └─────────────────┘                     │     │  │
│  │   └─────────────────────────────────────────────────────────────────────┘     │  │
│  │                                                                                │  │
│  │   Event Types (Bidirectional):                                                 │  │
│  │   ┌──────────┬──────────┬──────────┬──────────┬──────────┐                   │  │
│  │   │  init    │  input   │  delta   │  done    │  error   │                   │  │
│  │   │ (握手)   │ (用户输入)│ (流式输出)│ (完成)   │ (错误)   │                   │  │
│  │   ├──────────┼──────────┼──────────┼──────────┼──────────┤                   │  │
│  │   │  state   │  ping    │  pong    │  control │  raw     │                   │  │
│  │   │ (状态)   │ (心跳)   │ (心跳响应)│ (控制)   │ (原始)   │                   │  │
│  │   └──────────┴──────────┴──────────┴──────────┴──────────┘                   │  │
│  │                                                                                │  │
│  └────────────────────────────────────┬─────────────────────────────────────────┘  │
│                                       │                                                │
│  ┌────────────────────────────────────┼─────────────────────────────────────────┐  │
│  │                          🔗 Bridge (Orchestration Layer)                      │  │
│  │                                                                                │  │
│  │   Responsibilities:                                                           │  │
│  │   - Session ↔ Worker Lifecycle Management                                    │  │
│  │   - AEP Event → Worker Input Transformation                                  │  │
│  │   - Worker Output → AEP Event Forwarding                                     │  │
│  │                                                                                │  │
│  │                    ┌──────────────▼──────────────┐                            │  │
│  │                    │     Session Manager (会话管理)    │                            │  │
│  │                    │  ┌────────────────────────┐  │                            │  │
│  │                    │  │ Created → Running →    │  │                            │  │
│  │                    │  │        Idle → Done    │  │                            │  │
│  │                    │  └────────────────────────┘  │                            │  │
│  │                    └──────────────┬──────────────┘                            │  │
│  └───────────────────────────────────┼────────────────────────────────────────────┘  │
│                                      │                                                    │
└──────────────────────────────────────┼────────────────────────────────────────────────────┘
                                       │
                                       │ 2️⃣ stdio / Process Spawn
                                       │    - Environment Variables Injection
                                       │    - JWT Token Passing
                                       │    - Session Context
                                       ▼
┌──────────────────────────────────────────────────────────────────────────────────────┐
│                           ⚙️ Claude Code Worker Adapter                               │
│  ┌────────────────────────────────────────────────────────────────────────────────┐  │
│  │                                                                                │  │
│  │   ┌─────────────────────────────────────────────────────────────────────┐     │  │
│  │   │                     Claude Code CLI Process                            │     │  │
│  │   │                                                                      │     │  │
│  │   │   Parent (Worker)                          Child (claude)              │     │  │
│  │   │   ┌─────────────┐                        ┌─────────────┐             │     │  │
│  │   │   │  stdio      │◄──────────────────────►│  stdio       │             │     │  │
│  │   │   │  (pipe)     │      JSON Protocol      │  (pipe)      │             │     │  │
│  │   │   └──────┬──────┘                        └──────┬───────┘             │     │  │
│  │   │          │                                      │                     │     │  │
│  │   │   ┌──────▼──────────────────────────────────────────▼──────┐         │     │  │
│  │   │   │              stream-json Protocol Codec                   │         │     │  │
│  │   │   │                                                            │         │     │  │
│  │   │   │   Input: {"type":"user", "message": "Hello"}            │         │     │  │
│  │   │   │   Output: {"type":"assistant", "content": "..."}          │         │     │  │
│  │   │   │   Output: {"type":"content_block", "delta": "..."}       │         │     │  │
│  │   │   │   Output: {"type":"done"}                                │         │     │  │
│  │   │   └────────────────────────────────────────────────────────┘         │     │
│  │   │                                                                      │     │
│  │   └──────────────────────────────────────────────────────────────────────┘     │
│  │                                                                                │
│  └────────────────────────────────────────────────────────────────────────────────┘
└──────────────────────────────────────────────────────────────────────────────────────┘
```

---

## 2. Communication Sequence Diagram

```
═══════════════════════════════════════════════════════════════════════════════════════
                              📊 Full-Duplex Communication Sequence
═══════════════════════════════════════════════════════════════════════════════════════

  Client           Gateway              Session           Claude Code
    │                │                     │                   │
    │══ 1.Connection Establishment ══════════════════════════════════════════════│
    │                │                     │                   │
    │── WS Upgrade ──│                     │                   │
    │    GET /ws     │                     │                   │
    │◄── 101 Switching ──                  │                   │
    │                │                     │                   │
    │══ 2.Handshake ══════════════════════════════════════════════════════════════│
    │                │                     │                   │
    │── {init} ─────►│                     │                   │
    │    JWT Token   │── Validate ─────────│                   │
    │                │◄── OK ──────────────│                   │
    │                │── Create Session ──►│                   │
    │                │                     │                   │
    │◄─ {init_ack} ──│                     │                   │
    │    session_id  │                     │                   │
    │   (server-gen) │                     │                   │
    │                │                     │                   │
    │══ 3.User Input (Full-Duplex) ══════════════════════════════════════════════│
    │                │                     │                   │
    │── {input} ────►│── transform ───────►│── to Claude ────►│
    │   "帮我写代码"  │   AEP→CLI          │   JSON           │
    │                │                     │                   │
    │                │                     │◄──thinking──────│
    │                │                     │                   │
    │                │◄─ {reasoning} ─────│                   │
    │◄─ {delta} ─────│   Streaming Output  │                   │
    │   "让我来帮..." │                     │                   │
    │                │                     │                   │
    │◄─ {delta} ─────│                     │                   │
    │   "首先..."     │                     │                   │
    │                │                     │                   │
    │◄─ {delta} ─────│                     │                   │
    │   "代码如下..." │                     │                   │
    │                │                     │                   │
    │                │                     │◄──result─────────│
    │                │                     │                   │
    │◄─ {done} ──────│                     │                   │
    │                │                     │                   │
    │══ 4.Continuous Dialogue (Loop) ════════════════════════════════════════════│
    │                │                     │                   │
    │── {input} ────►│── transform ───────►│── to Claude ────►│
    │◄─ {delta} ◄────│                     │                   │
    │◄─ {done} ◄─────│                     │                   │
    │                │                     │                   │
    │══ 5.Heartbeat Keep-Alive ═════════════════════════════════════════════════│
    │                │                     │                   │
    │── {ping} ─────►│                     │                   │
    │                │  (seq=0, heartbeat control message)       │
    │◄─ {pong} ◄─────│                     │                   │
    │                │                     │                   │
    │══ 6.Connection Close & Reconnect ════════════════════════════════════════│
    │                │                     │                   │
    │── close ──────►│── Transition ──────►│                   │
    │                │   to StateIdle      │  Session paused   │
    │                │   (worker paused)   │  for reconnect    │
    │                │                     │                   │
    │◄─ FIN ◄────────│                     │                   │
    │                │                     │                   │
    │══ 7.Reconnect (Session Resume) ═════════════════════════════════════════│
    │                │                     │                   │
    │── WS Upgrade ──│                     │                   │
    │   (same sess)  │                     │                   │
    │                │                     │                   │
    │── {init} ─────►│── Detect ──────────►│                   │
    │   (session_id) │   StateIdle         │                   │
    │                │                     │                   │
    │                │── ResumeSession ───►│  Worker restart   │
    │                │   (Bridge)          │  & reattach       │
    │                │                     │                   │
    │◄─ {init_ack} ──│── StateIdle ────────│                   │
    │                │   → StateRunning    │                   │
    │                │                     │                   │
    │══ 8.Graceful Termination ═══════════════════════════════════════════════│
```

---

## 3. Protocol Data Flow Mapping

```
═══════════════════════════════════════════════════════════════════════════════════════
                              🔄 Data Flow Transformation Mapping
═══════════════════════════════════════════════════════════════════════════════════════

  Client                    Gateway                    Claude Code
  Format                    Format                     Format
  ─────────────────────────────────────────────────────────────────
  
  Markdown                  AEP Envelope               stream-json
  ┌────────┐              ┌────────────┐              ┌─────────┐
  │ Hello  │──transform──►│ init       │──transform──►│ user    │
  │        │              │ input      │              │ message │
  └────────┘              │ delta      │              │         │
                          │ done       │              │         │
  WebSocket               │ error      │              │         │
  Frame                   │ state      │              │ process │
  ┌────────┐              │ ping/pong  │              │ output  │
  │binary/ │◄────────────►│ (NDJSON)   │◄────────────►│ (JSON)  │
  │text    │              │            │              │         │
  └────────┘              └────────────┘              └─────────┘
  
  ─────────────────────────────────────────────────────────────────
  
  AEP Event Type            Gateway Handler            Claude Code
  ─────────────────────────────────────────────────────────────────
  init          ─────────►   Validate JWT               N/A
  input         ─────────►   Parse & Route ─────────►   stdin
  delta         ◄─────────   Format & Send              stdout
  done          ◄─────────   Format & Send              stdout
  error         ◄─────────   Format & Send              stderr
  state         ◄─────────   Broadcast                  N/A
  ping          ─────────►   Send pong                  N/A
  control       ─────────►   Worker Control             signal
  reasoning     ◄─────────   Forward                   stdout
```

---

## 4. Session State Machine

```
═══════════════════════════════════════════════════════════════════════════════════════
                              🔄 Session State Machine
═══════════════════════════════════════════════════════════════════════════════════════

  5 States, 3 Categories:

  Active (Internal Loop)              Convergence              Terminal
  ┌──────────────────┐                ┌──────────┐        ┌─────────┐
  │ CREATED          │                │          │        │         │
  │   ↓ exec         │  exception     │          │  GC    │         │
  │ RUNNING ←→ IDLE  │ ─────────────► │TERMINATED│──────► │ DELETED │
  │                  │                │          │        │         │
  └──────────────────┘                └────┬─────┘        └─────────┘
           ↑                              │
           └───────── resume ────────────┘

  Admin Shortcut: RUNNING / IDLE ──admin kill──► DELETED (bypass TERMINATED)
```

| State        | Meaning                    | AEP Event           |
| ------------ | -------------------------- | ------------------- |
| `CREATED`    | Created, not started       | `state(created)`    |
| `RUNNING`    | Executing                  | `state(running)`    |
| `IDLE`       | Waiting for input          | `state(idle)`       |
| `TERMINATED` | Terminated                 | `state(terminated)` |
| `DELETED`    | Cleaned up (control plane) | —                   |

---

## 5. Component Responsibilities

### 5.1 WebSocket Layer (`internal/gateway/conn.go`)

| Component | Responsibility                                                   |
| --------- | ---------------------------------------------------------------- |
| `Conn`    | WebSocket connection lifecycle, read/write pumps                 |
| `Hub`     | Connection registry, session routing, sequence number generation |
| `Handler` | AEP event dispatch (input, ping, control)                        |

### 5.2 Bridge Layer (`internal/gateway/bridge.go`)

| Responsibility             | Description                                                 |
| -------------------------- | ----------------------------------------------------------- |
| Session ↔ Worker Lifecycle | Orchestrates session creation, worker attachment/detachment |
| Event Transformation       | Converts AEP events to worker input and vice versa          |
| Error Propagation          | Maps worker errors to AEP error events                      |

### 5.3 Session Manager (`internal/session/manager.go`)

| Responsibility    | Description                                            |
| ----------------- | ------------------------------------------------------ |
| Session CRUD      | Create, read, update, delete sessions                  |
| State Transitions | Atomic state machine transitions with mutex protection |
| GC                | Expired session cleanup                                |
| Nil Guards        | Returns safely when called on nil Manager (test mode)  |

### 5.4 Worker Adapter (`internal/worker/`)

| Component              | Description                                             |
| ---------------------- | ------------------------------------------------------- |
| `base.BaseWorker`      | Shared lifecycle (Terminate, Kill, Wait, Health)        |
| `ClaudeCodeWorker`     | Claude CLI adapter with stream-json protocol            |
| `OpenCodeCLIWorker`    | OpenCode CLI adapter with json-lines protocol           |
| `OpenCodeSrvWorker`    | OpenCode server adapter with HTTP+SSE                   |
| Platform Compatibility | `proc.Manager` skips RLIMIT_AS on macOS (not supported) |

---

## 6. Event Type Reference

### 6.1 Client → Server Events

| Event Type | Description          | Payload                                              |
| ---------- | -------------------- | ---------------------------------------------------- |
| `init`     | Connection handshake | `{session_id, worker_type, config, auth}`            |
| `input`    | User message         | `{content, attachments?}`                           |
| `ping`     | Heartbeat request    | `{}`                                                |
| `control`  | Control action       | `{action: "terminate"\|"delete"\|"reset"\|"gc"}`    |

### 6.2 Server → Client Events

| Event Type      | Description              | Payload                                                 |
| --------------- | ------------------------ | ------------------------------------------------------- |
| `init_ack`      | Handshake acknowledgment | `{session_id, capabilities}`                          |
| `state`         | Session state change     | `{state: "running"\|"idle"\|"terminated", message?}` |
| `message.delta` | Streaming text           | `{content}`                                |
| `message.done`  | Message complete         | `{usage?}`                                 |
| `reasoning`     | Thinking process         | `{content}`                                |
| `error`         | Error occurred           | `{code, message}`                          |
| `pong`          | Heartbeat response       | `{}`                                       |
| `control`       | Server control           | `{action: "throttle"\|"reconnect"\|"session_invalid"}` |
| `raw`           | Passthrough              | `{data}`                                   |

---

## 7. Configuration

### 7.1 Gateway Config

```yaml
gateway:
  host: "0.0.0.0"
  port: 8888
  path: "/ws"
  read_buffer_size: 4096
  write_buffer_size: 4096

session:
  idle_timeout: 30m
  max_lifetime: 24h
  retention_period: 168h  # 7 days

pool:
  min_size: 0
  max_size: 100
  max_idle_per_user: 10
  max_memory_per_user: 8589934592  # 8GB
```

### 7.2 Client Config

| Environment Variable              | Default                  | Description                |
| --------------------------------- | ------------------------ | -------------------------- |
| `NEXT_PUBLIC_HOTPLEX_WS_URL`      | `ws://localhost:8888/ws` | WebSocket endpoint         |
| `NEXT_PUBLIC_HOTPLEX_WORKER_TYPE` | `claude_code`            | Worker type                |
| `NEXT_PUBLIC_HOTPLEX_API_KEY`     | `dev`                    | API key for authentication |

---

## 8. Related Documents

- [[architecture/AEP-v1-Protocol]] - Detailed AEP v1 protocol specification
- [[architecture/Worker-Gateway-Design]] - Worker adapter architecture
- [[specs/Worker-ClaudeCode-Spec]] - Claude Code worker implementation
- [[management/Admin-API-Design]] - Administrative API design

---

## 9. Session Lifecycle Details

### Session ID Generation

**Lifecycle**:
1. Client initiates WebSocket connection (no session_id generated yet)
2. Client sends `init` message with optional `session_id` (for reconnect)
3. Server creates or resumes session
4. Server returns `init_ack` with authoritative `session_id`
5. Client uses `init_ack.session_id` for all subsequent messages

**Design Principle**:
- Server is the **single source of truth** for session IDs
- Client can suggest a session_id (reconnect scenario)
- Server has final authority to accept or reject
- `init_ack.session_id` is the only reliable session ID

### WebSocket Close Behavior

When WebSocket closes (network interruption, browser tab close):

1. **Gateway Actions**:
   ```go
   // conn.go ReadPump defer
   defer func() {
       c.hb.Stop()
       c.Close()
       c.hub.UnregisterConn(c)

       // Transition to StateIdle (pause, not terminate)
       if c.sessionID != "" {
           if err := handler.sm.Transition(ctx, c.sessionID, events.StateIdle); err != nil {
               c.log.Debug("gateway: conn close transition to idle", "session_id", c.sessionID, "err", err)
           }
       }
       c.hub.LeaveSession(c.sessionID, c)
   }()
   ```

2. **Session State**:
   - Session enters `StateIdle` (paused state)
   - Worker is paused, not terminated
   - Conversation context preserved
   - Waits for `idle_timeout` GC or client reconnect

3. **Worker Behavior**:
   - Process continues running (paused state)
   - Awaiting new input or termination signal
   - Resources retained for quick resume

### Reconnection & Resume

When client reconnects with same `session_id`:

1. **Reconnect Detection**:
   ```go
   // conn.go performInit
   } else if si.State == events.StateIdle {
       c.log.Info("gateway: resuming idle session", "session_id", sessionID)
       if c.starter != nil {
           if err := c.starter.ResumeSession(ctx, sessionID); err != nil {
               c.sendInitError(events.ErrCodeInternalError, "failed to resume session")
               return fmt.Errorf("resume session: %w", err)
           }
       }
   }
   ```

2. **ResumeSession Implementation**:
   ```go
   // bridge.go ResumeSession
   func (b *Bridge) ResumeSession(ctx context.Context, id string) error {
       si, err := b.sm.Get(id)

       // Clean up stale worker (prevent leak)
       if existing := b.sm.GetWorker(id); existing != nil {
           _ = existing.Terminate(ctx)
           b.sm.DetachWorker(id)
       }

       // Create and attach new worker
       w, err := b.wf.NewWorker(si.WorkerType)
       b.sm.AttachWorker(id, w)

       // Transition IDLE → RUNNING
       b.sm.Transition(ctx, id, events.StateRunning)

       // Resume worker execution
       w.Resume(ctx, workerInfo)

       return nil
   }
   ```

3. **Key Principles**:
   - `StateIdle` = "Paused" (worker paused, not killed)
   - `StateTerminated` = "Stopped" (worker dead, needs full restart)
   - ResumeSession cleans up stale workers first
   - Seamless resume without losing context

### Sequence Number Assignment

**Messages Requiring Seq**:
- Business: `input`, `message`, `message.delta`, `message.done`, `done`
- State: `state`, `error`, `reasoning`
- Tools: `tool_call`, `tool_result`
- Raw: `raw`
- Control: `control`

**Messages Without Seq** (seq=0):
- Heartbeat: `ping`, `pong`
  - WebSocket-level control messages
  - Not part of business flow
  - Clients don't need to order them

**Implementation**:
```go
// conn.go ReadPump
env.SessionID = c.sessionID
env.OwnerID = c.userID

// Skip seq for heartbeat messages
if env.Event.Type != events.Ping {
    env.Seq = c.hub.NextSeq(c.sessionID)
}
```

### Platform Compatibility

**Memory Limit (RLIMIT_AS)**:
```go
// proc/manager.go Start
if runtime.GOOS != "darwin" && cmd.Process != nil {
    const memLimit = 512 * 1024 * 1024 // 512 MB
    if err := syscall.Setrlimit(syscall.RLIMIT_AS, &syscall.Rlimit{
        Cur: memLimit,
        Max: memLimit,
    }); err != nil {
        m.log.Warn("proc: setrlimit RLIMIT_AS failed", "error", err)
        // Non-fatal: log and continue
    }
}
```

**Platform Differences**:
- **Linux/POSIX**: Full `RLIMIT_AS` support
- **macOS (Darwin)**: No reliable `RLIMIT_AS` support (skipped)
- **Windows**: No POSIX `setrlimit` support

---

## 10. Changelog


| Date       | Version | Change                                                                                                                                                                                                                                                                                                        |
| ---------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 2026-04-05 | 1.1     | **Protocol & Session Improvements**:<br/>• Session ID: Server-side generation with client-side assignment<br/>• Session Resume: StateIdle transition on disconnect + ResumeSession on reconnect<br/>• Heartbeat: Ping/pong messages skip sequence numbering<br/>• Platform: macOS compatibility for RLIMIT_AS |
| 2026-04-05 | 1.0     | Initial document creation                                                                                                                                                                                                                                                                                     |
