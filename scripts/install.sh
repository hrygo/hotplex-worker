#!/usr/bin/env bash
#
# HotPlex Worker Gateway Installation Script
#
# This script installs HotPlex Worker Gateway with:
#   - Binary installation (build from source or download release)
#   - Configuration file generation with sensible defaults
#   - Secret generation (JWT secret, admin tokens)
#   - Directory structure setup
#   - Systemd service installation (Linux)
#   - TLS certificate generation (self-signed for dev)
#
# Usage:
#   ./install.sh [options]
#
# Options:
#   --non-interactive    Run without prompts (use defaults)
#   --prefix PATH        Installation prefix (default: /usr/local)
#   --config-dir PATH    Config directory (default: /etc/hotplex)
#   --data-dir PATH      Data directory (default: /var/lib/hotplex)
#   --dev                Development mode (self-signed certs, relaxed security)
#   --help               Show this help
#
# Best Practices:
#   - Run as root (sudo) for system-wide installation
#   - Use --non-interactive for automated deployments
#   - Store secrets in environment variables or vault
#   - Enable TLS for production
#   - Use strong, unique admin tokens
#

set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
PREFIX="/usr/local"
CONFIG_DIR="/etc/hotplex"
DATA_DIR="/var/lib/hotplex"
LOG_DIR="/var/log/hotplex"
BIN_NAME="hotplex"
NON_INTERACTIVE=false
DEV_MODE=false
INSTALL_SYSTEMD=false

# Generated secrets (will be set during installation)
JWT_SECRET=""
ADMIN_TOKEN_1=""
ADMIN_TOKEN_2=""

# Cleanup tracking
CLEANUP_ACTIONS=()
INSTALLATION_STARTED=false

# Parse command line arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --non-interactive)
            NON_INTERACTIVE=true
            shift
            ;;
        --prefix)
            PREFIX="$2"
            shift 2
            ;;
        --config-dir)
            CONFIG_DIR="$2"
            shift 2
            ;;
        --data-dir)
            DATA_DIR="$2"
            shift 2
            ;;
        --dev)
            DEV_MODE=true
            shift
            ;;
        --systemd)
            INSTALL_SYSTEMD=true
            shift
            ;;
        --help)
            sed -n '1,/^$/p' "$0" | sed '1d;$d'
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            exit 1
            ;;
    esac
done

# ─────────────────────────────────────────────────────────────────────────────
# Helper Functions
# ─────────────────────────────────────────────────────────────────────────────

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_section() {
    echo ""
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${BLUE}  $1${NC}"
    echo -e "${BLUE}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
}

check_root() {
    if [[ $EUID -ne 0 ]] && [[ "$PREFIX" == /usr* || "$CONFIG_DIR" == /etc* ]]; then
        log_error "This script must be run as root for system-wide installation"
        log_info "Run with: sudo $0"
        exit 1
    fi
}

check_dependencies() {
    log_section "Checking Dependencies"

    local missing=()

    # Check for required commands
    for cmd in go openssl; do
        if ! command -v $cmd &> /dev/null; then
            missing+=($cmd)
        fi
    done

    if [[ ${#missing[@]} -gt 0 ]]; then
        log_error "Missing dependencies: ${missing[*]}"
        log_info "Install them with:"
        log_info "  macOS: brew install ${missing[*]}"
        log_info "  Ubuntu/Debian: apt-get install ${missing[*]}"
        log_info "  RHEL/CentOS: yum install ${missing[*]}"
        exit 1
    fi

    # Check Go version
    GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
    MIN_GO_VERSION="1.21"

    if [[ $(printf '%s\n' "$MIN_GO_VERSION" "$GO_VERSION" | sort -V | head -n1) != "$MIN_GO_VERSION" ]]; then
        log_error "Go version $GO_VERSION is too old. Minimum required: $MIN_GO_VERSION"
        exit 1
    fi

    log_info "Go version: $GO_VERSION ✓"
    log_info "OpenSSL: $(openssl version) ✓"
}

generate_random_secret() {
    local length="${1:-32}"
    openssl rand -base64 "$length" | tr -d '\n+/='
}

generate_admin_token() {
    # 32 bytes = 256 bits entropy, no prefix
    openssl rand -base64 32 | tr -d '\n+/=' | head -c 43
}

validate_path() {
    local path="$1"
    local type="${2:-generic}"

    # Normalize path (resolve . and ..)
    path=$(cd "$(dirname "$path")" 2>/dev/null && pwd)/$(basename "$path") || {
        log_error "Invalid path: $path"
        return 1
    }

    # Check for dangerous system paths
    case "$path" in
        /|/etc|/usr|/bin|/sbin|/lib|/lib64|/var|/home|/root|/boot)
            log_error "Cannot use system directory: $path"
            return 1
            ;;
    esac

    # Validate path format
    if [[ ! "$path" =~ ^/[a-zA-Z0-9_./-]+$ ]]; then
        log_error "Invalid path format: $path"
        return 1
    fi

    echo "$path"
    return 0
}

validate_hostname() {
    local hostname="$1"
    # RFC 1123 hostname validation
    if [[ ! "$hostname" =~ ^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$ ]]; then
        log_error "Invalid hostname: $hostname"
        return 1
    fi
    echo "$hostname"
    return 0
}

check_system_requirements() {
    log_section "Checking System Requirements"

    # Disk space (need 500MB for build + 2GB for data)
    local available_kb
    if available_kb=$(df -k "$PREFIX" 2>/dev/null | awk 'NR==2 {print $4}'); then
        if [[ $available_kb -lt 2500000 ]]; then
            log_error "Insufficient disk space: need 2.5GB, have $((available_kb/1024))MB"
            exit 1
        fi
        log_info "Disk space: $((available_kb/1024))MB available ✓"
    fi

    # Memory check (recommend 2GB+)
    if [[ -f /proc/meminfo ]]; then
        local total_mem_kb
        total_mem_kb=$(grep MemTotal /proc/meminfo | awk '{print $2}')
        if [[ $total_mem_kb -lt 1048576 ]]; then
            log_warn "Low memory: recommend 2GB+ for build (have $((total_mem_kb/1024))MB)"
        else
            log_info "Memory: $((total_mem_kb/1024))MB ✓"
        fi
    fi

    # Architecture check
    case $(uname -m) in
        x86_64|aarch64|arm64)
            log_info "Architecture: $(uname -m) ✓"
            ;;
        *)
            log_error "Unsupported architecture: $(uname -m)"
            exit 1
            ;;
    esac
}

rollback() {
    if [[ "$INSTALLATION_STARTED" == false ]]; then
        return
    fi

    log_error "Installation failed, rolling back..."
    for action in "${CLEANUP_ACTIONS[@]}"; do
        eval "$action" 2>/dev/null || true
    done
    log_info "Rollback complete"
    exit 1
}

prompt_yes_no() {
    local prompt="$1"
    local default="${2:-n}"

    if [[ "$NON_INTERACTIVE" == true ]]; then
        echo "$default"
        return
    fi

    local response
    read -r -p "$prompt (y/n) [$default]: " response
    response=${response:-$default}
    echo "$response"
}

prompt_input() {
    local prompt="$1"
    local default="$2"

    if [[ "$NON_INTERACTIVE" == true ]]; then
        echo "$default"
        return
    fi

    local response
    read -r -p "$prompt [$default]: " response
    echo "${response:-$default}"
}

prompt_password() {
    local prompt="$1"

    if [[ "$NON_INTERACTIVE" == true ]]; then
        generate_random_secret 32
        return
    fi

    local response
    read -r -s -p "$prompt: " response
    echo ""
    echo "$response"
}

# ─────────────────────────────────────────────────────────────────────────────
# Installation Steps
# ─────────────────────────────────────────────────────────────────────────────

create_directories() {
    log_section "Creating Directories"

    log_info "Installation prefix: $PREFIX"
    log_info "Config directory: $CONFIG_DIR"
    log_info "Data directory: $DATA_DIR"
    log_info "Log directory: $LOG_DIR"

    # Create directories
    mkdir -p "$PREFIX/bin"
    mkdir -p "$CONFIG_DIR"
    mkdir -p "$CONFIG_DIR/tls"
    mkdir -p "$DATA_DIR"
    mkdir -p "$LOG_DIR"

    # Set permissions
    chmod 755 "$CONFIG_DIR"
    chmod 750 "$DATA_DIR"
    chmod 750 "$LOG_DIR"

    log_info "Directories created ✓"
}

build_binary() {
    log_section "Building Binary"

    local os=$(uname -s | tr '[:upper:]' '[:lower:]')
    local arch=$(uname -m)

    case $arch in
        x86_64)  arch="amd64" ;;
        aarch64) arch="arm64" ;;
        arm64)   arch="arm64" ;;
        *)
            log_error "Unsupported architecture: $arch"
            exit 1
            ;;
    esac

    log_info "Building for $os/$arch..."

    # Verify go.sum integrity
    if ! go mod verify; then
        log_error "Go module verification failed"
        exit 1
    fi

    # Build with ldflags
    GIT_SHA=$(git rev-parse --short=8 HEAD 2>/dev/null || echo "unknown")
    BUILD_TIME=$(date -u '+%Y-%m-%dT%H:%M:%SZ')
    GO_VERSION=$(go version | awk '{print $3}')

    LDFLAGS="-s -w \
        -X main.version=$GIT_SHA \
        -X main.buildTime=$BUILD_TIME \
        -X main.goVersion=$GO_VERSION"

    if ! go build -trimpath -ldflags="$LDFLAGS" \
        -o "$PREFIX/bin/$BIN_NAME" ./cmd/hotplex 2>&1; then
        log_error "Build failed!"
        log_error "Go version: $(go version)"
        log_error "Check build logs above for details"
        exit 1
    fi

    chmod +x "$PREFIX/bin/$BIN_NAME"

    CLEANUP_ACTIONS+=("rm -f $PREFIX/bin/$BIN_NAME")

    log_info "Binary installed: $PREFIX/bin/$BIN_NAME ✓"
    log_info "Version: $GIT_SHA"
}

generate_secrets() {
    log_section "Generating Secrets"

    log_info "Generating JWT secret..."
    JWT_SECRET=$(generate_random_secret 32)

    log_info "Generating admin tokens..."
    ADMIN_TOKEN_1=$(generate_admin_token)
    ADMIN_TOKEN_2=$(generate_admin_token)

    # Create secrets file (for reference, not used by binary)
    cat > "$CONFIG_DIR/secrets.env" <<EOF
# HotPlex Worker Gateway Secrets
# Generated on $(date -u '+%Y-%m-%d %H:%M:%S UTC')
#
# IMPORTANT: Keep this file secure!
# Add to .gitignore and never commit to version control.
#
# Usage:
#   export HOTPLEX_JWT_SECRET="\${JWT_SECRET}"
#   source $CONFIG_DIR/secrets.env

export HOTPLEX_JWT_SECRET="$JWT_SECRET"
export HOTPLEX_ADMIN_TOKEN_1="$ADMIN_TOKEN_1"
export HOTPLEX_ADMIN_TOKEN_2="$ADMIN_TOKEN_2"
EOF

    chmod 600 "$CONFIG_DIR/secrets.env"

    # Create admin tokens file (separate from secrets for easier rotation)
    cat > "$CONFIG_DIR/.admin-tokens" <<EOF
# HotPlex Admin Tokens
# Generated on $(date -u '+%Y-%m-%d %H:%M:%S UTC')
#
# IMPORTANT: Store these tokens in a secure location (vault, password manager)
# Delete this file after storing tokens securely!

TOKEN_1="$ADMIN_TOKEN_1"
TOKEN_2="$ADMIN_TOKEN_2"
EOF

    chmod 600 "$CONFIG_DIR/.admin-tokens"

    log_info "Secrets generated: $CONFIG_DIR/secrets.env ✓"
    log_info "Admin tokens saved to: $CONFIG_DIR/.admin-tokens ✓"
    log_warn "Review tokens and delete $CONFIG_DIR/.admin-tokens after storing securely!"

    CLEANUP_ACTIONS+=("rm -f $CONFIG_DIR/secrets.env $CONFIG_DIR/.admin-tokens")
}

generate_tls_certificates() {
    log_section "Generating TLS Certificates"

    if [[ "$DEV_MODE" == true ]]; then
        log_warn "Development mode: generating self-signed certificates"

        # Generate self-signed certificate with ECDSA P-384 (better security + performance)
        if ! openssl ecparam -genkey -name secp384r1 -out "$CONFIG_DIR/tls/server.key" 2>&1; then
            log_error "Failed to generate TLS private key"
            exit 1
        fi

        if ! openssl req -new -x509 -key "$CONFIG_DIR/tls/server.key" \
            -out "$CONFIG_DIR/tls/server.crt" \
            -days 365 -subj "/C=US/ST=State/L=City/O=HotPlex/CN=localhost" 2>&1; then
            log_error "Failed to generate TLS certificate"
            exit 1
        fi

        # Verify certificate and key match
        local cert_mod key_mod
        cert_mod=$(openssl x509 -noout -modulus -in "$CONFIG_DIR/tls/server.crt" 2>/dev/null | openssl md5)
        key_mod=$(openssl rsa -noout -modulus -in "$CONFIG_DIR/tls/server.key" 2>/dev/null | openssl md5)
        if [[ "$cert_mod" != "$key_mod" ]]; then
            log_error "TLS certificate and key do not match"
            exit 1
        fi

        chmod 600 "$CONFIG_DIR/tls/server.key"
        chmod 644 "$CONFIG_DIR/tls/server.crt"

        log_info "Self-signed certificate generated ✓"
        log_info "  Certificate: $CONFIG_DIR/tls/server.crt"
        log_info "  Key: $CONFIG_DIR/tls/server.key (ECDSA P-384)"
    else
        local generate_certs=$(prompt_yes_no "Generate self-signed TLS certificates?" "n")

        if [[ "$generate_certs" == "y" ]]; then
            local cert_hostname
            cert_hostname=$(prompt_input "Certificate hostname" "localhost")

            # Validate hostname
            if ! validate_hostname "$cert_hostname"; then
                exit 1
            fi

            # Generate ECDSA certificate
            if ! openssl ecparam -genkey -name secp384r1 -out "$CONFIG_DIR/tls/server.key" 2>&1; then
                log_error "Failed to generate TLS private key"
                exit 1
            fi

            if ! openssl req -new -x509 -key "$CONFIG_DIR/tls/server.key" \
                -out "$CONFIG_DIR/tls/server.crt" \
                -days 365 -subj "/C=US/ST=State/L=City/O=HotPlex/CN=$cert_hostname" 2>&1; then
                log_error "Failed to generate TLS certificate"
                exit 1
            fi

            chmod 600 "$CONFIG_DIR/tls/server.key"
            chmod 644 "$CONFIG_DIR/tls/server.crt"

            log_info "Self-signed certificate generated ✓"
        else
            log_info "Skipping TLS certificate generation"
            log_info "For production, use Let's Encrypt or provide your own certificates"
        fi
    fi
}

generate_config() {
    log_section "Generating Configuration"

    local config_file="$CONFIG_DIR/config.yaml"

    # Interactive configuration
    local gateway_addr=$(prompt_input "Gateway WebSocket address" ":8888")
    local admin_addr=$(prompt_input "Admin API address" ":9999")
    local db_path=$(prompt_input "Database path" "$DATA_DIR/hotplex.db")

    local tls_enabled="false"
    if [[ "$DEV_MODE" == true ]]; then
        tls_enabled="false"
    else
        local enable_tls=$(prompt_yes_no "Enable TLS?" "n")
        [[ "$enable_tls" == "y" ]] && tls_enabled="true"
    fi

    local worker_type=$(prompt_input "Default worker type" "claude-code")

    # Generate config file
    cat > "$config_file" <<EOF
# HotPlex Worker Gateway Configuration
# Generated on $(date -u '+%Y-%m-%d %H:%M:%S UTC')
#
# See docs/User-Manual.md for full configuration reference

gateway:
  addr: "$gateway_addr"
  ping_interval: 54s
  pong_timeout: 60s
  idle_timeout: 5m
  broadcast_queue_size: 256

db:
  path: "$db_path"
  wal_mode: true
  busy_timeout: 500ms

worker:
  max_lifetime: 24h
  idle_timeout: 30m
  execution_timeout: 10m
  env_whitelist:
    - HOME
    - PATH
    - USER
    - CLAUDE_API_KEY
    - CLAUDE_MODEL
    - CLAUDE_BASE_URL
    - OPENAI_API_KEY
    - OTEL_EXPORTER_OTLP_ENDPOINT

security:
  api_key_header: "X-API-Key"
  api_keys:
    - "hotplex-api-key-$(generate_random_secret 8)"
  tls_enabled: $tls_enabled
  tls_cert_file: "$CONFIG_DIR/tls/server.crt"
  tls_key_file: "$CONFIG_DIR/tls/server.key"
  jwt_audience: "hotplex-gateway"

session:
  retention_period: 168h
  gc_scan_interval: 1m
  max_concurrent: 1000
  event_store_enabled: true

pool:
  min_size: 0
  max_size: 100
  max_idle_per_user: 3
  max_memory_per_user: 2147483648  # 2 GB

admin:
  enabled: true
  addr: "$admin_addr"
  tokens:
    - "$ADMIN_TOKEN_1"
    - "$ADMIN_TOKEN_2"
  token_scopes:
    "$ADMIN_TOKEN_1":
      - session:read
      - session:write
      - session:delete
      - stats:read
      - health:read
      - admin:read
      - config:read
  default_scopes:
    - session:read
    - stats:read
    - health:read
  ip_whitelist_enabled: true
  allowed_cidrs:
    - 127.0.0.0/8
    - 10.0.0.0/8
  rate_limit_enabled: true
  requests_per_sec: 10
  burst: 20
EOF

    chmod 644 "$config_file"

    log_info "Configuration file generated: $config_file ✓"
}

install_systemd_service() {
    if [[ "$(uname -s)" != "Linux" ]] || [[ "$INSTALL_SYSTEMD" == false ]]; then
        return
    fi

    log_section "Installing Systemd Service"

    local service_file="/etc/systemd/system/hotplex.service"

    cat > "$service_file" <<EOF
[Unit]
Description=HotPlex Worker Gateway
Documentation=https://github.com/hrygo/hotplex
After=network.target

[Service]
Type=simple
User=hotplex
Group=hotplex
WorkingDirectory=$DATA_DIR

# Load secrets from environment file
EnvironmentFile=$CONFIG_DIR/secrets.env

# Main command
ExecStart=$PREFIX/bin/$BIN_NAME -config $CONFIG_DIR/config.yaml

# Restart policy
Restart=on-failure
RestartSec=5

# Security hardening
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$DATA_DIR $LOG_DIR

# Resource limits
LimitNOFILE=65536
LimitNPROC=4096

# Logging
StandardOutput=journal
StandardError=journal
SyslogIdentifier=hotplex

[Install]
WantedBy=multi-user.target
EOF

    chmod 644 "$service_file"

    # Create hotplex user if not exists
    if ! id -u hotplex &>/dev/null; then
        useradd -r -s /bin/false -d "$DATA_DIR" hotplex
        log_info "Created hotplex user"
    fi

    # Set ownership
    chown -R hotplex:hotplex "$DATA_DIR"
    chown -R hotplex:hotplex "$LOG_DIR"
    chown hotplex:hotplex "$CONFIG_DIR/secrets.env"

    # Reload systemd
    systemctl daemon-reload
    systemctl enable hotplex

    log_info "Systemd service installed ✓"
    log_info "  Start: systemctl start hotplex"
    log_info "  Status: systemctl status hotplex"
    log_info "  Logs: journalctl -u hotplex -f"
}

create_env_example() {
    log_section "Creating Environment Example"

    cat > "$CONFIG_DIR/config.env.example" <<EOF
# HotPlex Worker Gateway Environment Variables
#
# Copy this file and customize for your environment:
#   cp config.env.example config.env
#   source config.env
#
# Or export variables individually:
#   export HOTPLEX_JWT_SECRET="your-secret-here"

# ─── Secrets ────────────────────────────────────────────────────────────────

# JWT secret for token validation (required)
# Generate with: openssl rand -base64 32
export HOTPLEX_JWT_SECRET="${JWT_SECRET}"

# Admin tokens (for Admin API authentication)
export HOTPLEX_ADMIN_TOKEN_1="${ADMIN_TOKEN_1}"
export HOTPLEX_ADMIN_TOKEN_2="${ADMIN_TOKEN_2}"

# ─── Database ────────────────────────────────────────────────────────────────

export HOTPLEX_DB_PATH="${DATA_DIR}/hotplex.db"

# ─── TLS (Production) ────────────────────────────────────────────────────────

# export HOTPLEX_TLS_CERT="/etc/hotplex/tls/server.crt"
# export HOTPLEX_TLS_KEY="/etc/hotplex/tls/server.key"

# ─── Observability ────────────────────────────────────────────────────────────

# OpenTelemetry tracing endpoint (optional)
# export OTEL_EXPORTER_OTLP_ENDPOINT="http://localhost:4317"

# Log level: debug, info, warn, error
# export HOTPLEX_LOG_LEVEL="info"

# ─── Development ──────────────────────────────────────────────────────────────

# Enable development mode (relaxed security)
# export HOTPLEX_DEV_MODE="false"
EOF

    chmod 644 "$CONFIG_DIR/config.env.example"

    log_info "Environment example created: $CONFIG_DIR/config.env.example ✓"
}

print_summary() {
    log_section "Installation Complete"

    cat <<EOF
${GREEN}HotPlex Worker Gateway has been successfully installed!${NC}

${BLUE}Binary:${NC}
  $PREFIX/bin/$BIN_NAME

${BLUE}Configuration:${NC}
  $CONFIG_DIR/config.yaml

${BLUE}Secrets:${NC}
  $CONFIG_DIR/secrets.env
  ${YELLOW}⚠ Keep this file secure and add to .gitignore!${NC}

${BLUE}Admin Tokens:${NC}
  Saved to: $CONFIG_DIR/.admin-tokens
  ${YELLOW}⚠ Review and delete after storing in vault!${NC}

${BLUE}Data:${NC}
  $DATA_DIR/

${BLUE}Logs:${NC}
  $LOG_DIR/

${BLUE}Quick Start:${NC}

  1. Load secrets:
     source $CONFIG_DIR/secrets.env

  2. Start the gateway:
     $PREFIX/bin/$BIN_NAME -config $CONFIG_DIR/config.yaml

  3. Check health:
     curl http://localhost:9999/admin/health

  4. Connect via WebSocket:
     ws://localhost:8888

${BLUE}Production Checklist:${NC}

  ☐ Enable TLS in config.yaml
  ☐ Use strong, unique admin tokens (store in vault)
  ☐ Delete $CONFIG_DIR/.admin-tokens after storing tokens
  ☐ Set up log rotation
  ☐ Configure monitoring (Prometheus + Grafana)
  ☐ Set up database backup
  ☐ Review security settings in config.yaml
  ☐ Add HOTPLEX_JWT_SECRET to vault/secrets manager

${BLUE}Documentation:${NC}

  User Manual: docs/User-Manual.md
  Config Reference: docs/management/Config-Management.md
  Admin API: docs/management/Admin-API-Design.md

EOF

    if [[ -f "/etc/systemd/system/hotplex.service" ]]; then
        echo "${BLUE}Systemd Service:${NC}"
        echo "  Start:   systemctl start hotplex"
        echo "  Stop:    systemctl stop hotplex"
        echo "  Restart: systemctl restart hotplex"
        echo "  Status:  systemctl status hotplex"
        echo "  Logs:    journalctl -u hotplex -f"
        echo ""
    fi
}

# ─────────────────────────────────────────────────────────────────────────────
# Main Execution
# ─────────────────────────────────────────────────────────────────────────────

main() {
    clear

    cat <<EOF
${BLUE}
╔═══════════════════════════════════════════════════════════════════════════╗
║                    HotPlex Worker Gateway Installer                       ║
║                              v1.0.0                                       ║
╚═══════════════════════════════════════════════════════════════════════════╝
${NC}

EOF

    if [[ "$NON_INTERACTIVE" == true ]]; then
        log_info "Running in non-interactive mode"
    fi

    if [[ "$DEV_MODE" == true ]]; then
        log_warn "Development mode enabled"
    fi

    # Set up rollback trap
    trap rollback EXIT

    check_root
    check_dependencies
    check_system_requirements
    create_directories
    INSTALLATION_STARTED=true
    build_binary
    generate_secrets
    generate_tls_certificates
    generate_config
    install_systemd_service
    create_env_example

    # Success - disable rollback trap
    trap - EXIT
    INSTALLATION_STARTED=false

    print_summary
}

main "$@"
