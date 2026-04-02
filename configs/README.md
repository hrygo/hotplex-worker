# HotPlex Worker Gateway - Configuration

本目录包含 HotPlex Worker Gateway 的所有配置文件。

## 目录结构

```
configs/
├── config.yaml                    # 基础配置（生产就绪默认值）
├── config-dev.yaml                # 开发环境配置
├── config-prod.yaml               # 生产环境配置
├── env.example                    # 环境变量模板
├── README.md                      # 本文件
│
├── gateway/                       # Gateway 配置（预留扩展）
│
└── monitoring/                    # 监控配置
    ├── prometheus.yml             # Prometheus 抓取配置
    ├── alerts.yml                 # Prometheus 告警规则
    ├── slo.yaml                   # SLO 定义
    ├── otel-collector-config.yaml # OpenTelemetry 采集器配置
    └── grafana/                   # Grafana 配置
        ├── dashboards/
        │   ├── dashboards.yml     # Dashboard provider
        │   └── dashboard.json     # 主仪表板
        └── datasources/
            └── datasources.yml    # Datasource provider (Prometheus)
```

## 快速开始

### 1. 复制环境变量模板

```bash
cp configs/env.example .env
```

### 2. 生成必需密钥

```bash
# JWT 密钥（32+ 字节）
openssl rand -base64 32 | tr -d '\n'

# Admin API 令牌（43 字符）
openssl rand -base64 32 | tr -d '/+=' | head -c 43
```

### 3. 启动服务

```bash
# 开发环境
./hotplex-worker -config configs/config-dev.yaml

# 生产环境
./hotplex-worker -config configs/config-prod.yaml
```

## 配置继承

配置文件支持继承机制，子配置覆盖父配置的字段：

```yaml
# config-dev.yaml
inherits: config.yaml

gateway:
  addr: ":8888"        # 覆盖父配置的 gateway.addr
```

继承顺序（优先级从低到高）：

1. 代码默认值（`internal/config/config.go:Default()`）
2. 父配置文件（通过 `inherits` 指定）
3. 当前配置文件
4. 环境变量（`HOTPLEX_*`）

## 配置字段说明

### gateway

WebSocket 网关配置。

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `addr` | string | `:8888` | 网关监听地址 |
| `read_buffer_size` | int | `4096` | 读取缓冲区大小（字节） |
| `write_buffer_size` | int | `4096` | 写入缓冲区大小（字节） |
| `ping_interval` | duration | `54s` | WebSocket ping 间隔 |
| `pong_timeout` | duration | `60s` | pong 超时（超过则断开） |
| `write_timeout` | duration | `10s` | 写入超时 |
| `idle_timeout` | duration | `5m` | 空闲超时 |
| `max_frame_size` | int64 | `32768` | 最大帧大小（字节） |
| `broadcast_queue_size` | int | `256` | 广播消息队列大小 |

### admin

管理 API 配置。

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `enabled` | bool | `true` | 是否启用管理 API |
| `addr` | string | `:9999` | 管理 API 监听地址 |
| `tokens` | []string | `[]` | 认证令牌（建议通过环境变量设置） |
| `token_scopes` | map | `{}` | 令牌 → 权限范围映射 |
| `default_scopes` | []string | 见下方 | 默认权限范围 |
| `ip_whitelist_enabled` | bool | `false` | 是否启用 IP 白名单 |
| `allowed_cidrs` | []string | 见下方 | 允许的 CIDR 范围 |
| `rate_limit_enabled` | bool | `true` | 是否启用速率限制 |
| `requests_per_sec` | int | `10` | 每秒请求数限制 |
| `burst` | int | `20` | 突发请求配额 |

**默认权限范围：**

```yaml
default_scopes:
  - session:read   # 查看会话
  - stats:read    # 查看统计
  - health:read   # 查看健康状态
```

**默认允许的 CIDR：**

```yaml
allowed_cidrs:
  - 127.0.0.0/8   # 本地
  - 10.0.0.0/8    # 私有网络
```

### db

SQLite 数据库配置。

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `path` | string | `hotplex-worker.db` | 数据库文件路径 |
| `wal_mode` | bool | `true` | 启用 WAL 模式 |
| `busy_timeout` | duration | `500ms` | 锁等待超时 |
| `max_open_conns` | int | `1` | 最大连接数（SQLite 限制为 1） |

### security

安全配置。

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `api_key_header` | string | `X-API-Key` | API 密钥 HTTP 头 |
| `api_keys` | []string | `[]` | API 密钥列表 |
| `tls_enabled` | bool | `false` | 是否启用 TLS |
| `tls_cert_file` | string | - | TLS 证书文件路径 |
| `tls_key_file` | string | - | TLS 私钥文件路径 |
| `allowed_origins` | []string | `["*"]` | 允许的 CORS 来源 |
| `jwt_audience` | string | `hotplex-worker-gateway` | JWT audience 声明 |

> **注意**：`jwt_secret` 必须通过环境变量 `HOTPLEX_JWT_SECRET` 设置。

### session

会话生命周期配置。

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `retention_period` | duration | `168h` (7天) | 会话保留时长 |
| `gc_scan_interval` | duration | `1m` | 过期会话扫描间隔 |
| `max_concurrent` | int | `1000` | 最大并发会话数 |
| `event_store_enabled` | bool | `true` | 是否启用事件存储 |
| `event_store_type` | string | `sqlite` | 事件存储类型 |

### pool

会话池配置。

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `min_size` | int | `0` | 最小预热会话数 |
| `max_size` | int | `100` | 最大会话池大小 |
| `max_idle_per_user` | int | `3` | 每用户最大空闲会话 |
| `max_memory_per_user` | int64 | `2GB` | 每用户最大内存（字节） |

### worker

Worker 进程配置。

| 字段 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `max_lifetime` | duration | `24h` | Worker 最大生命周期 |
| `idle_timeout` | duration | `30m` | 空闲 Worker 清理超时 |
| `execution_timeout` | duration | `10m` | 单次执行超时 |
| `allowed_envs` | []string | `[]` | 额外允许的环境变量 |
| `env_whitelist` | []string | 见下方 | 始终允许的环境变量 |

**默认允许的环境变量：**

```yaml
env_whitelist:
  - PATH
  - HOME
  - USER
  - LANG
  - LC_ALL
  - TERM
  - TMPDIR
  - TEMP
  - TMP
```

## 环境变量

必需的环境变量（通过 `env.example` 模板设置）：

| 变量 | 必填 | 说明 |
|------|------|------|
| `HOTPLEX_JWT_SECRET` | 是 | JWT 签名密钥（32+ 字节） |
| `HOTPLEX_ADMIN_TOKEN_1` | 是 | 管理 API 令牌 1 |
| `HOTPLEX_ADMIN_TOKEN_2` | 否 | 管理 API 令牌 2（用于轮换） |

可选的环境变量：

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `HOTPLEX_API_KEYS` | - | 客户端 API 密钥（逗号分隔） |
| `HOTPLEX_LOG_LEVEL` | `info` | 日志级别 |
| `HOTPLEX_DB_PATH` | `hotplex-worker.db` | 数据库路径 |
| `HOTPLEX_GATEWAY_ADDR` | `:8888` | 网关地址 |
| `HOTPLEX_ADMIN_ADDR` | `:9999` | 管理 API 地址 |

## 环境特定配置

### 开发环境 (config-dev.yaml)

- 禁用 TLS
- 禁用速率限制
- 允许所有 CORS 来源
- 更短的超时和保留期
- 较小的资源限制

### 生产环境 (config-prod.yaml)

- 启用 TLS（必须配置证书）
- 启用速率限制
- 限制 CORS 来源
- 更长的超时和保留期
- 更大的资源限制
- 预热 10 个会话

## 热重载

以下配置项支持热重载（无需重启服务）：

- `gateway.ping_interval`
- `gateway.pong_timeout`
- `gateway.write_timeout`
- `gateway.idle_timeout`
- `gateway.broadcast_queue_size`
- `session.retention_period`
- `session.gc_scan_interval`
- `session.max_concurrent`
- `pool.*`
- `admin.*`

以下配置项修改后需要重启服务：

- `gateway.addr`（网络地址）
- `security.tls_*`（TLS 设置）
- `db.path`（数据库路径）
- `worker.*`（Worker 配置）

## 监控配置

监控组件通过 Docker Compose 启用：

```bash
# 启动监控栈
docker-compose --profile monitoring up -d
```

### Prometheus

- 配置：`configs/monitoring/prometheus.yml`
- 告警规则：`configs/monitoring/alerts.yml`
- SLO 定义：`configs/monitoring/slo.yaml`
- 端口：9090

### Grafana

- 配置：`configs/monitoring/grafana/`
- Dashboard：`configs/monitoring/grafana/dashboards/dashboard.json`
- Datasource：自动配置指向 Prometheus
- 端口：3000（默认账号 admin/admin）

### OpenTelemetry

- 采集器配置：`configs/monitoring/otel-collector-config.yaml`
- Gateway 支持通过 `OTEL_EXPORTER_OTLP_ENDPOINT` 环境变量配置

## 配置验证

启动时会自动验证配置：

```bash
./hotplex-worker -config configs/config.yaml
# 缺少必需配置时会输出错误
```
