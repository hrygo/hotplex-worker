#!/usr/bin/env bash
#
# HotPlex Worker Gateway — Uninstaller
#
# Removes the hotplex binary. Optionally purges config and data.
#
# Usage:
#   sudo ./scripts/uninstall.sh [options]
#
# Options:
#   --prefix PATH     Installation prefix (default: /usr/local)
#   --purge           Also remove config (~/.hotplex) and data
#   --non-interactive Skip confirmation prompt
#   --help            Show this help

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

PREFIX="/usr/local"
PURGE=false
NON_INTERACTIVE=false
USER_DIR="$HOME/.hotplex"

need_arg() {
    [[ $# -lt 2 || "$2" == --* ]] && { echo -e "${RED}error: $1 requires an argument${NC}"; exit 1; }
}

while [[ $# -gt 0 ]]; do
    case $1 in
        --prefix)         need_arg "$@"; PREFIX="$2"; shift 2 ;;
        --purge)          PURGE=true; shift ;;
        --non-interactive) NON_INTERACTIVE=true; shift ;;
        --help)           sed -n '2,/^$/p' "$0" | sed 's/^# //; /^$/d'; exit 0 ;;
        *)                echo -e "${RED}Unknown option: $1${NC}"; exit 1 ;;
    esac
done

info()  { echo -e "${GREEN}[INFO]${NC} $1"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }

# ── Confirm ──────────────────────────────────────────────────────────────────

if [[ "$NON_INTERACTIVE" == false ]]; then
    echo -e "${RED}This will uninstall HotPlex Worker Gateway${NC}"
    [[ "$PURGE" == true ]] && echo "  --purge: config and data will also be removed"
    read -r -p "Continue? [y/N] " confirm
    [[ "$confirm" != "y" && "$confirm" != "Y" ]] && exit 0
fi

# ── Stop services ────────────────────────────────────────────────────────────

if [[ $EUID -eq 0 ]] && systemctl is-active --quiet hotplex 2>/dev/null; then
    systemctl stop hotplex
    systemctl disable hotplex
    rm -f /etc/systemd/system/hotplex.service
    systemctl daemon-reload
    info "Systemd service stopped and removed"
fi

# Kill dev-mode gateway process and clean PID file
if [[ -f "$USER_DIR/.pids/gateway.pid" ]]; then
    pid=$(cat "$USER_DIR/.pids/gateway.pid" 2>/dev/null || true)
    if [[ -n "$pid" ]] && kill -0 "$pid" 2>/dev/null; then
        kill "$pid" 2>/dev/null || true
        sleep 1
        kill -9 "$pid" 2>/dev/null || true
        info "Gateway process stopped (PID $pid)"
    fi
    rm -f "$USER_DIR/.pids/gateway.pid"
fi

# ── Remove binary ────────────────────────────────────────────────────────────

TARGET="$PREFIX/bin/hotplex"
if [[ -f "$TARGET" ]]; then
    rm -f "$TARGET"
    info "Binary removed: $TARGET"
else
    warn "Binary not found: $TARGET"
fi

# ── Remove systemd user ──────────────────────────────────────────────────────

if [[ $EUID -eq 0 ]] && id -u hotplex &>/dev/null 2>/dev/null && ! pgrep -u hotplex &>/dev/null; then
    userdel hotplex 2>/dev/null || true
    info "System user 'hotplex' removed"
fi

# ── Purge config & data ──────────────────────────────────────────────────────

if [[ "$PURGE" == true && -d "$USER_DIR" ]]; then
    rm -rf "$USER_DIR"
    info "User directory removed: $USER_DIR"
fi

echo ""
info "Uninstall complete."
[[ "$PURGE" == false ]] && echo "  Config preserved in $USER_DIR (use --purge to remove)"
