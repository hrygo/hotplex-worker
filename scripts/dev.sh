#!/usr/bin/env bash
# scripts/dev.sh — Unified dev service manager (gateway + webchat)
# All production lifecycle (systemd, docker) bypasses this script entirely.
#
# Usage:
#   ./dev.sh <start|stop|status|logs|tail> [gateway|webchat|all]
#
# Environment:
#   CONFIG          Path to config file (default: configs/config.yaml)
#   BUILD_DIR       Build output directory (default: bin)
#   LOG_DIR         Log output directory (default: logs)

set -euo pipefail

# Load .env if present.
if [[ -f "${BASH_SOURCE[0]%/*}/../.env" ]]; then
    # shellcheck disable=SC1091
    set -a && source "${BASH_SOURCE[0]%/*}/../.env" && set +a
fi

# ── Constants ─────────────────────────────────────────────────────────────────

readonly SCRIPT_DIR="${BASH_SOURCE[0]%/*}"
readonly ROOT_DIR="${SCRIPT_DIR}/.."
readonly BIN_NAME="hotplex-worker"
readonly BUILD_DIR="${BUILD_DIR:-${ROOT_DIR}/bin}"
readonly LOG_DIR="${LOG_DIR:-${ROOT_DIR}/logs}"
readonly CONFIG="${CONFIG:-${ROOT_DIR}/configs/config.yaml}"

readonly GATEWAY_PID="${HOME}/.hotplex/.pid/hotplex-worker.pid"
readonly GATEWAY_LOG="${LOG_DIR}/hotplex-worker.log"

readonly WEBCHAT_DIR="${ROOT_DIR}/webchat"
readonly WEBCHAT_PID="${HOME}/.hotplex/.pid/hotplex-webchat.pid"
readonly WEBCHAT_PORT="${WEBCHAT_PORT:-3000}"
readonly WEBCHAT_LOG="${LOG_DIR}/webchat.log"

# ── Helpers ───────────────────────────────────────────────────────────────────

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; DIM='\033[2m'; NC='\033[0m'

err()  { echo -e "${RED}✗ $*${NC}" >&2; }
ok()   { echo -e "${GREEN}✓ $*${NC}"; }
warn() { echo -e "${YELLOW}⚠ $*${NC}"; }
info() { echo -e "${CYAN}› $*${NC}"; }

die()  { err "$@"; exit 1; }

# Kill process by PID file, then remove the file.
kill_pidfile() {
    local pidfile=$1; local name=${2:-service}
    [[ ! -f "$pidfile" ]] && return 0
    local pid; pid=$(cat "$pidfile" 2>/dev/null || true)
    [[ -z "$pid" ]] && rm -f "$pidfile" && return 0
    if kill -0 "$pid" 2>/dev/null; then
        info "Stopping $name (PID $pid)..."
        kill -TERM "$pid" 2>/dev/null || true
        for i in $(seq 1 5); do
            sleep 1
            kill -0 "$pid" 2>/dev/null || { rm -f "$pidfile"; ok "$name stopped"; return 0; }
        done
        kill -9 "$pid" 2>/dev/null || true
        rm -f "$pidfile"
        ok "$name force-stopped"
    else
        info "$name: stale PID file"
        rm -f "$pidfile"
    fi
}

# Kill processes listening on a port.
kill_port() {
    local port=$1; local name=${2:-service}
    local pids; pids=$(lsof -ti:"$port" 2>/dev/null || true)
    [[ -z "$pids" ]] && return 0
    for pid in $pids; do
        if kill -0 "$pid" 2>/dev/null; then
            info "Killing $name on port $port (PID $pid)..."
            kill -TERM "$pid" 2>/dev/null || true
            sleep 1
            kill -0 "$pid" 2>/dev/null && kill -9 "$pid" 2>/dev/null || true
        fi
    done
}

# ── Gateway ────────────────────────────────────────────────────────────────────

gateway_running() {
    [[ -f "$GATEWAY_PID" ]] && kill -0 "$(cat "$GATEWAY_PID")" 2>/dev/null
}

start_gateway() {
    mkdir -p "$LOG_DIR" "$BUILD_DIR"

    if gateway_running; then
        warn "Gateway already running (PID $(cat "$GATEWAY_PID"))"
        return 0
    fi

    info "Starting gateway..."
    local binary="${BUILD_DIR}/${BIN_NAME}-$(go env GOOS)-$(go env GOARCH)"
    if [[ ! -x "$binary" ]]; then
        warn "Binary not found: $binary (run: make build)"
        warn "Building now..."
        if ! (cd "$ROOT_DIR" && make --no-print-directory build >/dev/null 2>&1); then
            die "Build failed"
        fi
    fi

    "$binary" -config "$CONFIG" >> "$GATEWAY_LOG" 2>&1 &
    echo $! > "$GATEWAY_PID"
    sleep 1

    if kill -0 "$(cat "$GATEWAY_PID")" 2>/dev/null; then
        ok "Gateway started (PID $(cat "$GATEWAY_PID"))"
    else
        err "Gateway failed to start"
        cat "$GATEWAY_LOG" | tail -20
        rm -f "$GATEWAY_PID"
        exit 1
    fi
}

stop_gateway() {
    kill_pidfile "$GATEWAY_PID" "gateway"
}

status_gateway() {
    if gateway_running; then
        local pid; pid=$(cat "$GATEWAY_PID")
        local mem cpu
        mem=$(ps -o rss= -p "$pid" 2>/dev/null | awk '{printf "%.1f MB", $1/1024}' || echo "N/A")
        cpu=$(ps -o %cpu= -p "$pid" 2>/dev/null | awk '{print $1"%"}' || echo "N/A")
        echo -e "${GREEN}🟢 Gateway running${NC} (PID $pid)  mem=$mem  cpu=$cpu"
    else
        echo -e "${RED}🔴 Gateway not running${NC}"
        [[ -f "$GATEWAY_PID" ]] && echo -e "${DIM}  (stale PID file)${NC}"
    fi
}

logs_gateway() {
    [[ -f "$GATEWAY_LOG" ]] && cat "$GATEWAY_LOG" || echo "${DIM}No log file: $GATEWAY_LOG${NC}"
}

tail_gateway() {
    if [[ -f "$GATEWAY_LOG" ]]; then
        exec tail -f "$GATEWAY_LOG"
    else
        err "Log file not found: $GATEWAY_LOG"
        exit 1
    fi
}

# ── WebChat ────────────────────────────────────────────────────────────────────

webchat_running() {
    [[ -f "$WEBCHAT_PID" ]] && kill -0 "$(cat "$WEBCHAT_PID")" 2>/dev/null
}

start_webchat() {
    mkdir -p "$LOG_DIR"

    if webchat_running; then
        warn "Web-chat already running (PID $(cat "$WEBCHAT_PID"))"
        return 0
    fi

    info "Starting webchat dev server (port $WEBCHAT_PORT)..."
    (cd "$WEBCHAT_DIR" && pnpm dev >> "$WEBCHAT_LOG" 2>&1) &
    echo $! > "$WEBCHAT_PID"
    sleep 3

    if webchat_running; then
        ok "Web-chat started (PID $(cat "$WEBCHAT_PID")) → http://localhost:$WEBCHAT_PORT"
    else
        err "Web-chat failed to start"
        cat "$WEBCHAT_LOG" | tail -20
        rm -f "$WEBCHAT_PID"
        exit 1
    fi
}

stop_webchat() {
    kill_pidfile "$WEBCHAT_PID" "webchat"
    kill_port "$WEBCHAT_PORT" "webchat (port)"
}

status_webchat() {
    if webchat_running; then
        echo -e "${GREEN}🟢 Web-chat running${NC} (PID $(cat "$WEBCHAT_PID")) → http://localhost:$WEBCHAT_PORT"
    else
        local ghost; ghost=$(lsof -ti:"$WEBCHAT_PORT" 2>/dev/null || true)
        if [[ -n "$ghost" ]]; then
            echo -e "${RED}🔴 Web-chat not running (ghost on port $WEBCHAT_PORT: $ghost)${NC}"
        else
            echo -e "${RED}🔴 Web-chat not running${NC}"
        fi
        [[ -f "$WEBCHAT_PID" ]] && echo -e "${DIM}  (stale PID file)${NC}"
    fi
}

status_all() {
    status_gateway || true
    echo ""
    status_webchat || true
}

logs_webchat() {
    [[ -f "$WEBCHAT_LOG" ]] && cat "$WEBCHAT_LOG" || echo "${DIM}No log file: $WEBCHAT_LOG${NC}"
}

tail_webchat() {
    if [[ -f "$WEBCHAT_LOG" ]]; then
        exec tail -f "$WEBCHAT_LOG"
    else
        err "Log file not found: $WEBCHAT_LOG"
        exit 1
    fi
}

# ── Dispatch ────────────────────────────────────────────────────────────────────

CMD=${1:-}; SVC=${2:-all}

case "$CMD" in
    start)  start_"$SVC" ;;
    stop)   stop_"$SVC" ;;
    status) status_"$SVC" ;;
    logs)   logs_"$SVC" ;;
    tail)   tail_"$SVC" ;;
    *)      echo "Usage: $0 <start|stop|status|logs|tail> [gateway|webchat|all]"
            echo ""
            echo "  This script manages the LOCAL DEV environment only."
            echo "  Production deployments do not use this script."
            exit 1 ;;
esac
