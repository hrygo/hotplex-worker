#!/usr/bin/env bash
#
# HotPlex Offline Bundle — Install OpenCode + Oh My OpenAgent
#
# Installs the offline bundle onto an air-gapped machine.
# No internet connection required.
#
# Usage:
#   ./install.sh [options]
#
# Options:
#   --prefix PATH     Installation prefix (default: ~/.opencode)
#   --skip-omo        Skip Oh My OpenAgent installation
#   --skip-env        Skip environment variable setup
#   --help            Show this help
#
set -euo pipefail

# ── Colors ──────────────────────────────────────────────────────────────────

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
DIM='\033[0;2m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC} $1"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
die()   { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

# ── Defaults ────────────────────────────────────────────────────────────────

PREFIX="$HOME/.opencode"
SKIP_OMO=false
SKIP_ENV=false

# ── Args ────────────────────────────────────────────────────────────────────

need_arg() {
    [[ $# -lt 2 || "$2" == --* ]] && { echo -e "${RED}error: $1 requires an argument${NC}"; exit 1; }
}

while [[ $# -gt 0 ]]; do
    case $1 in
        --prefix)    need_arg "$@"; PREFIX="$2"; shift 2 ;;
        --skip-omo)  SKIP_OMO=true; shift ;;
        --skip-env)  SKIP_ENV=true; shift ;;
        --help)      sed -n '2,/^$/p' "$0" | sed 's/^# //; /^$/d'; exit 0 ;;
        *)           die "Unknown option: $1" ;;
    esac
done

# ── Resolve bundle directory ────────────────────────────────────────────────

BUNDLE_DIR="$(cd "$(dirname "$0")" && pwd)"

[[ -f "$BUNDLE_DIR/versions.env" ]] || die "versions.env not found. Run this script from the bundle directory."

# shellcheck source=/dev/null
source "$BUNDLE_DIR/versions.env"

echo ""
echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${CYAN}  📦 Offline Bundle Installer${NC}"
echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo -e "  OpenCode:    ${GREEN}v${OPENCODE_VERSION}${NC}"
echo -e "  OMO:         ${GREEN}v${OMO_VERSION}${NC}"
echo -e "  Platform:    ${GREEN}${PLATFORM}${NC}"
echo -e "  Prefix:      ${DIM}${PREFIX}${NC}"
echo ""

# ── 1. Install OpenCode binary ──────────────────────────────────────────────

info "[1/3] Installing OpenCode binary..."

INSTALL_BIN="${PREFIX}/bin"
mkdir -p "$INSTALL_BIN"

# Find and extract OpenCode archive
OPENCODE_ARCHIVE=""
for ext in .tar.gz .zip; do
    candidate="${BUNDLE_DIR}/opencode${ext}"
    if [[ -f "$candidate" ]]; then
        OPENCODE_ARCHIVE="$candidate"
        break
    fi
done

[[ -z "$OPENCODE_ARCHIVE" ]] && die "OpenCode archive not found in bundle directory."

EXTRACT_DIR=$(mktemp -d)
trap 'rm -rf "$EXTRACT_DIR"' EXIT

case "$OPENCODE_ARCHIVE" in
    *.tar.gz)
        tar -xzf "$OPENCODE_ARCHIVE" -C "$EXTRACT_DIR"
        ;;
    *.zip)
        command -v unzip &>/dev/null || die "unzip required. Install: apt-get install unzip"
        unzip -q "$OPENCODE_ARCHIVE" -d "$EXTRACT_DIR"
        ;;
esac

# Find the opencode binary in extracted directory
OPENCODE_BIN=$(find "$EXTRACT_DIR" -name "opencode" -o -name "opencode.exe" | head -1)
[[ -z "$OPENCODE_BIN" ]] && die "opencode binary not found in archive."

cp "$OPENCODE_BIN" "${INSTALL_BIN}/opencode"
chmod +x "${INSTALL_BIN}/opencode"

# Verify
INSTALLED_VER=$("${INSTALL_BIN}/opencode" --version 2>/dev/null || echo "unknown")
info "  ✓ OpenCode ${INSTALLED_VER} installed to ${INSTALL_BIN}/opencode"

# ── 2. Install Oh My OpenAgent ─────────────────────────────────────────────

if [[ "$SKIP_OMO" != true ]]; then
    info "[2/3] Installing Oh My OpenAgent..."

    OMO_DIR="${PREFIX}/plugins/oh-my-opencode"
    mkdir -p "$OMO_DIR"

    # Find main OMO npm package
    OMO_TGZ=$(find "$BUNDLE_DIR" -maxdepth 1 -name "oh-my-opencode-[0-9]*.tgz" ! -name "oh-my-opencode-*-*" | head -1)

    if [[ -n "$OMO_TGZ" ]]; then
        # Extract npm package to plugin directory
        tar -xzf "$OMO_TGZ" -C "$OMO_DIR" --strip-components=1

        # Find and extract platform-specific native binary
        # Determine current system platform
        DETECTED_OS=$(uname -s | tr '[:upper:]' '[:lower:]')
        DETECTED_ARCH=$(uname -m)
        case "$DETECTED_ARCH" in
            x86_64)       DETECTED_ARCH="x64" ;;
            aarch64|arm64) DETECTED_ARCH="arm64" ;;
        esac

        # Check for musl
        DETECTED_MUSL=""
        if [[ "$DETECTED_OS" == "linux" ]]; then
            if [[ -f /etc/alpine-release ]] || ldd --version 2>&1 | grep -qi musl; then
                DETECTED_MUSL="-musl"
            fi
        fi

        PLATFORM_TGZ=$(find "$BUNDLE_DIR" -maxdepth 1 \
            -name "oh-my-opencode-${DETECTED_OS}-${DETECTED_ARCH}${DETECTED_MUSL}-*.tgz" | head -1)

        # Fallback: try without musl suffix
        if [[ -z "$PLATFORM_TGZ" && -n "$DETECTED_MUSL" ]]; then
            PLATFORM_TGZ=$(find "$BUNDLE_DIR" -maxdepth 1 \
                -name "oh-my-opencode-${DETECTED_OS}-${DETECTED_ARCH}-*.tgz" | head -1)
        fi

        if [[ -n "$PLATFORM_TGZ" ]]; then
            # Extract platform binary into node_modules alongside the plugin
            PLAT_DIR="${OMO_DIR}/node_modules/oh-my-opencode-${DETECTED_OS}-${DETECTED_ARCH}${DETECTED_MUSL}"
            mkdir -p "$PLAT_DIR"
            tar -xzf "$PLATFORM_TGZ" -C "$PLAT_DIR" --strip-components=1
            info "  ✓ Platform binary: $(basename "$PLATFORM_TGZ")"
        else
            warn "  No platform binary found for ${DETECTED_OS}-${DETECTED_ARCH}${DETECTED_MUSL}"
            warn "  OMO CLI features may not work, but plugin will load"
        fi

        info "  ✓ Oh My OpenAgent installed to ${OMO_DIR}"
    else
        warn "  OMO npm package not found in bundle, skipping"
    fi
else
    info "[2/3] Skipping OMO (--skip-omo)"
fi

# ── 3. Setup PATH and environment ──────────────────────────────────────────

info "[3/3] Configuring environment..."

# Add to PATH in shell config
SHELL_RC=""
CURRENT_SHELL=$(basename "${SHELL:-bash}")
case "$CURRENT_SHELL" in
    fish) SHELL_RC="$HOME/.config/fish/config.fish" ;;
    zsh)  SHELL_RC="${ZDOTDIR:-$HOME}/.zshrc" ;;
    bash) SHELL_RC="$HOME/.bashrc" ;;
    *)    SHELL_RC="$HOME/.profile" ;;
esac

PATH_LINE="export PATH=\"${INSTALL_BIN}:\$PATH\""
if [[ -f "$SHELL_RC" ]] && grep -qF "$INSTALL_BIN" "$SHELL_RC" 2>/dev/null; then
    info "  PATH already configured in ${SHELL_RC}"
else
    if [[ -w "$SHELL_RC" ]] || [[ -w "$(dirname "$SHELL_RC")" ]]; then
        echo "" >> "$SHELL_RC"
        echo "# opencode" >> "$SHELL_RC"
        echo "$PATH_LINE" >> "$SHELL_RC"
        info "  ✓ Added to PATH in ${SHELL_RC}"
    else
        warn "  Cannot write to ${SHELL_RC}"
        warn "  Add manually: ${PATH_LINE}"
    fi
fi

# Export PATH for current session
export PATH="${INSTALL_BIN}:${PATH}"

# Setup offline environment variables
if [[ "$SKIP_ENV" != true ]]; then
    ENV_FILE="${PREFIX}/env-offline.sh"

    if [[ -f "$BUNDLE_DIR/env-offline.sh" ]]; then
        cp "$BUNDLE_DIR/env-offline.sh" "$ENV_FILE"
        chmod +x "$ENV_FILE"
    fi

    # Also write into shell rc (idempotent)
    ENV_LINE="[ -f \"${ENV_FILE}\" ] && source \"${ENV_FILE}\""
    if [[ -f "$SHELL_RC" ]] && ! grep -qF "env-offline.sh" "$SHELL_RC" 2>/dev/null; then
        if [[ -w "$SHELL_RC" ]] || [[ -w "$(dirname "$SHELL_RC")" ]]; then
            echo "$ENV_LINE" >> "$SHELL_RC"
        fi
    fi

    # Source for current session
    # shellcheck source=/dev/null
    [[ -f "$ENV_FILE" ]] && source "$ENV_FILE"

    info "  ✓ Offline environment configured"
fi

# ── Done ────────────────────────────────────────────────────────────────────

echo ""
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo -e "${GREEN}  ✓ Installation complete${NC}"
echo -e "${GREEN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
echo ""
echo -e "  ${DIM}Next steps:${NC}"
echo ""
echo -e "  ${CYAN}1.${NC} Reload shell:"
echo -e "     ${DIM}source ${SHELL_RC}${NC}"
echo ""
echo -e "  ${CYAN}2.${NC} Configure internal LLM endpoint:"
echo -e "     ${DIM}mkdir -p ${PREFIX}"
echo -e "     ${DIM}cat > ${PREFIX}/opencode.json << 'EOF'"
echo -e '     {'
echo -e '       "provider": {'
echo -e '         "my-vllm": {'
echo -e '           "npm": "@ai-sdk/openai-compatible",'
echo -e '           "options": { "baseURL": "http://YOUR_LLM_HOST:PORT/v1" }'
echo -e '         }'
echo -e '       },'
echo -e '       "model": { "default": "my-vllm/YOUR_MODEL_NAME" }'
echo -e '     }'
echo -e '     EOF'"${NC}"
echo ""
echo -e "  ${CYAN}3.${NC} Start:"
echo -e "     ${DIM}cd /your/project && opencode${NC}"
echo ""
echo -e "  ${DIM}Docs: https://github.com/hrygo/hotplex${NC}"
echo ""
