# 项目知识库

**最后更新:** 2026-04-30 · **分支:** feat/62-webchat-persistence-enhancement

## 概览

HotPlex Gateway — Go 1.26 构建的 AI Coding Agent 统一接入层。
基于 WebSocket (AEP v1) 网关，抹平 Claude Code、OpenCode Server、Pi-mono 协议差异。
多语言客户端 SDK (TS/Python/Java/Go) + AI SDK transport 适配器 + Web Chat UI + 双向消息 (Slack/飞书)。

## 环境

**首次安装**:
```bash
cp configs/env.example .env
# 编辑 .env 填入 API 密钥
```

**开发** (`make dev`):
- Gateway → localhost:8888（默认仅监听本地，安全基线）
- Webchat → http://localhost:3000
- Admin API → localhost:9999

**日志**: `./logs/` · **PID 文件**: `~/.hotplex/.pids/`

## 结构

### 入口文件
```
cmd/hotplex/main.go          (~54 行)  cobra CLI 根命令: gateway, doctor, security, onboard, version
cmd/hotplex/gateway_run.go   (~498 行) 网关运行: DI 容器、信号处理、hub/session/bridge 初始化、HTTP 路由
cmd/hotplex/serve.go         (~182 行) gateway 子命令: start/stop/restart，支持 -d 守护进程模式
cmd/hotplex/routes.go        (~197 行) HTTP 路由注册: gateway WS、admin API、health、metrics
cmd/hotplex/messaging_init.go (~233 行) 消息适配器生命周期: 初始化 Slack/飞书、STT 设置
cmd/hotplex/doctor.go        (~150 行) doctor 子命令: 通过 Checker 注册表执行诊断检查
cmd/hotplex/security.go      (~182 行) security 子命令: 路径/环境变量安全审计
cmd/hotplex/onboard.go       (~105 行) onboard 子命令: 交互式配置向导
cmd/hotplex/config_cmd.go    (~61 行)  config 子命令: validate/dump/show
cmd/hotplex/status.go        (~95 行)  status 子命令: 网关进程状态查询
cmd/hotplex/banner.go        (~167 行) 启动 banner 渲染 (ASCII art + 配置摘要)
cmd/hotplex/dev.go           (~29 行)  dev 子命令: 启动 gateway + webchat
cmd/hotplex/pid.go           (~72 行)  PID 文件辅助: 网关进程管理
cmd/hotplex/version.go       (~46 行)  version 子命令
cmd/hotplex/service.go       (~28 行)  service 子命令根: install/uninstall/start/stop/restart/status/logs
cmd/hotplex/service_install.go  (~79 行) 安装为系统服务: systemd/launchd/SCM，支持 user/system 级别
cmd/hotplex/service_uninstall.go (~38 行) 卸载系统服务
cmd/hotplex/service_start.go  (~82 行) start/stop/restart: 委托给 Manager
cmd/hotplex/service_status.go (~61 行) status: 已安装/运行中/PID/单元路径
cmd/hotplex/service_logs.go   (~38 行) logs: journalctl/tail/Get-Content
cmd/hotplex/gateway_service_windows.go (~60 行) Windows SCM 集成 (build tag)
cmd/hotplex/gateway_service_other.go  (~15 行) 非 Windows 空操作桩 (build tag)
```

### internal/

**核心**
- `admin/`      Admin API: handlers、middleware、rate-limit、log buffer
- `aep/`        AEP v1 编解码: JSON envelope 编码/解码/校验
- `config/`     Viper 配置 + 文件监控 + 热重载
- `agentconfig/` Agent 人格/上下文加载器: B 通道 (SOUL/AGENTS/SKILLS 在 `<directives>`) + C 通道 (`META-COGNITION.md`/USER/MEMORY 在 `<context>`); `META-COGNITION.md` (5 节自模型, go:embed 初始化)

**Gateway** (WebSocket)
- `gateway/hub.go`     WS 广播 hub: 连接注册、会话路由、序列号生成
- `gateway/conn.go`    单个 WS 连接: 读/写泵、心跳
- `gateway/handler.go`  AEP 事件分发 (input、ping、control、worker 命令、skills 列表) + 透传反馈
- `gateway/bridge.go`  Session ↔ Worker 生命周期编排 + LLM 重试 + Agent 配置注入
- `gateway/llm_retry.go`  LLMRetryController: 可重试错误的指数退避
- `gateway/api.go`     GatewayAPI: HTTP session 端点 (list/get/terminate/create，含幂等性)
- `gateway/init.go`    Init 握手: InitData、InitAckData、caps、30s 超时
- `gateway/heartbeat.go` 丢包计数器 (带 stop channel)
- `gateway/session_stats.go` 会话统计追踪

**Session**
- `session/manager.go`   5 状态机、状态迁移、GC、物理删除
- `session/store.go`     SQLite 持久化 (Upsert、Get、List、expired、DeletePhysical)
- `session/message_store.go`  事件日志，单写入 goroutine
- `session/key.go`       DeriveSessionKey (UUIDv5) + PlatformContext 用于确定性 session ID
- `session/pool.go`      PoolManager: 全局 + 每用户配额 + 每用户内存追踪
- `session/pgstore.go`   Postgres 桩 (ErrNotImplemented)
- `session/sql/`         外部化 .sql 文件 (schema、migrations、queries)
- `session/queries.go`  embed.FS 加载器 + stripComments
- `session/stores.go`   多存储注册表 (SQLite/Postgres)

**Messaging** (Slack/飞书双向)
- `messaging/bridge.go`   SessionStarter + ConnFactory + 去重 (3 步: StartSession → Join → Handle)
- `messaging/platform_conn.go`  PlatformConn 接口: WriteCtx + Close
- `messaging/platform_adapter.go`  基础适配器 + 自注册 (Register/New/RegisteredTypes)
- `messaging/interaction.go`  InteractionManager: 用户权限/Q&A/elicitation，带超时 + 自动拒绝
- `messaging/control_command.go`  斜杠命令 + 自然语言控制触发器 ($ 前缀)
- `messaging/sanitize.go`  文本清洗: 控制字符、null 字节、BOM、代理对
- `messaging/slack/`      Socket Mode: 流式 writer、分块器、去重、校验、交互、退避、斜杠命令、图片、状态
- `messaging/feishu/`     ws.Client: P2 事件、转换器、流式、打字提示、交互卡片、STT (语音转文字)
- `scripts/stt_server.py`  持久化 STT 子进程 (SenseVoice-Small ONNX)
- `scripts/fix_onnx_model.py`  ONNX 模型 Less 节点类型不匹配自动修补
- `messaging/mock/`       Mock 适配器 (测试用)

**Worker** (3 个运行时适配器 + 1 个空操作)
- `worker/claudecode/`    Claude Code 适配器
- `worker/opencodeserver/`  OpenCode Server 适配器 (通过 `SingletonProcessManager` 单例进程)
- `worker/pi/`            Pi-mono 适配器
- `worker/noop/`          No-op 适配器 (测试用)
- `worker/acpx/`          ACPX 仅类型常量 (无实现)
- `worker/base/`          共享 BaseWorker + Conn + BuildEnv
- `worker/proc/`          进程生命周期: PGID 隔离、分层 SIGTERM→SIGKILL、PID 文件孤儿清理

**CLI** (自服务命令 — 见 `internal/cli/AGENTS.md`)
- `cli/checker.go`       Checker 接口 + CheckerRegistry 诊断注册表
- `cli/checkers/`        7 个检查器: config、dependencies、environment、messaging、runtime、security、stt
- `cli/onboard/`         交互式向导 + Slack/飞书 YAML 模板
- `cli/output/`          终端输出: printer (颜色/格式)、report (结构化诊断结果)

**支撑**
- `security/`   JWT (ES256)、SSRF、命令白名单、环境隔离、路径安全
- `skills/`     Skills 发现: locator + scanner 扫描项目/用户/插件 skill 目录
- `metrics/`    Prometheus 计数器/仪表盘/直方图
- `tracing/`    OpenTelemetry 设置 (幂等)
- `eventstore/` SQLite 事件存储: session 事件持久化和回放
- `service/`    系统服务管理: install/uninstall/start/stop/restart (systemd/launchd/SCM)

### pkg/
- `events/`   Envelope、Event、SessionState、所有数据结构
- `events/helpers.go`  共享 mapper 辅助函数
- `aep/`      AEP v1 编解码

### 顶层目录
```
client/    Go 客户端 SDK (独立模块，强类型事件)
webchat/   Next.js Web Chat UI + AI SDK transport
examples/  TS / Python / Java 客户端 SDK
docs/      架构、规范、安全、测试、运维
scripts/   构建/校验脚本
configs/   config.yaml、config-dev.yaml、env.example
```

## 定位指南

**新增组件**
- 新 AEP 事件类型 → `pkg/events/events.go` — 添加 Kind 常量 + Data 结构体 + Validate
- 新 Worker 适配器 → `internal/worker/<name>/` — 嵌入 `base.BaseWorker`，实现 `Start`/`Input`/`Resume`，在 `init()` 中注册
- 新消息适配器 → `internal/messaging/<name>/` — 嵌入 `PlatformAdapter`，实现 `Start`/`HandleTextMessage`/`Close`
- 新增诊断检查 → `internal/cli/checkers/` — 实现 `Checker` 接口，在 `DefaultRegistry` 注册
- 新 cobra 子命令 → `cmd/hotplex/<name>.go` — 在 `main.go` 根命令注册
- 新 admin 端点 → `internal/admin/handlers.go` — 遵循 `Handle*` 模式，检查 scopes
- 新 skill 发现源 → `internal/skills/scanner.go` — 扩展扫描函数

**CLI 自服务** (见 `internal/cli/AGENTS.md` 和 `cmd/hotplex/AGENTS.md`)
- 修改 onboard 向导 → `internal/cli/onboard/wizard.go` — 交互式提示和模板
- CLI 输出格式化 → `internal/cli/output/` — printer (颜色/状态) 和 report (结构化输出)
- 网关启动/DI → `cmd/hotplex/gateway_run.go` — DI 容器、信号处理、hub/session/bridge 设置、OCS 单例初始化
- 消息适配器接线 → `cmd/hotplex/messaging_init.go` — 初始化 Slack/飞书、STT 设置
- 路由注册 → `cmd/hotplex/routes.go` — gateway WS、admin API、health、metrics 的 HTTP 路由

**修改已有组件**
- Agent 配置文件 → `internal/agentconfig/loader.go` — 文件加载、大小限制、frontmatter 剥离; `prompt.go` 统一系统提示词组装 (嵌套 XML: `<directives>` + `<context>` 分组)
- Agent 配置目录 → `~/.hotplex/agent-configs/` — 放置 SOUL.md、AGENTS.md、SKILLS.md (B 通道) + USER.md、MEMORY.md (C 通道); 平台变体如 SOUL.slack.md
- 元认知核心 → `internal/agentconfig/META-COGNITION.md` — 5 节自模型: 身份、系统架构、session 生命周期、agent config 架构、控制命令; 作为 Agent 的"大脑"用于自我引用
- Session 生命周期 → `internal/session/manager.go` — 状态机 + `TransitionWithInput` 原子性 + `DeletePhysical` 强制删除
- Session key 派生 → `internal/session/key.go` — UUIDv5 确定性 session ID + 平台上下文
- WebSocket 协议 → `internal/gateway/conn.go` — ReadPump/WritePump + Handler 分发
- LLM 自动重试 → `internal/gateway/llm_retry.go` — 可重试错误检测 + 指数退避
- Gateway HTTP API → `internal/gateway/api.go` — session list/get/terminate/create (CreateSession 支持幂等: 复用活跃 session，物理删除已删除 session)
- 配置结构 → `internal/config/config.go` — 结构体 + Default() + Validate() (含 `AgentConfig`)
- Agent 配置注入 → `internal/gateway/bridge.go` — `injectAgentConfig()` 在 Start/Resume/Fresh-start 时按 worker 类型加载并应用 B/C 通道
- STT 配置 → `internal/config/config.go` — FeishuConfig.STTProvider/STTLocalCmd/STTLocalMode/STTLocalIdleTTL + SlackConfig 对应字段
- 消息适配器接线 → `cmd/hotplex/serve.go` — `startMessagingAdapters()`: config → New → Configure → SetConnFactory → Start

**安全**
- 新增校验 → `internal/security/` — 每个关注点一个文件 (jwt、ssrf、path、env、tool、command)

**监控 & API**
- Prometheus 指标 → `internal/metrics/` — 遵循 `hotplex_<group>_<metric>_<unit>` 命名
- Admin 端点 → `internal/admin/` — handlers.go (stats/health/config)、sessions.go (CRUD)

## 代码地图

**入口**
- `main` → `cmd/hotplex/main.go` — cobra CLI 根 (gateway、doctor、security、onboard、version)
- `GatewayDeps` → `cmd/hotplex/serve.go` — 网关 DI 容器、信号处理、消息初始化、LLM 重试初始化

**Gateway** (`internal/gateway/`)
- `Hub` → `hub.go:68` — WS 广播 hub、连接注册、会话路由、序列号生成
- `Conn` → `conn.go:35` — 单个 WS 连接、读/写泵、心跳
- `Handler` → `handler.go` — AEP 事件分发 (input、ping、control、/cd 切目录、worker 命令、skills 列表、透传反馈) + panic 恢复
- `Bridge` → `bridge.go` — session ↔ worker 生命周期、StartPlatformSession、fresh start 兜底、InputRecoverer、LLM 重试集成、Agent 配置注入、SwitchWorkDir
- `LLMRetryController` → `llm_retry.go` — 可重试错误模式检测、per-session 尝试计数、指数退避
- `GatewayAPI` → `api.go` — HTTP session 端点: ListSessions、GetSession、TerminateSession、CreateSession (幂等，含 DeletePhysical 兜底)
- `pcEntry` → `hub.go` — 包装 PlatformConn 用于 sessions map

**Session** (`internal/session/`)
- `Manager` → `manager.go:34` — 5 状态机、迁移、GC、worker 附着/分离、`DeletePhysical` 绕过状态机强制删除
- `managedSession` → `manager.go:54` — per-session 状态 + mutex + worker 引用
- `DeriveSessionKey` → `key.go` — 从 (ownerID、workerType、clientSessionID、workDir) 派生 UUIDv5 确定性 session ID
- `PlatformContext` → `key.go` — 平台特定字段用于 DerivePlatformSessionKey (Slack channel/thread、飞书 chat)
- `PoolManager` → `pool.go` — 全局 + 每用户配额，每用户内存追踪 (每个 worker 估算 512MB)
- `Store` (接口) → `store.go:22` — SQLite: Upsert、Get、List、expired 查询、DeletePhysical
- `MessageStore` (接口) → `message_store.go` — 事件日志，单写入 goroutine

**Worker** (`internal/worker/`)
- `Worker` (接口) → `worker.go:84` — Start/Input/Resume/Terminate/Kill/Wait/Conn/Health
- `SessionConn` (接口) → `worker.go:19` — 双向通道: Send/Recv/Close
- `Capabilities` (接口) → `worker.go:40` — 特性查询: resume、streaming、tools、env
- `InputRecoverer` (接口) → `worker.go:141` — LastInput() 用于崩溃恢复输入重投递
- `base.BaseWorker` → `base/worker.go` — 共享生命周期: Terminate/Kill/Wait/Health/LastIO
- `base.Conn` → `base/conn.go` — stdin SessionConn: NDJSON over stdio，导出 `WriteAll`，实现 `InputRecoverer`
- `base.BuildEnv` → `base/env.go` — 环境变量构建: 白名单 + session 变量
- `proc.Manager` → `proc/manager.go:26` — PGID 隔离，分层 SIGTERM→SIGKILL
- `proc.Tracker` → `proc/pidfile.go` — PID 文件孤儿清理: Write/Remove/RemoveAll/CleanupOrphans、globalTracker、PID 回收防御

**OpenCode Server** (`internal/worker/opencodeserver/`)
- `SingletonProcessManager` → `singleton.go` — 懒启动的共享 `opencode serve` 进程，引用计数，30m 空闲回收，崩溃检测
- `Worker` → `worker.go` — 轻量 session 适配器; Start/Resume 获取单例引用，Terminate/Kill 仅释放引用 + 关闭 SSE (不杀进程)
- `InitSingleton` / `ShutdownSingleton` → `singleton.go` — 全局单例的网关生命周期钩子

**Messaging** (`internal/messaging/`)
- `Bridge` → `bridge.go` — 3 步: StartSession → Join → handler.Handle
- `PlatformConn` (接口) → `platform_conn.go` — WriteCtx + Close
- `PlatformAdapter` → `platform_adapter.go` — 基础: SetHub/SetSM/SetHandler/SetBridge
- `InteractionManager` → `interaction.go` — PendingInteraction 注册表，带超时 + 自动拒绝 (默认 5 分钟)
- `ParseControlCommand` → `control_command.go` — 斜杠命令 (/gc、/reset、/park、/new、/cd) + $ 前缀自然语言
- `SanitizeText` → `sanitize.go` — 移除控制字符、null 字节、BOM、代理对
- `FeishuSTT` → `feishu/stt.go` — 飞书 speech_to_text API 云端转写
- `LocalSTT` → `stt/stt.go` — 临时外部命令转写 (每次请求新建进程)
- `PersistentSTT` → `stt/stt.go` — 长驻子进程，JSON-over-stdio，PGID 隔离
- `FallbackSTT` → `stt/stt.go` — 主 + 备降级链
- `Transcriber` (接口) → `stt/stt.go` — Transcribe(ctx, audioData) → (text, error)，飞书和 Slack 共用
- `PlatformAdapterInterface` → `platform_adapter.go:21` — Platform/Start/HandleTextMessage/Close
- 适配器注册 → `platform_adapter.go:47` — `Register(t PlatformType, b Builder)`，main.go 中 blank import

**Agent Config** (`internal/agentconfig/`)
- `AgentConfigs` → `loader.go` — 承载已加载内容: Soul/Agents/Skills (B 通道) + User/Memory (C 通道)
- `Load` → `loader.go` — 读取配置目录，追加平台变体 (如 SOUL.slack.md)，剥离 YAML frontmatter，限制大小 (8K/文件, 40K 总计)
- `BuildSystemPrompt` → `prompt.go` — 组装统一的 B+C 系统提示词，嵌套 XML 标签 (`<directives>/<context>`)，CC 和 OCS 共用; 通过 go:embed 在 init 时从 `META-COGNITION.md` 计算

**核心**
- `Envelope` → `pkg/events/events.go:73` — AEP v1 信封 (id、version、seq、session_id、event)
- `SessionState` → `pkg/events/events.go:240` — Created/Running/Idle/Terminated/Deleted
- `Config` → `config/config.go:118` — 所有配置结构体
- `JWTValidator` → `security/jwt.go:27` — ES256 + JTI 黑名单
- `client.Client` → `client/client.go:33` — Go SDK: Connect/Resume/SendInput/SendPermissionResponse/SendControl/Close
- `client.Event` → `client/events.go` — 强类型事件常量 + data helpers (AsDoneData、AsErrorData、AsToolCallData)
- `client.Option` → `client/options.go` — 函数式选项 (AutoReconnect、ClientSessionID、Metadata、Logger)
- `admin.AdminAPI` → `admin/admin.go` — stats、health、config、session CRUD

## 约定

- **Mutex**: 显式 `mu` 字段，零值，不嵌入，不传指针
- **错误**: 哨兵变量用 `Err` 前缀，自定义类型用 `Error` 后缀，包装用 `fmt.Errorf("%w", ...)`
- **日志**: `log/slog` JSON handler，key-value 对，`service.name=hotplex-gateway`
- **测试**: `testify/require` (不用 `t.Fatal`)，table-driven，`t.Parallel()`，`t.Cleanup()`
- **配置**: Viper YAML + 环境变量展开，`SecretsProvider` 接口管理密钥
- **Worker 注册**: `init()` + `worker.Register(WorkerType, Builder)` 模式，通过 blank import
- **消息适配器 import**: `cmd/hotplex/messaging_init.go` 必须导入所有适配器包 — 飞书 (直接导入，用于 `NewFeishuSTT`) + slack (blank import `_`，用于 `init()` 注册)。重构时移除直接类型引用必须保留 blank `_` 导入; 编译器不会警告静默注册丢失
- **STT 引擎**: SenseVoice-Small via `funasr-onnx` (ONNX FP32，非量化)，首次加载自动修补 ONNX 模型，持久化子进程零冷启动
- **DI**: 手动构造函数注入 (不用 wire/dig)，`GatewayDeps` 结构体在 serve.go
- **关闭顺序**: signal → cancel ctx → tracing → hub → configWatcher → `sm.TerminateAllWorkers()` → bridge.Shutdown → sessionMgr → HTTP server
- **Panic 恢复**: Gateway handler + bridge forwardEvents 必须 recover panic，记录日志，返回 `handler panic` / `bridge panic`
- **控制命令**: 自然语言触发需 `$` 前缀 (如 `$gc`、`$休眠`) 防止误匹配; 斜杠命令 (`/gc`、`/reset`、`/park`、`/new`、`/cd <path>`) 无前缀
- **文本清洗**: 所有面向用户的文本输出经过 `SanitizeText()` 后再投递到消息平台
- **交互超时**: 权限/Q&A/elicitation 请求 5 分钟后自动拒绝，防止无限阻塞
- **Session key 派生**: UUIDv5 从 (ownerID、workerType、clientSessionID、workDir) 确定性映射，跨环境一致
- **LLM 自动重试**: 可配置的可重试错误模式 (429、5xx、网络错误) 带指数退避; per-session 尝试计数
- **Agent 配置注入**: `agentconfig` 包从 `~/.hotplex/agent-configs/` 加载人格/上下文; B 通道 (SOUL.md、AGENTS.md、SKILLS.md) 在 `<directives>` XML 组; C 通道 (USER.md、MEMORY.md) 在 `<context>` XML 组; 平台变体 (如 SOUL.slack.md) 自动追加; 大小限制: 8K/文件, 40K 总计
- **Session 物理删除**: `DeletePhysical` 绕过状态机强制删除 — 用于 GatewayAPI 在前一个 session 处于 `deleted` 状态时幂等创建新 session
- **文档**: 增量文档中文优先，重要文档中英双语。技术术语保留英文原文
- **文件安全 (多 Agent)**: 当前环境存在多 Agent 协同工作，对文件执行还原（`git restore`）、恢复、撤销（`git checkout`）、暂存（`git stash`）等操作前，**必须先在 `/tmp` 下创建备份**（`cp <file> /tmp/<file>.bak.$(date +%s)`），防止其他 Agent 的未提交改动被意外覆盖或丢失

## 反模式 (本项目)

- ❌ `sync.Mutex` 嵌入或传指针 — 始终使用显式 `mu` 字段
- ❌ 单个 `db.Exec()` 中执行多条 SQL — SQLite 驱动静默忽略除第一条外的所有语句; 拆分为单独的 `db.Exec()` 调用
- ❌ `math/rand` 用于加密 (JTI、tokens) — 使用 `crypto/rand`
- ❌ Shell 执行 — 仅允许 `claude` 二进制，禁止 shell 解释器
- ❌ 非 ES256 JWT 算法
- ❌ 缺少 goroutine 关闭路径 — 每个 goroutine 需要 ctx cancel / channel close / WaitGroup
- ❌ 测试中使用 `t.Fatal` — 使用 `testify/require`
- ❌ 跳过 SQLite WAL 模式
- ❌ 跨 Bot 访问 session
- ❌ 无 mutex 下同时处理 `done` + `input` — 在 `TransitionWithInput` 中必须原子操作
- ❌ 注册无实现的 ACPX worker 类型 — 目录为空，仅存在类型常量

## 独特风格

- **锁顺序**: `m.mu` (Manager) → `ms.mu` (per-session) — 始终按此顺序防止死锁
- **背压**: 广播通道满时静默丢弃 `message.delta` 和 `raw` 事件; `state`/`done`/`error` 永不丢弃
- **Seq 分配**: Per-session 原子单调计数器; 被丢弃的 delta 不消耗 seq
- **进程终止**: 3 层: SIGTERM → 等待 5s → SIGKILL，PGID 隔离清理子进程
- **Worker 类型常量**: `TypeClaudeCode`、`TypeOpenCodeSrv`、`TypeACPX`、`TypePimon`
- **BaseWorker 嵌入**: 适配器嵌入 `*base.BaseWorker` 获取共享生命周期; 每个适配器仅实现 `Start`、`Input`、`Resume` + 独特的 I/O 解析
- **Admin API 独立包**: `internal/admin/` 使用 SessionManager/Hub/Bridge 接口避免循环依赖; main.go 中桥接具体类型
- **Gateway 拆分**: conn.go (WebSocket 生命周期)、handler.go (AEP 分发)、bridge.go (session 编排)、llm_retry.go (自动重试)、api.go (HTTP 端点) — 同包，关注点分离
- **配置热重载**: 文件监控器带回滚能力，更新活跃配置引用
- **单写入 SQLite**: 基于通道的写入序列化，批量刷盘 (50 条 / 100ms)
- **InputRecoverer**: Worker 通过 base.Conn 实现 `LastInput() string`; bridge 从已死 worker 提取最后输入用于崩溃恢复重投递
- **Fresh start 兜底**: resume 重试失败后，bridge 创建新 worker 并重投最后输入 — 对话历史丢失但用户能得到响应
- **飞书流式卡片 4 层防御**: TTL 守卫 → 完整性检查 → 带退避重试 → IM Patch 兜底 (CardKit 降级时)
- **Slack 消息管道**: chunker (分片长消息) → dedup (TTL 去重) → format (markdown 转换) → rate limiter → send
- **Slack 流式**: SlackStreamingWriter，150ms 刷新间隔，20 字符阈值，最多 3 次追加重试，10min TTL
- **Slack tool status**: `toolStatusFormatters` 注册表将工具名映射到专用显示格式器 (TodoWrite → `📋 Fixing auth bug`、Read → `📖 Reading main.go` 等); 未注册工具回退到 `Name(key=val)` 通用格式; `LogOnceUnregistered` 每个名称仅记录一次 DEBUG 日志; 见 `internal/messaging/slack/AGENTS.md`
- **LLM 自动重试**: LLMRetryController 通过正则模式 (429/5xx/网络错误) 检测可重试错误，指数退避 (初始 2s，最大 60s)，per-session 尝试计数
- **确定性 session ID**: DeriveSessionKey 使用 UUIDv5 (SHA-1 namespace+name) 跨环境一致; PlatformContext 用于平台特定 key 派生
- **每用户内存追踪**: PoolManager 追踪每用户估算内存 (512MB/worker) 和 session 计数配额
- **Agent 配置统一提示词**: B+C 通道合并到单个 `BuildSystemPrompt`，嵌套 XML 标签 (`<agent-configuration>` → `<directives>` + `<context>`); B 通道 (SOUL.md/AGENTS.md/SKILLS.md) 在 `<directives>` = 高优先级，不含糊; C 通道 (`META-COGNITION.md` 在 `<hotplex>` + USER.md/MEMORY.md) 在 `<context>` = 背景引用; `META-COGNITION.md` 通过 go:embed 在 init 时计算; CC (`--append-system-prompt`) 和 OCS (`system` 字段) 注入方式相同
- **Webchat session 粘性**: 通过 DeriveSessionKey 确定性"主" session ID + localStorage 持久化跨页面刷新保持活跃 session; 无 session 时自动创建
- **OCS 单例进程**: 所有 OpenCode Server session 共享一个懒启动的 `opencode serve` 进程，由 `SingletonProcessManager` 管理; 引用计数，30m 空闲回收; Worker 是轻量适配器，获取/释放引用; Terminate/Kill 仅关闭 SSE 连接，永不杀共享进程; 通过 `monitorProcess` goroutine 检测崩溃，每个生命周期创建新 `crashCh`
- **切换工作目录**: `/cd <path>` (WebSocket 控制) 或 `POST /api/sessions/{id}/cd` (REST) 终止旧 worker，通过 PlatformContext 用新 workDir 派生新 session ID，在同一单例进程上启动新 session; 安全校验通过 `config.ExpandAndAbs` + `security.ValidateWorkDir`
- **透传命令反馈**: `handlePassthroughCommand` 在 WorkerCommander 操作 (compact/clear/rewind) 成功后发送可见的 `message` AEP 事件; 不支持的命令 (/effort、/commit) 返回明确的 `NOT_SUPPORTED`; OCS `Compact` 无 `pendingModel` 时自动从消息历史解析模型; OCS `Rewind` 无 targetID 时自动解析最后 assistant messageID
- **快速重连状态守卫**: `conn.go` 在 WebSocket 重连时若 session 已处于 `running` 状态则跳过 `Transition(running)` — 避免无效的 `running→running` 迁移错误
- **元认知核心**: `internal/agentconfig/META-COGNITION.md` — 5 节 agent 自模型: 身份 (AEP v1, Gateway 托管)、系统架构、session 生命周期、agent config 架构 (B/C 通道)、控制命令; 作为 C 通道 (`<hotplex>`) 注入到 `<context>`，充当 agent 的"大脑"

## 命令

所有构建/测试/lint 操作必须使用 `make` 目标。不要直接使用 `go build` / `go test` / `golangci-lint`。

```bash
# 构建 & 质量检查
make build                    # 构建网关二进制 (输出: bin/hotplex-<os>-<arch>)
make test                     # 运行测试 (含 -race，超时 15m)
make test-short               # 快速测试 (-short)
make lint                     # golangci-lint
make coverage                 # 覆盖率报告
make check                    # 完整 CI: quality + build
make quality                  # fmt + lint + test (不构建)
make fmt                      # 格式化 (gofmt + goimports)
make clean                    # 清理构建产物

# 开发
make quickstart               # 首次安装 (check-tools + build + test-short)
make run                      # 构建并运行网关
make dev                      # 启动开发环境 (gateway + webchat)
make dev-stop                 # 停止所有开发服务
make dev-status               # 查看运行服务
make dev-logs                 # 查看网关日志
make dev-reset                # 停止并重启所有服务

# 网关管理 (Make)
make gateway-start            # 构建并启动网关
make gateway-stop             # 停止网关
make gateway-status           # 查看网关状态
make gateway-logs             # 查看网关日志

# 网关管理 (CLI)
hotplex gateway start         # 前台启动网关
hotplex gateway start -d      # 后台守护进程启动
hotplex gateway stop          # 停止网关
hotplex gateway restart       # 重启网关
hotplex gateway restart -d    # 后台守护进程重启

# 系统服务 (systemd/launchd/SCM)
hotplex service install       # 安装为用户级服务 (无需 root)
hotplex service install --level system  # 系统级安装 (需要 sudo)
hotplex service uninstall     # 卸载服务
hotplex service start         # 启动服务
hotplex service stop          # 停止服务
hotplex service restart       # 重启服务
hotplex service status        # 查看服务状态
hotplex service logs -f       # 实时查看服务日志

# Webchat
make webchat-dev              # 启动 webchat 开发服务器
make webchat-stop             # 停止 webchat 开发服务器
```

## 备注

- `configs/` 目录包含 config-dev.yaml / config-prod.yaml / config.yaml / env.example / grafana / monitoring
- `CLAUDE.md` 是 `AGENTS.md` 的符号链接 — 只编辑 AGENTS.md，CLAUDE.md 自动跟随
- `.claude` 是 `.agent` 的符号链接 — 两个目录都存在
- 无 `api/` 目录 — 项目使用 JSON over WebSocket，不是 protobuf
- 跨平台支持: POSIX (PGID 隔离 `Setpgid`) + Windows (Job Object `KILL_ON_JOB_CLOSE` 级联终止); 进程管理、安全、配置、服务管理均有平台适配文件 (`*_windows.go` / `*_unix.go`)
- 最大文件: `bridge.go` (1256)、`feishu/adapter.go` (1240)、`slack/adapter.go` (1165)、`manager.go` (961)、`config.go` (835)、`opencodeserver/worker.go` (824)、`hub.go` (818)
- STT 脚本 (`scripts/stt_server.py`、`scripts/fix_onnx_model.py`) 也部署到 `~/.agents/skills/audio-transcribe/scripts/` 供 Claude Code skill 使用
- STT 模型: `~/.cache/modelscope/hub/models/iic/SenseVoiceSmall` (~900MB)，ONNX FP32 非量化
- Zombie IO 超时默认: 30 分钟 (通过 `worker.execution_timeout` 配置); Worker 空闲超时默认: 60 分钟 (通过 `worker.idle_timeout` 配置)
- OpenCode CLI 适配器已移除 — 由 OpenCode Server 适配器替代 (单例进程模型)
- OCS 单例配置默认值: `idle_drain_period=30m`、`ready_timeout=10s`、`ready_poll_interval=200ms`、`http_timeout=30s` — 通过 `worker.opencode_server` 在 config.yaml 配置
- Onboard 向导在选择 `opencode_server` worker 类型时自动生成 OCS 单例配置
- ACPX 适配器仅存在类型常量 (`TypeACPX`) 但无实现 — `internal/worker/acpx/` 为空
- Postgres store 仅为桩 (`ErrNotImplemented`) — 仅 SQLite 可用于生产
- `internal/gateway/api.go` 提供与 WebSocket gateway 并行的 REST session 管理
- Agent 配置文件位于 `~/.hotplex/agent-configs/` (通过 `agent_config.config_dir` 配置): SOUL.md、AGENTS.md、SKILLS.md (B 通道)，USER.md、MEMORY.md (C 通道); 平台变体如 SOUL.slack.md 自动追加
- `DeletePhysical` 在 session.Manager 中绕过状态机强制删除 — 用于重新创建已软删除的 session
