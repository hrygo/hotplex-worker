#!/usr/bin/env bash
#
# HotPlex Worker Gateway — Binary Installer (macOS / Linux)
#
# Downloads a GitHub release binary and installs it.
# For config, secrets, and service setup, run: hotplex onboard
#
# Usage:
#   ./install.sh [options]
#
# Options:
#   --prefix PATH     Installation prefix (default: /usr/local)
#   --release TAG     Download a specific release (e.g. v1.3.0)
#   --latest          Download the latest release
#   --version TAG     Alias for --release
#   --help            Show this help
#
# Examples:
#   ./install.sh --latest --prefix ~/.local
#   ./install.sh --release v1.3.0
#   curl -fsSL https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.sh | bash -s -- --latest

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
BOLD='\033[1m'
DIM='\033[2m'
NC='\033[0m'

PREFIX="/usr/local"
BIN_NAME="hotplex"
RELEASE=""
REPO="hrygo/hotplex"

info()  { echo -e "${GREEN}[INFO]${NC} $1"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
die()   { echo -e "${RED}[ERROR]${NC} $1" >&2; exit 1; }

need_arg() {
    [[ $# -lt 2 || "$2" == --* ]] && { echo -e "${RED}error: $1 requires an argument${NC}" >&2; exit 1; }
}

# ── Parse arguments ──────────────────────────────────────────────────────────

while [[ $# -gt 0 ]]; do
    case $1 in
        --prefix)      need_arg "$@"; PREFIX="$2"; shift 2 ;;
        --release|--version) need_arg "$@"; RELEASE="$2"; shift 2 ;;
        --latest)      RELEASE="__LATEST__"; shift ;;
        --help)        sed -n '2,/^$/p' "$0" | sed 's/^# //; /^$/d'; exit 0 ;;
        *)             echo -e "${RED}Unknown option: $1${NC}" >&2; exit 1 ;;
    esac
done

[[ -z "$RELEASE" ]] && die "No installation mode specified. Use --latest or --release <tag>."

# ── Resolve OS / Arch ────────────────────────────────────────────────────────

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)         ARCH="amd64" ;;
    aarch64|arm64)  ARCH="arm64" ;;
    *)              die "Unsupported architecture: $ARCH" ;;
esac

info "Platform: ${OS}/${ARCH}"

# ── Check dependencies ───────────────────────────────────────────────────────

if command -v curl &>/dev/null; then
    DL_CMD="curl"
elif command -v wget &>/dev/null; then
    DL_CMD="wget"
else
    die "curl or wget is required."
fi

command -v sha256sum &>/dev/null || command -v shasum &>/dev/null \
    || warn "sha256sum not found — checksum verification will be skipped"

# ── Check permissions ────────────────────────────────────────────────────────

if [[ $EUID -ne 0 ]] && [[ "$PREFIX" == /usr* ]]; then
    die "Run with sudo for system-wide installation, or use --prefix ~/.local"
fi

# ── Resolve release tag ──────────────────────────────────────────────────────

if [[ "$RELEASE" == "__LATEST__" ]]; then
    info "Querying latest release from GitHub..."
    LATEST_URL="https://api.github.com/repos/${REPO}/releases/latest"
    if [[ "$DL_CMD" == "curl" ]]; then
        RELEASE=$(curl -fsSL "$LATEST_URL" | grep '"tag_name"' | head -1 | sed -E 's/.*"([^"]+)".*/\1/')
    else
        RELEASE=$(wget -qO- "$LATEST_URL" | grep '"tag_name"' | head -1 | sed -E 's/.*"([^"]+)".*/\1/')
    fi
    [[ -z "$RELEASE" ]] && die "Failed to detect latest release. Specify --release <tag> manually."
    info "Latest release: ${RELEASE}"
fi

[[ "$RELEASE" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$ ]] \
    || die "Invalid release tag: $RELEASE (expected vX.Y.Z)"

# ── Download ─────────────────────────────────────────────────────────────────

TARGET="$PREFIX/bin/$BIN_NAME"
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

BINARY_NAME="hotplex-${OS}-${ARCH}"
CHECKSUM_NAME="checksums.txt"
BASE_URL="https://github.com/${REPO}/releases/download/${RELEASE}"

BINARY_PATH="${TMPDIR}/${BINARY_NAME}"
CHECKSUM_PATH="${TMPDIR}/${CHECKSUM_NAME}"

info "Downloading hotplex ${RELEASE} for ${OS}/${ARCH}..."
mkdir -p "$PREFIX/bin"

if [[ "$DL_CMD" == "curl" ]]; then
    curl -fSL --progress-bar "${BASE_URL}/${BINARY_NAME}" -o "$BINARY_PATH"
else
    wget -q --show-progress "${BASE_URL}/${BINARY_NAME}" -O "$BINARY_PATH"
fi

# ── Verify checksum ──────────────────────────────────────────────────────────

VERIFY=true
if command -v sha256sum &>/dev/null; then
    info "Downloading checksums..."
    if [[ "$DL_CMD" == "curl" ]]; then
        curl -fsSL "${BASE_URL}/${CHECKSUM_NAME}" -o "$CHECKSUM_PATH" 2>/dev/null || VERIFY=false
    else
        wget -q "${BASE_URL}/${CHECKSUM_NAME}" -O "$CHECKSUM_PATH" 2>/dev/null || VERIFY=false
    fi

    if $VERIFY && [[ -f "$CHECKSUM_PATH" ]]; then
        EXPECTED=$(grep "$BINARY_NAME" "$CHECKSUM_PATH" | awk '{print $1}')
        if [[ -n "$EXPECTED" ]]; then
            ACTUAL=$(sha256sum "$BINARY_PATH" | awk '{print $1}')
            if [[ "$EXPECTED" != "$ACTUAL" ]]; then
                die "Checksum mismatch!\n  Expected: $EXPECTED\n  Actual:   $ACTUAL"
            fi
            info "Checksum verified."
        else
            warn "Binary not found in checksums file — skipping verification."
        fi
    else
        warn "Checksums file unavailable — skipping verification."
    fi
else
    warn "sha256sum not found — skipping checksum verification."
fi

# ── Install ──────────────────────────────────────────────────────────────────

mv "$BINARY_PATH" "$TARGET"
chmod +x "$TARGET"

# ── Verify installation ─────────────────────────────────────────────────────

info "Installed: ${TARGET}"
"$TARGET" version

# ── PATH check ───────────────────────────────────────────────────────────────

BIN_DIR="$PREFIX/bin"
if [[ ":$PATH:" != *":${BIN_DIR}:"* ]]; then
    SHELL_NAME=$(basename "${SHELL:-bash}")
    case "$SHELL_NAME" in
        zsh)  RC_FILE="$HOME/.zshrc" ;;
        bash) RC_FILE="$HOME/.bashrc" ;;
        fish) RC_FILE="$HOME/.config/fish/config.fish" ;;
        *)    RC_FILE="" ;;
    esac

    echo ""
    warn "${BIN_DIR} is not in your PATH."
    if [[ -n "$RC_FILE" ]]; then
        case "$SHELL_NAME" in
            fish) echo -e "  Add this line to ${RC_FILE}:" ;;
            *)    echo -e "  Add this line to ${RC_FILE}:" ;;
        esac
        case "$SHELL_NAME" in
            fish) echo -e "  ${CYAN}set -gx PATH ${BIN_DIR} \$PATH${NC}" ;;
            *)    echo -e "  ${CYAN}export PATH=\"${BIN_DIR}:\$PATH\"${NC}" ;;
        esac
        echo -e "  Then run: ${CYAN}source ${RC_FILE}${NC}"
    fi
fi

# ── Next steps ───────────────────────────────────────────────────────────────

echo ""
echo -e "${BOLD}Next steps:${NC}"
echo -e "  ${CYAN}hotplex onboard${NC}          # Interactive setup (config, secrets, messaging)"
echo -e "  ${CYAN}hotplex gateway start${NC}    # Start the gateway"
echo -e "  ${CYAN}hotplex dev${NC}              # Start in dev mode"
echo ""
echo -e "${DIM}Shell completions: hotplex completion bash|zsh|fish${NC}"
