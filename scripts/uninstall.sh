#!/usr/bin/env bash
#
# HotPlex Worker Gateway - Uninstall Script
#
# This script completely removes HotPlex Worker Gateway from the system.
#
# Usage:
#   sudo ./scripts/uninstall.sh
#
# Options:
#   --keep-data      Preserve data directory (database, backups)
#   --keep-config    Preserve configuration directory
#   --non-interactive Run without prompts
#

set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Default paths
PREFIX="/usr/local"
CONFIG_DIR="/etc/hotplex"
DATA_DIR="/var/lib/hotplex"
LOG_DIR="/var/log/hotplex"

# Options
KEEP_DATA=false
KEEP_CONFIG=false
NON_INTERACTIVE=false

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

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --keep-data)
            KEEP_DATA=true
            shift
            ;;
        --keep-config)
            KEEP_CONFIG=true
            shift
            ;;
        --non-interactive)
            NON_INTERACTIVE=true
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

# Confirmation
if [[ "$NON_INTERACTIVE" == false ]]; then
    echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${RED}  ⚠️  WARNING: This will completely uninstall HotPlex Worker Gateway${NC}"
    echo -e "${RED}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
    echo "The following will be removed:"
    echo "  - Binary: $PREFIX/bin/hotplex"
    [[ "$KEEP_CONFIG" == false ]] && echo "  - Config: $CONFIG_DIR"
    [[ "$KEEP_DATA" == false ]] && echo "  - Data: $DATA_DIR"
    [[ "$KEEP_DATA" == false ]] && echo "  - Logs: $LOG_DIR"
    echo ""

    read -r -p "Are you sure you want to continue? (y/n) " confirm
    if [[ "$confirm" != "y" ]]; then
        log_info "Uninstall cancelled"
        exit 0
    fi

    # Final warning
    read -r -p "This action cannot be undone. Type 'uninstall' to confirm: " final_confirm
    if [[ "$final_confirm" != "uninstall" ]]; then
        log_info "Uninstall cancelled"
        exit 0
    fi
fi

log_section "Stopping Services"

# Stop systemd service
if systemctl is-active --quiet hotplex 2>/dev/null; then
    log_info "Stopping systemd service..."
    systemctl stop hotplex
    systemctl disable hotplex
    rm -f /etc/systemd/system/hotplex.service
    systemctl daemon-reload
    log_info "Systemd service stopped and disabled ✓"
fi

# Stop Docker containers
if command -v docker-compose &>/dev/null && [[ -f docker-compose.yml ]]; then
    log_info "Stopping Docker containers..."
    docker-compose down || true
    log_info "Docker containers stopped ✓"
fi

log_section "Removing Files"

# Remove binary
if [[ -f "$PREFIX/bin/hotplex" ]]; then
    rm -f "$PREFIX/bin/hotplex"
    log_info "Binary removed: $PREFIX/bin/hotplex ✓"
fi

# Remove config directory
if [[ "$KEEP_CONFIG" == false && -d "$CONFIG_DIR" ]]; then
    rm -rf "$CONFIG_DIR"
    log_info "Config directory removed: $CONFIG_DIR ✓"
elif [[ "$KEEP_CONFIG" == true && -d "$CONFIG_DIR" ]]; then
    log_warn "Keeping config directory: $CONFIG_DIR"
fi

# Remove data directory
if [[ "$KEEP_DATA" == false && -d "$DATA_DIR" ]]; then
    rm -rf "$DATA_DIR"
    log_info "Data directory removed: $DATA_DIR ✓"
elif [[ "$KEEP_DATA" == true && -d "$DATA_DIR" ]]; then
    log_warn "Keeping data directory: $DATA_DIR"
fi

# Remove log directory
if [[ "$KEEP_DATA" == false && -d "$LOG_DIR" ]]; then
    rm -rf "$LOG_DIR"
    log_info "Log directory removed: $LOG_DIR ✓"
elif [[ "$KEEP_DATA" == true && -d "$LOG_DIR" ]]; then
    log_warn "Keeping log directory: $LOG_DIR"
fi

# Remove hotplex user (if exists and not used by other processes)
if id -u hotplex &>/dev/null; then
    # Check if user is running any processes
    if ! pgrep -u hotplex &>/dev/null; then
        userdel hotplex 2>/dev/null || true
        log_info "User 'hotplex' removed ✓"
    else
        log_warn "User 'hotplex' still has running processes, skipping removal"
    fi
fi

log_section "Uninstall Complete"

cat <<EOF
${GREEN}HotPlex Worker Gateway has been uninstalled.${NC}

${BLUE}Summary:${NC}
  - Binary: Removed
  $([[ "$KEEP_CONFIG" == false ]] && echo "  - Config: Removed" || echo "  - Config: Preserved")
  $([[ "$KEEP_DATA" == false ]] && echo "  - Data: Removed" || echo "  - Data: Preserved")
  $([[ "$KEEP_DATA" == false ]] && echo "  - Logs: Removed" || echo "  - Logs: Preserved")

${YELLOW}Note:${NC}
  - Docker images are preserved (run 'docker rmi hotplex:latest' to remove)
  - Docker volumes are preserved (run 'docker volume rm hotplex-data' to remove)

${BLUE}Reinstallation:${NC}
  To reinstall, run: ./scripts/install.sh

EOF
