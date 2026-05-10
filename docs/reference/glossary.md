---
title: HotPlex 术语表
weight: 10
description: HotPlex Worker Gateway 全部核心术语的权威中英文对照参考，覆盖协议、架构、模块和运维概念。
persona: all
difficulty: beginner
---

# HotPlex 术语表

本文档收录 HotPlex 项目涉及的全部核心术语。按拼音字母顺序排列，每个术语包含英文原名、中文释义和详细说明。

---

## A

### AEP (Agent Event Protocol)

HotPlex 自定义的 WebSocket 全双工通信协议。基于 JSON 信封（Envelope）格式，定义了客户端与网关之间的统一消息交互规范，包括事件类型、序列号分配、背压策略和握手流程。当前版本为 `aep/v1`，定义于 `pkg/events/events.go` 和 `pkg/aep/codec.go`。

AEP 事件分为三个方向：C→S（客户端到服务端，如 `init`、`input`、`control`）、S→C（服务端到客户端，如 `state`、`message.delta`、`done`）和双向（如 `ping`/`pong`）。所有业务消息通过严格的 `seq` 序列号在 session 级别有序编排。

### Admin API

HotPlex 提供的 HTTP 管理接口，默认监听 `localhost:9999`。支持 session 管理、配置查询、诊断端点等运维操作。通过 Token 认证和细粒度 Scope 授权（如 `session:read`、`config:write`）控制访问权限。

---

## B

### B 通道 (B Channel)

Agent 配置的指令通道（Directives Channel），以 XML `<directives>` 标签注入 Worker 的系统提示词。内容包括 `<hotplex>`（META-COGNITION.md，始终存在且排首位）、`<persona>`（SOUL.md）、`<rules>`（AGENTS.md）和 `<skills>`（SKILLS.md）。B 通道为强制性指令，与 C 通道冲突时无条件覆盖。

配置文件存放于 `~/.hotplex/agent-configs/`，通过三级 fallback 加载：全局 → 平台（`slack/`）→ Bot（`slack/U12345/`），每文件独立解析，命中即终止。

### Brain

HotPlex 的 LLM 编排层（`internal/brain/`），提供四个独立接口（能力递增），通过 `enhancedBrainWrapper` 统一实现：`Brain`（基础文本生成）、`StreamingBrain`（流式输出）、`RoutableBrain`（模型路由）、`ObservableBrain`（指标观测）。这四个接口是概念层级（Interface Segregation Principle），而非代码继承关系。

Brain 通过装饰器链（decorator chain）组合功能：retry → cache → rate limit → circuit breaker → metrics。核心子模块包括 Intent Router（意图分发）、Safety Guard（安全审计）、Context Compressor（上下文压缩）和 LLM 客户端子包（OpenAI/Anthropic 适配 + 模型路由 + 成本估算）。

### Bridge

Session 与 Worker 之间的生命周期编排器（`internal/gateway/bridge.go`）。负责 Worker 的 fork/exec、事件转发（Worker stdout → Hub 广播）、崩溃检测与恢复（resume → fresh start fallback）、LLM 自动重试，以及 Agent 配置注入。

Bridge 还实现了 `resetGenerationer` 接口的 reset-aware 崩溃处理：通过单调递增的 generation 计数器区分"正常的 reset 重启"和"异常崩溃"，避免误触发 crash recovery。

---

## C

### C 通道 (C Channel)

Agent 配置的上下文通道（Context Channel），以 XML `<context>` 标签注入 Worker 的参考信息。内容包括 `<user>`（USER.md，用户偏好）和 `<memory>`（MEMORY.md，长期记忆）。C 通道为参考性内容，与 B 通道冲突时以 B 通道为准。

### CardKit

飞书卡片渲染框架 v2（`internal/messaging/feishu/streaming.go`）。支持流式更新（Streaming Card），在 Worker 输出过程中实时刷新卡片内容。HotPlex 实现了四层防御机制：TTL guard（10 分钟超时）→ 完整性检查 → 带退避重试 → IM Patch 降级兜底，以处理 CardKit 服务降级的场景。

### Context Compressor

Brain 模块的上下文压缩组件（`internal/brain/memory.go`）。当对话 token 数超过阈值（默认 8000）时，保留最近 N 轮（默认 5 轮），将更早的对话通过 Brain AI 总结压缩为摘要，防止 LLM 上下文窗口溢出。同时管理用户偏好提取和 TTL 清理。

### Cron

HotPlex 的 AI-native 定时任务引擎（`internal/cron/`），支持三种调度方式：`cron`（标准 cron 表达式）、`every`（固定间隔）和 `at`（一次性执行）。任务 payload 为自然语言提示词，由 Worker 以 Agent Turn 形式执行。

Cron 系统包括：timerLoop tick 引擎（`timer.go`）、并发槽 CAS 控制、SQLite 持久化（`store.go`）、YAML 批量导入（`loader.go`）、执行结果投递（`delivery.go`，按平台回传飞书卡片/Slack 消息），以及 `at` 类型指数退避重试（`retry.go`）。Cron 的技能手册通过 `go:embed` 嵌入 B 通道（`skill.go`）。

---

## D

### Delta Coalescing

流式消息增量合并机制。`pcEntry`（`internal/gateway/platform_writer.go`）在 writeLoop 中将连续的可丢弃事件（`message.delta`、`raw`）合并后批量发送到消息平台，减少 HTTP API 调用次数。

配置项包括 `DeltaCoalesceInterval`（合并时间窗口）和 `DeltaCoalesceSize`（合并大小阈值），通过 `pcEntryConfig` 控制。

---

## E

### Envelope

AEP v1 消息信封（`pkg/events/events.go`），所有客户端-网关通信的统一封装格式。结构体字段包括 `version`（必须为 `aep/v1`）、`id`（消息唯一标识）、`seq`（session 内严格递增序列号）、`session_id`、`timestamp`（Unix ms）、`priority`（`data`/`control`，control 跳过背压）、`event`（包含 `type` 和 `data`），以及仅服务端使用的 `OwnerID`（`json:"-"`，不序列化到网络）。

Envelope 通过 `Clone()` 方法实现深拷贝（递归复制嵌套 map/slice），确保热路径上的并发安全。

---

## G

### GC (Garbage Collection)

Session 垃圾回收机制（`internal/session/manager.go`）。定期扫描（默认 60s 间隔）并清理过期或僵尸 session：

- **IDLE 超时**：`idle_expires_at ≤ now` → TERMINATED
- **最大生命周期**：`expires_at ≤ now` → TERMINATED
- **僵尸检测**：RUNNING session 的 `LastIO()` 超过 execution_timeout（默认 30 分钟）→ TERMINATED
- **保留期清理**：TERMINATED session 的 `updated_at ≤ now - retention_period` → DELETE

---

## H

### Hot-Multiplexing

HotPlex 的核心复用模式：单个 Worker 进程服务多个 Turn（对话轮次），避免冷启动开销。Session 进入 IDLE 状态时 Worker 不终止，收到新 input 时直接复用现有进程恢复执行（`IDLE → RUNNING`），跳过 fork+exec 的延迟。

对于 OpenCode Server 适配器，这一模式通过全局 SingletonProcessManager 进一步强化：所有 OCS Worker 共享一个 `opencode serve` 进程（通过 `atomic.Pointer` 实现并发安全的单例管理），Worker 本身仅为轻量级 SSE 适配器。SingletonProcessManager 使用引用计数跟踪活跃 Worker，当引用归零后启动空闲排空计时器（默认 30 分钟），超时后自动关闭共享进程以释放资源。

### Hub

WebSocket 广播中心（`internal/gateway/hub.go`），管理连接注册和消息路由。维护两张映射表：`conns`（所有活跃 WS 连接集合）和 `sessions`（sessionID → SessionWriter 集合的多对多映射）。

Hub 实现背压策略：`message.delta` 和 `raw` 类型事件在通道满时静默丢弃（非阻塞 select），`state`/`done`/`error` 等关键事件阻塞发送、永不丢弃。SeqGen（`sync.Map` + per-session `atomic.Int64`）为每个 session 分配严格递增的序列号。

---

## I

### Intent Router

Brain 模块的意图分发组件（`internal/brain/router.go`），将用户消息分类为四种意图：`chat`（闲聊，Brain 直接响应）、`command`（状态/配置查询，Brain 处理）、`task`（代码操作，转发到 Worker）和 `unknown`（默认转发到 Worker 以保安全）。

内置快速路径检测：问候语（"hi"、"hello"）→ chat，状态命令（"ping"、"status"）→ command，代码关键词（"function"、"debug"）→ task。LRU 缓存减少重复的 Brain API 调用。

---

## J

### Job Object

Windows 平台的进程隔离机制（`internal/worker/proc/`），等效于 POSIX 的 PGID。通过 Windows Job Object 确保 Worker 进程树在父进程退出时被完全清理。HotPlex 使用 `*_unix.go` / `*_windows.go` build tags 实现跨平台进程管理。

---

## M

### META-COGNITION

自动注入 B 通道的元认知文件（`internal/agentconfig/META-COGNITION.md`），通过 `go:embed` 嵌入，始终存在于 B 通道首位。定义 Worker 的身份边界（不管理 Transport/状态/协议）、B/C 通道冲突隔离法则、配置替换的"命中即终止"机制，以及配置修改 SOP（禁止改全局来影响 Bot）。

### Messaging Bridge

消息平台（Slack/飞书）与 Gateway 之间的桥接层（`internal/messaging/bridge.go`）。实现 3 步 session 生命周期：`StartPlatformSession`（创建 session + fork Worker）→ `JoinPlatformSession`（注册到 Hub 接收事件）→ `Handle`（处理 Worker 输出和交互请求）。

---

## N

### NDJSON (Newline-Delimited JSON)

每行一个 JSON 对象的序列化格式。HotPlex 的 AEP 协议使用 NDJSON 作为 WebSocket 帧的编码格式（`pkg/aep/codec.go`），Worker 的 stdin/stdout 也使用 NDJSON 通信（Claude Code 通过 `--print --session-id` 模式的 stdio 接口）。

---

## O

### OTEL (OpenTelemetry)

分布式追踪标准。HotPlex 通过 `internal/tracing/tracing.go` 集成 OpenTelemetry SDK，为每个 AEP 事件创建 Span（命名格式 `aep.{event_type}`），传播 trace context 到下游组件。支持尾部采样策略：ERROR trace 100% 保留，高延迟 trace 优先保留，正常 trace 1% 采样。

### OCS (OpenCode Server)

OpenCode Server Worker 适配器（`internal/worker/opencodeserver/`），通过单例 `opencode serve` 进程（HTTP+SSE）提供 Agent 能力。与 Claude Code 的 stdio 模式不同，OCS 使用全局 SingletonProcessManager 管理共享进程，通过引用计数和空闲排空（默认 30 分钟无引用 → 杀进程）优化资源使用。

---

## P

### pcEntry

PlatformConn 包装器（`internal/gateway/platform_writer.go`），将消息平台的 PlatformConn 适配为 Hub 的 SessionWriter 接口。内置异步 writeLoop goroutine 和 Delta Coalescing：写请求进入缓冲 channel，writeLoop 从 channel 读取，合并连续的可丢弃事件后批量发送。

pcEntry 解耦了 Hub.Run() 的主循环与消息平台 HTTP API 调用的阻塞延迟，防止平台 API 响应慢时影响 WebSocket 广播性能。

### PGID (Process Group ID)

进程组标识符，用于 POSIX 系统（Linux/macOS）的进程隔离和清理。HotPlex 在 fork Worker 进程时设置 `Setpgid: true` 创建独立进程组，终止时通过 `syscall.Kill(-pid, signal)` 向整个进程组发送信号，确保子进程也被清理。

分层终止流程：SIGTERM → 等待 5s → SIGKILL，在 `internal/worker/proc/` 中实现。

### Platform Bridge

消息平台 ↔ Gateway 的桥接层，参见 [Messaging Bridge](#messaging-bridge)。

### PlatformConn

消息平台连接的写端抽象接口（`internal/messaging/platform_conn.go`），定义两个方法：

- `WriteCtx(ctx, env)` — 将 AEP Envelope 写入平台（如飞书 API、Slack Web API）
- `Close()` — 永久关闭连接及其关联 goroutine

PlatformConn 是 Hub.JoinPlatformSession 所需的最小接口，使消息平台适配器无需包装为 WebSocket 连接即可注册到 Hub。

### PoolManager

Session 配额管理器（`internal/session/pool.go`），维护两级配额：全局最大活跃 Worker 数（默认 20）和每用户最大空闲 Session 数（默认 5）。通过 `Acquire`/`Release` 原子计数器管理配额，超限时返回 `ErrPoolExhausted` 或 `ErrUserQuotaExceeded`。

---

## R

### ReadPump / WritePump

WebSocket 连接的读写泵（`internal/gateway/conn.go`）。ReadPump 在独立 goroutine 中循环读取 WS 帧，解析为 AEP Envelope 后分发到 Handler；WritePump 将 Hub 广播的消息写入 WS 连接。两者协同实现全双工通信，通过 ping/pong 心跳机制检测连接存活（默认 60s 超时）。

---

## S

### Safety Guard

Brain 模块的安全审计组件（`internal/brain/guard.go`），提供三层防护：

- **输入验证**：正则模式匹配（快速路径）+ Brain AI 威胁分类（深度分析），检测 prompt 注入、越狱等攻击
- **输出清洗**：模式匹配 + 脱敏处理，移除 API key、凭证、内部 IP 等敏感信息
- **Chat2Config**：自然语言配置变更（默认禁用，存在安全风险）

检测结果分为三种动作：allow（安全）、block（威胁）、sanitize（脱敏后放行）。

### Seq (Sequence Number)

AEP 消息序列号。每个 session 内从 1 开始严格递增，由 Hub 的 SeqGen（`sync.Map` + per-session `atomic.Int64`）原子分配。`ping`/`pong` 不消耗 seq（字段为 0）。

Seq 保证同一 session 内消息的有序性，用于客户端检测丢包和重排序。被背压丢弃的 `message.delta` 不消耗 seq。

### Session

HotPlex 的核心生命周期单元，代表一次完整的对话。Session 独立于底层连接（WebSocket/Slack/飞书），通过 5 状态机管理生命周期：`CREATED → RUNNING → IDLE → TERMINATED → DELETED`。

Session ID 通过 UUIDv5 确定性生成（`internal/session/key.go`），相同的输入参数（ownerID + workerType + clientKey + workDir）始终产生相同的 ID，确保对话恢复的一致性。

### Socket Mode

Slack 的 WebSocket 连接模式。HotPlex 的 Slack 适配器（`internal/messaging/slack/`）使用 Socket Mode 建立到 Slack 的持久 WS 连接，无需公网入口（ingress）即可接收 Slack 事件。支持流式消息、交互管理和 STT 语音转写。

### STT (Speech-to-Text)

语音转文本功能（`internal/messaging/stt/`）。`PersistentSTT` 组件将飞书语音消息转写为文本后送入 Worker 处理。支持多策略转写（云端 API + 本地工具），跨平台进程隔离（POSIX PGID / Windows Job Object）。

### State Machine

Session 的 5 状态机（`internal/session/manager.go`）：

| 状态 | IsActive | 语义 |
|------|----------|------|
| `CREATED` | true | Session 已创建，瞬态（<1s） |
| `RUNNING` | true | Worker 正在执行 |
| `IDLE` | true | Worker 暂停，等待新输入或重连 |
| `TERMINATED` | false | Worker 已终止，保留历史记录 |
| `DELETED` | false | 终态，DB 记录已删除 |

状态转换规则严格定义，非法转换返回 `ErrInvalidTransition`。`TransitionWithInput` 在同一 mutex 内完成状态转换 + input 记录，防止 done/input 竞态。

---

## T

### Token Scopes

Admin API 的权限粒度控制。每个 API Token 可配置一组 scope（如 `session:read`、`session:write`、`config:read`、`config:write`），在 `config.yaml` 的 `admin.token_scopes` 字段中以 `token → scopes` 映射定义。未配置 scope 的 Token 使用 `default_scopes` 兜底。

### TTS (Text-to-Speech)

文本转语音功能（`internal/messaging/tts/`）。使用 Edge-TTS（微软语音合成引擎）+ FFmpeg Opus 转换。支持 fallback 机制（`FallbackSynthesizer`）：primary 失败时自动切换到 backup 合成器。配置项包括 provider（`edge+moss`）、voice（如 `zh-CN-XiaoxiaoNeural`）和 maxChars（单次合成字符上限）。

### Turn

一个请求-响应周期：用户输入（`input`）→ AI 处理（`state: running` → `message.delta` 流式输出 → `message.end`）→ 完成（`done`）。Turn 是 HotPlex 计费、配额和超时的基本计量单位。Session 忙碌时（RUNNING 状态）新 input 被硬拒绝（`SESSION_BUSY`），不排队。

---

## U

### UUIDv5

确定性 session ID 生成算法（`internal/session/key.go`），基于 SHA-1 哈希（RFC 4122 §4.3）。核心函数 `DeriveSessionKey(ownerID, workerType, clientKey, workDir)` 保证相同的输入参数始终产生相同的 session ID，支持对话恢复。

HotPlex 使用固定命名空间 UUID（`6ba7b810-9dad-11d1-80b4-00c04fd430c8`）作为 UUIDv5 的 namespace。Cron 系统使用独立的 `CronNamespace`（从主 namespace 派生）确保定时任务 session 与普通 session 永不冲突。

---

## W

### WAL (Write-Ahead Logging)

SQLite 的预写日志模式。HotPlex 强制启用 `PRAGMA journal_mode=WAL` + `PRAGMA busy_timeout=5000`，写入通过单写 goroutine 串行化。WAL 模式允许读写并发（读不阻塞写、写不阻塞读），提升 session 持久化（`internal/session/store.go`）和 cron 存储（`internal/cron/store.go`）的吞吐量。

### Worker

AI Coding Agent 适配器，将不同的 Agent 运行时统一接入 HotPlex。Worker 通过 `init()` + `worker.Register()` 模式注册，当前实现：

- **Claude Code**（`internal/worker/claudecode/`）— stdio 模式，通过 `--print --session-id` 交互
- **OpenCode Server**（`internal/worker/opencodeserver/`）— 单例进程，HTTP+SSE 交互

所有 Worker 嵌入 `base.BaseWorker`，共享 Terminate/Kill/Wait/Health/LastIO/ResetContext 生命周期。Worker 接口定义：`Start(ctx, env)`、`Input(ctx, content)`、`Terminate()`、`Kill()`、`Wait()`、`Health()` 等。
