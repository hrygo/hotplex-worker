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
    │◄─ {pong} ◄─────│                     │                   │
    │                │                     │                   │
    │══ 6.Connection Close ══════════════════════════════════════════════════════│
    │                │                     │                   │
    │── close ──────►│── cleanup ─────────►│── terminate ────►│
    │                │                     │                   │
    │◄─ FIN ◄────────│                     │                   │
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

| State       | Meaning                | AEP Event        |
| ----------- | ---------------------- | ---------------- |
| `CREATED`   | Created, not started   | `state(created)` |
| `RUNNING`   | Executing              | `state(running)` |
| `IDLE`      | Waiting for input      | `state(idle)`    |
| `TERMINATED`| Terminated             | `state(terminated)` |
| `DELETED`   | Cleaned up (control plane) | —           |

---

## 5. Component Responsibilities

### 5.1 WebSocket Layer (`internal/gateway/conn.go`)

| Component | Responsibility |
|-----------|----------------|
| `Conn` | WebSocket connection lifecycle, read/write pumps |
| `Hub` | Connection registry, session routing, sequence number generation |
| `Handler` | AEP event dispatch (input, ping, control) |

### 5.2 Bridge Layer (`internal/gateway/bridge.go`)

| Responsibility | Description |
|----------------|-------------|
| Session ↔ Worker Lifecycle | Orchestrates session creation, worker attachment/detachment |
| Event Transformation | Converts AEP events to worker input and vice versa |
| Error Propagation | Maps worker errors to AEP error events |

### 5.3 Session Manager (`internal/session/manager.go`)

| Responsibility | Description |
|----------------|-------------|
| Session CRUD | Create, read, update, delete sessions |
| State Transitions | Atomic state machine transitions with mutex protection |
| GC | Expired session cleanup |

### 5.4 Worker Adapter (`internal/worker/`)

| Component | Description |
|-----------|-------------|
| `base.BaseWorker` | Shared lifecycle (Terminate, Kill, Wait, Health) |
| `ClaudeCodeWorker` | Claude CLI adapter with stream-json protocol |
| `OpenCodeCLIWorker` | OpenCode CLI adapter with json-lines protocol |
| `OpenCodeSrvWorker` | OpenCode server adapter with HTTP+SSE |

---

## 6. Event Type Reference

### 6.1 Client → Server Events

| Event Type | Description | Payload |
|------------|-------------|---------|
| `init` | Connection handshake | `{session_id, worker_type, config, auth}` |
| `input` | User message | `{content, attachments?}` |
| `ping` | Heartbeat request | `{}` |
| `control` | Control action | `{action: "terminate"\|"delete"}` |

### 6.2 Server → Client Events

| Event Type | Description | Payload |
|------------|-------------|---------|
| `init_ack` | Handshake acknowledgment | `{session_id, capabilities}` |
| `state` | Session state change | `{state: "running"\|"idle"\|"terminated"}` |
| `message.delta` | Streaming text | `{content}` |
| `message.done` | Message complete | `{usage?}` |
| `reasoning` | Thinking process | `{content}` |
| `error` | Error occurred | `{code, message}` |
| `pong` | Heartbeat response | `{}` |
| `control` | Server control | `{action: "throttle"\|"reconnect"}` |
| `raw` | Passthrough | `{data}` |

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

| Environment Variable | Default | Description |
|---------------------|---------|-------------|
| `NEXT_PUBLIC_HOTPLEX_WS_URL` | `ws://localhost:8888/ws` | WebSocket endpoint |
| `NEXT_PUBLIC_HOTPLEX_WORKER_TYPE` | `claude_code` | Worker type |
| `NEXT_PUBLIC_HOTPLEX_API_KEY` | `dev` | API key for authentication |

---

## 8. Related Documents

- [[architecture/AEP-v1-Protocol]] - Detailed AEP v1 protocol specification
- [[architecture/Worker-Gateway-Design]] - Worker adapter architecture
- [[specs/Worker-ClaudeCode-Spec]] - Claude Code worker implementation
- [[management/Admin-API-Design]] - Administrative API design

---

## 9. Changelog

| Date | Version | Change |
|------|---------|--------|
| 2026-04-05 | 1.0 | Initial document creation |
