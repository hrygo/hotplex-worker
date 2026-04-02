# Configuration Management

> **Purpose**: Complete guide to configuring HotPlex Worker Gateway
>
> **Last Updated**: 2026-04-02

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
