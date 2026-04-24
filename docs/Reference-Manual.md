# HotPlex Worker Gateway — User Manual

> HotPlex Worker Gateway is a WebSocket-based access layer for AI Coding Agent sessions, supporting Claude Code, and OpenCode Server adapters.
>
> **Version:** `v1.0.1` (Git SHA injected at build time)
> **Binary:** `hotplex`
> **Protocol:** AEP v1 (Agent Exchange Protocol)
> **Runtime:** Go 1.26+

---

## Table of Contents

1. [Overview](#1-overview)
2. [Quick Start](#2-quick-start)
3. [Installation & Build](#3-installation--build)
4. [Configuration](#4-configuration)
5. [CLI Flags](#5-cli-flags)
6. [AEP WebSocket Protocol](#6-aep-websocket-protocol)
7. [Admin API Reference](#7-admin-api-reference)
8. [Session Lifecycle](#8-session-lifecycle)
   - [8.4 LLM Error Auto-Retry](#84-llm-error-auto-retry)
9. [Security](#9-security)
10. [Observability](#10-observability)
11. [Hot Reload](#11-hot-reload)
12. [Troubleshooting](#12-troubleshooting)

---

## 1. Overview

HotPlex Worker Gateway exposes a WebSocket interface (`AEP v1`) that bridges client sessions with underlying AI coding agent workers.

```
Client (WebSocket) ──→ Gateway (AEP v1) ──→ Worker (Claude Code / OpenCode)
                              │
                              ├── Admin HTTP API (:9999)
                              ├── SQLite (sessions + audit)
                              └── OTEL Tracing (optional)
```

### Supported Worker Types

| Type | Description | Protocol |
|------|-------------|----------|
| `claude-code` | Anthropic Claude Code CLI | stdio / NDJSON |
| `opencode-server` | OpenCode Server | HTTP / SSE |
| `pi-mono` | Pi-mono protocol | stdio (stub) |

### Key Architecture Properties

- **No hardcoded configuration** — binary runs with zero config; all values have production defaults
- **Config inheritance** — `inherits:` field chains config files (with cycle detection)
- **Hot reload** — file watcher debounces reloads (500ms), retains config history (64 versions) with rollback
- **Process isolation** — each worker runs in its own process group (PGID); 3-layer termination: SIGTERM → wait 5s → SIGKILL
- **JWT + API Key auth** — ES256 JWT with botID isolation; API key header configurable
- **Multi-layer security** — SSRF protection, command whitelist, env variable filtering, input validation

---

## 2. Quick Start

### 2.1 Run with Zero Config

```bash
./bin/hotplex-darwin-arm64
```

Binary starts with all defaults (`:8888` WebSocket, `:9999` Admin, `hotplex.db` SQLite).

### 2.2 Run with Config File

```bash
./bin/hotplex-darwin-arm64 -config /etc/hotplex/config.yaml
```

### 2.3 Run in Dev Mode

```bash
./bin/hotplex-darwin-arm64 -dev
```

Dev mode relaxes security: any API key header value is accepted.

### 2.4 Docker

```bash
docker run -p 8888:8888 -p 9999:9999 \
  -v /path/to/config.yaml:/config.yaml \
  -e HOTPLEX_JWT_SECRET=your-secret \
  hotplex:latest
```

---

## 3. Installation & Build

### 3.1 Build from Source

```bash
git clone https://github.com/hrygo/hotplex.git
cd hotplex
make build
```

Output: `bin/hotplex-<os>-<arch>` (e.g. `bin/hotplex-darwin-arm64`)

### 3.2 Cross-Compile

```bash
make build-all   # linux/amd64 + darwin/arm64
```

Output directory: `bin/`

### 3.3 PGO-Optimized Build

```bash
make build-pgo   # uses -pgo=auto
```

### 3.4 Development Tools

```bash
make setup        # install golangci-lint v1.64.8
make lint         # run linter
make lint-fix     # auto-fix lint issues
make test         # race-detected tests
make test-short   # quick tests (skip integration)
make coverage     # coverage report
```

---

## 4. Configuration

### 4.1 Config File Format

HotPlex uses [Viper](https://github.com/spf13/viper) — supports YAML, JSON, TOML.

```yaml
# config.yaml
gateway:
  addr: ":8888"
  ping_interval: 54s
  pong_timeout: 60s
  idle_timeout: 5m
  broadcast_queue_size: 256

db:
  path: "/var/hotplex/hotplex.db"
  wal_mode: true
  busy_timeout: 500ms

worker:
  max_lifetime: 24h
  idle_timeout: 30m
  execution_timeout: 10m
  env_whitelist:
    - HOME
    - PATH
    - CLAUDE_API_KEY
    - CLAUDE_MODEL
    - CLAUDE_BASE_URL

security:
  api_key_header: "X-API-Key"
  api_keys:
    - "sk-hotplex-secret-key-1"
    - "sk-hotplex-secret-key-2"
  tls_enabled: true
  tls_cert_file: "/etc/hotplex/tls.crt"
  tls_key_file: "/etc/hotplex/tls.key"
  jwt_audience: "hotplex-gateway"

session:
  retention_period: 168h    # 7 days
  gc_scan_interval: 1m
  max_concurrent: 1000
  event_store_enabled: true

pool:
  min_size: 0
  max_size: 100
  max_idle_per_user: 3
  max_memory_per_user: 2147483648   # 2 GB

admin:
  enabled: true
  addr: ":9999"
  tokens:
    - "admin-token-1"
    - "admin-token-2"
  token_scopes:
    "admin-token-1":
      - "session:read"
      - "session:write"
      - "session:delete"
      - "stats:read"
      - "health:read"
      - "admin:read"
      - "config:read"
  default_scopes:
    - "session:read"
    - "stats:read"
    - "health:read"
  ip_whitelist_enabled: true
  allowed_cidrs:
    - "127.0.0.0/8"
    - "10.0.0.0/8"
  rate_limit_enabled: true
  requests_per_sec: 10
  burst: 20

messaging:
  feishu:
    enabled: true
    dm_policy: "allowlist"
    group_policy: "allowlist"
    require_mention: true
    allow_dm_from: ["ou_dm_only"]
    allow_group_from: ["ou_group_only"]
    allow_from: ["ou_admin"]
  slack:
    enabled: true
    require_mention: true

inherits: "./defaults.yaml"   # optional: parent config
```

### 4.2 Environment Variable Expansion

Config values support `${VAR}` and `${VAR:-default}` syntax (Go's `os.ExpandEnv` **not** used — use the custom `ExpandEnv`).

```yaml
gateway:
  addr: "${HOTPLEX_GATEWAY_ADDR:-:8888}"

db:
  path: "${HOTPLEX_DB_PATH}"

security:
  tls_cert_file: "${HOTPLEX_TLS_CERT}"
  tls_key_file: "${HOTPLEX_TLS_KEY}"
```

Set `HOTPLEX_JWT_SECRET` for JWT authentication:

```bash
export HOTPLEX_JWT_SECRET="your-es256-secret-key-base64"
./hotplex -config config.yaml
```

### 4.3 Config Defaults

All non-sensitive fields have production defaults. Binary runs with zero config.

| Field | Default | Notes |
|-------|---------|-------|
| `gateway.addr` | `:8888` | WebSocket listen address |
| `gateway.ping_interval` | `54s` | |
| `gateway.pong_timeout` | `60s` | |
| `gateway.idle_timeout` | `5m` | |
| `gateway.broadcast_queue_size` | `256` | |
| `db.path` | `hotplex.db` | SQLite path |
| `db.wal_mode` | `true` | |
| `worker.max_lifetime` | `24h` | |
| `worker.idle_timeout` | `30m` | |
| `worker.execution_timeout` | `10m` | |
| `security.api_key_header` | `X-API-Key` | |
| `security.tls_enabled` | `false` | |
| `session.retention_period` | `7d` | |
| `session.gc_scan_interval` | `1m` | |
| `pool.max_size` | `100` | |
| `pool.max_memory_per_user` | `2GB` | |
| `admin.enabled` | `true` | |
| `admin.addr` | `:9999` | |
| `admin.rate_limit_enabled` | `true` | |
| `admin.requests_per_sec` | `10` | |
| `messaging.*.dm_policy` | `allowlist` | `open`, `allowlist`, `disabled` |
| `messaging.*.group_policy` | `allowlist` | `open`, `allowlist`, `disabled` |
| `messaging.*.require_mention` | `true` | |

### 4.4 Config Inheritance

Config files support inheritance with cycle detection:

```yaml
# base.yaml
pool:
  max_size: 50

# prod.yaml
inherits: "./base.yaml"
pool:
  max_size: 200
```

### 4.5 Hot Reload Fields

**Dynamic** (hot-reloadable without restart):

- `gateway.ping_interval`, `gateway.pong_timeout`, `gateway.idle_timeout`
- `gateway.broadcast_queue_size`
- `pool.max_size`, `pool.max_idle_per_user`
- `session.gc_scan_interval`
- `admin.rate_limit_enabled`, `admin.requests_per_sec`

**Static** (requires restart):

- `gateway.addr`, `gateway.tls_*`
- `db.path`
- `security.*` (except JWT runtime rotation)
- `worker.max_lifetime`, `worker.idle_timeout`, `worker.env_whitelist`

---

## 5. CLI Flags

```bash
./hotplex [flags]
```

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `-config` | string | `""` | Path to YAML/JSON/TOML config file |
| `-dev` | bool | `false` | Enable dev mode (relaxed security, any API key accepted) |

No `-version` or `-help` flag is defined (falls through to standard `flag` package usage output).

---

## 6. AEP WebSocket Protocol

### 6.1 Connection

```javascript
const ws = new WebSocket("ws://localhost:8888");
```

### 6.2 Authentication

Send API key via configured header (`X-API-Key` by default):

```javascript
ws.addEventListener("open", () => {
  ws.send(JSON.stringify({
    type: "auth",
    api_key: "sk-hotplex-secret-key-1"
  }));
});
```

### 6.3 Session Init

```javascript
// Client → Gateway
ws.send(JSON.stringify({
  type: "session.init",
  session_id: "sess_abc123",       // optional; auto-generated if omitted
  worker_type: "claude-code",
  user_id: "user_001",
  metadata: {
    model: "claude-sonnet-4-6",
    work_dir: "/projects/my-app"
  }
}));

// Gateway → Client
ws.send(JSON.stringify({
  type: "session.init_ack",
  session_id: "sess_abc123",
  status: "ok"
}));
```

### 6.4 Input Events

```javascript
// Send user input to worker
ws.send(JSON.stringify({
  type: "input",
  session_id: "sess_abc123",
  text: "Write a hello world in Go"
}));
```

### 6.5 Output Events (Gateway → Client)

| `type` | Description |
|--------|-------------|
| `stream` | Streaming delta content |
| `stream_event` | Structured event (tool_use, tool_result, etc.) |
| `state` | Session state change |
| `done` | Worker finished, includes stats |
| `error` | Error event |
| `ping` / `pong` | Keepalive |
| `control` | Control signal (interrupt, resume, etc.) |

### 6.6 Session State Machine

```
created → running → idle ↔ running → terminated → deleted
                      └──────────────→ terminated
```

### 6.7 Control Signals

```javascript
// Interrupt worker
ws.send(JSON.stringify({
  type: "control",
  action: "interrupt",
  session_id: "sess_abc123"
}));

// Resume worker
ws.send(JSON.stringify({
  type: "control",
  action: "resume",
  session_id: "sess_abc123"
}));

// Terminate worker
ws.send(JSON.stringify({
  type: "control",
  action: "terminate",
  session_id: "sess_abc123"
}));
```

### 6.8 Envelope Format

All AEP v1 messages use NDJSON over WebSocket. Each line is a JSON object:

```json
{"id":"msg_001","v":1,"seq":1,"session_id":"sess_abc123","event":{"type":"stream","data":{"delta":"Hello"}}}
```

Full protocol specification: `docs/architecture/AEP-v1-Protocol.md`

---

## 7. Admin API Reference

Admin API runs on `:9999` (configurable). All endpoints require Bearer token authentication unless IP whitelist bypass is configured.

### 7.1 Authentication

```http
Authorization: Bearer <admin_token>
```

Tokens and scopes are configured in `admin.tokens` and `admin.token_scopes`.

### 7.2 Endpoints

#### `GET /admin/health`

Health check. No auth required.

```bash
curl http://localhost:9999/admin/health
```

```json
{
  "status": "healthy",
  "checks": {
    "gateway": { "status": "healthy", "uptime_seconds": 3600 },
    "database": { "status": "healthy", "type": "sqlite", "path": "/var/hotplex/hotplex.db" },
    "workers": { "status": "healthy" }
  },
  "version": "88e4e3e8"
}
```

#### `GET /admin/health/workers`

Worker health check. Requires `health:read`.

```bash
curl -H "Authorization: Bearer admin-token-1" \
  http://localhost:9999/admin/health/workers
```

```json
{
  "status": "ok",
  "workers": [
    { "type": "claude-code", "healthy": true, "sessions": 5 },
  ]
}
```

#### `GET /admin/stats`

Stats summary. Requires `stats:read`.

```bash
curl -H "Authorization: Bearer admin-token-1" \
  http://localhost:9999/admin/stats
```

```json
{
  "gateway": {
    "uptime_seconds": 3600,
    "websocket_connections": 10,
    "sessions_active": 8,
    "sessions_total": 50
  },
  "workers": {
    "claude-code": { "sessions": 5 },
  }
}
```

#### `GET /admin/pool`

Pool statistics. Requires `stats:read`.

```bash
curl -H "Authorization: Bearer admin-token-1" \
  http://localhost:9999/admin/pool
```

```json
{ "total": 8, "max": 100, "users": 3 }
```

#### `POST /admin/sessions`

Create a new session. Requires `session:write`.

```bash
curl -X POST \
  -H "Authorization: Bearer admin-token-1" \
  "http://localhost:9999/admin/sessions?user_id=user_001&worker_type=claude-code"
```

```json
{ "session_id": "sess_xyz789" }
```

#### `GET /admin/sessions`

List sessions. Requires `session:read`.

```bash
curl -H "Authorization: Bearer admin-token-1" \
  "http://localhost:9999/admin/sessions?limit=20&offset=0"
```

Query params: `limit` (default 100), `offset`

#### `GET /admin/sessions/:id`

Get session details. Requires `session:read`.

```bash
curl -H "Authorization: Bearer admin-token-1" \
  http://localhost:9999/admin/sessions/sess_abc123
```

#### `DELETE /admin/sessions/:id`

Terminate a session. Requires `session:delete`.

```bash
curl -X DELETE \
  -H "Authorization: Bearer admin-token-1" \
  http://localhost:9999/admin/sessions/sess_abc123
```

Returns `204 No Content`.

#### `POST /admin/sessions/:id/terminate`

Send terminate signal to session worker. Requires `session:write`.

```bash
curl -X POST \
  -H "Authorization: Bearer admin-token-1" \
  http://localhost:9999/admin/sessions/sess_abc123/terminate
```

Returns `204 No Content`.

#### `GET /admin/config/history`

Config change audit log. Requires `config:read`.

```bash
curl -H "Authorization: Bearer admin-token-1" \
  http://localhost:9999/admin/config/history
```

#### `POST /admin/config/rollback/:version`

Rollback config to a previous version. Requires `config:write`.

```bash
curl -X POST \
  -H "Authorization: Bearer admin-token-1" \
  http://localhost:9999/admin/config/rollback/3
```

#### `POST /admin/config/validate`

Validate a config update before applying. Requires `config:read`.

```http
POST /admin/config/validate
Content-Type: application/json

{
  "gateway": { "broadcast_queue_size": 512 },
  "pool": { "max_size": 200 }
}
```

```json
{
  "valid": true,
  "warnings": [],
  "errors": []
}
```

#### `GET /admin/logs`

Recent log entries from the in-memory ring buffer. Requires `admin:read`.

```bash
curl -H "Authorization: Bearer admin-token-1" \
  "http://localhost:9999/admin/logs?limit=50"
```

### 7.3 Scope Matrix

| Scope | Endpoints |
|-------|-----------|
| `session:read` | GET sessions, GET sessions/:id |
| `session:write` | POST sessions, POST sessions/:id/terminate |
| `session:delete` | DELETE sessions/:id |
| `stats:read` | GET stats, GET pool |
| `health:read` | GET health, GET health/workers |
| `config:read` | GET config/history, POST config/validate |
| `config:write` | POST config/rollback |
| `admin:read` | GET logs |

---

## 8. Session Lifecycle

### 8.1 Session States

| State | Description |
|-------|-------------|
| `created` | Session initialized, worker not started |
| `running` | Worker process active, accepting input |
| `idle` | Worker idle, waiting for input |
| `terminated` | Worker exited (normal or forced) |
| `deleted` | Session cleaned up by GC |

### 8.2 State Transitions

```
session.init     → created
worker started   → running
no input (30m)  → idle
input received  → running
control.terminate→ terminated
GC (after 7d)   → deleted
```

### 8.3 Session Garbage Collection

- Idle sessions (no activity) are cleaned up after `session.retention_period` (default 7 days)
- GC scan runs every `session.gc_scan_interval` (default 1 minute)
- Terminated sessions are immediately eligible for GC

### 8.4 LLM Error Auto-Retry

When the AI provider returns temporary errors (429 rate limit, 529 overload, network issues, 5xx errors), the gateway automatically retries with exponential backoff — no manual "继续" needed.

- **Enabled by default** — configurable via `worker.auto_retry.enabled`
- **Max 3 retries** — configurable via `worker.auto_retry.max_retries`
- **Backoff**: 5s → 10s → 20s (with ±25% jitter, cap at 120s)
- **User interrupt**: Sending a new message cancels pending retry immediately
- **Notifications**: User sees `🔄 遇到临时错误，正在自动重试...` during retry

See [[management/Config-Reference]] for full configuration options.

---

## 9. Security

### 9.1 Authentication

**API Key**: Clients send API key via `X-API-Key` header (configurable). Keys are in `security.api_keys`.

**JWT**: If a `JWTValidator` is configured, a Bearer token in the `Authorization` header is validated. The `bot_id` claim in the JWT must match the session's bot ID (SEC-007 isolation).

**Dev Mode**: `-dev` flag accepts any API key header value.

### 9.2 Command Whitelist

Only two binaries are allowed to run as workers:

```go
var AllowedCommands = map[string]bool{
    "claude":   true,
    "opencode": true,
}
```

### 9.3 Environment Variable Isolation

- **Protected vars** (cannot be set by Worker): `HOME`, `PATH`, `GOPATH`, `GOROOT`, `CLAUDECODE`, `GATEWAY_ADDR`, `GATEWAY_TOKEN`
- **Sensitive prefixes** (auto-redacted in logs): `AWS_`, `AZURE_`, `GITHUB_`, `ANTHROPIC_`, `OPENAI_`, `SECRET`, `PASSWORD`
- **Nested agent prevention**: `CLAUDECODE=` env var is stripped from worker env to prevent nested sessions

### 9.4 SSRF Protection

Only `http://` and `https://` URLs are allowed. Blocked:

- Private IP ranges: `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, `127.0.0.0/8`
- Link-local: `169.254.0.0/16` (includes AWS metadata at `169.254.169.254`)
- IPv6: `::1`, `fc00::/7`, `fe80::/10`
- Blocked hostnames: `localhost`, `metadata.google.internal`

### 9.5 Input Validation

- Null bytes (`\x00`) rejected
- Max envelope size: 1MB
- Max input field length: 1MB
- Path traversal: `SafePathJoin` validates resolved paths stay within base directory

### 9.6 TLS

```yaml
security:
  tls_enabled: true
  tls_cert_file: "/etc/hotplex/tls.crt"
  tls_key_file: "/etc/hotplex/tls.key"
```

Warning is issued if TLS is disabled on a non-local address.

---

## 10. Observability

### 10.1 Structured Logging

HotPlex uses `log/slog` with JSON output to stdout:

```json
{"time":"2026-04-02T13:40:41+08:00","level":"INFO","msg":"gateway: starting","version":"88e4e3e8","go":"go1.26.0","addr":":8888","config":""}
```

Key log fields:

| Field | Description |
|-------|-------------|
| `service.name` | Always `hotplex-gateway` |
| `session_id` | Attached to session-scoped log entries |
| `user_id` | Authenticated user |
| `bot_id` | Bot ID from JWT (if present) |

### 10.2 Prometheus Metrics

Metrics endpoint: `GET /metrics` on the admin port (`:9999`).

Enabled by importing `github.com/prometheus/client_golang/prometheus/promhttp`.

### 10.3 OpenTelemetry Tracing

Tracing is disabled by default. Enable via OTEL environment variables:

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT="http://collector:4317"
./hotplex
```

### 10.4 Health Checks

- `GET /admin/health` — overall gateway + DB + workers health
- `GET /admin/health/workers` — per-worker-type health with test results

---

## 11. Hot Reload

### 11.1 How It Works

HotPlex watches the config file's directory using `fsnotify`. On file change:

1. Debounce: wait 500ms for file to settle
2. Load new config (inheritance chain + env expansion)
3. Validate: return early on error (log warning, keep running)
4. Diff: compute changed fields
5. Dynamic fields: apply immediately
6. Static fields: log warning, require restart
7. Audit: record change in audit log

### 11.2 Config History

Last 64 config versions are retained in memory. View via `GET /admin/config/history`.

### 11.3 Rollback

```bash
curl -X POST \
  -H "Authorization: Bearer admin-token-1" \
  http://localhost:9999/admin/config/rollback/5
```

Rollback to version `N` steps back from current (not absolute version number).

### 11.4 Static Field Changes

Changes to static fields (`gateway.addr`, `db.path`, etc.) require a restart. The binary logs a warning but continues running with the old config.

---

## 12. Troubleshooting

### 12.1 Binary Won't Start

**Error: `config: missing required secrets: security.jwt_secret`**

Set `HOTPLEX_JWT_SECRET` environment variable:

```bash
export HOTPLEX_JWT_SECRET="your-256-bit-secret"
./hotplex
```

**Error: `config: read "config.yaml": no such file or directory`**

Verify config file path. Use absolute path:

```bash
./hotplex -config /absolute/path/to/config.yaml
```

**Error: `TLS is disabled on non-local address`**

Either enable TLS or bind to localhost:

```yaml
gateway:
  addr: "127.0.0.1:8888"   # localhost
# OR
security:
  tls_enabled: true
  tls_cert_file: "/etc/hotplex/tls.crt"
  tls_key_file: "/etc/hotplex/tls.key"
```

### 12.2 WebSocket Connection Refused

Ensure the gateway is listening on the expected address:

```bash
# Check startup log
{"msg":"gateway: listening","addr":":8888"}

# Test connection
curl -v http://localhost:8888   # should get "400 Bad Request" (not WS upgrade)
```

### 12.3 Authentication Failures

**401 Unauthorized**: Verify API key matches one in `security.api_keys`:

```yaml
security:
  api_keys:
    - "sk-hotplex-secret-key-1"   # must match client header
```

**JWT validation failed**: Ensure JWT is signed with ES256 and `jwt_audience` matches:

```bash
export HOTPLEX_JWT_SECRET="$(echo -n 'your-secret' | base64)"
```

### 12.4 Worker Not Starting

Check worker binary is in `PATH`:

```bash
which claude     # for claude-code worker
```

Worker logs go to stderr (not captured by HotPlex). Run worker manually to diagnose:

```bash
claude --dir /tmp/session --json-stream
```

### 12.5 High Memory Usage

- Check pool config: `pool.max_size`, `pool.max_memory_per_user`
- Session GC may be backlogged: verify `session.gc_scan_interval` and `session.retention_period`
- Worker processes may not be cleaning up: check process tree

### 12.6 Config Hot Reload Not Working

- Verify file watcher has permissions on config directory
- Check log for reload events: `config reloaded successfully` or `failed to reload`
- Static field changes don't trigger reload — must restart

### 12.7 Race Detection Failures

Run tests with race detector:

```bash
make test        # full race test (up to 15m)
make test-short  # quick race test (up to 5m)
```

### 12.8 Build Issues

**`command not found: golangci-lint`**

```bash
make setup       # install golangci-lint v1.64.8
```

**PGO build fails**

PGO requires a prior profile run. Use standard build first:

```bash
make build       # no PGO
make build-pgo   # PGO (requires prior profiling data)
```
