# Hotplex — 产品白皮书

> **版本**: v1.1 · **协议**: AEP v1 · **运行时**: Go 1.26+
> **最后更新**: 2026-04-22

---

## 目录

1. [产品概述](#1-产品概述)
2. [核心价值](#2-核心价值)
3. [架构设计](#3-架构设计)
4. [AEP v1 协议](#4-aep-v1-协议)
5. [Session 生命周期](#5-session-生命周期)
6. [Worker 适配器](#6-worker-适配器)
7. [消息平台集成](#7-消息平台集成)
8. [客户端 SDK](#8-客户端-sdk)
9. [Web Chat UI](#9-web-chat-ui)
10. [安全架构](#10-安全架构)
11. [配置管理](#11-配置管理)
12. [运维管理](#12-运维管理)
13. [可观测性](#13-可观测性)
14. [部署指南](#14-部署指南)
15. [测试策略](#15-测试策略)
16. [灾备与高可用](#16-灾备与高可用)
17. [故障排查](#17-故障排查)
18. [API 参考](#18-api-参考)
19. [术语表](#19-术语表)

---

## 1. 产品概述

### 1.1 什么是 Hotplex

Hotplex 是一个面向 AI Coding Agent 的**统一管理平台**。其核心管理能力——**Hotplex Gateway**，通过 WebSocket 全双工协议（AEP v1）屏蔽 Claude Code、OpenCode Server 等 Agent 在运行协议、通信方式、生命周期管理上的差异，为上层客户端（Web/IDE/SDK）和消息平台（Slack/飞书）提供一致的交互接口。

```
Client (Web / IDE / CLI / SDK)         Messaging Platform
        │                                       │
   WebSocket API (AEP v1)              Slack Socket Mode
        │                              Feishu WebSocket
        ▼                                       ▼
  ┌─────────────────────────────────────────────────┐
  │                 Hotplex Gateway                   │
  │                                                   │
  │   WebSocket Hub · Session Manager · Admin API     │
  │   Worker Adapters · Platform Bridge               │
  │   Security · Config · Observability               │
  │                                                   │
  │   SQLite (sessions) · Prometheus · OTEL            │
  └─────────────────────────────────────────────────┘
        │              │
   Claude Code    OpenCode Server
   (stdio/NDJSON)  (HTTP+SSE)
```

### 1.2 适用场景

| 场景 | 说明 |
|------|------|
| **企业级 AI 编程助手** | 通过 Slack/飞书将 Claude Code 集成到团队工作流 |
| **AI 开发平台** | Web Chat UI + 多语言 SDK 构建自定义 AI 编程产品 |
| **IDE 集成** | 通过 WebSocket 协议将 AI Agent 嵌入 JetBrains/VS Code |
| **多 Agent 编排** | 统一网关管理不同类型的 AI Coding Agent |
| **AI SDK 集成** | 通过 Transport 适配器桥接 Vercel AI SDK |

### 1.3 支持的 Worker 类型

| Worker Type | Agent Runtime | Transport | Protocol | Lifecycle | 状态 |
|-------------|---------------|-----------|----------|-----------|------|
| `claude-code` | Anthropic Claude Code CLI | stdio | NDJSON | Persistent (Hot-Multiplexing) | 生产可用 |
| `opencode-server` | OpenCode Server | HTTP | SSE/JSON | Managed (单进程共享) | 生产可用 |
| `acpx` | ACPX Agent | stdio | NDJSON | Persistent | 占位（未实现） |
| `pi-mono` | Pi-mono Coding Agent | stdio | Raw text | Ephemeral | 桩文件 |

### 1.4 技术规格

| 维度 | 规格 |
|------|------|
| 语言 | Go 1.26+ |
| 协议 | AEP v1 over WebSocket |
| 数据库 | SQLite WAL mode |
| 认证 | ES256 JWT + API Key + Admin Token |
| 可观测性 | slog JSON + Prometheus + OpenTelemetry |
| 部署 | 单二进制 / Docker / systemd |
| 支持 OS | Linux, macOS (PGID 隔离需要 POSIX) |

---

## 2. 核心价值

### 2.1 协议统一

AEP v1 提供统一的 WebSocket 信封格式，覆盖全双工双向通信。`message.delta` 流式输出作为一等公民，定义 26 种事件类型常量（含 `init_ack` 共 27 种）和 23 种结构化错误码。无论底层 Agent 是 stdio、HTTP+SSE 还是原始文本，上层客户端始终使用同一套协议。

### 2.2 Worker 黑盒抽象

Gateway 不侵入 Agent 内部，通过 Transport × Protocol × Lifecycle 三维分类适配不同 Worker。新增 Worker 只需填充三维属性并通过 `init()` 自注册，无需修改 Gateway 核心代码。

### 2.3 Session 一等公民

Session 独立于连接存在，支持断线重连、Resume、多客户端 attach。UUIDv5 确定性映射保证同一对话路由到同一 Session，实现幂等会话管理。

### 2.4 平台消息零侵入扩展

Slack/飞书适配器通过 `PlatformConn` 接口和 `pcEntry` wrapper 模式接入 Hub，核心文件（handler/bridge/worker）零改动。新增消息平台只需实现 `PlatformConn` 接口。

### 2.5 竞态安全

`TransitionWithInput` 在同一 mutex 内完成状态检查和转换，杜绝 done/input 竞态。Lock ordering（Manager mu → Session mu）防止死锁。

---

## 3. 架构设计

### 3.1 总体架构图

```
                          ┌──────────────────────────────────────────────────────┐
                          │                HOTPLEX GATEWAY (单进程)                │
                          │                                                        │
                          │  ┌──────────┐  ┌──────────┐  ┌──────────────────┐  │
                          │  │  Hub     │  │ Handler  │  │ SessionManager   │  │
                          │  │ (分发层) │─▶│ (AEP派发)│─▶│ (5状态机+DB+GC)  │  │
                          │  └────┬─────┘  └──────────┘  └──────────────────┘  │
                          │       │                                                   │
                          │       │              ┌─────────────────────┐           │
                          │       │              │ Gateway Bridge      │           │
                          │       │              │ (session+worker编排)│           │
                          │       │              └────────┬────────────┘           │
                          │       │                       │                         │
                          │       │    ┌──────────────────┼──────────────┐         │
                          │       │    │                  │              │         │
                          │       ▼    ▼                  ▼              ▼         │
  ┌───────────────┐  ┌───┴────┐ ┌─────────┐  ┌─────────┐  ┌─────────┐  │
  │ WebSocket     │  │WS Conn │ │Slack    │  │ 飞书     │  │ 更多    │  │
  │ Clients       │  │(读写泵)│ │Adapter  │  │ Adapter  │  │ 平台... │  │
  │ (浏览器/SDK)  │  └───┬────┘ └────┬────┘  └────┬────┘  └─────────┘  │
  └───────────────┘      │           │            │                      │
                         │           │     ┌──────┴──────┐              │
                         │           │     │PlatformConn │              │
                         │           │     │(WriteCtx)   │              │
                         │           │     └──────┬──────┘              │
                         │           ▼            ▼                      │
                         │     Platform Bridge (共享编排)                │
                         │     ┌────────────────────────────────┐      │
                         │     │ StartSession→Join→Handler.Handle│      │
                         │     └────────────────────────────────┘      │
                         │                                               │
                         │  ┌──────────────────────────────────────┐   │
                         │  │        Worker Adapter Layer            │   │
                         │  │  ClaudeCode │ OpenCodeSrv │ ACPX │ Pi │   │
                         │  │  (stdio)    │ (HTTP+SSE)  │(stdio)│(raw)│   │
                         │  └──────────────────────────────────────┘   │
                         └───────────────────────────────────────────────┘
                                           │
                                           ▼  AEP over stdio / HTTP / NDJSON
                               ┌──────────────────────────────┐
                               │     AI Coding Agent Runtime    │
                               │  Claude Code / OpenCode / etc  │
                               └──────────────────────────────┘
```

### 3.2 各层职责

| 层级 | 组件 | 职责 | 关键文件 |
|------|------|------|---------|
| **接入层** | `gateway.Conn` | WebSocket 连接生命周期、读写泵、心跳、init 握手 | `internal/gateway/conn.go` |
| **接入层** | `slack.Adapter` | Slack Socket Mode 长连接、消息去重、流式输出 | `internal/messaging/slack/adapter.go` |
| **接入层** | `feishu.Adapter` | 飞书 larkws SDK 长连接、P2 事件、语音转录 | `internal/messaging/feishu/adapter.go` |
| **分发层** | `Hub` | 会话路由、seq 分配、背压广播、连接注册 | `internal/gateway/hub.go` |
| **逻辑层** | `Handler` | AEP 事件派发（input/ping/control），纯函数 | `internal/gateway/handler.go` |
| **编排层** | `Bridge` | Session 创建/Resume、Worker 生命周期、事件转发 | `internal/gateway/bridge.go` |
| **编排层** | `Platform Bridge` | 共享编排：Envelope 构建、Hub 订阅、Handler 调用 | `internal/messaging/bridge.go` |
| **数据层** | `Manager` | 5 状态机、SQLite WAL 持久化、GC、Pool 配额 | `internal/session/manager.go` |
| **执行层** | `Worker Adapters` | Agent Runtime 进程管理、协议解析、事件映射 | `internal/worker/*/` |

### 3.3 进程模型

- Gateway 为**单进程**应用，所有组件运行在同一进程内
- 每个 Session 对应一个独立的 Agent Runtime **子进程**（PGID 隔离）
- OpenCode Server 例外：单一共享进程服务所有 Session
- 持久化使用 SQLite WAL mode，单写 goroutine 串行化写入

### 3.4 控制面 / 数据面分离

| 类型 | 说明 |
|------|------|
| Session 状态 | 内存 + 持久化（SQLite） |
| Agent 输出 | 不存储（流式透传） |
| Agent 内部状态 | Worker 自身管理（Claude Code 的 `.jsonl` 文件） |

---

## 4. AEP v1 协议

### 4.1 设计理念

- **Streaming-first**：`message.delta` 一等公民，token 级实时流
- **Bidirectional**：统一 Envelope 覆盖双向，不区分方向
- **弱 schema**：允许 passthrough（`raw` type），不侵入 Agent 语义
- **可扩展**：未知 type 忽略（forward compatible），`custom.*` / `vendor.*` 命名空间

### 4.2 消息信封格式

所有消息共用同一 Envelope：

```json
{
  "version": "aep/v1",
  "id": "evt_<uuid>",
  "seq": 42,
  "priority": "data",
  "session_id": "sess_<uuid>",
  "timestamp": 1710000000123,
  "event": {
    "type": "message.delta",
    "data": { ... }
  }
}
```

| 字段 | 说明 |
|------|------|
| `version` | 固定 `aep/v1`，握手时协商 |
| `id` | UUID v4，错误响应通过此字段引用触发事件 |
| `seq` | per-session 严格递增序列号，仅分配给实际发送的事件 |
| `priority` | `control` 跳过背压队列优先发送，`data` 正常排队 |
| `session_id` | Session 标识 |
| `timestamp` | Unix 毫秒时间戳 |

### 4.3 事件类型

源码定义 26 种 Kind 常量（`pkg/events/events.go`），加上协议中的 `init_ack` 响应共 27 种。

**Client → Server**（7 种）：

| Kind | 说明 |
|------|------|
| `init` | 连接握手（Session 创建/Resume） |
| `input` | 用户输入 |
| `permission_response` | 权限响应 |
| `question_response` | 问题回复 |
| `elicitation_response` | MCP Elicitation 响应 |
| `worker_command` | Worker stdio 命令触发（查询上下文、切换模型等） |
| `ping` | 心跳保活 |

**Server → Client**（16 种）：

| Kind | 说明 |
|------|------|
| `init_ack` | 握手响应（协议定义，非 Kind 常量） |
| `message.delta` | 增量流式输出（唯一流式 type） |
| `message` / `message.start` / `message.end` | 完整消息 |
| `tool_call` / `tool_result` | 工具调用通知（Autonomous 模式，Client 仅展示） |
| `state` | 状态变更 |
| `done` | 执行完成（含 stats 统计） |
| `permission_request` / `question_request` / `elicitation_request` | 用户交互请求 |
| `reasoning` | 推理过程 |
| `step` | 执行阶段标记 |
| `raw` | 透传事件 |
| `pong` | 心跳响应 |
| `context_usage` | 上下文窗口用量报告（响应 `worker_command`） |
| `mcp_status` | MCP Server 连接状态报告（响应 `worker_command`） |

**双向**（3 种）：

| Kind | 说明 |
|------|------|
| `control` | 控制命令（详见 ControlAction 表） |
| `error` | 错误通知（详见 ErrorCode 表） |

#### ControlAction（`control` 事件的 Action 字段）

| ControlAction | 字符串值 | 方向 | 说明 |
|--------------|----------|------|------|
| `reconnect` | `"reconnect"` | S→C | 要求客户端重连 |
| `session_invalid` | `"session_invalid"` | S→C | Session 已失效 |
| `throttle` | `"throttle"` | S→C | 速率限制通知 |
| `terminate` | `"terminate"` | C→S | 终止 Session |
| `delete` | `"delete"` | C→S | 删除 Session |
| `reset` | `"reset"` | C→S | 重置对话上下文 |
| `gc` | `"gc"` | C→S | 休眠 Session |

#### ErrorCode（`error` 事件的 Code 字段）

| 分类 | ErrorCode | 字符串值 |
|------|-----------|----------|
| **Worker** | `WORKER_START_FAILED` | Worker 启动失败 |
| | `WORKER_CRASH` | Worker 崩溃 |
| | `WORKER_TIMEOUT` | Worker 超时 |
| | `WORKER_OOM` | Worker 内存溢出 |
| | `WORKER_OUTPUT_LIMIT` | Worker 输出超限 |
| | `PROCESS_SIGKILL` | 进程被 SIGKILL |
| **Session** | `SESSION_NOT_FOUND` | Session 不存在 |
| | `SESSION_EXPIRED` | Session 已过期 |
| | `SESSION_TERMINATED` | Session 已终止 |
| | `SESSION_INVALIDATED` | Session 已失效 |
| | `SESSION_BUSY` | Session 忙碌 |
| **Auth** | `UNAUTHORIZED` | 未授权 |
| | `AUTH_REQUIRED` | 需要认证 |
| **Protocol** | `INVALID_MESSAGE` | 无效消息 |
| | `PROTOCOL_VIOLATION` | 协议违规 |
| | `VERSION_MISMATCH` | 版本不匹配 |
| | `CONFIG_INVALID` | 配置无效 |
| **Gateway** | `INTERNAL_ERROR` | 内部错误 |
| | `RATE_LIMITED` | 速率限制 |
| | `GATEWAY_OVERLOAD` | 网关过载 |
| **Process** | `EXECUTION_TIMEOUT` | 执行超时 |
| **Control** | `RECONNECT_REQUIRED` | 需要重连 |
| **Resume** | `RESUME_RETRY` | Resume 重试 |

### 4.4 序列号与背压

- per-session 单调递增计数器，从 1 开始
- **仅分配给实际发送的事件**：被 backpressure 丢弃的 `message.delta` 和 `raw` 不消耗 seq
- broadcast channel 容量 256（可配置）
- `message.delta` 和 `raw` 可静默丢弃；`state`/`done`/`error`/`control` 永不丢弃
- `priority: "control"` 消息绕过 broadcast channel 直接发送

### 4.5 全双工通信流

```
Client                          Gateway                          Worker
  |-- init ---------------------->|                                |
  |<-- init_ack ------------------|                                |
  |-- input --------------------->|-- send ----------------------->|
  |<-- state(running) ------------|<-- message.delta --------------|  (streaming)
  |<-- message.delta -------------|                                |
  |<-- tool_call -----------------|<-- tool_use -------------------|  [Worker 自行执行 tool]
  |<-- tool_result ---------------|<-- tool_result ----------------|
  |<-- message.delta -------------|<-- delta ----------------------|
  |<-- done ----------------------|<-- result ---------------------|
```

### 4.6 Init 握手

```javascript
// Client → Gateway
ws.send(JSON.stringify({
  type: "init",
  session_id: "sess_abc123",       // optional; auto-generated if omitted
  worker_type: "claude-code",
  user_id: "user_001",
  metadata: { model: "claude-sonnet-4-6", work_dir: "/projects/my-app" }
}));

// Gateway → Client
ws.send(JSON.stringify({
  type: "init_ack",
  session_id: "sess_abc123",
  status: "ok"
}));
```

---

## 5. Session 生命周期

### 5.1 五状态模型

```
  活跃态（内部循环）         汇聚态              终态
  ┌──────────────────┐       ┌──────────┐      ┌─────────┐
  │ CREATED          │       │          │      │         │
  │   ↓ exec         │ 异常  │          │ admin│         │
  │ RUNNING ←→ IDLE  │────→  │TERMINATED│────→ │ DELETED │
  │                  │       │          │      │         │
  └──────────────────┘       └────┬─────┘      └─────────┘
           ↑                      │
           └──── resume ──────────┘

  注：RUNNING/IDLE 也可通过 admin delete 直接 → DELETED
      TERMINATED → DELETED 仅由管理员显式操作，不自动执行
```

| 状态 | `IsActive()` | 语义 |
|------|-------------|------|
| `CREATED` | true | 已创建，未启动 Runtime（瞬态 <1s） |
| `RUNNING` | true | 正在执行 Worker，处理输入 |
| `IDLE` | true | Worker 暂停，等待重连或新输入 |
| `TERMINATED` | false | Worker 已终止，保留元数据 |
| `DELETED` | false | 终态，DB 记录已删除 |

### 5.2 状态转换

```go
var ValidTransitions = map[SessionState]map[SessionState]bool{
    StateCreated:    {StateRunning: true, StateTerminated: true},
    StateRunning:    {StateIdle: true, StateTerminated: true, StateDeleted: true},
    StateIdle:       {StateRunning: true, StateTerminated: true, StateDeleted: true},
    StateTerminated: {StateRunning: true, StateDeleted: true},  // resume / admin delete
    StateDeleted:    {},                                         // terminal
}
```

**关键原子操作**：`TransitionWithInput` 在同一 mutex 内完成 TurnCount 递增 + max_turns 检查 + 状态转换，杜绝 done/input 竞态。

### 5.3 GC 机制

GC 后台 goroutine 以 `gc_scan_interval`（默认 1 分钟）为周期执行三步清理：

**Step 0 — 僵尸检测**：遍历 `RUNNING` 状态的 session，若 `LastIO()` 超过 `execution_timeout`，转为 `TERMINATED`。

**Step 1+2 — 过期清理**（并行查询 SQLite）：
- `max_lifetime`（默认 24h）到期的 session → `TERMINATED`
- `idle_timeout`（默认 60min）到期的 IDLE session → `TERMINATED`

> **重要**：代码中**不执行** TERMINATED → DELETED 的自动清理。TERMINATED 记录作为 "resume 决策标记" 保留，确保后续消息能以 `--resume` 恢复对话历史而非创建新 session。DELETED 仅由管理员通过 Admin API 显式触发。

| 资源 | 触发条件 | 动作 |
|------|---------|------|
| Session | `RUNNING` 且 `LastIO()` 超过 `execution_timeout` | → TERMINATED（僵尸进程） |
| Session | 总存活超过 `max_lifetime`（默认 24h） | → TERMINATED |
| Session | `IDLE` 超过 `idle_timeout`（默认 60min） | → TERMINATED |

### 5.4 持久化

| 层 | 职责 | 实现 |
|----|------|------|
| **控制面**（Gateway） | Session 元数据 | SQLite WAL mode + 单写 goroutine 批量写入 |
| **数据面**（Worker） | 对话历史、上下文 | Worker 自行管理（Claude Code: `.jsonl`） |

---

## 6. Worker 适配器

### 6.1 Worker 接口

```go
type Worker interface {
    Start(ctx context.Context, info SessionInfo) error
    Input(ctx context.Context, content string) error
    Resume(ctx context.Context, info SessionInfo) error
    Terminate(ctx context.Context) error
    Kill() error
    Wait() (int, error)
    Conn() SessionConn
    Health() WorkerHealth
    MaxTurns() int
}
```

### 6.2 适配器注册

遵循 **OCP 开闭原则**的插件注册模式：

1. 中央集控：`internal/worker/` 维护线程安全的 Registry 字典
2. 包级自组装：各 Worker 子包通过 `init()` 回调注册自身
3. 空导入编排：`cmd/hotplex/main.go` 通过 `import _ "..."` 唤醒所有合法挂载项
4. Fail Fast：请求未注册的 Worker 类型，Gateway 在 Session 初期强阻断

### 6.3 进程隔离

```go
cmd := exec.CommandContext(ctx, binary, args...)
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}  // 独立进程组
```

每个 Session 独立进程，PGID 隔离确保 SIGTERM/SIGKILL 能杀掉整个进程树。内存限制：Linux 通过 `RLIMIT_AS` 设置 512MB（macOS 不支持，自动跳过）。

### 6.4 三层终止策略

```
SIGTERM (优雅终止)
  → 等待 gracePeriod (调用方传入的宽限期)
    → SIGKILL (强制终止)
```

### 6.5 各 Worker 类型

| Worker | Transport | Protocol | Lifecycle | 状态 | 特点 |
|--------|-----------|----------|-----------|------|------|
| **Claude Code** | stdio | NDJSON | Persistent | ✅ 生产可用 | 多轮进程复用、`--resume` 恢复、权限协议 |
| **OpenCode Server** | HTTP+SSE | SSE/JSON | Managed | ✅ 生产可用 | 单进程共享、动态端口、健康检查自动重启 |
| **ACPX** | stdio | NDJSON | Persistent | ⬚ 占位 | 目录存在但未实现 |
| **Pi-mono** | stdio | Raw text | Ephemeral | ⬚ 桩文件 | 仅占位 stub |

### 6.6 开发自定义 Worker

新增 Worker 适配器需：

1. **嵌入 BaseWorker**：获取 `Terminate`/`Kill`/`Wait`/`Health`/`LastIO` 共享实现
2. **实现三个核心方法**：`Start`（启动进程）、`Input`（发送输入）、`Resume`（恢复会话）
3. **注册适配器**：`init()` 函数中调用 `worker.Register(worker.TypeXxx, New)`
4. **环境变量白名单**：通过 `base.BuildEnv` 构建安全的进程环境
5. **输出解析**：将 Worker stdout 映射为 AEP 事件

---

## 7. 消息平台集成

### 7.1 概述

Hotplex 通过 `PlatformBridge` 实现 AI Agent 与消息平台的双向桥接，支持 Slack 和飞书两个平台。核心设计理念是**零侵入扩展**——新增平台只需实现 `PlatformConn` 接口，Gateway 核心代码无需修改。

### 7.2 三步会话流程

所有平台消息遵循 `StartSession → Join → Handle` 三步流程：

```
1. StartPlatformSession → 自动创建 session（幂等，已存在则跳过）
2. JoinPlatformSession  → 将 PlatformConn 注册到 Hub 广播列表
3. handler.Handle       → 将 AEP Input Envelope 投递到 Gateway Handler
```

Session ID 通过 `session.DerivePlatformSessionKey`（UUIDv5）确定性生成：
- Slack: `slack:{team_id}:{channel_id}:{thread_ts}:{user_id}`
- 飞书: `feishu:{chat_id}:{thread_ts}:{user_id}`

### 7.3 Slack 集成

#### 连接方式

通过 `slack-go/slack` SDK 的 **Socket Mode** 建立长连接，无需公网入口。

#### 事件处理管线

9 步消息过滤管线：Bot 过滤 → 子类型过滤 → 消息过期(30min) → 媒体转换 → 访问控制 → 线程归属 → 去重 → Mention 解析 → 控制命令 / Worker 命令检测

#### 流式输出

`NativeStreamingWriter` 通过 Slack API 增量更新已发送的消息，实现 token 级实时流。

#### 斜杠命令

Slack 原生斜杠命令（通过 Socket Mode 接收，非消息文本解析）：

| 命令 | 效果 |
|------|------|
| `/reset` | 重置上下文（杀进程 + 新建 session） |
| `/dc` | 断开连接，终止 Session（`ControlActionTerminate`），保留元数据供后续 resume |

> **注意**：消息文本中的斜杠命令（如 `/gc`、`/reset`、`/park`、`/new`）由 `ParseControlCommand()` 解析，详见 §7.6。

#### 交互按钮

通过 Block Kit 实现三种交互 UI：

| 交互类型 | UI 形式 | 触发场景 |
|---------|---------|---------|
| Permission | Allow/Deny 按钮 | 工具执行需要用户授权 |
| Question | 选项按钮列表 | Worker 向用户提问 |
| Elicitation | Accept/Decline 按钮 | MCP Server 请求用户输入 |

#### Status 指示

`StatusManager` 支持双模式：原生 Assistant API（付费 workspace）和 Emoji 反应降级（免费 workspace）。支持 7 种状态表情：初始化、思考中、工具使用、回答中等。

### 7.4 飞书集成

#### 连接方式

通过 `larksuite/oapi-sdk-go/v3/ws` 建立 WebSocket 长连接，2h token 自动刷新。

#### 事件处理

P2 事件处理，9 步管线与 Slack 类似，额外支持：
- **语音转录（STT）**：4 种实现
- **ChatQueue**：同一 chatID 的消息串行处理，10 分钟超时防泄漏

#### 流式卡片

`StreamingCardController` 实现 7 阶段状态机的流式卡片更新，使用 CardKit v2 格式。

#### STT 语音转文字

| 实现 | 说明 | 特点 |
|------|------|------|
| **FeishuSTT** | 飞书云端 API | 无需磁盘，audio → PCM → base64 |
| **LocalSTT** | 外部命令 | 每次启动新进程 |
| **PersistentSTT** | 长驻子进程 | JSON-over-stdio，零冷启动，PGID 隔离，idle TTL 自动关闭 |
| **FallbackSTT** | 主备链 | primary 失败自动切换 secondary |

PersistentSTT 使用 SenseVoice-Small ONNX 模型，通过 `funasr-onnx` 运行。

#### 交互处理

飞书 WebSocket 不转发 `card.action.trigger` 事件，因此采用**文本回复**模式：
- Permission：回复 "允许/allow" 或 "拒绝/deny"
- Question：回复选项文本
- Elicitation：回复 "accept/decline"

#### Typing 指示

通过 emoji 表情反应实现：添加 "eyes" 表情 → 工具执行时切换表情 → Done 时清理。

### 7.5 用户交互机制

**InteractionManager** 是平台无关的交互基础设施：

- `Register`：注册 pending interaction 并启动超时 goroutine
- `Complete`：用户响应后移除
- `watchTimeout`：超时自动拒绝（Permission → denied，Question → 空答案，Elicitation → cancel）
- `CancelAll`：session 结束时清理

### 7.6 交互指令体系

用户通过消息平台与 Worker 交互的指令分为三层：**会话控制**（Gateway 层状态变更）、**Worker 命令**（Worker 运行时操作）、**用户交互响应**（权限/Q&A/Elicitation）。

#### 7.6.1 会话控制指令

会话控制指令通过 `ParseControlCommand()` 解析，影响 Session 状态机的转换。支持斜杠命令和 `$` 前缀自然语言两种触发方式。

| 斜杠命令 | 自然语言 | ControlAction | 说明 |
|---------|---------|--------------|------|
| `/gc` | `$gc` `$休眠` `$挂起` | `gc` | 休眠 Session：终止 Worker 进程，释放资源，Session 保留为 TERMINATED 状态，发消息可 `--resume` 恢复 |
| `/park` | — | `gc` | 同 `/gc`（语义别名：park = 停靠） |
| `/reset` | `$reset` `$重置` | `reset` | 重置上下文：复用 Session ID，杀掉旧 Worker，启动全新 Worker 进程，对话历史清空 |
| `/new` | — | `reset` | 同 `/reset`（语义别名：new = 新对话） |

> **设计决策**：自然语言触发必须带 `$` 前缀（如 `$休眠`），防止用户日常对话意外触发控制命令。斜杠命令（如 `/gc`）无此前缀要求。

**解析规则**：
- 输入文本经 `TrimSpace` → `ToLower` → 去除尾部标点（`.!?,;:…，。；：！？、`）后精确匹配
- 匹配优先级：斜杠命令 → 自然语言 → 当作普通 Input 处理

**控制效果**：

| ControlAction | Session 状态变更 | Worker 动作 | 后续行为 |
|--------------|----------------|------------|---------|
| `gc` | → TERMINATED | Terminate（优雅终止） | 下次消息触发 `--resume` 恢复 |
| `reset` | 保持 RUNNING（若活跃）或 → RUNNING | Terminate → 删除文件 → Start 全新进程 | 新进程接收下一条 Input |

**AEP 协议层**：除消息平台外，WebSocket 客户端也可通过 `control` 事件发送额外两种命令：

| ControlAction | 方向 | 说明 |
|--------------|------|------|
| `terminate` | C→S | 终止 Session（需 Owner 验证），发送 `session_terminated` 错误 + `done` |
| `delete` | C→S | 强制删除 Session（需 Owner 验证），跳过 TERMINATED 直达 DELETED |

#### 7.6.2 Worker Stdio 命令

Worker 命令通过 `ParseWorkerCommand()` 解析，直接作用于正在运行的 Worker 子进程，**不改变 Session 状态**。分为两类：

**结构化请求（ControlRequest）**：通过 `SendControlRequest()` 发送，Worker 返回结构化响应。

| 斜杠命令 | 自然语言 | StdioCommand | 说明 | 响应事件 |
|---------|---------|-------------|------|---------|
| `/context` | `$context` `$上下文` | `context_usage` | 查询当前上下文窗口用量（总 tokens / 最大 tokens / 百分比 / 分类明细） | `context_usage` |
| `/skills` | `$skills` `$技能` | `skills` | 查询当前已加载的 Agent 技能列表及其 token 占用 | `context_usage` |
| `/mcp` | `$mcp` | `mcp_status` | 查询 MCP Server 连接状态（各 server 名称 + 状态） | `mcp_status` |
| `/model <name>` | `$model` `$切换模型` | `set_model` | 切换当前使用的模型（如 `sonnet-4`、`opus-4`） | 无（静默生效） |
| `/perm <mode>` | `$perm` `$权限模式` | `set_permission` | 设置权限模式（如 `bypassPermissions`） | 无（静默生效） |

**消息透传（Passthrough）**：通过 `Input()` 以 `/command args` 形式发送给 Worker，由 Worker CLI 自行解析执行。

| 斜杠命令 | 自然语言 | StdioCommand | 说明 |
|---------|---------|-------------|------|
| `/compact` | `$compact` `$压缩` | `compact` | 压缩上下文，释放 token 空间 |
| `/clear` | `$clear` `$清空` | `clear` | 清空对话历史 |
| `/effort <level>` | `$effort` | `effort` | 设置推理努力级别（如 `high`、`low`） |
| `/rewind` | `$rewind` `$回退` | `rewind` | 回退上一步操作 |
| `/commit` | `$commit` `$提交` | `commit` | 提交当前变更（生成 git commit） |

> **注意**：带参数的命令（`/model`、`/perm`、`/effort`）通过空格分隔参数（如 `/model sonnet-4`）。自然语言触发不支持参数，使用默认值。

**AEP 协议层**：WebSocket 客户端通过 `worker_command` 事件（Kind = `"worker_command"`）发送 Worker 命令，载荷格式：

```json
{
  "type": "worker_command",
  "data": {
    "command": "context_usage",
    "args": "",
    "extra": {}
  }
}
```

#### 7.6.3 用户交互响应

当 Worker 执行过程中需要用户确认或输入时，通过 `InteractionManager` 管理。三种交互类型各有 5 分钟超时，超时后自动拒绝。

| 交互类型 | 触发事件 | 响应方式 | 超时默认行为 | 说明 |
|---------|---------|---------|------------|------|
| **权限审批** | `permission_request` | Allow / Deny | 自动 Deny | 高危工具执行需用户授权（如 Bash 命令、文件写入） |
| **问题回答** | `question_request` | 选择选项 / 文本回复 | 空答案 | Worker 向用户提问（如选择分支、确认意图） |
| **MCP Elicitation** | `elicitation_request` | Accept / Decline | 自动 Cancel | MCP Server 请求用户提供输入（如 API Key、配置值） |

**平台特定交互 UI**：

| 平台 | Permission | Question | Elicitation |
|------|-----------|----------|-------------|
| **Slack** | Block Kit Allow/Deny 按钮 | Block Kit 选项按钮列表 | Block Kit Accept/Decline 按钮 |
| **飞书** | 文本回复 `允许`/`allow` 或 `拒绝`/`deny` | 文本回复选项文本 | 文本回复 `accept`/`decline` |
| **WebSocket** | `permission_response` 事件 | `question_response` 事件 | `elicitation_response` 事件 |

**交互生命周期**：
1. Worker 发出请求 → `InteractionManager.Register()` 注册 + 启动超时计时器
2. 适配器渲染平台 UI（按钮/卡片/提示）
3. 用户响应 → `InteractionManager.Complete()` 移除 + 回调 `SendResponse()`
4. 若 5 分钟无响应 → `watchTimeout` 自动发送拒绝/取消响应
5. Session 结束（gc/reset/close） → `CancelAll()` 清理所有 pending 交互

#### 7.6.4 完整交互指令速查表

**会话控制**（改变 Session 状态）：

```
/gc          → 休眠 Session（释放进程，保留元数据，可 resume）
/park        → 同 /gc
/reset       → 重置上下文（杀进程 + 新建，Session ID 不变）
/new         → 同 /reset
$gc          → 同 /gc（自然语言触发）
$休眠         → 同 /gc
$挂起         → 同 /gc
$reset       → 同 /reset（自然语言触发）
$重置         → 同 /reset
```

**Worker 命令**（不改变 Session 状态，作用于运行中的 Worker）：

```
/context                → 查询上下文窗口用量
/mcp                    → 查询 MCP Server 状态
/model <name>           → 切换模型（如 sonnet-4, opus-4）
/perm <mode>            → 设置权限模式
/compact                → 压缩上下文
/clear                  → 清空对话历史
/effort <level>         → 设置推理努力级别
/rewind                 → 回退上一步
/commit                 → 提交变更
$context / $上下文       → 同 /context
$mcp                    → 同 /mcp
$model / $切换模型       → 同 /model（无参数）
$perm / $权限模式        → 同 /perm（无参数）
$compact / $压缩        → 同 /compact
$clear / $清空          → 同 /clear
$effort                 → 同 /effort（无参数）
$rewind / $回退         → 同 /rewind
$commit / $提交         → 同 /commit
```

**用户交互响应**（被动触发，用户回复 Worker 请求）：

```
Permission:  允许/allow → 授权  |  拒绝/deny → 拒绝
Question:    回复选项文本或答案
Elicitation: accept → 接受  |  decline → 拒绝
```

---

## 8. 客户端 SDK

### 8.1 SDK 概览

| SDK | 位置 | 成熟度 | Transport | 事件模式 |
|-----|------|--------|-----------|---------|
| **Go** | `client/client.go` | 生产级 | gorilla/websocket | channel (`<-chan Event`) |
| **TypeScript** | `examples/typescript-client/` | 生产级 | ws / native WebSocket | EventEmitter3 |
| **Python** | `examples/python-client/` | 已实现 | websockets | 装饰器回调 (async) |
| **Java** | `examples/java-client/` | 开发中 | Maven, Java 17+ | Listener 模式 |

### 8.2 Go Client SDK

Go SDK 是项目的一等公民，作为独立子模块实现。

```go
// 创建客户端
c, err := client.New(ctx,
    client.URL("ws://localhost:8888"),
    client.WorkerType("claude_code"),
    client.AuthToken("jwt-token"),
)

// 创建新会话
ack, err := c.Connect(ctx)

// 恢复已有会话
ack, err := c.Resume(ctx, "sess_existing-id")

// 发送用户输入
err := c.SendInput(ctx, "Write a hello world in Go")

// 回复权限请求
err := c.SendPermissionResponse(ctx, "perm_id", true, "approved")

// 发送控制指令
err := c.SendControl(ctx, "terminate")

// 接收事件
for evt := range c.Events() {
    switch evt.Type {
    case "message.delta":
        // 流式增量文本
    case "done":
        // Turn 完成
    case "error":
        // 错误
    }
}
```

内部通过三个 goroutine 实现：`recvPump`（持续读取 WebSocket）、`sendPump`（写入 WebSocket）、`pingPump`（54 秒心跳）。

### 8.3 TypeScript SDK

基于 EventEmitter3 事件模式，完整实现 17 种 AEP 事件类型：

```typescript
const client = new HotPlexClient({
  url: 'ws://localhost:8888/ws',
  workerType: WorkerType.ClaudeCode,
  apiKey: 'dev-api-key',
});

const ack = await client.connect();

client.on('delta', (data, env) => { /* 流式文本 */ });
client.on('done', (data, env) => { /* Turn 完成 */ });
client.on('error', (data, env) => { /* 错误 */ });
client.on('toolCall', (data, env) => { /* 工具调用 */ });
client.on('permissionRequest', (data, env) => { /* 权限请求 */ });
```

自动重连：指数退避（1s → 60s，最多 10 次）。Session Busy 自动重试。

### 8.4 Python SDK

三层架构：`protocol.py`（编解码）→ `transport.py`（WebSocket 管理）→ `client.py`（高层 API），全异步：

```python
async with HotPlexClient(
    url="ws://localhost:8888",
    worker_type=WorkerType.CLAUDE_CODE,
    auth_token="jwt-token",
) as client:
    @client.on_message_delta
    async def handle_delta(data: MessageDeltaData):
        print(data.content, end="")

    @client.on_done
    async def handle_done(data: DoneData):
        print(f"Done: {data.success}")

    await client.send_input("Write a hello world in Python")
```

### 8.5 Worker 通用协议

所有 Worker 通过 **stdin/stdout** 与 Gateway 通信，使用 NDJSON 格式。每行一个完整的 AEP v1 Envelope JSON 对象。

17 种事件类型分为：消息类（input/message/delta）、工具类（tool_call/tool_result）、状态类（state/done）、控制类（error/ping/pong/control/raw）、交互类（permission_request/response、question_request/response、elicitation_request/response）、Worker 命令类（worker_command、context_usage、mcp_status）、扩展类（step/reasoning）。

---

## 9. Web Chat UI

### 9.1 技术栈

| 技术 | 版本 | 用途 |
|------|------|------|
| Next.js | ^15.0.0 | 应用框架 |
| React | ^19.0.0 | UI 库 |
| Tailwind CSS | ^4.2.2 | 样式 |
| @assistant-ui/react | ^0.12.23 | 聊天 UI 组件库 |
| Vercel AI SDK | ^6.0.146 | Chat hook + Transport |
| Playwright | ^1.59.1 | E2E 测试 |

### 9.2 AI SDK Transport 适配器

AEP over WebSocket 与 Vercel AI SDK 的 HTTP-based 传输之间的桥接层：

| 组件 | 职责 |
|------|------|
| `BrowserHotPlexClient` | 浏览器端 WebSocket 客户端，native WebSocket 替代 Node ws |
| `ChunkMapper` | AEP 事件到 AI SDK DataStream 格式的映射 |
| `StreamController` | 将 BrowserClient 事件桥接到 AI SDK 的 DataStreamWriter |

### 9.3 功能特性

- 流式聊天界面（@assistant-ui/react 组件库）
- Session 管理和历史
- Markdown 渲染 + 代码高亮
- Dark/Light 主题
- SSR-safe（WebSocket 组件 dynamic import）

### 9.4 开发

```bash
cd webchat && pnpm dev     # http://localhost:3000
pnpm build                  # 构建
pnpm test:e2e               # Playwright E2E 测试
```

---

## 10. 安全架构

### 10.1 安全模型

基于「**白名单优先 + 深度防御**」原则，五层安全防护：

| 层级 | 防护内容 |
|------|---------|
| **协议层** | AEP v1 JSON Schema 验证 |
| **认证层** | ES256 JWT + API Key + Admin Token |
| **输入层** | 命令白名单、路径安全、环境变量隔离 |
| **网络层** | SSRF 四层防护（协议→IP→DNS→Host） |
| **AI 执行层** | AllowedTools + Bash 命令拦截 + 权限审批 |

### 10.2 认证

#### API Key

客户端发送 API key via `X-API-Key` header（可配置）。Dev 模式接受任意值。

#### JWT (ES256)

- **验证算法**：仅接受 ES256 (ECDSA P-256 SHA-256)，非 ES256 签名一律拒绝
- WebSocket 双保险：握手阶段（Cookie/Header）+ 首条消息（init envelope 内 token）
- JWT Claims：`iss`、`sub`、`aud`（必须为 `hotplex-gateway`）、`jti`（防重放）、`bot_id`（Bot 隔离）
- Token 分层 TTL：Access 5min / Gateway 1h / Refresh 7d

> **注意**：`GenerateTokenWithJTI()` 在使用 `[]byte` secret 时会生成 HS256 签名的 token（用于内部 JTI 场景），但 Gateway 验证路径仅接受 ES256，确保端到端安全性。

#### Admin Token + Scope

Bearer Token 认证 + Scope 粒度权限控制，支持滚动双 token 轮换。

#### Bot ID 隔离

共享 ES256 密钥 + JWT `bot_id` claim 隔离，Gateway 按 `bot_id` 路由到对应 Worker Pool。

### 10.3 SSRF 防护

四层防护：协议层（仅 http/https）→ IP 层（阻断私有 IP/环回/链路本地）→ DNS 重绑定防护（解析后二次验证）→ Host 头验证。

### 10.4 命令白名单

```go
var AllowedCommands = map[string]bool{
    "claude":   true,
    "opencode": true,
}
```

禁止 `exec.Command("sh", "-c", ...)` 模式。

### 10.5 环境变量隔离

`SafeEnvBuilder` 三层过滤：基础白名单 → Worker 特定白名单 → Hotplex 控制变量。

### 10.6 AI 工具策略

- **Tool 执行模型**：Autonomous（Worker 自行决定和执行 tool 调用）
- **AllowedTools**：通过 `--allowed-tools` 参数限制可用工具集
- **Bash 命令拦截**：P0 灭绝性命令（`rm -rf /`）自动拒绝，P1 凭据泄露命令拦截
- **权限审批协议**：高危操作通过 `permission_request` 事件交互式审批

### 10.7 TLS

```yaml
security:
  tls_enabled: true
  tls_cert_file: "/etc/hotplex/tls.crt"
  tls_key_file: "/etc/hotplex/tls.key"
```

非本地地址禁用 TLS 时会发出警告。

---

## 11. 配置管理

### 11.1 配置层级

四层优先级（从高到低）：

1. 命令行 flag：`-gateway.addr :9999`
2. 环境变量：`HOTPLEX_GATEWAY_ADDR`
3. 配置文件：`configs/config.yaml`
4. 代码默认值：`internal/config/config.go:Default()`

### 11.2 配置文件

```yaml
gateway:
  addr: ":8888"
  ping_interval: 54s
  pong_timeout: 60s
  idle_timeout: 5m
  broadcast_queue_size: 256

db:
  path: "/var/hotplex/hotplex.db"
  wal_mode: true
  busy_timeout: 500ms

worker:
  max_lifetime: 24h
  idle_timeout: 60m
  execution_timeout: 10m
  env_whitelist:
    - HOME
    - PATH
    - CLAUDE_API_KEY

security:
  api_key_header: "X-API-Key"
  api_keys:
    - "sk-hotplex-secret-key-1"
  tls_enabled: true
  tls_cert_file: "/etc/hotplex/tls.crt"
  tls_key_file: "/etc/hotplex/tls.key"

session:
  retention_period: 168h    # 7 days
  gc_scan_interval: 1m
  max_concurrent: 1000

admin:
  enabled: true
  addr: ":9999"
  tokens:
    - "admin-token-1"
  rate_limit_enabled: true
  requests_per_sec: 10

inherits: "./defaults.yaml"   # optional: parent config
```

### 11.3 环境变量展开

YAML 中支持 `${VAR}` 和 `${VAR:-default}` 语法。

### 11.4 热重载

| 类型 | 字段 |
|------|------|
| **动态**（无需重启） | gateway 超时、pool 设置、session 保留、admin 限流、STT 设置 |
| **静态**（需重启） | 网络地址、TLS 设置、数据库路径 |

热重载基于 `fsnotify` + 500ms debounce，保留最近 64 个配置版本。

### 11.5 配置历史与回滚

```bash
# 查看历史
curl -H "Authorization: Bearer admin-token-1" \
  http://localhost:9999/admin/config/history

# 回滚到 N 版本前
curl -X POST -H "Authorization: Bearer admin-token-1" \
  http://localhost:9999/admin/config/rollback/3
```

---

## 12. 运维管理

### 12.1 Admin API

| 端点 | 方法 | Scope | 用途 |
|------|------|-------|------|
| `/admin/health` | GET | 无需认证 | 健康检查 |
| `/admin/health/workers` | GET | `health:read` | Worker 健康状态 |
| `/admin/stats` | GET | `stats:read` | 统计摘要 |
| `/admin/pool` | GET | `stats:read` | Pool 统计 |
| `/admin/sessions` | GET | `session:list` | 列出 session |
| `/admin/sessions/:id` | GET | `session:read` | Session 详情 |
| `/admin/sessions` | POST | `session:write` | 创建 session |
| `/admin/sessions/:id/terminate` | POST | `session:write` | 终止 session |
| `/admin/sessions/:id` | DELETE | `session:kill` | 强制终止 session |
| `/admin/config/history` | GET | `config:read` | 配置历史 |
| `/admin/config/validate` | POST | `config:validate` | 验证配置 |
| `/admin/config/rollback/:v` | POST | `config:write` | 回滚配置 |
| `/admin/logs` | GET | `logs:read` | 日志查询 |
| `/admin/debug/sessions/:id` | GET | `debug:read` | Session 调试 |
| `/admin/metrics` | GET | `stats:read` | Prometheus 指标 |

### 12.2 资源管理

| 维度 | 默认限制 |
|------|---------|
| 全局最大并发 Worker | 20 |
| 每用户最大并发 | 5 |
| 每用户最大内存 | 2048 MB |
| Input 队列 | 100 |
| Output 队列 | 50 |
| 单行输出限制 | 10MB |
| 单轮输出限制 | 20MB |
| Envelope 大小限制 | 1MB |

---

## 13. 可观测性

### 13.1 结构化日志

`log/slog` JSON handler，关键字段：

| 字段 | 说明 |
|------|------|
| `service.name` | 固定 `hotplex-gateway` |
| `session_id` | Session 作用域日志 |
| `user_id` | 认证用户 |
| `bot_id` | Bot ID（JWT） |
| `trace_id` / `span_id` | OTEL 链路追踪 |

### 13.2 Prometheus 指标

命名规范：`hotplex_<group>_<metric>_<unit>`

| 维度 | 示例指标 |
|------|---------|
| Session | `hotplex_sessions_active`, `hotplex_session_duration_seconds` |
| Event | `hotplex_events_sent_total` |
| Worker | `hotplex_worker_cpu_seconds_total`, `hotplex_worker_crashes_total` |
| WebSocket | `hotplex_ws_connections_active` |
| Error | `hotplex_errors_total` |

端点：`GET /admin/metrics`（`:9999` 端口）

### 13.3 OpenTelemetry 链路追踪

通过 OTEL 环境变量启用：

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT="http://collector:4317"
./hotplex
```

AEP 事件自动注入 trace context（`trace_id`、`span_id` 存入 `event.Metadata`）。

---

## 14. 部署指南

### 14.1 快速开始

```bash
# 零配置启动
./bin/hotplex-darwin-arm64

# 指定配置
./bin/hotplex-darwin-arm64 -config /etc/hotplex/config.yaml

# 开发模式（放宽安全）
./bin/hotplex-darwin-arm64 -dev
```

### 14.2 从源码构建

```bash
git clone https://github.com/hrygo/hotplex.git
cd hotplex
make build                    # 输出: bin/hotplex-<os>-<arch>
make build-all                # 交叉编译 linux/amd64 + darwin/arm64
make build-pgo                # PGO 优化构建
```

### 14.3 Docker 部署

```bash
docker run -p 8888:8888 -p 9999:9999 \
  -v /path/to/config.yaml:/config.yaml \
  -e HOTPLEX_JWT_SECRET=your-secret \
  hotplex:latest
```

### 14.4 开发环境

```bash
cp configs/env.example .env    # 配置环境变量
make dev                       # Gateway :8888, Webchat :3000, Admin :9999
```

### 14.5 开发工具

```bash
make setup        # 安装 golangci-lint v1.64.8
make lint         # 运行 linter
make lint-fix     # 自动修复 lint 问题
make test         # 测试（含 -race）
make test-short   # 快速测试（跳过集成测试）
make coverage     # 覆盖率报告
make quality      # fmt + vet + lint + test
make check        # 完整 CI 流程
```

---

## 15. 测试策略

### 15.1 测试层次

| 层次 | 目标数 | 工具 | 特点 |
|------|--------|------|------|
| 单元测试 | 200+ | testify/mock, table-driven | `t.Parallel()`, 核心安全逻辑不 Mock |
| 集成测试 | 50-80 | Testcontainers | 真实并发测试，`testing.Short()` 跳过 |
| E2E 测试 | 10-20 | Playwright | 真实 Gateway 实例，`-tags=e2e` 隔离 |

### 15.2 覆盖率要求

| 模块 | 目标 | 理由 |
|------|------|------|
| `internal/security/` | 85%+ | 安全核心 |
| `pkg/events/`, `pkg/aep/` | 85%+ | 协议编解码 |
| `internal/gateway/` | 75%+ | WebSocket 核心引擎 |
| `internal/session/` | 70%+ | 状态机 + SQLite |
| `internal/worker/claudecode/` | 60%+ | 外部进程适配器 |
| `internal/messaging/` | 50%+ | 平台适配层 |

### 15.3 安全测试

- 命令注入 payload 测试（`; rm -rf /`、`| cat /etc/passwd`、`$(id)`）
- Fuzzing（`FuzzEnvelopeValidation`）
- 性能测试：P95 < 500ms，失败率 < 1%（k6）

---

## 16. 灾备与高可用

### 16.1 RTO/RPO 目标

| 场景 | RTO | RPO |
|------|-----|-----|
| 进程崩溃 | < 1 分钟 | 0（自动重启） |
| 主机故障 | < 5 分钟 | < 1 小时 |
| 数据损坏 | < 15 分钟 | < 1 小时 |
| 全量灾难 | < 1 小时 | < 24 小时 |

### 16.2 自动恢复

- systemd：`RestartSec=5s`
- Docker：`restart: unless-stopped`

### 16.3 数据库备份

Docker Compose 包含每小时自动备份（retention 30 天）。手动备份推荐先停止服务。

```bash
# 验证备份
sqlite3 backup.db "PRAGMA integrity_check;"
```

### 16.4 恢复流程

数据库损坏：停止 → 验证损坏 → 查找备份 → 验证完整性 → 恢复 → 启动 → 健康检查

Secret 轮换：生成新 JWT Secret + Admin Token → 写入 secrets.env → 重启 → 更新客户端

---

## 17. 故障排查

### 17.1 常见问题

**Binary 无法启动**

```bash
# JWT Secret 缺失
export HOTPLEX_JWT_SECRET="your-256-bit-secret"
./hotplex

# 配置文件不存在
./hotplex -config /absolute/path/to/config.yaml
```

**WebSocket 连接被拒绝**

```bash
# 检查监听地址
curl -v http://localhost:8888   # 应返回 400 Bad Request（非 WS 升级）
```

**认证失败**

- 401：验证 API key 与 `security.api_keys` 匹配
- JWT：确保 ES256 签名 + `jwt_audience` 正确

**Worker 未启动**

```bash
which claude     # 确保 claude 二进制在 PATH 中
claude --dir /tmp/session --json-stream   # 手动测试
```

**高内存**

- 检查 `pool.max_size`、`pool.max_memory_per_user`
- Session GC 积压：验证 `gc_scan_interval` 和 `retention_period`
- Worker 进程未清理：检查进程树

**热重载不生效**

- 文件权限检查
- 静态字段需重启
- 查看日志：`config reloaded successfully` 或 `failed to reload`

### 17.2 调试

```bash
# Session 调试（需要 debug:read scope）
curl -H "Authorization: Bearer admin-token-1" \
  http://localhost:9999/admin/debug/sessions/sess_abc123

# 最近日志
curl -H "Authorization: Bearer admin-token-1" \
  "http://localhost:9999/admin/logs?limit=50"
```

---

## 18. API 参考

### 18.1 AEP WebSocket API

**端点**: `ws://<host>:8888`

**认证**: 首条消息发送 `init` 包含 `auth.token`

**消息格式**: NDJSON（每行一个 JSON Envelope）

**心跳**: Client 54s ping / Server 60s pong timeout

### 18.2 Admin HTTP API

**端点**: `http://<host>:9999/admin/*`

**认证**: Bearer Token（`Authorization: Bearer <token>`）

**Scope 矩阵**:

| Scope | 端点 |
|-------|------|
| `session:list` | GET sessions |
| `session:read` | GET sessions/:id |
| `session:write` | POST sessions, POST sessions/:id/terminate |
| `session:kill` | DELETE sessions/:id |
| `stats:read` | GET stats, GET pool, GET metrics |
| `health:read` | GET health, GET health/workers |
| `config:read` | GET config/history |
| `config:validate` | POST config/validate |
| `config:write` | POST config/rollback |
| `logs:read` | GET logs |
| `debug:read` | GET debug/sessions/:id |

---

## 19. 术语表

| 术语 | 说明 |
|------|------|
| **AEP** | Agent Exchange Protocol — Hotplex 自定义的 Agent 通信协议 |
| **Worker** | AI Coding Agent 的适配器封装（如 claude-code、opencode-server） |
| **Session** | 一次 Agent 对话的生命周期单元，独立于连接存在 |
| **Hub** | WebSocket 广播中心，管理连接注册和消息路由 |
| **Bridge** | Session ↔ Worker 生命周期编排器 |
| **Platform Bridge** | 消息平台与 Gateway 的桥接层 |
| **PlatformConn** | 消息平台连接的写入侧抽象接口 |
| **Envelope** | AEP v1 消息信封，包含 id/seq/session_id/event 等字段 |
| **GC** | Session 垃圾回收，清理过期或终止的 Session |
| **PGID** | Process Group ID，用于进程隔离和清理子进程树 |
| **Socket Mode** | Slack 的 WebSocket 连接模式，无需公网入口 |
| **STT** | Speech-to-Text，语音转文字 |
| **CardKit** | 飞书卡片渲染框架 v2，支持流式更新 |
| **NDJSON** | Newline-Delimited JSON，每行一个 JSON 对象 |
| **WAL** | Write-Ahead Logging，SQLite 的日志写入模式 |
| **OTEL** | OpenTelemetry，分布式链路追踪标准 |
| **Hot-Multiplexing** | 单 Worker 进程服务多轮对话，避免冷启动 |
| **pcEntry** | PlatformConn wrapper，适配到 Hub 的 sessions map |

---

> **Hotplex** — AI Coding Agent 的统一管理平台
> 
> 更多信息请参阅：
> - 架构设计：`docs/architecture/`
> - 功能规格：`docs/specs/`
> - 安全文档：`docs/security/`
> - 运维文档：`docs/management/`
> - 用户手册：`docs/User-Manual.md`
