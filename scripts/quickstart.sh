#!/usr/bin/env bash
#
# HotPlex Worker Gateway - Quick Start (Development)
#
# This script sets up a minimal development environment in 30 seconds.
# For production installation, use install.sh instead.
#
# Usage:
#   ./scripts/quickstart.sh
#
# What it does:
#   - Builds the binary
#   - Generates minimal config
#   - Generates secrets
#   - Starts the gateway in dev mode
#

set -euo pipefail

# Colors
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_section() {
    echo ""
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
}

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN_DIR="$PROJECT_ROOT/bin"
CONFIG_DIR="$PROJECT_ROOT/.dev"
DATA_DIR="$PROJECT_ROOT/.dev/data"

command -v openssl &>/dev/null || { echo "error: openssl is required"; exit 1; }

# Build
log_section "Building Binary"
make build

# Create directories
log_section "Setting Up Development Environment"
mkdir -p "$CONFIG_DIR"
mkdir -p "$DATA_DIR"

# Generate secrets
log_info "Generating secrets..."
JWT_SECRET=$(openssl rand -base64 32)
ADMIN_TOKEN="dev-admin-token-$(openssl rand -hex 8)"

# Generate minimal config
log_info "Generating dev config..."
cat > "$CONFIG_DIR/config.yaml" <<EOF
gateway:
  addr: ":8888"

db:
  path: "$DATA_DIR/hotplex.db"

worker:
  max_lifetime: 24h
  idle_timeout: 60m

security:
  api_keys:
    - "dev-api-key"

admin:
  enabled: true
  addr: ":9999"
  tokens:
    - "$ADMIN_TOKEN"
  ip_whitelist_enabled: false

session:
  retention_period: 24h
  gc_scan_interval: 1m

pool:
  max_size: 10
EOF

# Export secrets
export HOTPLEX_JWT_SECRET="$JWT_SECRET"

log_section "Quick Start Complete"

cat <<EOF
${GREEN}Development environment ready!${NC}

${BLUE}Config:${NC}    $CONFIG_DIR/config.yaml
${BLUE}Database:${NC}  $DATA_DIR/hotplex.db

${BLUE}Admin Token:${NC} $ADMIN_TOKEN

${BLUE}Commands:${NC}

  Start gateway:
    export HOTPLEX_JWT_SECRET='$JWT_SECRET'
    $BIN_DIR/hotplex-\$(go env GOOS)-\$(go env GOARCH) \\
      gateway start -c $CONFIG_DIR/config.yaml --dev

  Test health:
    curl http://localhost:9999/admin/health

  WebSocket:
    ws://localhost:8888

${BLUE}Admin API:${NC}

  Admin token: $ADMIN_TOKEN

  curl -H "Authorization: Bearer $ADMIN_TOKEN" \\
    http://localhost:9999/admin/stats

${BLUE}Dev Mode:${NC}

  - Any API key header value is accepted
  - TLS disabled
  - Relaxed security
  - Admin IP whitelist disabled

EOF

# Offer to start
read -r -p "Start gateway now? (y/n) [n]: " start_now
if [[ "$start_now" == "y" ]]; then
    log_info "Starting gateway..."
    export HOTPLEX_JWT_SECRET="$JWT_SECRET"
    exec "$BIN_DIR/hotplex-$(go env GOOS)-$(go env GOARCH)" \
        gateway start -c "$CONFIG_DIR/config.yaml" --dev
fi
