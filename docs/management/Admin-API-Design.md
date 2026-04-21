---
type: design
tags:
  - project/HotPlex
  - api/admin
  - architecture/management
---

# Admin API Design

> HotPlex v1.0 Admin API 设计文档，提供 session 管理、统计查询、健康检查等管理能力。

---

## 1. 功能目标

### 1.1 核心能力

| 功能类别 | 用途 | 优先级 |
|---------|------|--------|
| **Session 管理** | CRUD 操作、强制终止、状态查询 | P0 |
| **统计监控** | 实时 metrics、历史趋势 | P0 |
| **健康检查** | Gateway/Worker/DB 健康状态 | P0 |
| **诊断工具** | 日志查询、配置验证、debug 端点 | P1 |

### 1.2 设计约束

- **独立通道**：Admin API 通过 HTTP REST，独立于 AEP WebSocket
- **强认证**：Admin Token + IP Whitelist（参见 [[security/Security-Authentication]] §4）
- **只读为主**：除 `DELETE /admin/sessions/{id}` 外，默认只读
- **低频访问**：管理端点，不参与实时消息流

---

## 2. API 端点设计

### 2.1 Session 管理

#### List Sessions

```http
GET /admin/sessions HTTP/1.1
Authorization: Bearer <admin_token>
```

**Query Parameters**：

| 参数 | 类型 | 说明 |
|------|------|------|
| `state` | string | 过滤状态：`created|running|idle|terminated|deleted` |
| `owner` | string | 过滤用户 ID |
| `worker_type` | string | 过滤 Worker 类型 |
| `limit` | int | 返回数量限制（默认 100，最大 1000） |
| `offset` | int | 分页偏移 |

**Response**：

```json
{
  "sessions": [
    {
      "id": "sess_abc123",
      "owner": "user_001",
      "state": "running",
      "worker_type": "claude-code",
      "created_at": 1710000000,
      "updated_at": 1710000100,
      "metadata": {
        "model": "claude-sonnet-4-6",
        "work_dir": "/var/hotplex/projects/session_abc123"
      }
    }
  ],
  "total": 150,
  "limit": 100,
  "offset": 0
}
```

---

#### Get Session Details

```http
GET /admin/sessions/{session_id} HTTP/1.1
Authorization: Bearer <admin_token>
```

**Response**：

```json
{
  "id": "sess_abc123",
  "owner": "user_001",
  "state": "running",
  "worker_type": "claude-code",
  "created_at": 1710000000,
  "updated_at": 1710000100,
  "metadata": {
    "model": "claude-sonnet-4-6",
    "work_dir": "/var/hotplex/projects/session_abc123"
  },
  "worker_process": {
    "pid": 12345,
    "pgid": 12345,
    "started_at": 1710000000,
    "uptime_seconds": 100,
    "memory_mb": 512,
    "cpu_percent": 25.0
  },
  "stats": {
    "events_sent": 150,
    "events_received": 10,
    "last_event_at": 1710000100,
    "tokens_used": 5000,
    "cost_usd": 0.25
  }
}
```

---

#### Kill Session（强制终止）

```http
DELETE /admin/sessions/{session_id} HTTP/1.1
Authorization: Bearer <admin_token>
```

**Request Body**（可选）：

```json
{
  "reason": "user_request",
  "notify_client": true,
  "grace_period_seconds": 5
}
```

**Response**：

```json
{
  "status": "terminated",
  "session_id": "sess_abc123",
  "worker_pid": 12345,
  "terminated_at": 1710000200,
  "termination_method": "SIGTERM→SIGKILL",
  "client_notified": true
}
```

**流程**：
1. Gateway 发送 `control.terminate`（AEP §3.4）
2. Worker 进程收到 SIGTERM
3. 5 秒后若未退出，发送 SIGKILL
4. 更新 Session 状态为 `TERMINATED`
5. GC 后转为 `DELETED`

---

### 2.2 统计监控

#### Get Stats Summary

```http
GET /admin/stats HTTP/1.1
Authorization: Bearer <admin_token>
```

**Response**：

```json
{
  "gateway": {
    "uptime_seconds": 86400,
    "websocket_connections": 50,
    "sessions_active": 45,
    "sessions_total": 150,
    "events_throughput_per_sec": 100
  },
  "workers": {
    "claude_code": {
      "sessions": 20,
      "processes": 20,
      "avg_memory_mb": 512,
      "avg_cpu_percent": 30.0
    },
    "opencode_server": {
      "sessions": 15,
      "processes": 15,
      "avg_memory_mb": 256,
      "avg_cpu_percent": 20.0
    },
    "opencode_server": {
      "sessions": 10,
      "processes": 1,
      "avg_memory_mb": 1024,
      "avg_cpu_percent": 50.0
    }
  },
  "database": {
    "sessions_count": 150,
    "audit_log_count": 5000,
    "message_store_count": 0,
    "db_size_mb": 50
  }
}
```

---

#### Get Real-time Metrics

```http
GET /admin/metrics HTTP/1.1
Authorization: Bearer <admin_token>
```

**Response**（Prometheus format）：

```text
# HotPlex Gateway Metrics
hotplex_gateway_uptime_seconds 86400
hotplex_gateway_ws_connections_active 50
hotplex_gateway_sessions_active 45
hotplex_gateway_events_sent_total 15000
hotplex_gateway_events_received_total 1000

# Worker Metrics
hotplex_worker_sessions{type="claude_code"} 20
hotplex_worker_memory_mb{type="claude_code"} 512
hotplex_worker_cpu_percent{type="claude_code"} 30.0

# Database Metrics
hotplex_db_sessions_count 150
hotplex_db_size_mb 50
```

---

### 2.3 健康检查

#### Gateway Health Check

```http
GET /admin/health HTTP/1.1
```

**Response**：

```json
{
  "status": "healthy",
  "checks": {
    "gateway": {
      "status": "healthy",
      "uptime_seconds": 86400
    },
    "database": {
      "status": "healthy",
      "type": "sqlite",
      "path": "/var/hotplex/data/hotplex-worker.db",
      "size_mb": 50
    },
    "workers": {
      "status": "healthy",
      "types": ["claude-code", "opencode-server"]
    }
  },
  "version": "v1.0",
  "config_path": "/etc/hotplex/config.yaml"
}
```

**Status Values**：
- `healthy`：所有检查通过
- `degraded`：部分功能降级（如 Worker 不可用）
- `unhealthy`：关键组件失败

---

#### Worker Health Check

```http
GET /admin/health/workers HTTP/1.1
Authorization: Bearer <admin_token>
```

**Response**：

```json
{
  "workers": [
    {
      "type": "claude-code",
      "status": "healthy",
      "available_sessions": 5,
      "test_passed": true
    },
    {
      "type": "opencode-server",
      "status": "healthy",
      "available_sessions": 5,
      "test_passed": true
    },
    {
      "type": "opencode-server",
      "status": "degraded",
      "error": "serve process crashed",
      "auto_restart_attempts": 3
    }
  ]
}
```

---

### 2.4 诊断工具

#### Get Logs

```http
GET /admin/logs HTTP/1.1
Authorization: Bearer <admin_token>
```

**Query Parameters**：

| 参数 | 类型 | 说明 |
|------|------|------|
| `level` | string | 日志级别：`debug|info|warn|error` |
| `session_id` | string | 过滤 session |
| `user_id` | string | 过滤用户 |
| `start_time` | int | Unix timestamp（开始时间） |
| `end_time` | int | Unix timestamp（结束时间） |
| `limit` | int | 返回数量（默认 100，最大 1000） |

**Response**：

```json
{
  "logs": [
    {
      "timestamp": 1710000000,
      "level": "info",
      "event": "session.create",
      "session_id": "sess_abc123",
      "user_id": "user_001",
      "message": "Session created successfully"
    },
    {
      "timestamp": 1710000100,
      "level": "warn",
      "event": "worker.timeout",
      "session_id": "sess_abc123",
      "message": "Worker execution timeout (30s)"
    }
  ],
  "total": 500,
  "limit": 100
}
```

---

#### Validate Config

```http
POST /admin/config/validate HTTP/1.1
Authorization: Bearer <admin_token>
Content-Type: application/json

{
  "config_path": "/etc/hotplex/config.yaml"
}
```

**Response**：

```json
{
  "valid": true,
  "warnings": [
    {
      "field": "gateway.tls.require_tls",
      "message": "TLS disabled in production environment"
    }
  ],
  "errors": []
}
```

---

#### Debug Session State

```http
GET /admin/debug/sessions/{session_id} HTTP/1.1
Authorization: Bearer <admin_token>
```

**Response**：

```json
{
  "session": {
    "id": "sess_abc123",
    "state": "running",
    "internal_state": {
      "input_queue_size": 5,
      "output_channel_capacity": 100,
      "last_seq_sent": 150,
      "mutex_locked": false
    }
  },
  "worker": {
    "pid": 12345,
    "cmdline": "claude --dir /var/hotplex/projects/session_abc123",
    "stdout_buffer_bytes": 512,
    "stderr_buffer_bytes": 128,
    "process_state": "running"
  },
  "websocket": {
    "connected": true,
    "client_ip": "192.168.1.100",
    "last_ping_at": 1710000050,
    "last_pong_at": 1710000060
  }
}
```

---

## 3. 认证授权

参见 [[security/Security-Authentication]] §4：

- Admin Token Bearer Authentication
- IP Whitelist Verification
- RBAC Permission Check

**权限矩阵**：

| 端点 | Required Permission |
|------|---------------------|
| `GET /admin/sessions` | `session:list` |
| `GET /admin/sessions/{id}` | `session:read` |
| `DELETE /admin/sessions/{id}` | `session:kill` |
| `GET /admin/stats` | `stats:read` |
| `GET /admin/metrics` | `stats:read` |
| `GET /admin/health` | `health:read` |
| `GET /admin/logs` | `logs:read` |
| `POST /admin/config/validate` | `config:validate` |
| `GET /admin/debug/*` | `debug:read` |

---

## 4. 实现架构

### 4.1 Admin Server

```go
type AdminServer struct {
    config       *AdminConfig
    sessionMgr   *SessionManager
    db           *sql.DB
    logger       *slog.Logger
    httpServer   *http.Server
}

func NewAdminServer(config *AdminConfig, sessionMgr *SessionManager) *AdminServer {
    s := &AdminServer{
        config:     config,
        sessionMgr: sessionMgr,
        logger:     slog.Default(),
    }

    // HTTP Router
    mux := http.NewServeMux()
    mux.HandleFunc("/admin/sessions", s.handleSessions)
    mux.HandleFunc("/admin/stats", s.handleStats)
    mux.HandleFunc("/admin/health", s.handleHealth)

    // Authentication Middleware
    authMiddleware := s.authMiddleware()

    s.httpServer = &http.Server{
        Addr:    config.Addr,
        Handler: authMiddleware(mux),
    }

    return s
}

func (s *AdminServer) Start() error {
    return s.httpServer.ListenAndServe()
}
```

---

### 4.2 Authentication Middleware

```go
func (s *AdminServer) authMiddleware() func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // 1. Extract Token
            token := extractBearerToken(r)
            if token == "" {
                respondError(w, 401, "missing_admin_token")
                return
            }

            // 2. Validate Token
            adminToken, err := s.validateAdminToken(token)
            if err != nil {
                respondError(w, 401, "invalid_admin_token")
                return
            }

            // 3. IP Whitelist Check
            if err := s.checkIPWhitelist(r); err != nil {
                respondError(w, 403, "ip_not_allowed")
                return
            }

            // 4. Permission Check
            requiredPerm := getRequiredPermission(r)
            if err := s.checkPermission(adminToken, requiredPerm); err != nil {
                respondError(w, 403, "permission_denied")
                return
            }

            // 5. Inject Token Context
            ctx := context.WithValue(r.Context(), "admin_token", adminToken)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}
```

---

### 4.3 Session List Handler

```go
func (s *AdminServer) handleSessions(w http.ResponseWriter, r *http.Request) {
    // Parse Query Parameters
    state := r.URL.Query().Get("state")
    owner := r.URL.Query().Get("owner")
    limit := parseIntParam(r, "limit", 100)

    // Query Sessions
    sessions, err := s.sessionMgr.ListSessions(state, owner, limit)
    if err != nil {
        respondError(w, 500, "internal_error")
        return
    }

    // Respond
    respondJSON(w, 200, map[string]interface{}{
        "sessions": sessions,
        "total":    len(sessions),
        "limit":    limit,
    })
}
```

---

## 5. 配置管理

### 5.1 Admin Config File

```yaml
# configs/admin.yaml
admin:
  server_addr: ":9999"

  auth:
    enabled: true
    tokens_config: "${HOTPLEX_ADMIN_TOKENS_CONFIG}"
    ip_whitelist_enabled: true
    allowed_cidrs:
      - "10.0.0.0/8"
      - "192.168.1.0/24"
      - "127.0.0.1/32"

  rate_limit:
    enabled: true
    requests_per_sec: 10
    burst: 20

  cors:
    enabled: false  # Admin API 不需要 CORS

  logging:
    level: "info"
    format: "json"
```

---

### 5.2 环境变量

```bash
# .env
HOTPLEX_ADMIN_SERVER_ADDR=:9999
HOTPLEX_ADMIN_TOKEN_1=admin_secret_001
HOTPLEX_ADMIN_TOKEN_2=admin_secret_002
```

---

## 6. 安全考虑

### 6.1 Rate Limiting

防止 API abuse：

```go
func (s *AdminServer) rateLimitMiddleware() func(http.Handler) http.Handler {
    limiter := rate.NewLimiter(rate.Limit(s.config.RateLimit.RequestsPerSec), s.config.RateLimit.Burst)

    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            if !limiter.Allow() {
                respondError(w, 429, "rate_limit_exceeded")
                return
            }
            next.ServeHTTP(w, r)
        })
    }
}
```

---

### 6.2 CORS Disabled

Admin API 不需要 CORS（仅内部访问）。

---

### 6.3 HTTPS（生产环境）

```yaml
admin:
  tls:
    enabled: true
    cert_file: "${HOTPLEX_ADMIN_TLS_CERT}"
    key_file: "${HOTPLEX_ADMIN_TLS_KEY}"
```

---

## 7. 实施检查清单

### Phase 1（Week 1）

- 实现 `/admin/sessions` 端点（List + Get）
- 实现 `DELETE /admin/sessions/{id}` 端点
- 实现 `/admin/stats` 端点
- 实现 `/admin/health` 端点
- 实现 Admin Token 认证 middleware
- 实现 IP Whitelist middleware
- 实现 Permission Check middleware

### Phase 2（Week 2）

- 实现 `/admin/metrics` 端点（Prometheus format）
- 实现 `/admin/logs` 端点
- 实现 `/admin/config/validate` 端点
- 实现 `/admin/debug/sessions/{id}` 端点
- 实现 Rate Limiting middleware
- 实现 HTTPS 支持（生产环境）
- 编写 Admin API 测试（单元测试 + 集成测试）

---

## 8. 集成示例

### 8.1 命令行工具

```bash
# 列出所有活跃 session
hotplexd admin sessions --state=running

# 强制终止 session
hotplexd admin kill sess_abc123 --reason=user_request

# 查看统计
hotplexd admin stats

# 健康检查
hotplexd admin health
```

---

### 8.2 监控集成

**Prometheus Scraping**：

```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'hotplex-admin'
    static_configs:
      - targets: ['hotplex:9999']
    metrics_path: '/admin/metrics'
    basic_auth:
      username: 'admin'
      password: '${HOTPLEX_ADMIN_TOKEN_1}'
```

---

## 9. API Reference

| Method | Path | Permission | Description |
|--------|------|------------|-------------|
| GET | `/admin/sessions` | `session:list` | List sessions |
| GET | `/admin/sessions/{id}` | `session:read` | Get session details |
| DELETE | `/admin/sessions/{id}` | `session:kill` | Kill session |
| GET | `/admin/stats` | `stats:read` | Get stats summary |
| GET | `/admin/metrics` | `stats:read` | Get Prometheus metrics |
| GET | `/admin/health` | `health:read` | Health check |
| GET | `/admin/health/workers` | `health:read` | Worker health check |
| GET | `/admin/logs` | `logs:read` | Get logs |
| POST | `/admin/config/validate` | `config:validate` | Validate config |
| GET | `/admin/debug/sessions/{id}` | `debug:read` | Debug session state |

---

## 10. 后续增强（v1.1）

- **Batch Operations**：批量终止 sessions
- **WebSocket Admin Channel**：实时事件流（复用 AEP）
- **Auto-scaling Trigger**：基于 metrics 自动扩展 Worker 池
- **History Export**：导出历史数据为 CSV/JSON
- **Dashboard UI**：Web 管理界面（React/Vue）