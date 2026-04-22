# HotPlex Worker Gateway — Configuration Guide

`configs/` 目录包含完整的配置体系。项目遵循 **Convention Over Configuration** 原则——代码内嵌生产就绪的默认值 (`internal/config/config.go:Default()`)，所有 YAML 字段均可选。

---

## 目录结构

```text
configs/
├── config.yaml          # Base — 生产中性默认值，覆盖代码 Default() 的部分字段
├── config-dev.yaml      # Dev  — 继承 base，覆盖开发环境值
├── config-prod.yaml     # Prod — 继承 base，覆盖生产环境值
├── env.example          # 环境变量模板 → cp 到 .env 使用
└── README.md
```

---

## 快速开始

```bash
cp configs/env.example .env     # 填入 HOTPLEX_JWT_SECRET、HOTPLEX_ADMIN_TOKEN_1
mkdir -p data
make dev                        # 自动使用 config-dev.yaml
```

---

## 配置优先级（从低到高）

| 优先级 | 来源 | 说明 |
|:------:|:-----|:-----|
| 1 | `Default()` 代码默认值 | `internal/config/config.go` 编译时内嵌 |
| 2 | 父级配置文件 | `inherits` 递归加载，支持多级继承与循环检测 |
| 3 | 当前配置文件 | `-config` 指定的 YAML |
| 4 | 环境变量 `HOTPLEX_*` | Viper AutomaticEnv + `applyMessagingEnv()` 手动映射 |
| 5 | Secrets Provider | JWT/Token 等敏感字段，仅此渠道 |

环境变量映射公式：`HOTPLEX_<SECTION>_<FIELD>`，全大写下划线连接。
例如 `pool.max_size` → `HOTPLEX_POOL_MAX_SIZE`。

编号式环境变量支持令牌轮转：`HOTPLEX_ADMIN_TOKEN_1` ... `HOTPLEX_ADMIN_TOKEN_N`。

---

## 配置继承

子配置通过 `inherits` 指定父文件，只需列出与 base 不同的字段。支持多级继承和循环检测。

```yaml
inherits: config.yaml
log:
  level: "debug"
```

---

## 完整字段参考

> **默认值列**为 `config.yaml` 的值（即实际生效的 base 值）。括号内标注代码 `Default()` 的原始值，仅当两者不同时显示。

### gateway — WebSocket 网关

控制 WebSocket 连接的底层传输参数。Hub 为每个连接启动独立的 ReadPump / WritePump goroutine，通过带缓冲的 broadcast channel 路由事件。

| 字段 | 类型 | 默认值 | 热重载 | 说明 |
|:-----|:-----|:-------|:------:|:-----|
| `addr` | string | `:8888` | ❌ | 网关监听地址。客户端通过此端口建立 WebSocket 连接（AEP v1 协议）。生产环境建议置于反向代理（Nginx/ALB）之后并启用 TLS。**启动后不可变更**——HTTP Server 在启动时绑定端口 |
| `read_buffer_size` | int | `4096` | ❌ | WebSocket 读缓冲区大小（bytes）。`gorilla/websocket.Upgrader` 在 HTTP → WS 升级时分配，影响单连接内存占用。4KB 适合以 JSON 文本为主的消息流。**启动后不可变更**——Upgrader 在 NewHub 时初始化 |
| `write_buffer_size` | int | `4096` | ❌ | WebSocket 写缓冲区大小（bytes）。同上，为写方向预分配的缓冲。**启动后不可变更**——同 `read_buffer_size` |
| `ping_interval` | duration | `54s` | — | 服务端发送 WebSocket Ping 帧的间隔。必须小于 `pong_timeout`，推荐为其 90%。ReadPump goroutine 在此间隔触发 WritePump 发送 Ping，用于检测半开连接（客户端进程崩溃但 TCP 未关闭） |
| `pong_timeout` | duration | `60s` | — | 等待客户端 Pong 响应的超时。超时后 ReadPump 关闭连接，触发 Hub 注销和 session 清理。此值应略大于客户端的 ping-pong 往返延迟 |
| `write_timeout` | duration | `10s` | — | 单次 WebSocket 写操作的截止时间。WritePump 在发送每条消息前设置 `SetWriteDeadline`，超时则判定连接已死。防止慢速客户端阻塞其他连接的写操作 |
| `idle_timeout` | duration | `5m` | — | WebSocket 物理连接空闲超时。如果连接在此时间内没有任何帧（包括 ping/pong），服务端主动断开。用于清理已断开但 TCP 未关闭的僵尸连接 |
| `max_frame_size` | int64 | `32768` | — | 单个 WebSocket 帧最大允许字节数（32KB）。ReadPump 使用此值设置 `SetReadLimit`，超过则立即关闭连接。防止恶意客户端发送超大帧耗尽内存 |
| `broadcast_queue_size` | int | `256` | ❌ | Hub broadcast channel 的缓冲大小。Hub.Run() 从此 channel 消费事件并路由到各 session 连接。增大可缓解瞬时事件突发，但增加内存占用。**启动后不可变更**——Go channel 大小在 make 时确定 |
| `platform_write_buffer` | int | `64` | — | 平台连接（Slack/Feishu）的异步写 channel 容量。每个 platform conn 内部有一个带缓冲 channel 接收待发送事件，WriteCtx 将事件入队后立即返回。64 个槽位在 120ms 合并窗口下可容纳约 8 个批次 |
| `platform_drop_threshold` | int | `56` | — | 平台连接开始丢弃可丢弃事件的水位线（channel 填充度）。当 channel 使用量超过此阈值（87.5%），`message.delta` 和 `raw` 事件被静默丢弃以缓解反压，但 `state`/`done`/`error` 等关键事件永不丢弃 |
| `delta_coalesce_interval` | duration | `120ms` | — | 平台连接 Delta 事件的合并窗口。在此时间窗口内的多个 `message.delta` 事件会被合并为一次 API 调用发送给 Slack/Feishu。120ms 约等于每秒 8.3 次更新，适配飞书 CardKit 的 10 次/秒限制，同时保持首 token 延迟在 200ms 感知阈值内 |
| `delta_coalesce_size` | int | `200` | — | Delta 合并的 rune 数量阈值。当合并窗口内累积的文本超过 200 个 rune（约 40 个 token），立即刷新而非等待定时器。作为突发流量的安全阀，仅在输出高峰时触发 |

### admin — 管理 API

提供 REST 端点用于监控（stats、health）和会话管理（list、get、terminate）。应通过防火墙或网络策略限制访问。

| 字段 | 类型 | 默认值 | 热重载 | 说明 |
|:-----|:-----|:-------|:------:|:-----|
| `enabled` | bool | `true` | — | 是否启动 Admin HTTP 服务。设为 `false` 则不监听管理端口，所有 `/admin/*` 端点不可用 |
| `addr` | string | `:9999` | — | Admin API 监听地址。生产环境应绑定到内网 IP（如 `10.0.0.1:9999`）或通过 `allowed_cidrs` 限制访问 |
| `tokens` | []string | `[]` | ✅ | 授权令牌列表。通过 `HOTPLEX_ADMIN_TOKEN_1..N` 编号式环境变量设置。每个请求需携带 `Authorization: Bearer <token>` 头。支持多令牌用于无损轮转 |
| `token_scopes` | map | `{}` | — | 令牌到权限的 RBAC 映射。key 为令牌值，value 为权限列表（如 `["session:read", "session:write"]`）。未映射的令牌使用 `default_scopes` |
| `default_scopes` | []string | `["session:read", "stats:read", "health:read"]` | — | 未在 `token_scopes` 中显式映射的令牌的默认权限集 |
| `ip_whitelist_enabled` | bool | `false` | — | 启用 CIDR 白名单。开启后仅 `allowed_cidrs` 中的网段可访问 Admin API。Docker/Kubernetes 环境建议使用网络策略替代 |
| `allowed_cidrs` | []string | `127.0.0.0/8, 10.0.0.0/8` | — | 信任的 CIDR 网段列表。仅当 `ip_whitelist_enabled: true` 时生效 |
| `rate_limit_enabled` | bool | `true` | ✅ | 启用基于令牌桶的速率限制。按客户端 IP 独立计数，防止单个客户端滥用管理接口 |
| `requests_per_sec` | int | `10` | ✅ | 令牌桶的持续填充速率（每秒令牌数）。超出此速率的请求返回 429 |
| `burst` | int | `20` | ✅ | 令牌桶的最大容量（突发上限）。允许短时间内的请求突发，如监控面板轮询 |

### db — 数据存储

控制 SQLite 持久化行为。所有写操作通过单写 goroutine 串行化（channel 缓冲 50 条/100ms 批量刷盘），保证 WAL 模式下的写入一致性。

| 字段 | 类型 | 默认值 | 热重载 | 说明 |
|:-----|:-----|:-------|:------:|:-----|
| `path` | string | `data/hotplex-worker.db` | — | SQLite 数据库文件路径。相对路径基于工作目录解析。文件不存在时自动创建。加载时自动转为绝对路径 |
| `wal_mode` | bool | `true` | — | 启用 Write-Ahead Logging 模式。WAL 允许并发读写（读不阻塞写），是单写 goroutine 架构的前提。**禁止关闭**，否则写性能急剧下降且并发读不可用 |
| `busy_timeout` | duration | `500ms` | — | SQLite 锁等待超时。当另一个写操作持有时，当前操作在此时间内重试。500ms 在单写 goroutine 模型下足够覆盖一次批量刷盘 |
| `max_open_conns` | int | `1` | — | 最大数据库连接数。SQLite 的并发写入受限于单连接，设为 1 确保所有操作在同一连接上串行化 |

### security — 安全与认证

控制客户端认证、TLS 加密、CORS 跨域策略。

| 字段 | 类型 | 默认值 | 热重载 | 说明 |
|:-----|:-----|:-------|:------:|:-----|
| `api_key_header` | string | `X-API-Key` | — | API Key 认证的 HTTP 头名称。客户端通过此头发送 API Key，Hub 在 WebSocket 升级前校验 |
| `api_keys` | []string | `[]` | ✅ | 允许访问的 API 密钥列表。通过 `HOTPLEX_SECURITY_API_KEY_1..N` 编号式环境变量设置。为空时不做 API Key 校验（依赖 JWT 或网络策略保护）。热重载时原子替换整个 key 集合，不影响进行中的请求 |
| `tls_enabled` | bool | `false` | — | 启用 TLS（WSS）。生产环境**必须**设为 `true`。启用后网关使用 `tls_cert_file` 和 `tls_key_file` 加载证书 |
| `tls_cert_file` | string | `/etc/hotplex/tls/server.crt` | — | TLS 证书文件路径。仅当 `tls_enabled: true` 时使用 |
| `tls_key_file` | string | `/etc/hotplex/tls/server.key` | — | TLS 私钥文件路径。仅当 `tls_enabled: true` 时使用 |
| `allowed_origins` | []string | `["*"]` | ✅ | CORS 允许的 Origin 列表。WebSocket 升级时 `Upgrader.CheckOrigin` 校验请求的 Origin 头。`["*"]` 允许所有来源（仅开发用），生产应限制为具体域名。热重载即时生效——每次 WS 升级请求读取最新配置 |
| `jwt_audience` | string | `hotplex-worker-gateway` | — | JWT `aud` 声明的期望值。用于验证令牌的目标受众，防止令牌跨服务复用 |
| `jwt_secret` | []byte | — | — | JWT 签名密钥（ES256）。**仅**通过 `HOTPLEX_JWT_SECRET` 环境变量提供（base64 编码），禁止写入 YAML。用于签发和验证 session token |

### session — 会话生命周期

会话遵循 5 状态机：`CREATED → RUNNING → IDLE → TERMINATED → DELETED`。Manager 持有内存中的活跃会话映射，SQLite 持久化元数据，后台 GC goroutine 按 `gc_scan_interval` 周期扫描过期会话。

| 字段 | 类型 | 默认值 | 热重载 | 说明 |
|:-----|:-----|:-------|:------:|:-----|
| `retention_period` | duration | `168h` (7天) | — | TERMINATED 会话在数据库中的保留时长。过期后 GC 扫描将其标记为 DELETED 并从数据库物理删除。较长的保留期支持会话历史回溯和调试 |
| `gc_scan_interval` | duration | `1m` | ✅ | GC 后台扫描间隔。每次扫描检查：① IDLE 会话的 idle_expires_at 是否到期 → TERMINATED ② 会话的 max_lifetime 是否到期 → TERMINATED ③ TERMINATED 会话的 retention_period 是否到期 → DELETE。热重载通过 channel 信号重置 ticker，不中断正在执行的 GC 周期 |
| `max_concurrent` | int | `1000` | — | 全局最大并发活跃会话数（CREATED + RUNNING + IDLE）。超出时新会话创建请求被拒绝并返回 `POOL_EXHAUSTED` 错误。根据服务器内存和 Worker 资源需求调整。**非热重载**——实际配额由 `pool.max_size` 控制 |
| `event_store_enabled` | bool | `true` | — | 启用事件持久化。Bridge 在每个 `done` 事件时将完整 Envelope 写入 MessageStore，用于会话回放、调试和审计。关闭后不写事件日志，减少 I/O |
| `event_store_type` | string | `sqlite` (代码: `""`) | — | 事件存储后端类型。目前仅支持 `"sqlite"`。空字符串表示未指定，依赖 config.yaml 设置 |

### pool — 会话池

PoolManager 在内存中追踪全局和每用户的会话配额。每个 Worker 进程按 512MB 估算内存占用。

| 字段 | 类型 | 默认值 | 热重载 | 说明 |
|:-----|:-----|:-------|:------:|:-----|
| `min_size` | int | `0` | — | 预热池最小维持数量。大于 0 时启动后立即预创建指定数量的 Worker 进程，减少首次请求延迟。生产环境建议 `>0` 以消除冷启动 |
| `max_size` | int | `100` | ✅ | 全局最大活跃会话数（所有用户合计）。PoolManager.Acquire() 在 `totalCount >= maxSize` 时拒绝新会话。与 `session.max_concurrent` 协同工作 |
| `max_idle_per_user` | int | `10` (代码: `3`) | ✅ | 单个用户（bot_id）允许的最大同时活跃会话数。防止单个用户占用过多资源。0 = 不限 |
| `max_memory_per_user` | int64 | `8589934592` (8GB, 代码: 2GB) | — | 单个用户的总估算内存配额（bytes）。按每 Worker 512MB 估算，8GB 允许约 16 个并发 Worker。超出时拒绝新会话并返回 `MEMORY_EXCEEDED`。Linux 上 Worker 通过 RLIMIT_AS 硬限制为 512MB，macOS 不支持此机制。**非热重载**——内存配额检查使用启动时快照 |

### worker — Worker 进程

每个会话对应一个独立的 Worker 子进程（如 `claude` CLI），通过 stdin/stdout 的 NDJSON 协议通信。进程使用 PGID 隔离，终止时对整个进程组发送信号。

| 字段 | 类型 | 默认值 | 热重载 | 说明 |
|:-----|:-----|:-------|:------:|:-----|
| `max_lifetime` | duration | `24h` | — | Worker 进程的强制最大存活时间。到期后 GC 将其标记为 TERMINATED。防止长期运行的 Worker 累积内存泄漏或状态异常。Worker 可通过 resume 机制重新启动。**非热重载**——GC 使用启动时快照判定过期 |
| `idle_timeout` | duration | `60m` | — | Worker 空闲超时。Worker 执行完毕进入 IDLE 状态后，如果在此时间内没有新 input，GC 将其 TERMINATED。较短的超时释放资源更快，但可能导致频繁的 Worker 冷启动。**非热重载**——同 max_lifetime |
| `execution_timeout` | duration | `30m` | — | 僵尸 IO 超时。如果 RUNNING 状态的 Worker 在此时间内没有任何 stdout 输出（`LastIO()` 超时），判定为僵尸进程并强制终止。防止 Worker 挂死导致会话永久阻塞。**非热重载**——同 max_lifetime |
| `default_work_dir` | string | `/tmp/hotplex/workspace` | — | Worker 进程的默认工作目录。当 session 或 platform 未指定 `work_dir` 时使用。目录不存在时自动创建（`mkdir -p`） |
| `pid_dir` | string | `~/.hotplex/.pids/` | — | PID 文件目录。proc.Manager 在启动 Worker 时写入 PID 文件用于孤儿进程清理。网关重启时自动扫描此目录，杀死不再有父进程的孤儿 Worker |
| `allowed_envs` | []string | `[]` | — | 额外透传给 Worker 的环境变量名白名单。这些环境变量从网关进程继承到 Worker 子进程。与 `env_whitelist` 合并去重 |
| `env_whitelist` | []string | `["PATH", "HOME", ...]` | — | 安全透传的环境变量白名单。Worker 进程默认**不继承**网关的任何环境变量（安全隔离），仅白名单中的变量会被透传。`allowed_envs` 会被合并到此列表 |

### worker.auto_retry — LLM 自动重试

当 Worker（如 Claude Code）遇到 LLM Provider 的临时错误（429 限流、529 过载、网络超时等），Bridge 自动检测并触发重试，对用户透明。

**错误拦截机制**：当 `enabled: true` 时，Worker 发出的 `error` 事件会被 Bridge 暂存而非立即转发。等到 `done` 事件到达时判断是否可重试——如果可重试，原始 error 事件被丢弃，用户只看到 notify 消息（如"🔄 正在自动重试"）；如果不可重试，error 事件在 done 之后放行给客户端。这样用户不会看到原始的 LLM 技术错误信息（如 "429 rate_limit_error"），只看到友好的重试通知。

| 字段 | 类型 | 默认值 | 热重载 | 说明 |
|:-----|:-----|:-------|:------:|:-----|
| `enabled` | bool | `true` | ✅ | 启用自动重试。关闭后 Worker 遇到临时错误时直接将错误事件转发给客户端 |
| `max_retries` | int | `9` | ✅ | 单个会话的最大重试次数。每次 Worker 完成一个 turn（`done` 事件），Bridge 检查输出是否匹配可重试错误模式。达到上限后停止重试，返回提示让用户手动操作。9 次重试在 base_delay=5s 指数退避下总耗时约 5+10+20+40+80+120+120+120+120 ≈ 640s（11 分钟），不会超过网关的 `execution_timeout`（默认 30 分钟） |
| `base_delay` | duration | `5s` | ✅ | 首次重试的等待时间。采用指数退避：第 1 次等 `base_delay`，第 2 次等 `2×base_delay`，依此类推，直到 `max_delay` |
| `max_delay` | duration | `120s` | ✅ | 退避延迟上限。即使指数增长超过此值，实际延迟也不会超过 120s |
| `retry_input` | string | `继续` | ✅ | 重试时发送给 Worker 的文本。Bridge 在 Worker 的 stdin 写入此文本，触发 Worker 重新发起 LLM 请求 |
| `notify_user` | bool | `true` | ✅ | 重试期间是否通知用户。启用时在重试前发送一条 `message` 事件（如"🔄 正在自动重试 (1/9)..."），替代原始 LLM 错误信息。关闭后用户看不到任何重试提示，但错误信息仍然会被拦截 |
| `patterns` | []string | `[]` | ✅ | **追加**到内置默认模式的自定义正则表达式。内置模式匹配：`429/rate limit`、`529/overloaded`、`500-503/server error`、`network/connection reset/ECONNREFUSED/timeout`、`API Error.*reject`。自定义模式与内置模式共同生效，不会覆盖 |

### log — 日志

控制网关进程的日志输出。使用 `log/slog` 结构化日志，所有日志带有 `service=hotplex-gateway` 和 `version` 字段。

| 字段 | 类型 | 默认值 | 热重载 | 说明 |
|:-----|:-----|:-------|:------:|:-----|
| `level` | string | `info` | ✅ | 最低日志级别。`debug` 输出所有事件流转细节（每个收发的事件），适合开发调试。`info` 仅输出关键生命周期事件（连接注册、会话创建/终止等）。`warn`/`error` 仅输出异常情况。热重载通过 `slog.LevelVar` 即时生效，无需重启 |
| `format` | string | `json` | ❌ | 日志格式。`json` 为结构化 JSON（适合日志聚合系统如 ELK/Loki）。`text` 为人类可读的 key=value 格式（适合终端直接查看）。Makefile `dev-logs` 使用 `tail -f` 展示。**启动后不可变更**——Handler 在初始化时确定格式 |

### messaging.slack — Slack Socket Mode

通过 Slack Socket Mode 建立 WebSocket 连接到 Slack 服务器，实现无需公网入口的消息收发。消息经过 chunker（长消息拆分）→ dedup（TTL 去重）→ format（Markdown 转换）→ rate limiter → send 的流水线处理。Streaming 使用 SlackStreamingWriter，以 150ms 间隔、20 rune 阈值增量更新消息。

| 字段 | 类型 | 默认值 | 热重载 | 说明 |
|:-----|:-----|:-------|:------:|:-----|
| `enabled` | bool | `false` | — | 启用 Slack 适配器。启动时通过 Socket Mode 连接到 Slack 服务器，开始监听消息事件 |
| `bot_token` | string | — | — | Slack Bot User OAuth Token（`xoxb-` 前缀）。用于调用 Slack Web API 发送消息、更新卡片。通过环境变量 `HOTPLEX_MESSAGING_SLACK_BOT_TOKEN` 设置 |
| `app_token` | string | — | — | Slack App-Level Token（`xapp-` 前缀）。用于建立 Socket Mode WebSocket 连接。需要在 Slack App 配置中启用 Socket Mode 并生成。通过环境变量 `HOTPLEX_MESSAGING_SLACK_APP_TOKEN` 设置 |
| `socket_mode` | bool | `true` (代码: `false`) | — | 启用 Socket Mode。Socket Mode 通过 WebSocket 与 Slack 服务器通信，无需公网可访问的 HTTP 端点。关闭则需要配置 Events API URL |
| `worker_type` | string | `claude_code` | — | 为 Slack 会话创建的 Worker 类型。决定使用哪个适配器启动 Worker 进程（`claude_code` = Claude Code CLI，`opencodeserver` = OpenCode Server） |
| `work_dir` | string | — | — | Worker 进程的工作目录。为空时使用 `worker.default_work_dir`。可按平台设置不同目录 |
| `dm_policy` | string | `allowlist` | — | 私聊（DM）的访问策略。`open` = 允许所有人，`allowlist` = 仅 `allow_from` + `allow_dm_from` 中的用户，`disabled` = 禁止所有私聊 |
| `group_policy` | string | `allowlist` | — | 频道和群组 DM 的访问策略。选项同 `dm_policy`。`require_mention: true` 时，即使策略允许，也需要 @机器人 才触发 |
| `require_mention` | bool | `true` | — | 群组中是否需要 @机器人 才触发处理。私聊始终触发，不受此限制。`true` 避免群组中的每条消息都创建 Worker 会话 |
| `allow_from` | []string | `[]` | — | 全局白名单（同时授权 DM 和群组）。值为 Slack User ID 或显示名。在 `allowlist` 策略下生效 |
| `allow_dm_from` | []string | `[]` | — | 仅私聊白名单。与 `allow_from` 合并去重 |
| `allow_group_from` | []string | `[]` | — | 仅群组白名单。与 `allow_from` 合并去重 |
| `reconnect_base_delay` | duration | `1s` | — | Socket Mode 连接断开后的首次重连延迟。采用指数退避，每次翻倍直到 `reconnect_max_delay` |
| `reconnect_max_delay` | duration | `60s` | — | 重连延迟上限。避免在网络故障时过于频繁地重试 |
| `typing_stages` | []object | 内置 5 阶段 | — | 多阶段表情进度指示器配置（免费 Workspace 无法使用 Slack native typing indicator）。每项含 `after`（延迟）和 `emoji`（表情符号）。默认：eyes → clock1 → hourglass → gear → hourglass |
| `assistant_api_enabled` | *bool | `nil` | — | 是否启用 Assistant API 模式。`nil`（未设置）= 自动检测。`true` = 强制使用 Assistant API，`false` = 使用标准对话模式 |

### messaging.feishu — 飞书 WebSocket

通过飞书 WebSocket 客户端接收事件（P2 模式），使用流式卡片（CardKit）实现增量更新。消息发送经过 4 层防护：TTL 守卫 → 完整性检查 → 带退避重试 → IM Patch 降级。

| 字段 | 类型 | 默认值 | 热重载 | 说明 |
|:-----|:-----|:-------|:------:|:-----|
| `enabled` | bool | `false` | — | 启用飞书适配器。启动时通过 WebSocket 连接到飞书服务器，监听消息事件 |
| `app_id` | string | — | — | 飞书应用 ID（`cli_` 前缀）。在飞书开放平台创建应用后获取。通过环境变量 `HOTPLEX_MESSAGING_FEISHU_APP_ID` 设置 |
| `app_secret` | string | — | — | 飞书应用密钥。用于获取 tenant_access_token 调用飞书 API。通过环境变量 `HOTPLEX_MESSAGING_FEISHU_APP_SECRET` 设置 |
| `worker_type` | string | `claude_code` | — | 为飞书会话创建的 Worker 类型。同 Slack 的 `worker_type` |
| `work_dir` | string | — | — | Worker 进程工作目录。同 Slack 的 `work_dir` |
| `dm_policy` | string | `allowlist` | — | 单聊访问策略。选项同 Slack |
| `group_policy` | string | `allowlist` | — | 群组和话题群访问策略。选项同 Slack |
| `require_mention` | bool | `true` | — | 群组中是否需要 @机器人。同 Slack |
| `allow_from` | []string | `[]` | — | 全局白名单。值为飞书 User ID 或 Open ID |
| `allow_dm_from` | []string | `[]` | — | 仅单聊白名单 |
| `allow_group_from` | []string | `[]` | — | 仅群组白名单 |
| `stt_provider` | string | `feishu+local` | — | 语音转文字引擎。`feishu` = 飞书 speech_to_text API（需开通权限），`local` = 本地命令行引擎（SenseVoice-Small ONNX），`feishu+local` = 云端优先本地降级（推荐），空 = 禁用 STT |
| `stt_local_cmd` | string | `python3 scripts/stt_once.py {file}` | — | 本地 STT 命令模板。`{file}` 占位符替换为音频文件路径。ephemeral 模式下每次请求直接执行此命令；persistent 模式下需配合 `stt_server.py` 使用 JSON-over-stdio 协议。详细的安装和配置说明见 [STT 安装手册](../docs/STT-SETUP.md) |
| `stt_local_mode` | string | `ephemeral` | — | 本地 STT 运行模式。`ephemeral` = 每次请求启动新进程（冷启动约 3-5s，简单可靠），`persistent` = 常驻子进程（零冷启动，适合高频使用，模型约占 900MB 内存） |
| `stt_local_idle_ttl` | duration | `1h` | — | 持久模式空闲超时。常驻 STT 子进程在此时间内无转写请求则自动关闭，节省内存（模型约占 900MB）。0 = 永不关闭。仅 `persistent` 模式下生效 |

> **完整的 STT 安装配置说明**见 [STT-SETUP.md](../docs/STT-SETUP.md) —— 涵盖 Python 依赖安装、模型下载、Ephemeral/Persistent 模式配置、Docker 部署和故障排查。

---

## 环境变量速查

所有环境变量以 `HOTPLEX_` 前缀。编号式变量 (`_1..N`) 支持多值轮转。

| 变量 | 必填 | 说明 |
|:-----|:----:|:-----|
| `HOTPLEX_JWT_SECRET` | **是** | JWT 签名密钥（base64 编码，ES256 算法） |
| `HOTPLEX_ADMIN_TOKEN_1` | **是** | 主管理端令牌 |
| `HOTPLEX_ADMIN_TOKEN_2..N` | 否 | 备用管理端令牌（轮转用） |
| `HOTPLEX_SECURITY_API_KEY_1..N` | 否 | 客户端 API 密钥 |
| `HOTPLEX_LOG_LEVEL` | 否 | 覆盖 `log.level` |
| `HOTPLEX_LOG_FORMAT` | 否 | 覆盖 `log.format` |
| `HOTPLEX_DB_PATH` | 否 | 覆盖 `db.path` |
| `HOTPLEX_GATEWAY_ADDR` | 否 | 覆盖 `gateway.addr` |
| `HOTPLEX_ADMIN_ADDR` | 否 | 覆盖 `admin.addr` |
| `HOTPLEX_SESSION_MAX_CONCURRENT` | 否 | 覆盖 `session.max_concurrent` |
| `HOTPLEX_POOL_MAX_SIZE` | 否 | 覆盖 `pool.max_size` |
| `HOTPLEX_POOL_MAX_MEMORY_PER_USER` | 否 | 覆盖 `pool.max_memory_per_user` |
| `HOTPLEX_MESSAGING_SLACK_*` | 否 | 覆盖 `messaging.slack.*` 全部字段 |
| `HOTPLEX_MESSAGING_FEISHU_*` | 否 | 覆盖 `messaging.feishu.*` 全部字段 |

---

## 热重载

Watcher 监听 `-config` 指定文件的变更（500ms 防抖），通过反射逐字段比较检测变化，验证新配置后原子替换 `ConfigStore` 并通知各组件的 observer。

### 可热重载字段（即时生效，无需重启）

| 模块 | 字段 | 生效机制 |
|:-----|:-----|:---------|
| log | `level` | `slog.LevelVar` 动态切换 |
| security | `api_keys` | Authenticator 原子替换 key map |
| security | `allowed_origins` | 每次 WS 升级请求读取最新值 |
| session | `gc_scan_interval` | Channel 信号重置 GC ticker |
| pool | `max_size` | PoolManager 加锁更新 |
| pool | `max_idle_per_user` | 同上 |
| worker.auto_retry | `enabled` / `max_retries` / `base_delay` / `max_delay` / `retry_input` / `notify_user` / `patterns` | LLMRetryController 原子替换配置和编译后的正则 |
| admin | `requests_per_sec` / `burst` | 令牌桶动态调整填充速率和容量 |
| admin | `tokens` | Admin API token 列表热更新 |

### 不可热重载字段（变更需重启）

以下字段涉及**启动时一次性创建的资源**，运行中无法变更：

| 模块 | 字段 | 原因 |
|:-----|:-----|:-----|
| gateway | `addr` | HTTP Server 启动时绑定端口 |
| gateway | `read_buffer_size` / `write_buffer_size` | WebSocket Upgrader 在 NewHub 时初始化 |
| gateway | `broadcast_queue_size` | Go channel 大小在 make 时确定 |
| log | `format` | slog Handler 在初始化时确定格式 |
| db | `path` / `wal_mode` | SQLite 连接在启动时建立 |
| security | `tls_*` / `jwt_secret` | TLS 证书和 JWT 密钥在启动时加载 |

> 变更不可热重载字段时，Watcher 会记录日志 `config: static field changed, restart required`，新值存入 ConfigStore 但不产生实际效果，需重启网关才能生效。

支持历史快照和回滚（最多 64 个版本），回滚操作同样通过 ConfigStore 原子传播到所有 observer。

---

## Dev vs Prod 差异

| 字段 | config-dev.yaml | config-prod.yaml |
|:-----|:----------------|:-----------------|
| `admin.rate_limit_enabled` | `false` | (base: `true`) |
| `session.retention_period` | `24h` | (base: `168h`) |
| `session.max_concurrent` | `100` | (base: `1000`) |
| `pool.max_size` | `50` | `500` |
| `pool.min_size` | (base: `0`) | `10`（预热） |
| `pool.max_memory_per_user` | 4GB | 2GB |
| `worker.idle_timeout` | `5m` | `30m` |
| `worker.execution_timeout` | `5m` | (base: `30m`) |
| `log.level` | `debug` | (base: `info`) |
| `log.format` | `text` | (base: `json`) |
| `security.tls_enabled` | (base: `false`) | `true` |
| `security.allowed_origins` | (base: `*`) | 具体域名 |
| `messaging.*.enabled` | `true` | (base: `false`) |
| `messaging.feishu.*_policy` | (base: `allowlist`) | `open` |

---

## 生产安全清单

- `jwt_secret` 仅通过 `HOTPLEX_JWT_SECRET` 设置，禁止写入 YAML
- `admin.tokens` 仅通过 `HOTPLEX_ADMIN_TOKEN_1..N` 设置
- 生产必须 `tls_enabled: true`
- `admin.addr` 绑定内网或通过 `allowed_cidrs` 限制访问
- 敏感凭据使用编号式环境变量支持轮转
- `db.wal_mode` 禁止关闭（`true`）
