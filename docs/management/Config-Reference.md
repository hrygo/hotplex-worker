# Configuration Management

> **Purpose**: Complete guide to configuring HotPlex Worker Gateway
>
> **Last Updated**: 2026-04-21

---

## Quick Start

### Development (Minimal Config)

```bash
# 1. Set required secret
export HOTPLEX_JWT_SECRET="dev-secret-change-in-production"

# 2. Run with defaults
./hotplex-worker

# Gateway: ws://localhost:8888
# Admin:  http://localhost:9999
```

### Production (Full Config)

```bash
# 1. Create secrets (use vault in production)
source /etc/hotplex/secrets.env

# 2. Run with production config
./hotplex-worker -config configs/config-prod.yaml

# 3. Verify health
curl http://localhost:9999/admin/health
```

---

## Configuration Hierarchy

Priority (highest to lowest):

1. **Command-line flags**: `-gateway.addr :9999`
2. **Environment variables**: `HOTPLEX_GATEWAY_ADDR`
3. **Config file**: `configs/config-prod.yaml`
4. **Code defaults**: `internal/config/config.go:Default()`

Example:

```yaml
# configs/config.yaml
gateway:
  addr: ":8888"    # Priority 3 (config file)
```

```bash
# Environment override
export HOTPLEX_GATEWAY_ADDR=":7777"  # Priority 2 (env var)
```

```bash
# Command-line override (highest priority)
./hotplex-worker -gateway.addr :6666  # Priority 1 (flag)
```

Result: Gateway listens on `:6667`

---

## Configuration Files

### configs/config.yaml

**Purpose**: Complete reference configuration with all options documented

**Use when**:
- First-time setup (understand all available options)
- Reference for configuration structure
- Base for environment-specific configs (via `inherits:`)

**Key sections**:
- Core network (`gateway`, `admin`)
- Data persistence (`db`)
- Security (`security`)
- Session lifecycle (`session`, `pool`)
- Worker processes (`worker`)

### configs/config-dev.yaml

**Purpose**: Development environment with relaxed security

**Use when**: Local development, testing

**Differences from production**:
- TLS disabled
- Rate limiting disabled
- Lower resource limits
- Shorter timeouts

### configs/config-prod.yaml

**Purpose**: Production environment with hardened security

**Use when**: Production deployment, staging

**Differences from development**:
- TLS enabled (mandatory)
- Rate limiting enabled
- Higher resource limits
- Stricter CORS

### configs/env.example

**Purpose**: Environment variable template

**Use when**: Setting up secrets, Docker deployments, Kubernetes ConfigMaps

**Copy to**: `.env` (never commit!)

---

## Required Secrets

### JWT Secret

**Purpose**: Validates session tokens from AI Coding Agents

**Generate**:
```bash
openssl rand -base64 32 | tr -d '\n'
```

**Set via**:
```bash
# Environment variable
export HOTPLEX_JWT_SECRET="your-secret-here"

# Config file (NOT RECOMMENDED for secrets)
# config.yaml:
# security:
#   jwt_secret: "your-secret-here"  # Don't do this!
```

**Security**:
- **Minimum length**: 32 bytes (256 bits)
- **Rotation**: Rotate every 90 days (invalidate all sessions)
- **Storage**: Use vault (HashiCorp Vault, AWS Secrets Manager, etc.)

### Admin Tokens

**Purpose**: Authenticate Admin API requests

**Generate**:
```bash
# Token 1 (primary)
openssl rand -base64 32 | tr -d '/+=' | head -c 43

# Token 2 (backup/rotation)
openssl rand -base64 32 | tr -d '/+=' | head -c 43
```

**Set via**:
```bash
# Environment variables
export HOTPLEX_ADMIN_TOKEN_1="token-1-here"
export HOTPLEX_ADMIN_TOKEN_2="token-2-here"
```

**Security**:
- **Minimum length**: 43 characters (256 bits)
- **Rotation**: Rotate every 30 days (keep both tokens during rotation)
- **Scopes**: Assign minimal required scopes per token

**Token rotation**:
```bash
# 1. Generate new token
NEW_TOKEN=$(openssl rand -base64 32 | tr -d '/+=' | head -c 43)

# 2. Set as TOKEN_2 (keep TOKEN_1 active)
export HOTPLEX_ADMIN_TOKEN_2="$NEW_TOKEN"

# 3. Update clients to use TOKEN_2
# 4. After 24h, rotate TOKEN_1
export HOTPLEX_ADMIN_TOKEN_1="$(openssl rand -base64 32 | tr -d '/+=' | head -c 43)"

# 5. Remove old token from clients
```

---

## Environment Variables

### Naming Convention

Pattern: `HOTPLEX_<SECTION>_<FIELD>`

Examples:
```bash
HOTPLEX_GATEWAY_ADDR=:8888
HOTPLEX_DB_PATH=/var/lib/hotplex/data/hotplex-worker.db
HOTPLEX_SECURITY_TLS_ENABLED=true
HOTPLEX_ADMIN_RATE_LIMIT_ENABLED=false
```

### Special Cases

**JWT Secret**: `HOTPLEX_JWT_SECRET` (not `HOTPLEX_SECURITY_JWT_SECRET`)

**Admin Tokens**:
- `HOTPLEX_ADMIN_TOKEN_1`
- `HOTPLEX_ADMIN_TOKEN_2`

**API Keys**: `HOTPLEX_API_KEYS` (comma-separated list)

### List/Map Fields

**Comma-separated for lists**:
```bash
export HOTPLEX_SECURITY_ALLOWED_ORIGINS="https://app.com,https://admin.com"
```

**JSON for maps** (advanced):
```bash
export HOTPLEX_ADMIN_TOKEN_SCOPES='{"token_1": ["session:read", "stats:read"]}'
```

---

## Speech-to-Text (STT) Configuration

Audio messages received via messaging platforms (Feishu, Slack) are automatically transcribed to text using a local STT engine. The engine runs as a persistent subprocess to avoid per-request model loading overhead (~2-3s cold start).

### Configuration Fields

All STT fields are under the `feishu` section in `config.yaml`:

```yaml
messaging:
  feishu:
    # STT provider: "feishu" (cloud API), "local" (external command),
    # "feishu+local" (cloud primary, local fallback), "" (disabled)
    stt_provider: "local"

    # Local STT command template. No {file} placeholder needed for persistent mode.
    stt_local_cmd: "python3 scripts/stt_server.py --model iic/SenseVoiceSmall"

    # Local STT mode: "ephemeral" (per-request process) or "persistent" (long-lived subprocess)
    # Persistent mode keeps the model in memory, avoiding ~2-3s cold start per request.
    stt_local_mode: "persistent"

    # Idle timeout for persistent mode. Subprocess auto-shuts down after this duration
    # with no transcription requests. 0 = disabled (never auto-shutdown).
    stt_local_idle_ttl: "10m"
```

### Provider Options

| Provider | Description | Disk Required | Latency |
|----------|-------------|---------------|---------|
| `""` | Disabled — audio messages not transcribed | N/A | N/A |
| `"feishu"` | Cloud API — Feishu `speech_to_text` endpoint | No | ~500ms + network |
| `"local"` | Local engine — SenseVoice-Small via funasr-onnx | Yes | ~350ms (ONNX FP32) |
| `"feishu+local"` | Fallback — cloud primary, local fallback on failure | Yes | ~350-500ms |

### STT Engine Details

- **Model**: SenseVoice-Small (`iic/SenseVoiceSmall`), ~900MB
- **Backend**: `funasr-onnx` ONNX FP32 (non-quantized)
- **Languages**: Chinese (zh), English (en), Japanese (ja), Korean (ko), Cantonese (yue)
- **Performance**: ~0.35s inference per file (ONNX FP32), CER ~2% (Chinese)
- **Model cache**: `~/.cache/modelscope/hub/models/iic/SenseVoiceSmall/`
- **Auto-patch**: ONNX model `Less` node type mismatch is automatically patched on first load by `scripts/fix_onnx_model.py`

### Persistent Mode Protocol

The persistent subprocess communicates via JSON-over-stdio:

```
Go → subprocess stdin:  {"audio_path": "/tmp/.../stt_123.opus"}\n
Subprocess → Go stdout: {"text": "transcription result", "error": ""}\n
```

The subprocess keeps the model loaded in memory. Lifecycle:
- **Lazy start**: Launched on first transcription request
- **Idle timeout**: Auto-shutdown after `stt_local_idle_ttl` (configurable)
- **Crash recovery**: Detected on next request, subprocess auto-restarts
- **Graceful shutdown**: SIGTERM → 5s wait → SIGKILL, PGID isolation

### Environment Variables

```bash
HOTPLEX_MESSAGING_FEISHU_REQUIRE_MENTION=true # Default: true
HOTPLEX_MESSAGING_FEISHU_DM_POLICY=allowlist  # open | allowlist | disabled (default: allowlist)
HOTPLEX_MESSAGING_FEISHU_GROUP_POLICY=allowlist # open | allowlist | disabled (default: allowlist)
HOTPLEX_MESSAGING_FEISHU_STT_PROVIDER=local
HOTPLEX_MESSAGING_FEISHU_STT_LOCAL_CMD="python3 /path/to/stt_server.py"
HOTPLEX_MESSAGING_FEISHU_STT_LOCAL_MODE=persistent
HOTPLEX_MESSAGING_FEISHU_STT_LOCAL_IDLE_TTL=10m
```

---

## LLM Provider Error Auto-Retry

When Claude Code encounters temporary errors (429 rate limit, 529 overload, network errors, 5xx server errors), the gateway can automatically detect → wait with exponential backoff → send "继续" to retry the turn. This eliminates the need for users to manually type "继续" when rate limits occur.

### Configuration Fields

```yaml
worker:
  auto_retry:
    enabled: true               # Enable auto-retry (default: true)
    max_retries: 3             # Maximum retry attempts (default: 3)
    base_delay: 5s              # Initial delay between retries (default: 5s)
    max_delay: 120s             # Maximum delay cap (default: 120s)
    retry_input: "继续"          # Text sent to worker on retry (default: "继续")
    notify_user: true            # Show retry notification to user (default: true)
    patterns: []                 # Custom regex patterns (empty = use built-in defaults)
```

### Error Patterns

Built-in patterns (case-insensitive regex, applied at turn completion):

| Pattern | Matches |
|---------|--------|
| `429` / `rate limit` / `too many requests` | Rate limit errors |
| `529` / `overloaded` / `service unavailable` | Service overload |
| `API Error.*reject` | API rejection errors |
| `network` / `connection reset` / `ECONNREFUSED` / `timeout` | Network errors |
| `500` / `502` / `503` / `server error` | Server-side errors |

### Backoff Strategy

- **Formula**: `base_delay × 2^(attempt-1) ± 25% jitter`
- **Example**: Attempt 1 → ~5s, Attempt 2 → ~10s, Attempt 3 → ~20s
- **Caps at**: `max_delay` (default 120s)
- **User interrupt**: If the user sends a new message during backoff, retry is cancelled immediately

### User Notifications

When `notify_user: true`, the gateway sends a synthetic `message` event to the user:

```
🔄 遇到临时错误，正在自动重试 (1/3)...
```

After all retries are exhausted:

```
⚠️ 自动重试已耗尽 (3次)，请手动发送「继续」或重新提问。
```

### Environment Variables

```bash
HOTPLEX_WORKER_AUTO_RETRY_ENABLED=true
HOTPLEX_WORKER_AUTO_RETRY_MAX_RETRIES=3
HOTPLEX_WORKER_AUTO_RETRY_BASE_DELAY=5s
HOTPLEX_WORKER_AUTO_RETRY_MAX_DELAY=120s
HOTPLEX_WORKER_AUTO_RETRY_RETRY_INPUT=继续
HOTPLEX_WORKER_AUTO_RETRY_NOTIFY_USER=true
```

### Disabling Auto-Retry

```yaml
worker:
  auto_retry:
    enabled: false  # Disable if you prefer manual control
```

---

## Messaging Access Control

Messaging platforms (Slack, Feishu) support granular access control to ensure the bot only responds to authorized users in specific contexts.

### Configuration Fields

Both Slack and Feishu adapters support these common fields:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `dm_policy` | string | `allowlist` | Policy for direct messages (`open`, `allowlist`, `disabled`). |
| `group_policy` | string | `allowlist` | Policy for channels/groups (`open`, `allowlist`, `disabled`). |
| `require_mention`| bool | `true` | If true, the bot only responds in groups when @mentioned. |
| `allow_from` | []string| `[]` | Global whitelist (authorized for both DM and groups). |
| `allow_dm_from`| []string| `[]` | Whitelist specifically for direct messages. |
| `allow_group_from`| []string| `[]` | Whitelist specifically for groups/channels. |

### Policies

| Policy | Description |
|--------|-------------|
| `open` | (Default) Allows all users to interact with the bot. |
| `allowlist` | Only allows users listed in `allow_from` to interact. |
| `disabled` | Completely disables the bot for that chat type. |

### Example: Locked-down Group

In this example, the bot is restricted to specific users in groups and requires an @mention.

```yaml
messaging:
  feishu:
    enabled: true
    dm_policy: "allowlist"
    group_policy: "allowlist"
    require_mention: true
    allow_from:
      - "ou_12345678"  # Only this user can trigger the bot in groups
```

### Example: Disabled DMs

```yaml
messaging:
  slack:
    dm_policy: "disabled"  # Users cannot interact with the bot in DMs
```

---

## Configuration Validation

### Validate Config File

```bash
# Check syntax and required fields
./hotplex-worker -config configs/config-prod.yaml -validate

# Expected output:
# Config validation successful
# Gateway: :8888
# Admin: :9999
# TLS: enabled
# Database: hotplex-worker.db
```

### Validate Secrets

```bash
# Set secrets
export HOTPLEX_JWT_SECRET="test-secret"
export HOTPLEX_ADMIN_TOKEN_1="test-token"

# Run with validation
./hotplex-worker -config configs/config.yaml -validate

# Should succeed if all required secrets are set
```

### Common Validation Errors

**Missing JWT secret**:
```
Error: config: missing required secrets: security.jwt_secret
(set via config file or HOTPLEX_JWT_SECRET env var)
```

**Invalid port**:
```
Error: gateway.addr: invalid address format (expected :PORT or IP:PORT)
```

**TLS warning** (non-localhost):
```
Warning: TLS is disabled on non-local address; enable tls_enabled in production
```

---

## Hot Reload

Configuration changes are applied without restart:

```bash
# 1. Edit config
vim configs/config.yaml

# 2. Save file (reload triggers automatically)
# Gateway logs:
# [INFO] Config file modified, reloading...
# [INFO] Config reloaded successfully
# [INFO] Applied changes:
#   - gateway.idle_timeout: 5m -> 10m
#   - pool.max_size: 100 -> 200
```

**Reloadable fields** (marked with `hot: true` in code):
- Gateway timeouts (`idle_timeout`, `ping_interval`, etc.)
- Pool settings (`max_size`, `max_idle_per_user`)
- Session retention (`retention_period`, `gc_scan_interval`)
- Admin settings (`rate_limit_enabled`, `requests_per_sec`)
- STT settings (`stt_provider`, `stt_local_cmd`, `stt_local_mode`, `stt_local_idle_ttl`)

**Non-reloadable fields** (require restart):
- Network addresses (`gateway.addr`, `admin.addr`)
- TLS settings (`tls_enabled`, `tls_cert_file`)
- Database path (`db.path`)

---

## Docker Configuration

### docker-compose.yml

```yaml
services:
  gateway:
    image: hotplex-worker:latest
    environment:
      - HOTPLEX_JWT_SECRET=${HOTPLEX_JWT_SECRET}
      - HOTPLEX_ADMIN_TOKEN_1=${HOTPLEX_ADMIN_TOKEN}
      - HOTPLEX_LOG_LEVEL=info
    volumes:
      - ./configs:/etc/hotplex:ro
      - hotplex-data:/var/lib/hotplex/data
      - stt-models:/root/.cache/modelscope  # STT model cache
```

### Kubernetes ConfigMap

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: hotplex-config
data:
  config.yaml: |
    inherits: config-prod.yaml
    gateway:
      addr: ":8888"
---
apiVersion: v1
kind: Secret
metadata:
  name: hotplex-secrets
type: Opaque
stringData:
  jwt-secret: "your-jwt-secret"
  admin-token-1: "your-admin-token"
```

---

## Troubleshooting

### Config not found

```bash
Error: open configs/config.yaml: no such file or directory
```

**Solution**:
```bash
# Use absolute path
./hotplex-worker -config /etc/hotplex/config.yaml

# Or copy default config
cp configs/config.yaml /etc/hotplex/
```

### Environment variable not expanding

```yaml
# config.yaml
db:
  path: "${DB_PATH}"  # Won't work!
```

**Solution**:
```yaml
db:
  path: "${DB_PATH:-/default/path}"  # Use ${VAR:-default} syntax
```

### Port already in use

```bash
Error: listen tcp :8888: bind: address already in use
```

**Solution**:
```bash
# Find process using port
lsof -i :8888

# Kill process or change port
export HOTPLEX_GATEWAY_ADDR=":7777"
```

---

## Best Practices

### 1. Secrets Management

❌ **Don't**: Commit secrets to git
```yaml
# config.yaml
security:
  jwt_secret: "my-secret"  # NEVER do this!
```

✅ **Do**: Use environment variables or vault
```bash
export HOTPLEX_JWT_SECRET="$(vault read -field=jwt_secret secret/hotplex)"
```

### 2. Environment-Specific Configs

❌ **Don't**: Modify config.yaml for each environment

✅ **Do**: Use inheritance
```yaml
# config-prod.yaml
inherits: config.yaml
security:
  tls_enabled: true
```

### 3. Validation

❌ **Don't**: Deploy without validation

✅ **Do**: Always validate first
```bash
./hotplex-worker -config configs/config-prod.yaml -validate || exit 1
```

### 4. Documentation

❌ **Don't**: Leave default values uncommented

✅ **Do**: Document every field
```yaml
gateway:
  idle_timeout: 5m  # Disconnect idle clients (lower = save memory, higher = better UX)
```

---

## Configuration Checklist

### Development Setup

- [ ] Copy `configs/env.example` to `.env`
- [ ] Set `HOTPLEX_JWT_SECRET` (any random value for dev)
- [ ] Run `./hotplex-worker` (uses defaults)
- [ ] Test: `curl http://localhost:9999/admin/health`

### Production Deployment

- [ ] Generate strong JWT secret (32+ bytes)
- [ ] Generate admin tokens (2 tokens for rotation)
- [ ] Store secrets in vault (HashiCorp Vault, AWS Secrets Manager, etc.)
- [ ] Create TLS certificates (Let's Encrypt or internal CA)
- [ ] Copy `configs/config-prod.yaml` to `/etc/hotplex/config.yaml`
- [ ] Set environment variables from vault
- [ ] Validate config: `./hotplex-worker -validate`
- [ ] Enable TLS in config
- [ ] Configure allowed origins (CORS)
- [ ] Set up log rotation
- [ ] Configure monitoring (Prometheus)
- [ ] Test admin API with real token

---

## See Also

- [Admin API Design](Admin-API-Design.md) - Admin endpoints and authentication
- [Security Authentication](../security/Security-Authentication.md) - JWT validation, API keys
- [Disaster Recovery](../Disaster-Recovery.md) - Config backup and restoration
