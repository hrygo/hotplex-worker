# HotPlex 项目知识库

**最后更新**: 2026-05-09 · **分支**: main · **版本**: v1.9.1

---

## 目录

- [快速开始](#快速开始)
- [项目概览](#项目概览)
- [环境配置](#环境配置)
- [项目结构](#项目结构)
- [贡献工作流](#贡献工作流)
- [开发指南](#开发指南)
- [代码地图](#代码地图)
- [约定与规范](#约定与规范)
- [命令参考](#命令参考)
- [备注](#备注)

---

## 快速开始

**首次使用**：
```bash
# 1. 环境配置
cp configs/env.example .env
# 编辑 .env 填入 API 密钥

# 2. 快速安装
make quickstart  # check-tools + build + test-short

# 3. 启动开发环境
make dev  # gateway + webchat
```

**开发验证**：
```bash
make check   # 完整 CI: quality + build
make dev-status  # 查看运行服务
```

**常用命令**：
- `make build` - 构建网关二进制
- `make test` - 运行测试（含 -race）
- `make lint` - golangci-lint 检查
- `make dev` - 启动开发环境
- `hotplex service start` - 启动系统服务
- `hotplex update` - 自更新到最新版本

---

## 项目概览

HotPlex Gateway 是基于 Go 1.26 构建的 **AI Coding Agent 统一接入层**。

**核心特性**：
- 🌐 基于 WebSocket (AEP v1) 网关
- 🔌 抹平 Claude Code、OpenCode Server 协议差异
- 💬 双向消息支持（Slack/飞书）
- 🎨 Web Chat UI
- 📦 多语言客户端 SDK (TS/Python/Java/Go)

**架构亮点**：
- 5 状态机 Session 管理
- WebSocket Hub 广播
- Worker 生命周期编排
- Agent 配置热注入
- LLM 自动重试机制
- 跨平台支持（Linux/macOS/Windows）

---

## 环境配置

### 端口分配

| 服务 | 地址 | 说明 |
|------|------|------|
| Gateway | localhost:8888 | 默认仅监听本地（安全基线） |
| Webchat | http://localhost:3000 | Web UI |
| Admin API | localhost:9999 | 管理 API |

### 目录结构

- **日志**: `./logs/`
- **PID 文件**: `~/.hotplex/.pids/`
- **配置**: `configs/` (config.yaml、config-dev.yaml、env.example)

### 环境变量

首次使用需复制并编辑环境配置：
```bash
cp configs/env.example .env
# 编辑 .env 填入 API 密钥
```

---

## 项目结构

### 入口文件 (`cmd/hotplex/`)

| 文件 | 行数 | 功能 |
|------|------|------|
| `main.go` | 54 | cobra CLI 根命令 |
| `gateway_run.go` | 498 | 网关运行：DI 容器、信号处理、hub/session/bridge 初始化 |
| `serve.go` | 182 | gateway 子命令：start/stop/restart |
| `routes.go` | 197 | HTTP 路由注册 |
| `messaging_init.go` | 233 | 消息适配器生命周期 |
| `service_*.go` | - | 系统服务管理（systemd/launchd/SCM） |
| `update.go` | 168 | 自更新命令：GitHub API、下载、校验、替换 |

### 核心模块 (`internal/`)

**Gateway** (WebSocket)：
- `hub.go` - WS 广播 hub
- `conn.go` - 单个 WS 连接
- `handler.go` - AEP 事件分发
- `bridge.go` - Session ↔ Worker 生命周期编排
- `llm_retry.go` - LLM 自动重试
- `api.go` - HTTP session 端点

**Session**：
- `manager.go` - 5 状态机、状态迁移、GC
- `store.go` - SQLite 持久化
- `key.go` - UUIDv5 确定性 session ID
- `pool.go` - 全局 + 每用户配额

**Messaging** (Slack/飞书/TTS)：
- `bridge.go` - SessionStarter + ConnFactory
- `platform_adapter.go` - 基础适配器
- `control_command.go` - 斜杠命令解析
- `slack/` - Socket Mode 适配器
- `feishu/` - WS 适配器 + STT
- `tts/` - Edge-TTS 语音合成 + FFmpeg Opus 转换
- `interaction/` - Q&A 交互流转

**Brain** (LLM 编排层)：
- `brain.go` - 核心接口 (Brain/StreamingBrain/RoutableBrain/ObservableBrain) + 全局单例
- `init.go` - Init() 编排 + enhancedBrainWrapper 中间件链 (retry → cache → rate limit)
- `config.go` - 13 子配置 + 4 层 API key 发现
- `guard.go` - 输入/输出安全审计 (Safety Guard)、威胁检测、Chat2Config
- `router.go` - 意图分发 (Intent Router)、LRU 缓存、快速路径检测
- `memory.go` - 上下文压缩 + 用户偏好提取 + TTL 清理
- `extractor.go` - 从 Claude Code / OpenCode 配置文件提取凭证
- `llm/` - LLM 客户端子包：OpenAI/Anthropic 客户端 + 装饰器链 (retry/cache/ratelimit/circuit/metrics) + 模型路由 + 成本估算

**Worker**：
- `claudecode/` - Claude Code 适配器 (stdio, `--print --session-id`)
- `opencodeserver/` - Open Code Server 适配器（单例进程, HTTP+SSE）
- `proc/` - 跨平台进程生命周期管理 (PGID/Job Object)
- `base/` - 共享 BaseWorker + Conn + MetadataHandler

**支撑模块**：
- `config/` - Viper 配置 + 热重载 + 继承 + 审计/回滚。消息层共享默认值（WorkerType, STT, TTS）通过 `FillFrom()` 传播到平台配置。三级优先级：platform > messaging > Default()
- `agentconfig/` - B/C 双通道 Agent 人格/上下文加载器
- `security/` - JWT、SSRF、路径安全
- `skills/` - Skills 发现
- `metrics/` - Prometheus 指标
- `service/` - 跨平台系统服务管理（systemd/launchd/SCM）
- `eventstore/` - 会话事件持久化 + delta 聚合
- `updater/` - 自更新（GitHub API、sha256 校验、原子替换）
- `sqlutil/` - SQLite CGO/no-CGO 驱动切换（build tags）
- `webchat/` - 嵌入式 Next.js SPA (go:embed)

### 公共包 (`pkg/`)

- `events/` - AEP v1 数据结构
- `aep/` - AEP v1 编解码

### 顶层目录

```
client/    - Go 客户端 SDK
webchat/   - Next.js Web Chat UI
examples/  - TS/Python/Java 客户端 SDK
docs/      - 架构、规范、安全文档
scripts/   - 构建/校验脚本
configs/   - 配置文件
```

---

## 贡献工作流

### Admin（仓库管理员）

拥有仓库 write/admin 权限的协作者直接在 origin 仓库创建分支和 PR。

```bash
# 1. 从最新 main 创建功能分支
git fetch origin main
git checkout -b feat/<feature-name> origin/main

# 2. 开发和提交
git add <files>
git commit -m "feat(scope): description"

# 3. 推送并创建 PR
git push -u origin feat/<feature-name>
gh pr create --title "feat(scope): description"

# 4. 合并后清理
git checkout main && git pull origin main
git branch -d feat/<feature-name>
git push origin --delete feat/<feature-name>
```

### 外部贡献者（Fork-PR）

无仓库直接权限的外部贡献者使用 fork-PR 工作流。

```bash
# 1. Fork 并添加远程
git remote add fork https://github.com/<your-username>/hotplex.git

# 2. 创建功能分支
git fetch origin main
git checkout -b feat/<feature-name> origin/main

# 3. 推送到 fork 并创建 PR
git push -u fork feat/<feature-name>
gh pr create --base main --head <your-username>:feat/<feature-name> --title "feat(scope): description"

# 4. 合并后清理
git branch -d feat/<feature-name>
git push fork --delete feat/<feature-name>
```

### 通用规范

- 所有变更通过 PR 合并，不直接推送到 main
- 遵循 Conventional Commits 格式
- PR 必须通过 CI（lint + test + build）

---

## 开发指南

### 新增组件

| 组件类型 | 位置 | 说明 |
|---------|------|------|
| 新 AEP 事件类型 | `pkg/events/events.go` | 添加 Kind 常量 + Data 结构体 |
| 新 Worker 适配器 | `internal/worker/<name>/` | 嵌入 `base.BaseWorker` |
| 新消息适配器 | `internal/messaging/<name>/` | 嵌入 `PlatformAdapter` |
| 新诊断检查 | `internal/cli/checkers/` | 实现 `Checker` 接口 |
| 新 cobra 子命令 | `cmd/hotplex/<name>.go` | 在根命令注册 |

### 修改已有组件

| 组件 | 文件 | 说明 |
|------|------|------|
| Agent 配置 | `internal/agentconfig/loader.go` | 文件加载、大小限制 |
| Session 管理 | `internal/session/manager.go` | 状态机、原子操作 |
| WebSocket 协议 | `internal/gateway/conn.go` | ReadPump/WritePump |
| LLM 重试 | `internal/gateway/llm_retry.go` | 可重试错误检测 |
| Worker 启动命令 | `configs/config.yaml` | `claude_code.command` / `opencode_server.command` |
| 路由注册 | `cmd/hotplex/routes.go` | HTTP 路由 |

### 跨平台兼容

**必须使用跨平台函数**：
- 路径：`filepath.Join()`、`filepath.Dir()`、`filepath.Base()`
- 临时目录：`os.TempDir()`
- 用户主目录：`os.UserHomeDir()`
- 进程终止：`process.Kill()`

**平台分离**：
- 使用 `*_unix.go` / `*_windows.go` build tags
- CI 必须通过 Linux + macOS + Windows 三平台测试

---

## 代码地图

### 入口点

- **CLI Root** → `cmd/hotplex/main.go` - cobra 根命令
- **GatewayDeps** → `cmd/hotplex/serve.go` - DI 容器

### 核心流程

**Gateway** (`internal/gateway/`):
- `Hub:68` - WS 广播 hub
- `Conn:35` - 单个 WS 连接
- `Handler` - AEP 事件分发
- `Bridge` - Session ↔ Worker 生命周期
- `LLMRetryController` - 自动重试
- `GatewayAPI` - HTTP session 端点

**Session** (`internal/session/`):
- `Manager:34` - 5 状态机
- `DeriveSessionKey` - UUIDv5 session ID
- `PoolManager` - 配额管理

**Messaging** (`internal/messaging/`):
- `Bridge` - StartSession → Join → Handle
- `InteractionManager` - 权限/Q&A 管理
- `ParseControlCommand` - 斜杠命令解析

**Agent Config** (`internal/agentconfig/`):
- `Load` - 配置加载
- `BuildSystemPrompt` - B+C 通道组装

---

## 约定与规范

### 必须遵守

- **Mutex**: 显式 `mu` 字段，不嵌入，不传指针
- **错误**: `Err` 前缀（哨兵）、`Error` 后缀（自定义）、`fmt.Errorf("%w")` 包装
- **日志**: `log/slog` JSON handler
- **测试**: `testify/require`、table-driven、`t.Parallel()`
- **Worker 注册**: `init()` + `worker.Register()` 模式
- **关闭顺序**: signal → cancel ctx → tracing → hub → bridge → sessionMgr → HTTP

### 反模式（禁止）

- ❌ `sync.Mutex` 嵌入或传指针
- ❌ `math/rand` 用于加密
- ❌ Shell 执行（仅允许 `claude` 二进制）
- ❌ 非 ES256 JWT
- ❌ 硬编码路径分隔符
- ❌ 直接使用 POSIX 信号
- ❌ 用 `sed`/`awk` 插入或修改源码行（缩进不可控，必须用 Edit 工具）

### 代码编辑规则

- **Edit 工具优先**：修改源码必须使用 Edit 工具，禁止用 `sed -i` 插入或修改代码行
- **Edit 匹配失败时**：重新 Read 文件获取精确内容，用精确字符串重试 Edit；扩大上下文使其唯一
- **Go 文件 tab 缩进**：Go 项目使用 tab 缩进（gofmt 标准）。使用 Edit 工具时，old_string 必须从 Read 输出中直接复制原文（保留 tab），禁止手敲空格缩进。Edit 匹配失败时先用 `cat -A` 确认实际空白字符
- **`sed` 适用场景**：仅限非代码操作（config 快速替换、日志过滤等简单唯一 token 替换）

### 独特风格

- **锁顺序**: `m.mu` → `ms.mu`（防止死锁）
- **背压**: 丢弃 `message.delta`，保留 `state`/`done`/`error`
- **Seq 分配**: Per-session 原子单调计数器
- **进程终止**: 3 层（SIGTERM → 等待 5s → SIGKILL）
- **Agent 配置**: **B 通道** (`<directives>`): `<hotplex>`(META-COGNITION.md, go:embed, 始终存在且排首位) + `<persona>`(SOUL) + `<rules>`(AGENTS) + `<skills>`(SKILLS) → **C 通道** (`<context>`): `<user>`(USER) + `<memory>`(MEMORY)
- **元认知层**: `internal/agentconfig/META-COGNITION.md` 定义 Worker 的身份边界（不管理 Transport/状态/协议）、B/C 通道冲突隔离法则（directives 无条件覆盖 context）、配置替换的"命中即终止"机制、配置修改 SOP（禁止改全局来影响 Bot）
- **XML 安全**: 强制开启 **XML Sanitizer**，对保留标签进行 HTML 转义预防注入
- **Windows 注入**: 强制使用 **临时文件注入**（`--append-system-prompt-file`），严禁使用内联参数防止 cmd.exe 截断

---

## 命令参考

### 构建与质量

```bash
make build          # 构建网关二进制
make test           # 运行测试（含 -race）
make test-short     # 快速测试（-short）
make lint           # golangci-lint
make coverage       # 覆盖率报告
make check          # 完整 CI: quality + build
make quality        # fmt + lint + test
make fmt            # 格式化
make clean          # 清理构建产物
```

### 开发

```bash
make quickstart      # 首次安装
make run             # 构建并运行网关
make dev             # 启动开发环境（gateway + webchat）
make dev-stop        # 停止所有开发服务
make dev-status      # 查看运行服务
make dev-logs        # 查看网关日志
make dev-reset       # 停止并重启
```

### 网关管理

```bash
# Make 方式
make gateway-start
make gateway-stop
make gateway-status
make gateway-logs

# CLI 方式
hotplex gateway start
hotplex gateway stop
hotplex gateway restart
```

### 系统服务

```bash
hotplex service install          # 用户级服务（无需 root）
hotplex service install --level system  # 系统级（需要 sudo）
hotplex service start
hotplex service stop
hotplex service status
hotplex service logs -f
```

### 自更新

```bash
hotplex update                # 交互式更新
hotplex update --check        # 仅检查，不下载
hotplex update -y             # 跳过确认提示
hotplex update --restart      # 更新后自动重启网关
```

### Slack 操作

```bash
hotplex slack send-message --text "Hello" --channel <id>
hotplex slack upload-file --file ./report.pdf --title "Report"
hotplex slack update-message --channel <id> --ts <ts> --text "Updated"
hotplex slack schedule-message --text "Reminder" --at "2026-05-04T09:00:00+08:00"
hotplex slack download-file --file-id <id> --output ./save.pdf
hotplex slack list-channels --types im,public_channel --json
hotplex slack bookmark add --channel <id> --title "Link" --url <url>
hotplex slack bookmark list --channel <id>
hotplex slack bookmark remove --channel <id> --bookmark-id <id>
hotplex slack react add --channel <id> --ts <ts> --emoji white_check_mark
```

---

## 备注

### 符号链接

- `CLAUDE.md` ← `AGENTS.md`（只编辑 AGENTS.md）
- `.claude` ← `.agent`

### 重要限制

- 无 `api/` 目录（使用 JSON over WebSocket）
- Postgres store 仅为桩（仅 SQLite 可用于生产）
- OpenCode CLI 适配器已移除（由 OCS 替代）
- ACPX 适配器仅存在类型常量（无实现）
- Windows 自更新不支持（exe 运行时被锁，使用 `scripts/install.ps1` 替代）

### 跨平台支持

- **支持平台**: Linux、macOS、Windows
- **进程隔离**: POSIX (PGID) / Windows (Job Object)
- **平台适配**: `*_unix.go` / `*_windows.go` build tags
- **CI 要求**: 三平台必须通过测试

### 配置文件

- Agent 配置目录：`~/.hotplex/agent-configs/`
- B 通道（`<directives>`）：`META-COGNITION.md`(go:embed, 首位) + SOUL.md + AGENTS.md + SKILLS.md
- C 通道（`<context>`）：USER.md + MEMORY.md
- 三级 fallback：全局 → 平台（slack/）→ Bot（slack/U12345/），每文件独立解析，命中即终止
- 配置热更新：仅在 session 初始化或 `/reset` 时加载，运行中修改不立即生效

### 最大文件

| 文件 | 行数 |
|------|------|
| `manager_test.go` | 2249 |
| `adapter_test.go` (slack) | 2212 |
| `init_test.go` (brain) | 1472 |
| `conn_test.go` | 1424 |
| `adapter.go` (feishu) | 1384 |
| `wizard.go` (onboard) | 1380 |
| `adapter.go` (slack) | 1366 |
| `guard_test.go` (brain) | 1179 |
| `hub_test.go` | 1172 |
| `handler_test.go` | 1105 |
| `memory_test.go` (brain) | 1062 |
| `commands_test.go` (ocs) | 1050 |
| `router_test.go` (brain) | 1035 |
| `manager.go` | 998 |
| `e2e_test.go` (slack) | 970 |
| `worker.go` (ocs) | 878 |
| `streaming.go` (feishu) | 870 |
| `worker.go` (claudecode) | 846 |
| `handler.go` | 843 |
