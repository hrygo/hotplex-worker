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

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
DIM='\033[0;2m'
NC='\033[0m'

info()  { echo -e "${GREEN}[INFO]${NC} $1"; }
warn()  { echo -e "${YELLOW}[WARN]${NC} $1"; }
die()   { echo -e "${RED}[ERROR]${NC} $1"; exit 1; }

PREFIX="$HOME/.opencode"
SKIP_OMO=false
SKIP_ENV=false

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

BUNDLE_DIR="$(cd "$(dirname "$0")" && pwd)"

[[ -f "$BUNDLE_DIR/versions.env" ]] || die "versions.env not found. Run this script from the bundle directory."

# shellcheck source=/dev/null
source "$BUNDLE_DIR/versions.env"

# OpenCode global config directory (Stage 2 loading: ~/.config/opencode/)
XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-$HOME/.config}"
OPENCODE_CONFIG_DIR="${XDG_CONFIG_HOME}/opencode"

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

info "[1/4] Installing OpenCode binary..."

INSTALL_BIN="${PREFIX}/bin"
mkdir -p "$INSTALL_BIN"

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

OPENCODE_BIN=$(find "$EXTRACT_DIR" -name "opencode" -o -name "opencode.exe" | head -1)
[[ -z "$OPENCODE_BIN" ]] && die "opencode binary not found in archive."

cp "$OPENCODE_BIN" "${INSTALL_BIN}/opencode"
chmod +x "${INSTALL_BIN}/opencode"

INSTALLED_VER=$("${INSTALL_BIN}/opencode" --version 2>/dev/null || echo "unknown")
info "  ✓ OpenCode ${INSTALLED_VER} installed to ${INSTALL_BIN}/opencode"

# ── 2. Install Oh My OpenAgent ─────────────────────────────────────────────

if [[ "$SKIP_OMO" != true ]]; then
    info "[2/4] Installing Oh My OpenAgent..."

    OMO_DIR="${PREFIX}/plugins/oh-my-opencode"
    mkdir -p "$OMO_DIR"

    OMO_TGZ=$(find "$BUNDLE_DIR" -maxdepth 1 -name "oh-my-opencode-[0-9]*.tgz" ! -name "oh-my-opencode-*-*" | head -1)

    if [[ -n "$OMO_TGZ" ]]; then
        tar -xzf "$OMO_TGZ" -C "$OMO_DIR" --strip-components=1

        # Detect current platform for native binary
        local_os=$(uname -s | tr '[:upper:]' '[:lower:]')
        local_arch=$(uname -m)
        case "$local_arch" in
            x86_64)       local_arch="x64" ;;
            aarch64|arm64) local_arch="arm64" ;;
        esac

        # musl detection
        local_musl=""
        if [[ "$local_os" == "linux" ]]; then
            if [[ -f /etc/alpine-release ]] || { command -v ldd &>/dev/null && ldd --version 2>&1 | grep -qi musl; }; then
                local_musl="-musl"
            fi
        fi

        # Find matching platform tgz (try musl first, then glibc fallback)
        PLAT_TGZ=""
        if [[ -n "$local_musl" ]]; then
            PLAT_TGZ=$(find "$BUNDLE_DIR" -maxdepth 1 \
                -name "oh-my-opencode-${local_os}-${local_arch}${local_musl}-*.tgz" | head -1)
        fi
        if [[ -z "$PLAT_TGZ" ]]; then
            PLAT_TGZ=$(find "$BUNDLE_DIR" -maxdepth 1 \
                -name "oh-my-opencode-${local_os}-${local_arch}-*.tgz" ! -name "*-musl-*" ! -name "*-baseline-*" | head -1)
        fi
        # baseline fallback
        if [[ -z "$PLAT_TGZ" && "$local_arch" == "x64" ]]; then
            PLAT_TGZ=$(find "$BUNDLE_DIR" -maxdepth 1 \
                -name "oh-my-opencode-${local_os}-${local_arch}-baseline-*.tgz" | head -1)
        fi

        if [[ -n "$PLAT_TGZ" ]]; then
            PLAT_PKG_NAME=$(basename "$PLAT_TGZ" | sed 's/-[0-9].*\.tgz$//')
            PLAT_DIR="${OMO_DIR}/node_modules/${PLAT_PKG_NAME}"
            mkdir -p "$PLAT_DIR"
            tar -xzf "$PLAT_TGZ" -C "$PLAT_DIR" --strip-components=1
            info "  ✓ Platform binary: $(basename "$PLAT_TGZ")"
        else
            warn "  No platform binary for ${local_os}-${local_arch}${local_musl}"
            warn "  OMO CLI features may not work, but plugin will still load"
        fi

        info "  ✓ Oh My OpenAgent installed to ${OMO_DIR}"
    else
        warn "  OMO npm package not found in bundle, skipping"
    fi
else
    info "[2/4] Skipping OMO (--skip-omo)"
fi

# ── 3. Register OMO plugin via auto-discovery ───────────────────────────────

# OpenCode auto-discovers plugins from {plugin,plugins}/*.{ts,js}
# under each config directory (~/.config/opencode/ and .opencode/).
# We create an entrypoint JS re-export that points to the installed plugin.

if [[ "$SKIP_OMO" != true && -n "${OMO_DIR:-}" && -d "$OMO_DIR" ]]; then
    info "[3/4] Registering OMO plugin via auto-discovery..."

    PLUGINS_DIR="${OPENCODE_CONFIG_DIR}/plugins"
    mkdir -p "$PLUGINS_DIR"

    ENTRYPOINT="${PLUGINS_DIR}/oh-my-opencode.js"

    if [[ -f "$ENTRYPOINT" ]]; then
        info "  Plugin already linked in ${PLUGINS_DIR}"
    else
        # Resolve the plugin's dist/index.js entry point
        PLUGIN_INDEX="${OMO_DIR}/dist/index.js"
        if [[ ! -f "$PLUGIN_INDEX" ]]; then
            # npm tgz extracts to package/, strip-components=1 should put dist/ at top
            PLUGIN_INDEX="${OMO_DIR}/dist/index.js"
        fi

        if [[ -f "$PLUGIN_INDEX" ]]; then
            cat > "$ENTRYPOINT" << 'ENTRYEOF'
// Auto-generated by hotplex offline installer
// Re-exports Oh My OpenAgent plugin for OpenCode auto-discovery
export * from "../../../../.opencode/plugins/oh-my-opencode/dist/index.js";
ENTRYEOF
            # Rewrite with actual resolved path
            RESOLVED_PATH="$(realpath "$PLUGIN_INDEX" 2>/dev/null || echo "$PLUGIN_INDEX")"
            echo "// Auto-generated by hotplex offline installer" > "$ENTRYPOINT"
            echo "// Re-exports Oh My OpenAgent plugin for OpenCode auto-discovery" >> "$ENTRYPOINT"
            echo "export * from \"file://${RESOLVED_PATH}\";" >> "$ENTRYPOINT"
            info "  ✓ Plugin linked: ${ENTRYPOINT} → ${RESOLVED_PATH}"
        else
            warn "  Plugin entry point not found at ${PLUGIN_INDEX}"
            warn "  Creating config-based registration as fallback..."

            mkdir -p "$OPENCODE_CONFIG_DIR"
            OPENCODE_JSON="${OPENCODE_CONFIG_DIR}/opencode.json"

            if [[ -f "$OPENCODE_JSON" ]] && grep -q "oh-my-opencode" "$OPENCODE_JSON" 2>/dev/null; then
                info "  Plugin already registered in ${OPENCODE_JSON}"
            else
                RELATIVE_PLUGIN_PATH="$(realpath --relative-to="$OPENCODE_CONFIG_DIR" "$OMO_DIR" 2>/dev/null || echo "$OMO_DIR")"
                python3 -c "
import json, re
path = '$OPENCODE_JSON'
try:
    with open(path, 'r') as f:
        data = json.loads(re.sub(r'//.*?\n|/\*.*?\*/', '', f.read(), flags=re.DOTALL))
except: data = {}
data.setdefault('plugin', []).append('$RELATIVE_PLUGIN_PATH')
with open(path, 'w') as f:
    json.dump(data, f, indent=2, ensure_ascii=False)
    f.write('\n')
" 2>/dev/null && info "  ✓ Plugin registered in ${OPENCODE_JSON} via 'plugin' key" || \
                warn "  Could not auto-register. Add to opencode.json: \"plugin\": [\"${RELATIVE_PLUGIN_PATH}\"]"
            fi
        fi
    fi
else
    info "[3/4] Skipping plugin registration (--skip-omo or no OMO installed)"
fi

# ── 4. Setup PATH and environment ──────────────────────────────────────────

info "[4/4] Configuring environment..."

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

export PATH="${INSTALL_BIN}:${PATH}"

if [[ "$SKIP_ENV" != true ]]; then
    ENV_FILE="${PREFIX}/env-offline.sh"

    if [[ -f "$BUNDLE_DIR/env-offline.sh" ]]; then
        cp "$BUNDLE_DIR/env-offline.sh" "$ENV_FILE"
        chmod +x "$ENV_FILE"
    fi

    ENV_LINE="[ -f \"${ENV_FILE}\" ] && source \"${ENV_FILE}\""
    if [[ -f "$SHELL_RC" ]] && ! grep -qF "env-offline.sh" "$SHELL_RC" 2>/dev/null; then
        if [[ -w "$SHELL_RC" ]] || [[ -w "$(dirname "$SHELL_RC")" ]]; then
            echo "$ENV_LINE" >> "$SHELL_RC"
        fi
    fi

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
echo -e "     ${DIM}mkdir -p ${OPENCODE_CONFIG_DIR}"
echo -e "     ${DIM}Edit ${OPENCODE_JSON} and add:${NC}"
echo ""
echo -e "     ${DIM}\"provider\": {"
echo -e "       \"my-vllm\": {"
echo -e "         \"npm\": \"@ai-sdk/openai-compatible\","
echo -e "         \"options\": { \"baseURL\": \"http://YOUR_LLM_HOST:PORT/v1\" }"
echo -e "       }"
echo -e "     },"
echo -e "     \"model\": { \"default\": \"my-vllm/YOUR_MODEL_NAME\" }${NC}"
echo ""
echo -e "  ${CYAN}3.${NC} Start:"
echo -e "     ${DIM}cd /your/project && opencode${NC}"
echo ""
echo -e "  ${DIM}Configs loaded from:${NC}"
echo -e "  ${DIM}  Global:  ${OPENCODE_JSON}${NC}"
echo -e "  ${DIM}  Project: .opencode/opencode.json (per-project)${NC}"
echo ""
