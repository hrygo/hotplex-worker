# HotPlex Worker Gateway - Configuration Guide

本目录包含 HotPlex Worker Gateway 的完整配置体系。项目遵循 **Convention Over Configuration** (约定优于配置) 原则，提供生产就绪的默认值，同时支持通过 YAML 和环境变量进行零停机热重载。

---

## 📂 目录结构

```text
configs/
├── config.yaml                    # 基础配置 (包含生产环境默认值)
├── config-dev.yaml                # 开发环境配置 (继承自 config.yaml)
├── config-prod.yaml               # 生产环境配置 (继承自 config.yaml)
├── env.example                    # 环境变量模板 (拷贝至 .env 使用)
├── README.md                      # 本手册
│
└── monitoring/                    # 可观测性协议栈
    ├── prometheus.yml             # Prometheus 抓取与告警规则
    ├── grafana/                   # Grafana 看板定义
    └── otel-collector-config.yaml # OpenTelemetry 采集器配置
```

---

## 🚀 快速开始 (Quick Start)

通过以下三个步骤，在 1 分钟内完成生产级环境初始化：

### 1. 初始化环境变量
复制模板并配置核心**安全凭据**：
```bash
cp configs/env.example .env
```
编辑 `.env` 文件，填入以下必填项：
- `HOTPLEX_JWT_SECRET`: 用于会话鉴权。生成命令：`openssl rand -base64 32`。
- `HOTPLEX_ADMIN_TOKEN_1`: 主管理端令牌。
- `HOTPLEX_ADMIN_TOKEN_2`: 备用管理端令牌（用于无损轮转，详见下文）。
- `HOTPLEX_ADMIN_ADDR`: (可选) 覆盖默认管理端口 `9999`。

### 2. 准备数据目录
默认 SQLite 存储路径为 `data/`：
```bash
mkdir -p data
```

### 3. 启动服务
**开发模式**:
```bash
./hotplex-worker -config configs/config-dev.yaml
```

**生产模式**:
```bash
export HOTPLEX_LOG_LEVEL=info
./hotplex-worker -config configs/config-prod.yaml
```

---

## 🏗️ 配置架构与优先级 (Precedence)

系统采用多层覆盖机制，优先级**从高到低**如下：

1.  **Secrets Provider**: 外部注入的**核心凭据**（如 `JWT_SECRET`）。主要用于对接 HashiCorp Vault 等**企业级密钥管理服务 (KMS)**。
2.  **环境变量 (Environment Variables)**: 以 `HOTPLEX_` 为前缀的所有变量。
3.  **当前配置文件**: 通过 `-config` 指定的 YAML 文件。
4.  **父级配置文件**: 通过 `inherits` 字段加载的基准配置。
5.  **代码默认值**: 编译时内嵌的 `internal/config/config.go:Default()`。

### 环境变量映射公式
任何 YAML 中的配置项均可通过环境变量覆盖，公式如下：
`HOTPLEX_<COMPONENT>_<FIELD>` (全大写，通过下划线连接)

*   示例: `gateway.read_buffer_size` -> `HOTPLEX_GATEWAY_READ_BUFFER_SIZE`
*   示例: `pool.max_size` -> `HOTPLEX_POOL_MAX_SIZE`

### 编号式环境变量 (Numbered Envs)
为了支持**安全令牌轮转 (Secret Rotation)**，以下字段支持通过编号后缀设置多个值（自动去重）：
- `admin.tokens` -> `HOTPLEX_ADMIN_TOKEN_1` ... `HOTPLEX_ADMIN_TOKEN_N`
- `security.api_keys` -> `HOTPLEX_SECURITY_API_KEY_1` ... `HOTPLEX_SECURITY_API_KEY_N`

---

## 🌐 网络与防火墙 (Network & Firewall)

HotPlex Worker Gateway 默认占用两个端口，请根据安全策略配置防火墙：

| 端口 | 服务 | 默认地址 | 建议访问策略 |
| :--- | :--- | :--- | :--- |
| **8888** | WebSocket Gateway | `:8888` | **Public/Internal**: 对终端设备或应用层开放。建议挂载在负载均衡器（如 Nginx）后并启用 WSS。 |
| **9999** | Admin API (Rest) | `:9999` | **Private/Strict**: 仅限内部监控系统或管理内网访问。**严禁暴露在公网**。 |

---

## 📒 完整配置参考 (Full Reference)

### 1. 通用配置 (General)
| 字段 | 类型 | 环境变量示例 | 说明 |
| :--- | :--- | :--- | :--- |
| `inherits` | string | - | 指定父级配置文件路径（递归继承） |

### 2. 日志 (Log)
| 字段 | 默认值 | 环境变量 | 说明 |
| :--- | :--- | :--- | :--- |
| `level` | `info` | `HOTPLEX_LOG_LEVEL` | 级别: `debug`, `info`, `warn`, `error` |
| `format` | `json` | `HOTPLEX_LOG_FORMAT` | 格式: `json` (生产), `text` (开发) |

### 3. 网关 (Gateway)
控制 WebSocket 连接行为。
| 字段 | 默认值 | 环境变量 | 说明 |
| :--- | :--- | :--- | :--- |
| `addr` | `:8888` | `HOTPLEX_GATEWAY_ADDR` | 监听地址 |
| `read_buffer_size` | `4096` | `HOTPLEX_GATEWAY_READ_BUFFER_SIZE` | WS 读取缓冲区 |
| `write_buffer_size` | `4096` | `HOTPLEX_GATEWAY_WRITE_BUFFER_SIZE` | WS 写入缓冲区 |
| `ping_interval` | `54s` | `HOTPLEX_GATEWAY_PING_INTERVAL` | 服务端心跳间隔 |
| `pong_timeout` | `60s` | `HOTPLEX_GATEWAY_PONG_TIMEOUT` | Pong 超时断开 |
| `write_timeout` | `10s` | `HOTPLEX_GATEWAY_WRITE_TIMEOUT` | 写入消息超时 |
| `idle_timeout` | `5m` | `HOTPLEX_GATEWAY_IDLE_TIMEOUT` | 物理层连接空闲超时 |
| `max_frame_size` | `32768` | `HOTPLEX_GATEWAY_MAX_FRAME_SIZE` | 单帧最大字节 (32KB) |
| `broadcast_queue_size`| `256` | - | 事件广播队列长度 |

### 4. 数据库 (DB)
| 字段 | 默认值 | 环境变量 | 说明 |
| :--- | :--- | :--- | :--- |
| `path` | `data/hotplex-worker.db` | `HOTPLEX_DB_PATH` | SQLite 文件路径 |
| `wal_mode` | `true` | - | 启用 Write-Ahead Logging |
| `busy_timeout` | `500ms` | - | 数据库锁重试时长 |
| `max_open_conns` | `1` | - | 最大并发连接 (SQLite 建议为 1) |

### 5. 安全与认证 (Security)
> [!CAUTION]
> `jwt_secret` 必须且只能通过环境变量 `HOTPLEX_JWT_SECRET` 设置，严禁写入 YAML 文件。

| 字段 | 默认值 | 环境变量 | 说明 |
| :--- | :--- | :--- | :--- |
| `api_key_header` | `X-API-Key` | - | 客户端检测 API Key 的 HTTP 头 |
| `api_keys` | `[]` | `HOTPLEX_SECURITY_API_KEY_1...N` | (编号式) 允许访问的 API 密钥列表 |
| `tls_enabled` | `false` | `HOTPLEX_SECURITY_TLS_ENABLED` | 是否启用 WSS (WebSocket Secure) |
| `tls_cert_file` | - | - | TLS 证书文件 (.crt) 路径 |
| `tls_key_file` | - | - | TLS 私钥文件 (.key) 路径 |
| `allowed_origins` | `["*"]` | - | CORS 跨域允许域名列表 |
| `jwt_audience` | `hotplex-worker-gateway`| - | 校验 JWT 载荷中的 `aud` 属性 |

### 6. 管理 API (Admin)
| 字段 | 默认值 | 环境变量 | 说明 |
| :--- | :--- | :--- | :--- |
| `enabled` | `true` | - | 是否启用管理端 RESTful 接口 |
| `addr` | `:9999` | `HOTPLEX_ADMIN_ADDR` | 监听地址 |
| `tokens` | `[]` | `HOTPLEX_ADMIN_TOKEN_1...N` | (编号式) 管理端授权令牌 |
| `token_scopes` | `{}` | - | **RBAC**: 针对特定令牌的精细化权限映射 |
| `default_scopes` | `["session:read", ...]` | - | 默认权限集 (包含 sessions, stats, health) |
| `ip_whitelist_enabled`| `false` | - | 启用基于物理网络 (CIDR) 的访问限制 |
| `allowed_cidrs` | `127.0.0.0/8...` | - | 允许访问的受信任网段列表 |
| `rate_limit_enabled` | `true` | - | 启用对管理接口的速率限制保护 |
| `requests_per_sec` | `10` | - | 令牌桶容量：每秒允许的请求数 |
| `burst` | `20` | - | 令牌桶容量：峰值并发请求数 |

### 7. 会话池与执行控制 (Session & Worker)
| 字段 | 默认值 | 环境变量 | 说明 |
| :--- | :--- | :--- | :--- |
| `session.retention_period` | `168h` | - | 历史会话在存储中的保留周期 (7天) |
| `session.gc_scan_interval` | `1m` | - | 会话清理任务扫描频率 |
| `session.max_concurrent` | `1000` | - | 整个实例允许的最大并发会话上限 |
| `pool.min_size` | `0` | - | 预热池最小维持数量 |
| `pool.max_size` | `100` | `HOTPLEX_POOL_MAX_SIZE` | 预热池总容量 |
| `pool.max_idle_per_user`| `3` | - | 单个用户/机器人允许留存的最大空闲会话 |
| `pool.max_memory_per_user`| `2GB` | `HOTPLEX_POOL_MAX_MEMORY_PER_USER` | 单个会话运行时的内存硬配额 (Bytes) |
| `worker.max_lifetime` | `24h` | - | 容器实例的强制生命周期限制 |
| `worker.idle_timeout` | `30m` | - | 容器无活动自动关停超长 |
| `worker.execution_timeout`| `30m` | - | Worker 无 IO 输出的僵尸超时 |
| `worker.env_whitelist` | `[]` | - | 注入到容器内的环境变量白名单 (Security) |

---

## 🛠️ 高阶专题 (Advanced Topics)

### 热重载 (Hot Reload)
系统监听主配置文件及继承链中所有文件的变化。以下模块支持**即时生效**（不需要重启）：
- `gateway.*` — 调整超时参数应对网络波动。
- `pool.*` — 动态缩扩容预热池。
- `admin.*` — 更新 IP 白名单或限流额度。

> [!NOTE]
> 基础设施类变更（如 `addr` 监听端口、`tls_enabled` 证书开关、`db.path` 数据库路径）必须重启进程。

### 配置继承案例
您可以创建一个基础配置，并根据不同环境进行微调：

```yaml
# configs/config-prod.yaml
inherits: "config.yaml"

gateway:
  idle_timeout: "30m"  # 生产环境允许更长的空闲时间

log:
  level: "info"
  format: "json"       # 生产环境使用 JSON 格式对接日志中心
```

### 生产环境安全建议
1.  **凭据隔离**: 绝不要在 YAML 中硬编码 `tokens` 或 `api_keys`，使用环境变量进行安全注入。
2.  **强制 TLS**: 生产环境务必开启 `tls_enabled: true`。
3.  **管理端加固**: 将 `admin.addr` 绑定到内网 IP，或通过 `allowed_cidrs` 限制访问来源。

---

## ✅ 验证与监控
- **配置语法校验**: `hotplex-worker -config <file> -test` (预留)
- **监控指标**: `http://<admin_addr>/admin/metrics` 提供 Prometheus 格式指标。
- **健康检查**: `http://<admin_addr>/admin/health` 返回 `200 OK`。
