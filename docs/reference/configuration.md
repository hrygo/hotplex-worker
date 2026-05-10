---
title: "配置完整参考"
weight: 3
description: "HotPlex Worker Gateway 所有配置项的权威参考，覆盖配置文件、环境变量、优先级和热重载机制。"
persona: "developer|enterprise"
difficulty: "intermediate"
---

# 配置完整参考

本文档是 HotPlex Worker Gateway 配置系统的权威参考。所有字段名、默认值和行为描述均基于源码 `internal/config/config.go:Default()` 和 `configs/config.yaml`。

---

## 目录

1. [配置优先级](#1-配置优先级)
2. [配置文件格式](#2-配置文件格式)
3. [配置项完整参考](#3-配置项完整参考)
   - [gateway — 网络网关](#31-gateway--网络网关)
   - [admin — 管理 API](#32-admin--管理-api)
   - [db — 数据持久化](#33-db--数据持久化)
   - [security — 安全与认证](#34-security--安全与认证)
   - [session — 会话管理](#35-session--会话管理)
   - [pool — 资源池](#36-pool--资源池)
   - [worker — Worker 运行时](#37-worker--worker-运行时)
   - [agent_config — Agent 人格与上下文](#38-agent_config--agent-人格与上下文)
   - [skills — Skills 发现](#39-skills--skills-发现)
   - [cron — 定时任务调度器](#310-cron--定时任务调度器)
   - [messaging — 消息平台](#311-messaging--消息平台)
   - [log — 日志](#312-log--日志)
   - [webchat — Web Chat UI](#313-webchat--web-chat-ui)
   - [inherits — 配置继承](#314-inherits--配置继承)
4. [热重载](#4-热重载)
5. [环境变量速查](#5-环境变量速查)

---

## 1. 配置优先级

HotPlex 采用分层覆盖策略，**高优先级覆盖低优先级**：

```
代码默认值 (Default())
  ↓ 被覆盖
配置继承链 (inherits 递归解析，含环检测)
  ↓ 被覆盖
配置文件 (YAML/JSON/TOML)
  ↓ 被覆盖
环境变量 (HOTPLEX_*)
  ↓ 被覆盖
Secrets Provider (JWT 等敏感字段，仅通过 Provider 加载)
```

**要点**：

- 代码默认值让二进制可以在零配置下启动（敏感字段除外）
- 配置文件是**非敏感值的权威来源**
- 敏感字段（JWT secret、Admin tokens、API keys）**永远不会从配置文件加载**，只能通过环境变量或 Secrets Provider 注入
- 消息平台配置有额外的三级优先级：`platform-level > messaging-level > Default()`

---

## 2. 配置文件格式

### 基本格式

配置文件为 YAML 格式，通过 Viper 解析。支持 YAML、JSON、TOML 三种格式。

### 环境变量展开

配置文件值支持 `${VAR}` 和 `${VAR:-default}` 语法引用环境变量：

```yaml
worker:
  environment:
    - GH_TOKEN=${HOTPLEX_WORKER_GH_TOKEN}
    - GITHUB_TOKEN=${HOTPLEX_WORKER_GITHUB_TOKEN:-}
```

- `${VAR}` — 引用环境变量，**未设置时该条目被排除**（不会传入空字符串）
- `${VAR:-default}` — 引用环境变量，未设置时使用 `default` 作为值
- `${VAR:-}` — 未设置时使用空字符串，**条目保留**

### 路径展开

所有路径字段自动进行以下处理：

- `~` 展开为用户主目录（`$HOME`）
- 相对路径转为绝对路径
- 符号链接解析（防止 TOCTOU 攻击）
- 支持 `${VAR}` 展开

### 加载顺序

环境变量文件加载顺序（后者覆盖前者）：

```
.env → .env.local → shell 环境变量
```

---

## 3. 配置项完整参考

### 3.1 gateway — 网络网关

WebSocket 网关的核心网络参数。

| 字段 | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `addr` | string | `localhost:8888` | `HOTPLEX_GATEWAY_ADDR` | 监听地址。默认仅绑定 localhost（安全基线）；反向代理场景设为 `:8888` 监听所有接口 |
| `read_buffer_size` | int | `4096` | — | WS 读缓冲区大小（字节） |
| `write_buffer_size` | int | `4096` | — | WS 写缓冲区大小（字节） |
| `ping_interval` | duration | `54s` | — | Ping 帧发送间隔。必须小于 `pong_timeout` |
| `pong_timeout` | duration | `60s` | — | Pong 响应超时。超时后连接被视为断开 |
| `write_timeout` | duration | `10s` | — | 单次写入操作超时 |
| `idle_timeout` | duration | `5m` | — | 连接空闲超时。超时后服务端关闭连接 |
| `max_frame_size` | int64 | `32768` (32KB) | — | 单个 WS 帧最大大小 |
| `broadcast_queue_size` | int | `256` | — | Hub 广播通道缓冲区大小 |
| `platform_write_buffer` | int | `64` | — | 每连接异步平台写入通道容量。64 个槽位在 120ms 合并窗口下可容纳约 8 批事件 |
| `platform_drop_threshold` | int | `56` | — | 开始丢弃可丢弃事件（delta/raw）的填充水位线。设为 `platform_write_buffer` 的 87.5% 以提供背压缓解 |
| `delta_coalesce_interval` | duration | `120ms` | — | 连续 delta 事件的合并时间窗口。120ms 针对 Feishu CardKit 10 次/秒/卡片 的速率限制（含余量约 8.3 次/秒），同时保持首 token 延迟远低于 200ms 人类感知阈值 |
| `delta_coalesce_size` | int | `200` | — | delta 立即 flush 的 rune 阈值。200 rune ≈ 40 token，仅在突发时触发提前 flush |

---

### 3.2 admin — 管理 API

Admin API 管理端点配置。

| 字段 | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `enabled` | bool | `true` | `HOTPLEX_ADMIN_ENABLED` | 是否启用 Admin API |
| `addr` | string | `localhost:9999` | `HOTPLEX_ADMIN_ADDR` | Admin API 监听地址。默认仅绑定 localhost |
| `tokens` | []string | `[]` | `HOTPLEX_ADMIN_TOKEN_1..N` | 认证 token 列表。**只能通过环境变量设置**，支持编号后缀用于轮换 |
| `token_scopes` | map[string][]string | `nil` | — | 每 token 的权限范围映射 |
| `default_scopes` | []string | `["session:read", "stats:read", "health:read"]` | — | 未配置 scopes 的 token 的默认权限范围 |
| `ip_whitelist_enabled` | bool | `false` | — | 是否启用 IP 白名单 |
| `allowed_cidrs` | []string | `["127.0.0.0/8", "10.0.0.0/8"]` | — | 允许访问的 CIDR 列表 |
| `rate_limit_enabled` | bool | `true` | — | 是否启用速率限制 |
| `requests_per_sec` | int | `10` | — | 每秒允许的请求数 |
| `burst` | int | `20` | — | 突发请求缓冲量 |

---

### 3.3 db — 数据持久化

SQLite 数据库配置，Session 和 Event Store 共享。

| 字段 | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `path` | string | `~/.hotplex/data/hotplex.db` | `HOTPLEX_DB_PATH` | 主数据库文件路径。Session 和 Event 共用 |
| `events_path` | string | `""` | — | **已废弃**：Event 表已迁移至主数据库，此字段不再使用 |
| `wal_mode` | bool | `true` | `HOTPLEX_DB_WAL_MODE` | 启用 WAL (Write-Ahead Logging) 模式。生产环境必须开启 |
| `busy_timeout` | duration | `5s` | — | SQLite 忙等待超时。写入冲突时的重试时间 |
| `max_open_conns` | int | `3` | — | 最大打开连接数。1 写 + 2 读，适用于共享 Session/Event Store |
| `vacuum_threshold` | float64 | `0.2` | — | VACUUM 触发阈值。当空闲页占比 ≥ 20% 时自动 VACUUM |
| `cache_size_kib` | int | `8192` (8MB) | — | SQLite 页面缓存大小（KiB） |
| `mmap_size_mib` | int | `64` (64MB) | — | SQLite mmap 大小（MiB） |
| `wal_autocheckpoint` | int | `2000` | — | WAL 自动 checkpoint 页数阈值 |

---

### 3.4 security — 安全与认证

安全、认证和输入验证配置。

| 字段 | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `api_key_header` | string | `X-API-Key` | `HOTPLEX_SECURITY_API_KEY_HEADER` | 客户端 API Key 的 HTTP 头名称 |
| `api_keys` | []string | `nil` | `HOTPLEX_SECURITY_API_KEY_1..N` | 客户端认证密钥列表。**只能通过环境变量设置**，支持编号后缀 |
| `tls_enabled` | bool | `false` | — | 是否启用 TLS。非 localhost 地址时强烈建议开启 |
| `tls_cert_file` | string | `/etc/hotplex/tls/server.crt` | — | TLS 证书文件路径 |
| `tls_key_file` | string | `/etc/hotplex/tls/server.key` | — | TLS 私钥文件路径 |
| `allowed_origins` | []string | `["*"]` | — | WebSocket CORS 允许的 Origin 列表 |
| `jwt_secret` | []byte | — | `HOTPLEX_JWT_SECRET` | JWT 签名密钥（ES256）。**仅通过环境变量注入**，支持 raw 32 字节或 base64 编码。配置文件中不可设置 |
| `jwt_audience` | string | `hotplex-gateway` | `HOTPLEX_SECURITY_JWT_AUDIENCE` | JWT audience 声明 |
| `work_dir_allowed_base_patterns` | []string | `[]` | — | 额外的工作目录白名单模式。支持 `~` 和 `${VAR}` 展开。程序内建默认值：`~/.hotplex/workspace`、`~/workspace`、`~/projects`、`~/work`、`~/dev`、`/var/hotplex/projects` |
| `work_dir_forbidden_dirs` | []string | `[]` | — | 额外的工作目录黑名单。显式禁止的目录列表 |

---

### 3.5 session — 会话管理

Session 生命周期管理配置。

| 字段 | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `retention_period` | duration | `168h` (7天) | `HOTPLEX_SESSION_RETENTION_PERIOD` | 事件和日志的数据保留期。过期数据由 GC 扫描清理 |
| `gc_scan_interval` | duration | `1m` | — | GC 扫描间隔。定期扫描过期 Session 并清理 |
| `max_concurrent` | int | `1000` | `HOTPLEX_SESSION_MAX_CONCURRENT` | 最大并发 Session 数。达到上限后新请求被拒绝 |

---

### 3.6 pool — 资源池

Session 资源池配额管理。

| 字段 | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `min_size` | int | `0` | — | 池最小大小（预留） |
| `max_size` | int | `100` | `HOTPLEX_POOL_MAX_SIZE` | 池最大大小。系统级 Session 总量上限 |
| `max_idle_per_user` | int | `5` | `HOTPLEX_POOL_MAX_IDLE_PER_USER` | 每用户最大空闲 Session 数 |
| `max_memory_per_user` | int64 | `3221225472` (3GB) | `HOTPLEX_POOL_MAX_MEMORY_PER_USER` | 每用户最大内存配额（字节）。0 表示无限制 |

---

### 3.7 worker — Worker 运行时

Worker 进程生命周期和环境配置。

#### 3.7.1 基础配置

| 字段 | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `max_lifetime` | duration | `24h` | `HOTPLEX_WORKER_MAX_LIFETIME` | Worker 最大存活时间。超时后强制终止 |
| `idle_timeout` | duration | `60m` | `HOTPLEX_WORKER_IDLE_TIMEOUT` | Worker 空闲超时。无 I/O 时自动回收 |
| `execution_timeout` | duration | `30m` | `HOTPLEX_WORKER_EXECUTION_TIMEOUT` | 单次执行超时。捕获僵尸进程 |
| `turn_timeout` | duration | `0` | — | 单 Turn 超时。0 = 禁用（由 `execution_timeout` 兜底） |
| `default_work_dir` | string | `~/.hotplex/workspace` | `HOTPLEX_WORKER_DEFAULT_WORK_DIR` | Worker 默认工作目录 |
| `pid_dir` | string | `~/.hotplex/.pids` | — | PID 文件存储目录 |
| `env_blocklist` | []string | `[]` | — | 需要从 Worker 环境中屏蔽的变量前缀列表（带尾部 `_`） |
| `environment` | []string | 见下方 | — | 注入到所有 Worker 进程的额外环境变量。支持 `${VAR}` 展开，未设置且无默认值的条目被排除 |

**默认 environment**：

```yaml
environment:
  - GH_TOKEN=${HOTPLEX_WORKER_GH_TOKEN}
  - GITHUB_TOKEN=${HOTPLEX_WORKER_GITHUB_TOKEN}
```

#### 3.7.2 auto_retry — 自动重试

LLM Provider 返回临时错误（429、529、400 等）时的自动重试配置。

| 字段 | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `enabled` | bool | `true` | `HOTPLEX_WORKER_AUTO_RETRY_ENABLED` | 是否启用自动重试 |
| `max_retries` | int | `9` | `HOTPLEX_WORKER_AUTO_RETRY_MAX_RETRIES` | 最大重试次数 |
| `base_delay` | duration | `5s` | — | 首次重试延迟。指数退避基数 |
| `max_delay` | duration | `120s` | — | 最大重试延迟。退避上限 |
| `retry_input` | string | `继续` | — | 重试时发送给 Worker 的输入文本（中文字符串，意为"continue"） |
| `notify_user` | bool | `true` | — | 重试时是否通知用户 |
| `patterns` | []string | `[]` | — | 额外的可重试错误正则模式。追加到系统内建模式（429、500 等） |

#### 3.7.3 claude_code — Claude Code Worker

| 字段 | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `command` | string | `claude` | `HOTPLEX_WORKER_CLAUDE_CODE_COMMAND` | Worker 启动命令。支持带子命令，如 `ccr code` |
| `permission_prompt` | bool | `false` | — | 启用 `--permission-prompt-tool` stdio 模式。开启后权限请求会转发到 Slack/飞书交互 UI |
| `permission_auto_approve` | []string | `["ExitPlanMode"]` | — | 自动批准的工具名称列表（无需转发用户交互 UI） |
| `mcp_servers` | map | `{}` | — | 用户配置的 MCP Server。空值 = 使用默认发现机制 |

#### 3.7.4 opencode_server — OpenCode Server Worker

| 字段 | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `command` | string | `opencode` | `HOTPLEX_WORKER_OPENCODE_SERVER_COMMAND` | Worker 启动命令。支持带子命令，如 `opencode serve` |
| `idle_drain_period` | duration | `30m` | `HOTPLEX_WORKER_OPENCODE_SERVER_IDLE_DRAIN_PERIOD` | 空闲排空周期。单例进程在此期间无任务后关闭 |
| `ready_timeout` | duration | `10s` | `HOTPLEX_WORKER_OPENCODE_SERVER_READY_TIMEOUT` | 进程就绪等待超时 |
| `ready_poll_interval` | duration | `200ms` | `HOTPLEX_WORKER_OPENCODE_SERVER_READY_POLL_INTERVAL` | 就绪状态轮询间隔 |
| `http_timeout` | duration | `30s` | `HOTPLEX_WORKER_OPENCODE_SERVER_HTTP_TIMEOUT` | HTTP 请求超时 |

---

### 3.8 agent_config — Agent 人格与上下文

Agent B/C 通道配置加载器。

| 字段 | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `enabled` | bool | `true` | `HOTPLEX_AGENT_CONFIG_ENABLED` | 是否启用 Agent 配置加载（SOUL.md、AGENTS.md 等） |
| `config_dir` | string | `~/.hotplex/agent-configs` | `HOTPLEX_AGENT_CONFIG_DIR` | 配置文件根目录。支持 `~` 和 `${VAR}` 展开 |

**B 通道**（`<directives>`）：`META-COGNITION.md`（go:embed，始终首位）+ `SOUL.md` + `AGENTS.md` + `SKILLS.md`

**C 通道**（`<context>`）：`USER.md` + `MEMORY.md`

**三级 fallback**：全局 → 平台（slack/） → Bot（slack/U12345/），每文件独立解析，命中即终止。

---

### 3.9 skills — Skills 发现

| 字段 | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `cache_ttl` | duration | `5m` | `HOTPLEX_SKILLS_CACHE_TTL` | Skills 列表缓存 TTL |

---

### 3.10 cron — 定时任务调度器

AI-native 定时任务引擎：自然语言 prompt 作为 payload，结果投递到创建者的平台（飞书卡片 / Slack 消息）。

| 字段 | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `enabled` | bool | `true` | — | 是否启用 Cron 调度器 |
| `max_concurrent_runs` | int | `3` | — | 最大并行 Job 执行数 |
| `max_jobs` | int | `50` | — | 最大注册 Job 数 |
| `default_timeout_sec` | int | `300` (5分钟) | — | 单 Job 执行超时（秒） |
| `tick_interval_sec` | int | `60` | — | 调度器 tick 间隔（秒） |
| `yaml_config_path` | string | `""` | — | 外部 YAML 配置文件路径（可选） |
| `jobs` | []map | `[]` | — | 内联 Job 定义（可选） |

---

### 3.11 messaging — 消息平台

消息平台适配器配置。采用**共享默认值 + 平台覆盖**模式。

#### 3.11.1 共享配置（顶层 messaging）

这些值被所有平台继承，平台级配置可覆盖。

| 字段 | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `turn_summary_enabled` | bool | `true` | `HOTPLEX_MESSAGING_TURN_SUMMARY_ENABLED` | 每次 Turn 完成后发送摘要 |
| `worker_type` | string | `claude_code` | `HOTPLEX_MESSAGING_WORKER_TYPE` | 默认 Worker 类型。平台级可覆盖 |

#### 3.11.2 STT（语音转文字）共享配置

| 字段 | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `stt_provider` | string | `local` | `HOTPLEX_MESSAGING_STT_PROVIDER` | STT 提供者：`local`（外部命令）、`feishu`（云端 API）、`feishu+local`（云端主 + 本地备）、空字符串 = 禁用 |
| `stt_local_cmd` | string | `python3 ~/.hotplex/scripts/stt_server.py` | `HOTPLEX_MESSAGING_STT_LOCAL_CMD` | 本地 STT 命令模板。含 `{file}` → 每次请求 fork 临时进程；不含 → 持久子进程模式（stdin/stdout JSON 协议） |
| `stt_local_idle_ttl` | duration | `1h` | `HOTPLEX_MESSAGING_STT_LOCAL_IDLE_TTL` | 持久子进程自动关闭的空闲 TTL。0 = 禁用自动关闭 |

#### 3.11.3 TTS（文字转语音）共享配置

> **注意**：以下 `tts_*` 字段在配置结构体中通过 mapstructure `,squash` 标签嵌入到平台级配置（`MessagingPlatformConfig`）。这意味着在 YAML 中这些字段直接位于平台键下（如 `messaging.slack.tts_voice`），而非嵌套在 `tts` 子对象中。

| 字段 | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `tts_enabled` | bool | `true` | `HOTPLEX_MESSAGING_TTS_ENABLED` | 是否启用语音回复（仅语音触发的 Turn） |
| `tts_provider` | string | `edge+moss` | `HOTPLEX_MESSAGING_TTS_PROVIDER` | TTS 提供者：`edge`（Edge TTS，免费）、`moss`（MOSS-TTS-Nano 本地 CPU）、`edge+moss`（Edge 主 + MOSS 备）、空字符串 = 禁用 |
| `tts_voice` | string | `zh-CN-XiaoxiaoNeural` | `HOTPLEX_MESSAGING_TTS_VOICE` | Edge TTS 语音名称 |
| `tts_max_chars` | int | `150` | `HOTPLEX_MESSAGING_TTS_MAX_CHARS` | TTS 合成前的 LLM 摘要最大长度 |
| `tts_moss_model_dir` | string | `~/.hotplex/models/moss-tts-nano` | `HOTPLEX_MESSAGING_TTS_MOSS_MODEL_DIR` | MOSS-TTS-Nano ONNX 模型目录 |
| `tts_moss_voice` | string | `Xiaoyu` | `HOTPLEX_MESSAGING_TTS_MOSS_VOICE` | MOSS 内置语音预设名称 |
| `tts_moss_port` | int | `18083` | `HOTPLEX_MESSAGING_TTS_MOSS_PORT` | MOSS sidecar 本地端口 |
| `tts_moss_idle_timeout` | duration | `30m` | `HOTPLEX_MESSAGING_TTS_MOSS_IDLE_TIMEOUT` | MOSS sidecar 最后使用后的自动关闭超时 |
| `tts_moss_cpu_threads` | int | `0` | `HOTPLEX_MESSAGING_TTS_MOSS_CPU_THREADS` | ONNX Runtime intra-op 线程数。0 = 自动检测（物理核心数） |

#### 3.11.4 平台通用配置

每个消息平台（Slack / Feishu）共享以下配置结构。`MessagingPlatformConfig` 字段由共享默认值传播填充，平台级显式设置优先。

| 字段 | 类型 | 默认值 | 环境变量前缀 | 说明 |
|------|------|--------|-------------|------|
| `enabled` | bool | `false` | `HOTPLEX_MESSAGING_{PLATFORM}_ENABLED` | 是否启用该平台 |
| `worker_type` | string | 继承 messaging 级 | `HOTPLEX_MESSAGING_{PLATFORM}_WORKER_TYPE` | 平台级 Worker 类型覆盖 |
| `work_dir` | string | `""` | `HOTPLEX_MESSAGING_{PLATFORM}_WORK_DIR` | 平台级工作目录覆盖 |
| `dm_policy` | string | `allowlist` | `HOTPLEX_MESSAGING_{PLATFORM}_DM_POLICY` | 私聊消息策略：`allowlist` = 仅允许白名单用户 |
| `group_policy` | string | `allowlist` | `HOTPLEX_MESSAGING_{PLATFORM}_GROUP_POLICY` | 群聊消息策略：`allowlist` = 仅允许白名单群组 |
| `require_mention` | bool | `true` | `HOTPLEX_MESSAGING_{PLATFORM}_REQUIRE_MENTION` | 群聊中是否需要 @机器人 才响应 |
| `allow_from` | []string | `[]` | `HOTPLEX_MESSAGING_{PLATFORM}_ALLOW_FROM` | 全局白名单用户/群组（逗号分隔） |
| `allow_dm_from` | []string | `[]` | `HOTPLEX_MESSAGING_{PLATFORM}_ALLOW_DM_FROM` | 私聊白名单用户（逗号分隔） |
| `allow_group_from` | []string | `[]` | `HOTPLEX_MESSAGING_{PLATFORM}_ALLOW_GROUP_FROM` | 群聊白名单群组（逗号分隔） |

平台还继承 messaging 级的 STT 和 TTS 共享配置，可通过平台级环境变量覆盖（如 `HOTPLEX_MESSAGING_SLACK_TTS_PROVIDER`）。

#### 3.11.5 Slack 专有配置

| 字段 | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `bot_token` | string | `""` | `HOTPLEX_MESSAGING_SLACK_BOT_TOKEN` | Slack Bot Token（`xoxb-...`） |
| `app_token` | string | `""` | `HOTPLEX_MESSAGING_SLACK_APP_TOKEN` | Slack App Token（`xapp-...`），Socket Mode 所需 |
| `socket_mode` | bool | `true` | — | 是否使用 Socket Mode（推荐） |
| `assistant_api_enabled` | *bool | `nil` | — | 是否启用 Slack Assistant API。nil = 未设置 |
| `reconnect_base_delay` | duration | — | — | 断线重连基础延迟 |
| `reconnect_max_delay` | duration | — | — | 断线重连最大延迟 |

#### 3.11.6 Feishu 专有配置

| 字段 | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `app_id` | string | `""` | `HOTPLEX_MESSAGING_FEISHU_APP_ID` | 飞书 App ID（`cli_xxxx`） |
| `app_secret` | string | `""` | `HOTPLEX_MESSAGING_FEISHU_APP_SECRET` | 飞书 App Secret |

#### 3.11.7 配置传播机制

共享配置通过 `propagateMessagingDefaults()` 从 `messaging` 级传播到各平台：

```
messaging.worker_type  ──→  slack.worker_type (如未设置)
messaging.stt_*        ──→  slack.stt_* (如未设置)
messaging.tts_*        ──→  slack.tts_* (如未设置)
```

**优先级**：`platform-level (YAML/env) > messaging-level > Default()`

仅 zero-value 字段被填充，已有值不会被覆盖。

---

### 3.12 log — 日志

| 字段 | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `level` | string | `info` | `HOTPLEX_LOG_LEVEL` | 日志级别：`debug`、`info`、`warn`、`error` |
| `format` | string | `json` | `HOTPLEX_LOG_FORMAT` | 日志格式：`json` 或 `text` |

---

### 3.13 webchat — Web Chat UI

| 字段 | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `addr` | string | `""` | — | Webchat 地址（仅用于 banner 展示信息） |
| `enabled` | bool | `true` | — | 是否从 Gateway 提供嵌入式 Webchat SPA（go:embed） |

---

### 3.14 inherits — 配置继承

| 字段 | 类型 | 默认值 | 环境变量 | 说明 |
|------|------|--------|----------|------|
| `inherits` | string | `""` | — | 父配置文件路径。支持相对路径（相对于当前文件目录）。内建环检测防止循环继承 |

**示例**（`config-dev.yaml`）：

```yaml
inherits: config.yaml

admin:
  rate_limit_enabled: false

log:
  level: debug
  format: text
```

继承解析过程：

1. 加载 `Default()` 作为基底
2. 递归加载父配置文件（含环检测）
3. 子配置文件值覆盖父配置值
4. 环境变量覆盖所有文件配置

---

## 4. 热重载

HotPlex 通过 `fsnotify` 监听配置文件变更，支持运行时热更新。配置字段分为**动态字段**和**静态字段**两类。

### 4.1 动态字段（Hot-Reloadable）

以下字段修改后**立即生效**，无需重启：

| 字段路径 | 说明 |
|----------|------|
| `log.level` | 日志级别 |
| `session.gc_scan_interval` | GC 扫描间隔 |
| `pool.max_size` | 资源池最大大小 |
| `pool.max_idle_per_user` | 每用户最大空闲 Session |
| `security.api_keys` | API Key 列表 |
| `security.allowed_origins` | CORS Origin 列表 |
| `worker.max_lifetime` | Worker 最大存活时间 |
| `worker.idle_timeout` | Worker 空闲超时 |
| `worker.execution_timeout` | 执行超时 |
| `worker.auto_retry` | 自动重试配置（整体） |
| `admin.requests_per_sec` | Admin API 速率限制 |
| `admin.burst` | Admin API 突发量 |
| `admin.tokens` | Admin Token 列表 |

### 4.2 静态字段（需重启）

以下字段修改后**仅在重启后生效**，热重载时会记录变更日志但不应用：

| 字段路径 | 说明 |
|----------|------|
| `gateway.addr` | 监听地址 |
| `gateway.broadcast_queue_size` | 广播队列大小 |
| `gateway.read_buffer_size` | 读缓冲区大小 |
| `gateway.write_buffer_size` | 写缓冲区大小 |
| `log.format` | 日志格式 |
| `security.tls_enabled` | TLS 开关 |
| `security.tls_cert_file` | TLS 证书路径 |
| `security.tls_key_file` | TLS 私钥路径 |
| `security.jwt_secret` | JWT 密钥 |
| `db.path` | 数据库路径 |
| `db.wal_mode` | WAL 模式 |

### 4.3 热重载行为

- **防抖**：500ms 去抖，避免编辑器保存时的重复触发
- **校验**：重载前执行 `Validate()`，校验失败保留旧配置
- **审计日志**：所有变更记录审计日志（敏感字段脱敏为 `[REDACTED]`）
- **历史快照**：保留最近 64 个配置快照用于回滚
- **回滚**：`Rollback(version)` 恢复内存中的历史快照（不修改磁盘文件）
- **并发控制**：回调信号量限制为 4 个并发，防止快速变更时 goroutine 膨胀

---

## 5. 环境变量速查

### 5.1 命名规则

环境变量统一使用 `HOTPLEX_` 前缀，层级用 `_` 分隔：

```
HOTPLEX_{SECTION}_{FIELD}
```

列表类型使用编号后缀（1-based）：

```
HOTPLEX_ADMIN_TOKEN_1, HOTPLEX_ADMIN_TOKEN_2, ...
HOTPLEX_SECURITY_API_KEY_1, HOTPLEX_SECURITY_API_KEY_2, ...
```

### 5.2 必需变量

| 变量 | 说明 | 示例 |
|------|------|------|
| `HOTPLEX_JWT_SECRET` | JWT 签名密钥（ES256，≥32 字节） | `openssl rand -base64 32 \| tr -d '\n'` |
| `HOTPLEX_ADMIN_TOKEN_1` | Admin API 认证 token | `openssl rand -base64 32 \| tr -d '/+=' \| head -c 43` |

### 5.3 完整环境变量列表

#### 核心覆盖

| 变量 | 对应配置 | 默认值 |
|------|----------|--------|
| `HOTPLEX_LOG_LEVEL` | `log.level` | `info` |
| `HOTPLEX_LOG_FORMAT` | `log.format` | `json` |
| `HOTPLEX_DB_PATH` | `db.path` | `~/.hotplex/data/hotplex.db` |
| `HOTPLEX_DB_WAL_MODE` | `db.wal_mode` | `true` |
| `HOTPLEX_GATEWAY_ADDR` | `gateway.addr` | `localhost:8888` |
| `HOTPLEX_ADMIN_ENABLED` | `admin.enabled` | `true` |
| `HOTPLEX_ADMIN_ADDR` | `admin.addr` | `localhost:9999` |

#### 资源限制

| 变量 | 对应配置 | 默认值 |
|------|----------|--------|
| `HOTPLEX_SESSION_MAX_CONCURRENT` | `session.max_concurrent` | `1000` |
| `HOTPLEX_SESSION_RETENTION_PERIOD` | `session.retention_period` | `168h` |
| `HOTPLEX_POOL_MAX_SIZE` | `pool.max_size` | `100` |
| `HOTPLEX_POOL_MAX_IDLE_PER_USER` | `pool.max_idle_per_user` | `5` |
| `HOTPLEX_POOL_MAX_MEMORY_PER_USER` | `pool.max_memory_per_user` | `3221225472` |

#### Worker

| 变量 | 对应配置 | 默认值 |
|------|----------|--------|
| `HOTPLEX_WORKER_MAX_LIFETIME` | `worker.max_lifetime` | `24h` |
| `HOTPLEX_WORKER_IDLE_TIMEOUT` | `worker.idle_timeout` | `60m` |
| `HOTPLEX_WORKER_EXECUTION_TIMEOUT` | `worker.execution_timeout` | `30m` |
| `HOTPLEX_WORKER_DEFAULT_WORK_DIR` | `worker.default_work_dir` | `~/.hotplex/workspace` |
| `HOTPLEX_WORKER_CLAUDE_CODE_COMMAND` | `worker.claude_code.command` | `claude` |
| `HOTPLEX_WORKER_OPENCODE_SERVER_COMMAND` | `worker.opencode_server.command` | `opencode` |
| `HOTPLEX_WORKER_OPENCODE_SERVER_IDLE_DRAIN_PERIOD` | `worker.opencode_server.idle_drain_period` | `30m` |
| `HOTPLEX_WORKER_OPENCODE_SERVER_READY_TIMEOUT` | `worker.opencode_server.ready_timeout` | `10s` |
| `HOTPLEX_WORKER_OPENCODE_SERVER_READY_POLL_INTERVAL` | `worker.opencode_server.ready_poll_interval` | `200ms` |
| `HOTPLEX_WORKER_OPENCODE_SERVER_HTTP_TIMEOUT` | `worker.opencode_server.http_timeout` | `30s` |
| `HOTPLEX_WORKER_AUTO_RETRY_ENABLED` | `worker.auto_retry.enabled` | `true` |
| `HOTPLEX_WORKER_AUTO_RETRY_MAX_RETRIES` | `worker.auto_retry.max_retries` | `9` |
| `HOTPLEX_WORKER_GH_TOKEN` | 通过 `worker.environment` 注入 | — |
| `HOTPLEX_WORKER_GITHUB_TOKEN` | 通过 `worker.environment` 注入 | — |

#### Security

| 变量 | 对应配置 | 说明 |
|------|----------|------|
| `HOTPLEX_JWT_SECRET` | `security.jwt_secret` | 仅通过此变量注入 |
| `HOTPLEX_ADMIN_TOKEN_1..N` | `admin.tokens` | 编号后缀，支持轮换 |
| `HOTPLEX_SECURITY_API_KEY_1..N` | `security.api_keys` | 编号后缀，支持轮换 |
| `HOTPLEX_SECURITY_API_KEY_HEADER` | `security.api_key_header` | 默认 `X-API-Key` |
| `HOTPLEX_SECURITY_JWT_AUDIENCE` | `security.jwt_audience` | 默认 `hotplex-gateway` |

#### Agent Config

| 变量 | 对应配置 | 默认值 |
|------|----------|--------|
| `HOTPLEX_AGENT_CONFIG_ENABLED` | `agent_config.enabled` | `true` |
| `HOTPLEX_AGENT_CONFIG_DIR` | `agent_config.config_dir` | `~/.hotplex/agent-configs` |
| `HOTPLEX_SKILLS_CACHE_TTL` | `skills.cache_ttl` | `5m` |

#### Messaging 全局

| 变量 | 对应配置 | 默认值 |
|------|----------|--------|
| `HOTPLEX_MESSAGING_TURN_SUMMARY_ENABLED` | `messaging.turn_summary_enabled` | `true` |
| `HOTPLEX_MESSAGING_WORKER_TYPE` | `messaging.worker_type` | `claude_code` |
| `HOTPLEX_MESSAGING_TTS_ENABLED` | `messaging.tts_enabled` | `true` |
| `HOTPLEX_MESSAGING_STT_PROVIDER` | `messaging.stt_provider` | `local` |
| `HOTPLEX_MESSAGING_STT_LOCAL_CMD` | `messaging.stt_local_cmd` | `python3 ~/.hotplex/scripts/stt_server.py` |
| `HOTPLEX_MESSAGING_STT_LOCAL_IDLE_TTL` | `messaging.stt_local_idle_ttl` | `1h` |
| `HOTPLEX_MESSAGING_TTS_PROVIDER` | `messaging.tts_provider` | `edge+moss` |
| `HOTPLEX_MESSAGING_TTS_VOICE` | `messaging.tts_voice` | `zh-CN-XiaoxiaoNeural` |
| `HOTPLEX_MESSAGING_TTS_MAX_CHARS` | `messaging.tts_max_chars` | `150` |
| `HOTPLEX_MESSAGING_TTS_MOSS_MODEL_DIR` | `messaging.tts_moss_model_dir` | `~/.hotplex/models/moss-tts-nano` |
| `HOTPLEX_MESSAGING_TTS_MOSS_VOICE` | `messaging.tts_moss_voice` | `Xiaoyu` |
| `HOTPLEX_MESSAGING_TTS_MOSS_PORT` | `messaging.tts_moss_port` | `18083` |
| `HOTPLEX_MESSAGING_TTS_MOSS_IDLE_TIMEOUT` | `messaging.tts_moss_idle_timeout` | `30m` |
| `HOTPLEX_MESSAGING_TTS_MOSS_CPU_THREADS` | `messaging.tts_moss_cpu_threads` | `0` |

#### Messaging — Slack

| 变量 | 对应配置 |
|------|----------|
| `HOTPLEX_MESSAGING_SLACK_ENABLED` | `messaging.slack.enabled` |
| `HOTPLEX_MESSAGING_SLACK_BOT_TOKEN` | `messaging.slack.bot_token` |
| `HOTPLEX_MESSAGING_SLACK_APP_TOKEN` | `messaging.slack.app_token` |
| `HOTPLEX_MESSAGING_SLACK_WORKER_TYPE` | `messaging.slack.worker_type` |
| `HOTPLEX_MESSAGING_SLACK_WORK_DIR` | `messaging.slack.work_dir` |
| `HOTPLEX_MESSAGING_SLACK_REQUIRE_MENTION` | `messaging.slack.require_mention` |
| `HOTPLEX_MESSAGING_SLACK_DM_POLICY` | `messaging.slack.dm_policy` |
| `HOTPLEX_MESSAGING_SLACK_GROUP_POLICY` | `messaging.slack.group_policy` |
| `HOTPLEX_MESSAGING_SLACK_ALLOW_FROM` | `messaging.slack.allow_from` |
| `HOTPLEX_MESSAGING_SLACK_ALLOW_DM_FROM` | `messaging.slack.allow_dm_from` |
| `HOTPLEX_MESSAGING_SLACK_ALLOW_GROUP_FROM` | `messaging.slack.allow_group_from` |
| `HOTPLEX_MESSAGING_SLACK_STT_PROVIDER` | `messaging.slack.stt_provider` |
| `HOTPLEX_MESSAGING_SLACK_STT_LOCAL_CMD` | `messaging.slack.stt_local_cmd` |
| `HOTPLEX_MESSAGING_SLACK_STT_LOCAL_IDLE_TTL` | `messaging.slack.stt_local_idle_ttl` |
| `HOTPLEX_MESSAGING_SLACK_TTS_ENABLED` | `messaging.slack.tts_enabled` |
| `HOTPLEX_MESSAGING_SLACK_TTS_PROVIDER` | `messaging.slack.tts_provider` |
| `HOTPLEX_MESSAGING_SLACK_TTS_VOICE` | `messaging.slack.tts_voice` |
| `HOTPLEX_MESSAGING_SLACK_TTS_MAX_CHARS` | `messaging.slack.tts_max_chars` |
| `HOTPLEX_MESSAGING_SLACK_TTS_MOSS_MODEL_DIR` | `messaging.slack.tts_moss_model_dir` |
| `HOTPLEX_MESSAGING_SLACK_TTS_MOSS_VOICE` | `messaging.slack.tts_moss_voice` |
| `HOTPLEX_MESSAGING_SLACK_TTS_MOSS_PORT` | `messaging.slack.tts_moss_port` |
| `HOTPLEX_MESSAGING_SLACK_TTS_MOSS_IDLE_TIMEOUT` | `messaging.slack.tts_moss_idle_timeout` |
| `HOTPLEX_MESSAGING_SLACK_TTS_MOSS_CPU_THREADS` | `messaging.slack.tts_moss_cpu_threads` |

#### Messaging — Feishu

| 变量 | 对应配置 |
|------|----------|
| `HOTPLEX_MESSAGING_FEISHU_ENABLED` | `messaging.feishu.enabled` |
| `HOTPLEX_MESSAGING_FEISHU_APP_ID` | `messaging.feishu.app_id` |
| `HOTPLEX_MESSAGING_FEISHU_APP_SECRET` | `messaging.feishu.app_secret` |
| `HOTPLEX_MESSAGING_FEISHU_WORKER_TYPE` | `messaging.feishu.worker_type` |
| `HOTPLEX_MESSAGING_FEISHU_WORK_DIR` | `messaging.feishu.work_dir` |
| `HOTPLEX_MESSAGING_FEISHU_REQUIRE_MENTION` | `messaging.feishu.require_mention` |
| `HOTPLEX_MESSAGING_FEISHU_DM_POLICY` | `messaging.feishu.dm_policy` |
| `HOTPLEX_MESSAGING_FEISHU_GROUP_POLICY` | `messaging.feishu.group_policy` |
| `HOTPLEX_MESSAGING_FEISHU_ALLOW_FROM` | `messaging.feishu.allow_from` |
| `HOTPLEX_MESSAGING_FEISHU_ALLOW_DM_FROM` | `messaging.feishu.allow_dm_from` |
| `HOTPLEX_MESSAGING_FEISHU_ALLOW_GROUP_FROM` | `messaging.feishu.allow_group_from` |
| `HOTPLEX_MESSAGING_FEISHU_STT_PROVIDER` | `messaging.feishu.stt_provider` |
| `HOTPLEX_MESSAGING_FEISHU_STT_LOCAL_CMD` | `messaging.feishu.stt_local_cmd` |
| `HOTPLEX_MESSAGING_FEISHU_STT_LOCAL_IDLE_TTL` | `messaging.feishu.stt_local_idle_ttl` |
| `HOTPLEX_MESSAGING_FEISHU_TTS_ENABLED` | `messaging.feishu.tts_enabled` |
| `HOTPLEX_MESSAGING_FEISHU_TTS_PROVIDER` | `messaging.feishu.tts_provider` |
| `HOTPLEX_MESSAGING_FEISHU_TTS_VOICE` | `messaging.feishu.tts_voice` |
| `HOTPLEX_MESSAGING_FEISHU_TTS_MAX_CHARS` | `messaging.feishu.tts_max_chars` |
| `HOTPLEX_MESSAGING_FEISHU_TTS_MOSS_MODEL_DIR` | `messaging.feishu.tts_moss_model_dir` |
| `HOTPLEX_MESSAGING_FEISHU_TTS_MOSS_VOICE` | `messaging.feishu.tts_moss_voice` |
| `HOTPLEX_MESSAGING_FEISHU_TTS_MOSS_PORT` | `messaging.feishu.tts_moss_port` |
| `HOTPLEX_MESSAGING_FEISHU_TTS_MOSS_IDLE_TIMEOUT` | `messaging.feishu.tts_moss_idle_timeout` |
| `HOTPLEX_MESSAGING_FEISHU_TTS_MOSS_CPU_THREADS` | `messaging.feishu.tts_moss_cpu_threads` |

#### Observability

| 变量 | 说明 |
|------|------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OpenTelemetry Collector 端点 |
| `OTEL_SERVICE_NAME` | 服务名称（默认 `hotplex-gateway`） |
