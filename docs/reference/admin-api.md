---
persona: enterprise
difficulty: advanced
---

# Admin API 完整参考

HotPlex Admin API 提供网关运维管理能力：会话管理、健康检查、监控指标、配置审计、日志查询和定时任务控制。默认监听 `localhost:9999`，独立于网关主端口（`8888`）。

## 认证

所有 Admin 端点（`/admin/health` 除外）均需 Bearer Token 认证。Token 通过以下两种方式传递：

```bash
# 方式一：Authorization header（推荐）
curl -H "Authorization: Bearer <token>" http://localhost:9999/admin/stats

# 方式二：Query string（适用于浏览器场景）
curl http://localhost:9999/admin/stats?access_token=<token>
```

Token 使用 `crypto/subtle.ConstantTimeCompare` 进行常量时间比较，防止时序攻击。

### Token 配置

```yaml
admin:
  enabled: true
  addr: "localhost:9999"
  tokens:                          # 简单 token，使用 default_scopes
    - "my-admin-token"
  token_scopes:                    # 细粒度 scope token
    "ops-token": ["session:read", "stats:read", "health:read"]
    "admin-token": ["session:read", "session:write", "session:delete", "stats:read", "health:read", "config:read", "config:write", "admin:read", "admin:write"]
  default_scopes: ["session:read", "stats:read", "health:read"]  # 简单 token 的默认 scope
```

### Scope 权限矩阵

| Scope | 健康检查 | 会话管理 | 监控 | 配置 | 日志/调试 | Cron |
|-------|---------|---------|------|------|----------|------|
| `health:read` | /health/workers, /health/ready | - | - | - | - | - |
| `session:read` | - | GET sessions, GET stats | - | - | - | - |
| `session:write` | - | POST create, POST terminate | - | - | - | - |
| `session:delete` | - | DELETE session | - | - | - | - |
| `stats:read` | - | - | GET stats, GET metrics | - | - | - |
| `config:read` | - | - | - | POST validate | - | - |
| `config:write` | - | - | - | POST rollback | - | - |
| `admin:read` | - | - | - | - | GET logs, GET debug | - |
| `admin:write` | - | - | - | - | - | Cron 写操作 |

## 安全中间件

按以下顺序执行：

1. **CORS** — `Access-Control-Allow-Origin: *`，允许 `GET/POST/DELETE/OPTIONS`
2. **Panic Recovery** — `defer recover()` 捕获 handler panic，返回 `500 Internal Server Error`
3. **Rate Limiting** — 令牌桶算法（默认 10 req/s，burst 20），超限返回 `429 Too Many Requests`
4. **IP Whitelist** — CIDR 匹配（默认 `127.0.0.0/8`, `10.0.0.0/8`），使用 `r.RemoteAddr` 防止 X-Forwarded-For 伪造
5. **Token Auth** — Bearer Token 提取 + scope 校验

Rate Limit 和 IP Whitelist 支持配置热重载，无需重启生效。

## 端点总览

### 健康检查

| 方法 | 路径 | Scope | 说明 |
|------|------|-------|------|
| GET | `/admin/health` | 无需认证 | 综合健康状态（gateway + DB + workers） |
| GET | `/admin/health/workers` | `health:read` | Worker 粒度健康状态 |
| GET | `/admin/health/ready` | `health:read` | 就绪探针（k8s readiness） |

**GET /admin/health** — 无需认证，适合负载均衡器探活。返回 `status`（healthy/degraded）、`checks`（gateway + database + workers）和 `version`。数据库不可用时降级为 `degraded`，`database.error` 附带错误信息。

**GET /admin/health/workers** — Worker 粒度健康状态，含 `workers[]`（healthy/type/pid）和 `checked_at`。任一 Worker 不健康时返回 `503`。

### 会话管理

| 方法 | 路径 | Scope | 说明 |
|------|------|-------|------|
| GET | `/admin/sessions` | `session:read` | 列出会话（分页） |
| GET | `/admin/sessions/{id}` | `session:read` | 获取单个会话 |
| POST | `/admin/sessions` | `session:write` | 创建会话 |
| DELETE | `/admin/sessions/{id}` | `session:delete` | 物理删除会话 |
| POST | `/admin/sessions/{id}/terminate` | `session:write` | 终止会话（状态迁移） |
| GET | `/admin/sessions/{id}/stats` | `session:read` | 会话 Turn 统计 |

**GET /admin/sessions** — 支持 query 参数过滤：

```bash
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9999/admin/sessions?limit=50&offset=0&platform=slack&user_id=U12345"
```

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `limit` | int | 100 | 每页数量 |
| `offset` | int | 0 | 偏移量 |
| `platform` | string | "" | 按平台过滤 |
| `user_id` | string | "" | 按用户过滤 |

**POST /admin/sessions** — 通过 query 参数创建：

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9999/admin/sessions?worker_type=claude_code&user_id=U12345"
```

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `session_id` | string | 自动生成 | 指定 session ID |
| `user_id` | string | "anonymous" | 用户标识 |
| `worker_type` | string | "claude_code" | Worker 类型 |

响应：

```json
{ "session_id": "550e8400-e29b-41d4-a716-446655440000" }
```

**POST /admin/sessions/{id}/terminate** — 将会话状态迁移至 `terminated`（软终止，保留记录）。DELETE 则为物理删除。

### 监控指标

| 方法 | 路径 | Scope | 说明 |
|------|------|-------|------|
| GET | `/admin/stats` | `stats:read` | 网关聚合统计 |
| GET | `/admin/metrics` | `stats:read` | Prometheus 格式指标 |

**GET /admin/stats** — 返回 `gateway`（uptime/websocket_connections/sessions_active/sessions_total）、`workers`（按 worker_type 分组统计）和 `database`（sessions_count/db_size_mb）。

### 配置管理

| 方法 | 路径 | Scope | 说明 |
|------|------|-------|------|
| POST | `/admin/config/validate` | `config:read` | 校验配置片段 |
| POST | `/admin/config/rollback` | `config:write` | 回滚到历史版本 |

**POST /admin/config/validate** — 校验配置合法性（不应用），请求体最大 1MB：

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"gateway":{"addr":":8888"},"pool":{"max_size":50}}' \
  http://localhost:9999/admin/config/validate
```

支持校验：`gateway`（buffer sizes >= 0）、`db`（path 长度 <= 4096）、`worker`（timeouts）、`pool`（max_size 1-10000）。返回 `{ "valid", "errors[]", "warnings[]" }`。

**POST /admin/config/rollback** — 回滚到指定版本，请求体 `{"version": 3}`，返回 `{ "ok", "rolled_back", "history_index" }`。无 configWatcher 时返回 `503`。

### 日志与调试

| 方法 | 路径 | Scope | 说明 |
|------|------|-------|------|
| GET | `/admin/logs` | `admin:read` | 最近日志（环形缓冲区） |
| GET | `/admin/debug/sessions/{id}` | `admin:read` | 会话调试快照 |

**GET /admin/logs** — 从 100 条环形缓冲区读取，`?limit=N`（最大 1000）。返回 `{ "logs[]", "total", "limit" }`。

**GET /admin/debug/sessions/{id}** — 会话详情 + 调试快照（`debug.available`、`has_worker`、`turn_count`、`last_seq_sent`、`worker_health`）。

### Cron 定时任务

| 方法 | 路径 | Scope | 说明 |
|------|------|-------|------|
| GET | `/api/cron/jobs` | `admin:read` | 列出所有任务 |
| GET | `/api/cron/jobs/{id}` | `admin:read` | 获取单个任务 |
| POST | `/api/cron/jobs` | `admin:write` | 创建任务 |
| PATCH | `/api/cron/jobs/{id}` | `admin:write` | 更新任务 |
| DELETE | `/api/cron/jobs/{id}` | `admin:write` | 删除任务 |
| POST | `/api/cron/jobs/{id}/run` | `admin:write` | 手动触发执行 |
| GET | `/api/cron/jobs/{id}/runs` | `admin:read` | 执行历史 |

Cron 未启用时返回 `503 Service Unavailable`。

**POST /api/cron/jobs** — JSON body 含 `name`、`schedule`（cron:/every:/at: 前缀）、`message`、`bot_id`、`owner_id`、`enabled`。返回 `201 Created`。

**PATCH /api/cron/jobs/{id}** — 部分更新，JSON body。返回 `204 No Content`。

**POST /api/cron/jobs/{id}/run** — 手动触发（异步），返回 `202 Accepted`。

**GET /api/cron/jobs/{id}/runs** — 查询执行历史。

## 错误响应格式

所有错误使用纯文本响应（`text/plain`），非 JSON：

| 状态码 | 含义 | 典型场景 |
|--------|------|----------|
| 400 | 请求无效 | JSON 解析失败、参数校验错误 |
| 401 | 未认证 | Token 缺失或无效 |
| 403 | 权限不足 | Scope 不满足 |
| 404 | 资源不存在 | Session/Cron Job 未找到 |
| 429 | 请求过频 | 触发 Rate Limit |
| 500 | 内部错误 | Handler panic、服务不可用 |
| 503 | 服务不可用 | 数据库故障、Cron 未启用 |

## 常用操作示例

```bash
# 快速健康检查（无需 Token）
curl http://localhost:9999/admin/health

# 查看活跃会话
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:9999/admin/sessions?limit=10

# 查看 Prometheus 指标
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:9999/admin/metrics

# 终止异常会话
curl -X POST -H "Authorization: Bearer $TOKEN" \
  http://localhost:9999/admin/sessions/abc-123/terminate

# 查看最近日志
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:9999/admin/logs?limit=20"

# 调试特定会话
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:9999/admin/debug/sessions/abc-123

# 触发 Cron 任务
curl -X POST -H "Authorization: Bearer $TOKEN" \
  http://localhost:9999/api/cron/jobs/daily-health/run
```
