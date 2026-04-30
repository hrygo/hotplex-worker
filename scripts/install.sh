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
    if [[ $# -lt 2 || "$2" == --* ]]; then
        echo -e "${RED}error: $1 requires an argument${NC}" >&2; exit 1
    fi
}

# ── Parse arguments ──────────────────────────────────────────────────────────

while [[ $# -gt 0 ]]; do
    case $1 in
        --prefix)           need_arg "$@"; PREFIX="$2"; shift 2 ;;
        --release|--version) need_arg "$@"; RELEASE="$2"; shift 2 ;;
        --latest)           RELEASE="__LATEST__"; shift ;;
        --help)
            if [[ -r "$0" ]]; then
                sed -n '2,/^$/p' "$0" | sed 's/^# //; /^$/d'
            else
                echo "Usage: ./install.sh --latest | --release <tag> [--prefix <path>]"
            fi
            exit 0 ;;
        *)                  echo -e "${RED}Unknown option: $1${NC}" >&2; exit 1 ;;
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

# Choose checksum tool: sha256sum (Linux/coreutils) or shasum (macOS)
if command -v sha256sum &>/dev/null; then
    HASH_CMD="sha256sum"
elif command -v shasum &>/dev/null; then
    HASH_CMD="shasum"
else
    HASH_CMD=""
    warn "Neither sha256sum nor shasum found — checksum verification will be skipped"
fi

# ── Check permissions ────────────────────────────────────────────────────────

if [[ $EUID -ne 0 ]] && [[ "$PREFIX" == /usr* ]]; then
    die "Run with sudo for system-wide installation, or use --prefix ~/.local"
fi

# ── Resolve release tag ──────────────────────────────────────────────────────

if [[ "$RELEASE" == "__LATEST__" ]]; then
    info "Querying latest release from GitHub..."
    LATEST_URL="https://api.github.com/repos/${REPO}/releases/latest"
    API_OUTPUT=""
    if [[ "$DL_CMD" == "curl" ]]; then
        API_OUTPUT=$(curl -fsSL -w "\n%{http_code}" "$LATEST_URL" 2>/dev/null) || true
    else
        API_OUTPUT=$(wget -qO- "$LATEST_URL" 2>/dev/null) || true
    fi
    # Check for rate limit (HTTP 403)
    HTTP_STATUS=$(echo "$API_OUTPUT" | tail -1)
    if [[ "$HTTP_STATUS" == "403" ]]; then
        die "GitHub API rate limit exceeded. Specify --release <tag> manually."
    fi
    RELEASE=$(echo "$API_OUTPUT" | grep '"tag_name"' | head -1 | sed -E 's/.*"([^"]+)".*/\1/')
    [[ -z "$RELEASE" ]] && die "Failed to detect latest release. Specify --release <tag> manually."
    info "Latest release: ${RELEASE}"
fi

[[ "$RELEASE" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$ ]] \
    || die "Invalid release tag: $RELEASE (expected vX.Y.Z)"

# ── Download ─────────────────────────────────────────────────────────────────

TARGET="$PREFIX/bin/$BIN_NAME"
WORK_DIR=$(mktemp -d)
trap 'rm -rf "$WORK_DIR"' EXIT

BINARY_NAME="hotplex-${OS}-${ARCH}"
CHECKSUM_NAME="checksums.txt"
BASE_URL="https://github.com/${REPO}/releases/download/${RELEASE}"

BINARY_PATH="${WORK_DIR}/${BINARY_NAME}"
CHECKSUM_PATH="${WORK_DIR}/${CHECKSUM_NAME}"

info "Downloading hotplex ${RELEASE} for ${OS}/${ARCH}..."
mkdir -p "$PREFIX/bin"

DL_OK=false
if [[ "$DL_CMD" == "curl" ]]; then
    curl -fSL --progress-bar "${BASE_URL}/${BINARY_NAME}" -o "$BINARY_PATH" && DL_OK=true
else
    wget -q --show-progress "${BASE_URL}/${BINARY_NAME}" -O "$BINARY_PATH" && DL_OK=true
fi

$DL_OK || die "Download failed. Release ${RELEASE} may not include a binary for ${OS}/${ARCH}.

  Check available releases: https://github.com/${REPO}/releases"
[[ $(stat -f%z "$BINARY_PATH" 2>/dev/null || stat -c%s "$BINARY_PATH") -eq 0 ]] \
    && die "Downloaded file is empty — release binary may not exist for this platform."

# ── Verify checksum ──────────────────────────────────────────────────────────

if [[ -n "$HASH_CMD" ]]; then
    info "Downloading checksums..."
    DL_OK=false
    if [[ "$DL_CMD" == "curl" ]]; then
        curl -fsSL "${BASE_URL}/${CHECKSUM_NAME}" -o "$CHECKSUM_PATH" 2>/dev/null && DL_OK=true
    else
        wget -q "${BASE_URL}/${CHECKSUM_NAME}" -O "$CHECKSUM_PATH" 2>/dev/null && DL_OK=true
    fi

    if $DL_OK && [[ -f "$CHECKSUM_PATH" ]]; then
        EXPECTED=$(grep "$BINARY_NAME" "$CHECKSUM_PATH" | awk '{print $1}')
        if [[ -n "$EXPECTED" ]]; then
            if [[ "$HASH_CMD" == "sha256sum" ]]; then
                ACTUAL=$(sha256sum "$BINARY_PATH" | awk '{print $1}')
            else
                ACTUAL=$(shasum -a 256 "$BINARY_PATH" | awk '{print $1}')
            fi
            if [[ "$EXPECTED" != "$ACTUAL" ]]; then
                die "Checksum mismatch! Expected: $EXPECTED  Actual: $ACTUAL"
            fi
            info "Checksum verified."
        else
            warn "Binary not found in checksums file — skipping verification."
        fi
    else
        warn "Checksums file unavailable — skipping verification."
    fi
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
            fish) echo -e "  Add to ${RC_FILE}:  ${CYAN}fish_add_path ${BIN_DIR}${NC}" ;;
            *)    echo -e "  Add to ${RC_FILE}:  ${CYAN}export PATH=\"${BIN_DIR}:\$PATH\"${NC}" ;;
        esac
        echo -e "  Then: ${CYAN}source ${RC_FILE}${NC}"
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
