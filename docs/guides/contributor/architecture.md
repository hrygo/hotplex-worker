---
title: 架构概览
weight: 31
description: HotPlex Gateway 系统架构全景视图，帮助贡献者理解各组件职责与交互方式
---

# 架构概览

> 阅读本文后，你将理解 HotPlex Gateway 的整体架构、核心模块职责、数据流路径和关键设计决策。

## 概述

HotPlex Gateway 是一个**单进程 Go 应用**，基于 Go 1.26 构建，作为 AI Coding Agent 的统一接入层。它通过 WebSocket（AEP v1 协议）对外提供服务，内部通过 goroutine 实现并发，每个用户会话对应一个 Worker 子进程。

核心能力：抹平 Claude Code 和 OpenCode Server 的协议差异，统一暴露 AEP v1 WebSocket 接口，同时支持 Slack/飞书双向消息。

## 前提条件

- 已完成[开发环境搭建](development-setup.md)
- 了解 Go 基本概念（goroutine、channel、interface）
- 了解 WebSocket 基本原理

## 架构总览

### 单进程模型

```
┌──────────────────────────────────────────────────────┐
│                   Gateway Process                     │
│                                                       │
│  ┌──────────┐   ┌───────┐   ┌──────────────────┐    │
│  │ HTTP/WS  │──▶│  Hub  │──▶│ Session Manager   │    │
│  │ Server   │   │       │   │ (5-state machine) │    │
│  └──────────┘   └───┬───┘   └──────────────────┘    │
│                     │                                 │
│              ┌──────┴──────┐                         │
│              │   Handler   │  AEP 事件分发             │
│              └──────┬──────┘                         │
│                     │                                 │
│              ┌──────┴──────┐                         │
│              │   Bridge    │  生命周期编排             │
│              └──────┬──────┘                         │
│                     │                                 │
│         ┌───────────┴───────────┐                    │
│         │                       │                     │
│    ┌────┴─────┐          ┌─────┴──────┐             │
│    │ Worker A │          │  Worker B   │             │
│    │ (claude) │          │ (opencode)  │             │
│    └──────────┘          └────────────┘             │
└──────────────────────────────────────────────────────┘
        │                       │
        ▼                       ▼
  子进程 (PGID隔离)       子进程 (PGID隔离)
```

### 进程模型

```
Gateway (主进程)
├── goroutine: Hub.Run()            # 消息分发循环
├── goroutine: Conn.ReadPump  x N   # 每连接一个读协程
├── goroutine: Conn.WritePump x N   # 每连接一个写协程
├── goroutine: forwardEvents  x N   # 每 Worker 一个转发协程
├── goroutine: SessionManager.runGC()  # GC 定时扫描
├── goroutine: cron.timerLoop()     # 定时任务引擎
├── goroutine: Slack Socket Mode    # Slack 消息监听
├── goroutine: Feishu WS Client     # 飞书消息监听
│
├── subprocess: claude (PGID隔离)    # Claude Code Worker
├── subprocess: claude (PGID隔离)    # Claude Code Worker
└── subprocess: opencode serve      # OCS 单例进程
```

## 核心包详解

### 1. gateway/ — WebSocket 网关核心

**位置**：`internal/gateway/`

网关的心脏，包含 WebSocket 连接管理、消息路由和会话编排。

| 文件 | 职责 |
|------|------|
| `hub.go` | WS 广播中心，管理连接与会话映射，实现背压控制 |
| `conn.go` | 单个 WS 连接，ReadPump/WritePump 双向数据流 |
| `handler.go` | AEP 事件分发器，按事件类型路由到处理函数 |
| `bridge.go` | Session 与 Worker 之间的生命周期编排器 |
| `llm_retry.go` | LLM 调用自动重试控制器 |
| `api.go` | HTTP session 端点（REST API） |

**Hub 关键特性**：

- **背压控制**：`message.delta` 和 `raw` 事件在通道满时静默丢弃，`state`/`done`/`error` 永不丢弃
- **Seq 分配**：每 session 独立的原子单调递增序号
- **平台连接注册**：`JoinPlatformSession` 将 `PlatformConn` 注册到 session 路由表

### 2. session/ — 5 状态机会话管理

**位置**：`internal/session/`

管理用户会话的完整生命周期，SQLite 持久化 + 内存索引。

```
CREATED → RUNNING → IDLE → TERMINATED → DELETED
   │        ↑          ↓         ↑
   │        └── RESUME ←─────────┘
   └──────────────────→ TERMINATED
```

| 状态 | IsActive() | 语义 | 持续时间 |
|------|-----------|------|---------|
| `CREATED` | true | Session 创建，未开始执行 | 瞬态（<1s） |
| `RUNNING` | true | 正在执行 Worker | 业务执行期间 |
| `IDLE` | true | Worker 暂停，等待重连或新输入 | `idle_timeout` GC 前 |
| `TERMINATED` | false | Worker 已终止，保留元数据 | `retention_period` GC 前 |
| `DELETED` | false | 终态，DB 记录已删除 | 永久 |

| 组件 | 职责 |
|------|------|
| `Manager` | 状态机转换、并发控制、GC 回收 |
| `Store` | SQLite WAL 模式持久化 |
| `PoolManager` | 全局 + 每用户配额（默认 20 全局 / 5 per-user） |
| `DeriveSessionKey` | UUIDv5 确定性 session ID 生成 |

**锁顺序**（防止死锁）：`Manager.mu` → `managedSession.mu`，固定顺序不可颠倒。

### 3. messaging/ — 消息平台适配

**位置**：`internal/messaging/`

抹平 Slack 和飞书的协议差异，统一转换为 AEP Envelope。

| 组件 | 位置 | 职责 |
|------|------|------|
| `Bridge` | `messaging/bridge.go` | SessionStarter + ConnFactory |
| `PlatformAdapter` | `messaging/platform_adapter.go` | 基础适配器接口 |
| `BotRegistry` | `messaging/bot_registry.go` | 并发安全多 bot 注册表（Register/Get/Unregister） |
| `config.go` | `messaging/config.go` | `AdapterConfig` 含 `BotName` 字段 |
| `slack/` | `messaging/slack/` | Slack Socket Mode 适配器 |
| `feishu/` | `messaging/feishu/` | 飞书 WS 适配器 + STT |
| `tts/` | `messaging/tts/` | Edge-TTS 语音合成 + FFmpeg Opus 转换 |
| `interaction.go` | `messaging/interaction.go` | 权限/Q&A 交互流转 |

**适配器 2 步初始化**：

1. `ConfigureWith(AdapterConfig)` — 统一注入 Hub/SM/Handler/Bridge/Gate/Extras
2. `Start(ctx)` — 启动连接（后台 goroutine）

### 4. worker/ — Worker 适配器层

**位置**：`internal/worker/`

与 AI Agent 运行时通信的适配器层，通过 `init()` + `worker.Register()` 模式注册。

| Worker | 类型 | 通信方式 | 进程模型 |
|--------|------|----------|---------|
| `claudecode` | 多进程 | stdio (`--print --session-id`) | 每 session 一个进程 |
| `opencode_server` | 单例进程 | HTTP + SSE | 共享一个 `opencode serve` 进程 |

**关键接口**：

```go
// Worker 主接口
type Worker interface {
    Capabilities
    Start(ctx context.Context, session SessionInfo) error
    Input(ctx context.Context, content string, metadata map[string]any) error
    Resume(ctx context.Context, session SessionInfo) error
    Terminate(ctx context.Context) error
    Kill() error
    Wait() (int, error)
    Conn() SessionConn
    Health() WorkerHealth
    LastIO() time.Time
    ResetContext(ctx context.Context) error
}
```

**进程隔离**：

- **POSIX**：PGID (Process Group ID) 隔离，进程组级终止
- **Windows**：Job Object 隔离，父进程退出时自动清理
- **分层终止**：SIGTERM → 等待 5s → SIGKILL

### 5. cron/ — AI-native 定时任务调度器

**位置**：`internal/cron/`

从自然语言意图识别到 CLI 创建再到自动执行+回传的完整调度链路。

| 文件 | 职责 |
|------|------|
| `cron.go` | Scheduler 核心：内存索引、CRUD 操作 |
| `timer.go` | timerLoop tick 引擎：collectDue → 并发槽 CAS → executeJob |
| `store.go` | SQLite 持久化 |
| `types.go` | 数据结构 + Clone() 深拷贝 |
| `schedule.go` | 三种调度：cron 表达式 / every 固定间隔 / at 一次性 |
| `executor.go` | Worker 执行适配：构造 session、注入环境变量 |
| `delivery.go` | 结果投递：按 platform 回传飞书卡片/Slack 消息 |
| `skill.go` | go:embed cron-skill-manual.md → B 通道技能手册 |

### 6. brain/ — LLM 编排层

**位置**：`internal/brain/`

LLM 调用的统一编排层，提供意图分发、安全审计、上下文压缩等能力。

| 文件 | 职责 |
|------|------|
| `brain.go` | 核心接口 + 全局单例 |
| `init.go` | Init() 编排 + 中间件链 (retry → cache → rate limit) |
| `config.go` | 13 子配置 + 4 层 API key 发现 |
| `guard.go` | 输入/输出安全审计、威胁检测 |
| `router.go` | 意图分发、LRU 缓存、快速路径检测 |
| `memory.go` | 上下文压缩 + 用户偏好提取 + TTL 清理 |
| `llm/` | OpenAI/Anthropic 客户端 + 装饰器链 |

### 7. 支撑模块

| 模块 | 位置 | 职责 |
|------|------|------|
| `config/` | `internal/config/` | Viper 配置 + 热重载 + 继承 + 审计/回滚 |
| `agentconfig/` | `internal/agentconfig/` | B/C 双通道 Agent 人格/上下文加载器 |
| `security/` | `internal/security/` | JWT (ES256)、SSRF 防护、路径安全 |
| `eventstore/` | `internal/eventstore/` | 会话事件持久化 + delta 聚合 |
| `metrics/` | `internal/metrics/` | Prometheus 指标 |
| `service/` | `internal/service/` | 跨平台系统服务管理（systemd/launchd/SCM） |
| `updater/` | `internal/updater/` | 自更新（GitHub API、sha256 校验、原子替换） |
| `sqlutil/` | `internal/sqlutil/` | SQLite CGO/no-CGO 驱动切换 |

## 核心数据流

### 消息输入到输出的完整路径

```
用户消息 (Slack/飞书/WebSocket)
    │
    ▼
[接入层] Adapter.HandleTextMessage()
    │  转换为 AEP Envelope
    ▼
[编排层] Bridge.Handle()
    │  1. StartPlatformSession() — 自动创建 session
    │  2. JoinPlatformSession() — 注册 conn 到 hub
    │  3. handler.Handle() — 分发事件
    ▼
[逻辑层] Handler → Bridge.StartSession()
    │  创建 Worker，启动子进程
    ▼
[执行层] Worker.Input()
    │  投递消息到 Worker 进程
    ▼
Worker 处理中... (Claude Code / OpenCode Server)
    │
    ▼
[执行层] Worker.Conn().Recv() → Envelope 流
    │
    ▼
[编排层] Bridge.forwardEvents()
    │  转发到 Hub
    ▼
[分发层] Hub.SendToSession()
    │  路由到所有注册的 SessionWriter
    ▼
[接入层] Conn.WritePump / PlatformConn.WriteCtx()
    │
    ▼
用户收到响应 (WebSocket/Slack/飞书)
```

### Bridge 生命周期编排

```
StartSession() → 创建 Session
       ↓
AttachWorker() → 绑定 Worker 到 Session
       ↓
forwardEvents() → 转发 Worker 输出到 Hub
       ↓
crashRecovery() → 崩溃检测 + 自动恢复
```

### 关闭顺序

系统遵循严格的关闭顺序，确保数据完整性和资源清理：

```
signal (SIGINT/SIGTERM)
    → cancel context
    → tracing shutdown
    → Hub.Close()             # 停止接收新消息
    → Bridge.Close()          # 终止所有 Worker 子进程
    → SessionManager.Close()  # 持久化最终状态
    → HTTP Server.Shutdown()  # 等待活跃请求完成
```

## 控制面与数据面分离

HotPlex 的架构天然分离了控制面和数据面：

**控制面（Control Plane）**：
- Session 创建、状态转换、GC 回收
- Worker 生命周期管理（启动/终止/恢复）
- 配置热重载、路由注册
- Admin API（端口 9999）

**数据面（Data Plane）**：
- AEP 消息收发（用户输入 → Worker 输出）
- WebSocket 帧读写
- 背压控制与 delta 丢弃
- 事件存储（EventStore）

控制面操作通过 mutex 保护，数据面通过 channel 和 atomic 操作实现无锁高并发。

## 关键设计决策

### AEP v1 协议

HotPlex 定义了统一的 Agent Exchange Protocol (AEP) v1，所有客户端和 Worker 之间通过 `Envelope` 通信：

```go
type Envelope struct {
    ID        string  `json:"id"`
    Version   string  `json:"version"`   // 必须为 "aep/v1"
    SessionID string  `json:"session_id"`
    Seq       int64   `json:"seq"`       // 从 1 严格递增
    Timestamp int64   `json:"timestamp"` // Unix ms
    Event     Event  `json:"event"`
    Priority  string  `json:"priority"`  // "data" | "control"
}
```

### Agent 配置双通道

Agent 人格/上下文通过 B/C 双通道注入 Worker：

- **B 通道** (`<directives>`)：`META-COGNITION.md` + `SOUL.md` + `AGENTS.md` + `SKILLS.md`
- **C 通道** (`<context>`)：`USER.md` + `MEMORY.md`

B 通道无条件覆盖 C 通道，防止上下文冲突。

### DI 注入模式

项目**禁止 wire/dig**，全部手动构造函数注入。`GatewayDeps` 结构承载所有依赖：

```go
type GatewayDeps struct {
    Log            *slog.Logger
    Config         *config.Config
    ConfigStore    *config.ConfigStore
    Hub            *gateway.Hub
    SessionMgr     *session.Manager
    EventStore     *eventstore.SQLiteStore
    EventCollector *eventstore.Collector
    Auth           *security.Authenticator
    Handler        *gateway.Handler
    Bridge         *gateway.Bridge
    ConfigWatcher  *config.Watcher
    CronScheduler  *cron.Scheduler
}
```

## 验证

理解架构后，通过以下方式加深认识：

1. 阅读入口文件 `cmd/hotplex/gateway_run.go`，追踪 DI 容器初始化流程
2. 阅读 `internal/gateway/hub.go`，理解消息路由机制
3. 阅读 `internal/session/manager.go`，理解状态机转换逻辑
4. 使用 `make dev` 启动服务，通过 WebChat 发送消息观察日志流

## 下一步

- [测试指南](testing-guide.md) — 学习项目的测试约定和模式
- [扩展指南](extending.md) — 学习如何添加新组件（Worker 适配器、消息平台等）
- [PR 工作流](pr-workflow.md) — 了解如何提交贡献
