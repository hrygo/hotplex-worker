# HotPlex Worker Gateway - Scripts

This directory contains installation and deployment scripts for HotPlex Worker Gateway.

## Overview

| Script | Purpose | Usage |
|--------|---------|-------|
| `install.sh` | Full production installation | `sudo ./scripts/install.sh` |
| `quickstart.sh` | Quick dev environment setup | `./scripts/quickstart.sh` |
| `docker-build.sh` | Build Docker image | `./scripts/docker-build.sh` |
| `uninstall.sh` | Complete uninstallation | `sudo ./scripts/uninstall.sh` |
| `hotplex-worker.service` | Systemd service unit | Install via `install.sh` |

## Installation Scripts

### install.sh

**Production installation script** that:

- Checks system dependencies (Go 1.21+, OpenSSL)
- Builds binary with version injection
- Creates directory structure (`/etc/hotplex`, `/var/lib/hotplex`, `/var/log/hotplex`)
- Generates secrets (JWT secret, admin tokens)
- Generates TLS certificates (self-signed or Let's Encrypt integration)
- Creates configuration file
- Installs systemd service (Linux)
- Creates environment file examples

**Usage:**

```bash
# Interactive installation (prompts for configuration)
sudo ./scripts/install.sh

# Non-interactive (uses defaults)
sudo ./scripts/install.sh --non-interactive

# Development mode (self-signed certs, relaxed security)
sudo ./scripts/install.sh --dev

# Custom directories
sudo ./scripts/install.sh \
  --prefix /opt/hotplex \
  --config-dir /opt/hotplex/config \
  --data-dir /data/hotplex

# Install systemd service
sudo ./scripts/install.sh --systemd
```

**What it creates:**

```
/usr/local/bin/hotplex-worker           # Binary
/etc/hotplex/
  ├── config.yaml                       # Main config
  ├── secrets.env                       # Secrets (JWT, tokens)
  ├── config.env.example                # Environment template
  └── tls/
      ├── server.crt                    # TLS certificate
      └── server.key                    # TLS private key
/var/lib/hotplex/                       # Data directory (SQLite)
/var/log/hotplex/                       # Log directory
/etc/systemd/system/hotplex-worker.service  # Systemd unit
```

**Post-installation:**

```bash
# Load secrets
source /etc/hotplex/secrets.env

# Start service
systemctl start hotplex-worker

# Check status
systemctl status hotplex-worker

# View logs
journalctl -u hotplex-worker -f

# Test health
curl http://localhost:9999/admin/health
```

### quickstart.sh

**Quick development setup** that:

- Builds binary
- Creates minimal dev config
- Generates dev secrets
- Offers to start gateway immediately

**Usage:**

```bash
./scripts/quickstart.sh
```

**What it creates:**

```
.dev/
  ├── config.yaml          # Dev config
  └── data/
      └── hotplex-worker.db       # SQLite database
```

**Dev mode features:**

- Any API key header value accepted
- TLS disabled
- Admin IP whitelist disabled
- Relaxed security settings

### docker-build.sh

**Build Docker image** with:

- Multi-stage build (minimal final image)
- Version injection (Git SHA, build time)
- Platform-specific builds
- Optional push to registry

**Usage:**

```bash
# Build with default tag
./scripts/docker-build.sh

# Custom tag
./scripts/docker-build.sh hotplex-worker:v1.0.0

# Build and push
./scripts/docker-build.sh hotplex-worker:latest --push

# Build without cache
./scripts/docker-build.sh --no-cache

# Multi-platform build
./scripts/docker-build.sh hotplex-worker:latest --platform linux/amd64
```

**Run container:**

```bash
# Development
docker run -p 8080:8888 -p 9080:9999 \
  -e HOTPLEX_JWT_SECRET=your-secret \
  hotplex-worker:latest

# With custom config
docker run -p 8080:8888 -p 9080:9999 \
  -v /path/to/config.yaml:/etc/hotplex/config.yaml \
  -e HOTPLEX_JWT_SECRET=your-secret \
  hotplex-worker:latest

# With TLS
docker run -p 8443:8443 -p 9080:9999 \
  -v /path/to/tls.crt:/etc/hotplex/tls/server.crt \
  -v /path/to/tls.key:/etc/hotplex/tls/server.key \
  -e HOTPLEX_JWT_SECRET=your-secret \
  hotplex-worker:latest
```

### hotplex-worker.service

**Systemd service unit** for Linux systems.

**Features:**

- Automatic restart on failure
- Security hardening (no new privileges, private tmp, etc.)
- Resource limits (65536 file descriptors)
- Graceful shutdown (30s timeout)
- Watchdog support (30s)
- Journal logging

**Installation:**

```bash
sudo cp scripts/hotplex-worker.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable hotplex-worker
sudo systemctl start hotplex-worker
```

**Management:**

```bash
# Start
sudo systemctl start hotplex-worker

# Stop
sudo systemctl stop hotplex-worker

# Restart
sudo systemctl restart hotplex-worker

# Status
sudo systemctl status hotplex-worker

# Logs
sudo journalctl -u hotplex-worker -f

# Resource usage
systemctl show hotplex-worker -p MemoryCurrent,CPUUsageNSec
```

## Docker Compose

### docker-compose.yml

**Development compose file** with:

- HotPlex Worker Gateway
- Optional Prometheus (monitoring profile)
- Optional Grafana (monitoring profile)

**Usage:**

```bash
# Start gateway only
docker-compose up -d

# Start with monitoring stack
docker-compose --profile monitoring up -d

# View logs
docker-compose logs -f gateway

# Stop
docker-compose down

# Stop and remove volumes
docker-compose down -v
```

**Environment variables:**

```bash
# Required
export HOTPLEX_JWT_SECRET="your-jwt-secret"
export HOTPLEX_ADMIN_TOKEN="your-admin-token"

# Optional
export HOTPLEX_LOG_LEVEL="info"
export TZ="UTC"
```

### docker-compose.prod.yml

**Production override** with:

- TLS enabled
- Traefik reverse proxy
- Let's Encrypt certificates
- Stricter resource limits
- External monitoring

**Usage:**

```bash
# Production deployment
docker-compose -f docker-compose.yml -f docker-compose.prod.yml up -d

# Create Traefik network (first time only)
docker network create traefik-network
```

**Production checklist:**

- [ ] Set strong `HOTPLEX_JWT_SECRET`
- [ ] Set strong `HOTPLEX_ADMIN_TOKEN`
- [ ] Configure `GRAFANA_PASSWORD`
- [ ] Update Traefik dashboard host (`traefik.hotplex.dev`)
- [ ] Update gateway hosts (`gateway.hotplex.dev`, `admin.hotplex.dev`)
- [ ] Set up external Prometheus/Grafana (remove monitoring profile)

## Security Best Practices

### Secrets Management

**Development:**

```bash
# Use secrets.env (generated by install.sh)
source /etc/hotplex/secrets.env
```

**Production:**

```bash
# Option 1: Vault
export HOTPLEX_JWT_SECRET=$(vault read -field=jwt_secret secret/hotplex)

# Option 2: Kubernetes Secrets
envFrom:
  - secretRef:
      name: hotplex-secrets

# Option 3: Docker Swarm Secrets
secrets:
  - hotplex_jwt_secret
```

### TLS Certificates

**Development (self-signed):**

```bash
./scripts/install.sh --dev
```

**Production (Let's Encrypt):**

```bash
# Use docker-compose.prod.yml with Traefik
# Certificates are automatically managed
```

**Manual:**

```bash
# Generate CSR
openssl req -new -newkey rsa:2048 -nodes \
  -keyout /etc/hotplex/tls/server.key \
  -out /tmp/hotplex.csr \
  -subj "/C=US/ST=State/L=City/O=HotPlex/CN=gateway.hotplex.dev"

# Get certificate from CA
# Then place at /etc/hotplex/tls/server.crt
```

### File Permissions

```bash
# Config files (read-only for service user)
chmod 644 /etc/hotplex/config.yaml

# Secrets (read-write for service user only)
chmod 600 /etc/hotplex/secrets.env
chown hotplex:hotplex /etc/hotplex/secrets.env

# TLS key (read-only for service user)
chmod 600 /etc/hotplex/tls/server.key
chown hotplex:hotplex /etc/hotplex/tls/server.key

# Data directory
chmod 750 /var/lib/hotplex
chown -R hotplex:hotplex /var/lib/hotplex

# Log directory
chmod 750 /var/log/hotplex
chown -R hotplex:hotplex /var/log/hotplex
```

## Troubleshooting

### install.sh fails with "permission denied"

```bash
# Run with sudo
sudo ./scripts/install.sh
```

### Binary not found after install

```bash
# Check PATH
which hotplex-worker

# Add to PATH if needed
export PATH=$PATH:/usr/local/bin
```

### Systemd service won't start

```bash
# Check logs
journalctl -u hotplex-worker -n 50

# Verify secrets file exists
ls -la /etc/hotplex/secrets.env

# Verify config is valid
hotplex-worker -config /etc/hotplex/config.yaml -validate
```

### Docker container unhealthy

```bash
# Check container logs
docker logs hotplex-worker

# Check health check output
docker inspect hotplex-worker | jq '.[0].State.Health'

# Run health check manually
docker exec hotplex-worker curl -f http://localhost:9999/admin/health
```

### Port already in use

```bash
# Find process using port
lsof -i :8888
lsof -i :9999

# Kill process or change ports in config
```

## Additional Resources

- **User Manual**: `docs/User-Manual.md`
- **Configuration Guide**: `docs/management/Config-Management.md`
- **Admin API**: `docs/management/Admin-API-Design.md`
- **Security**: `docs/security/Security-Authentication.md`
