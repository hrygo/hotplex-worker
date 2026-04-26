#!/usr/bin/env bash
#
# HotPlex Worker Gateway — Binary Installer
#
# Downloads or builds the hotplex binary and installs it.
# For config, secrets, and service setup, run: hotplex onboard
#
# Usage:
#   ./install.sh [options]
#
# Options:
#   --prefix PATH     Installation prefix (default: /usr/local)
#   --release TAG     Download a GitHub release (e.g. v0.3.0)
#                     Without this flag, builds from source
#   --help            Show this help

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

PREFIX="/usr/local"
BIN_NAME="hotplex"
RELEASE=""

need_arg() {
    [[ $# -lt 2 || "$2" == --* ]] && { echo -e "${RED}error: $1 requires an argument${NC}"; exit 1; }
}

while [[ $# -gt 0 ]]; do
    case $1 in
        --prefix)  need_arg "$@"; PREFIX="$2"; shift 2 ;;
        --release) need_arg "$@"; RELEASE="$2"; shift 2 ;;
        --help)    sed -n '2,/^$/p' "$0" | sed 's/^# //; /^$/d'; exit 0 ;;
        *)         echo -e "${RED}Unknown option: $1${NC}"; exit 1 ;;
    esac
done

info()  { echo -e "${GREEN}[INFO]${NC} $1"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
die()   { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

# ── Resolve OS/Arch ──────────────────────────────────────────────────────────

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case $ARCH in
    x86_64)  ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *) die "Unsupported architecture: $ARCH" ;;
esac

# ── Check root ───────────────────────────────────────────────────────────────

if [[ $EUID -ne 0 ]] && [[ "$PREFIX" == /usr* ]]; then
    die "Run with sudo for system-wide installation, or use --prefix ~/.local"
fi

# ── Install from release or build from source ────────────────────────────────

TARGET="$PREFIX/bin/$BIN_NAME"

if [[ -n "$RELEASE" ]]; then
    [[ "$RELEASE" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$ ]] \
        || die "Invalid release tag: $RELEASE (expected vX.Y.Z)"

    URL="https://github.com/hrygo/hotplex/releases/download/${RELEASE}/hotplex-${OS}-${ARCH}"
    info "Downloading hotplex ${RELEASE} for ${OS}/${ARCH}..."
    mkdir -p "$PREFIX/bin"
    if command -v curl &>/dev/null; then
        curl -fSL "$URL" -o "$TARGET"
    elif command -v wget &>/dev/null; then
        wget -q "$URL" -O "$TARGET"
    else
        die "curl or wget required"
    fi
else
    command -v go &>/dev/null || die "Go 1.26+ required. Install from https://go.dev/dl/"

    GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
    GO_MAJOR=$(echo "$GO_VERSION" | cut -d. -f1)
    GO_MINOR=$(echo "$GO_VERSION" | cut -d. -f2)
    if [[ "$GO_MAJOR" -lt 1 ]] || { [[ "$GO_MAJOR" -eq 1 ]] && [[ "$GO_MINOR" -lt 26 ]]; }; then
        die "Go $GO_VERSION too old — need 1.26+"
    fi

    info "Building hotplex for ${OS}/${ARCH}..."

    GIT_SHA=$(git rev-parse --short=8 HEAD 2>/dev/null || echo "unknown")
    BUILD_TIME=$(date '+%Y-%m-%dT%H:%M:%S%z')
    GO_VER=$(go version | awk '{print $3}')

    LDFLAGS="-s -w -X main.version=$GIT_SHA -X main.buildTime=$BUILD_TIME -X main.goVersion=$GO_VER"

    mkdir -p "$PREFIX/bin"
    go build -trimpath -ldflags="$LDFLAGS" -o "$TARGET" ./cmd/hotplex
fi

chmod +x "$TARGET"

# ── Verify ───────────────────────────────────────────────────────────────────

info "Installed: $TARGET"
"$TARGET" version

echo ""
info "Next steps:"
echo "  hotplex onboard          # Interactive setup (config, secrets, messaging)"
echo "  hotplex gateway start    # Start the gateway"
echo "  hotplex dev              # Start in dev mode"
