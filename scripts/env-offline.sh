#!/usr/bin/env bash
#
# OpenCode Offline Environment Variables
#
# Source this file to configure OpenCode for air-gapped operation.
# Automatically sourced by install-offline-bundle.sh.
#
# Usage:
#   source ~/.opencode/env-offline.sh
#

# Disable models.dev fetch — prevents "Unable to connect" errors
export OPENCODE_DISABLE_MODELS_FETCH=1

# Disable LSP binary auto-download (pyright, typescript-language-server, etc.)
export OPENCODE_DISABLE_LSP_DOWNLOAD=1

# Disable automatic update checks
export OPENCODE_DISABLE_AUTOUPDATE=1

# Disable default plugin installation (prevents npm install attempts)
export OPENCODE_DISABLE_DEFAULT_PLUGINS=1

# Optional: Custom models URL for internal mirror
# Uncomment and set to your internal models.json endpoint if available
# export OPENCODE_MODELS_URL=http://YOUR_INTERNAL_MIRROR/models.json

# Optional: Disable telemetry
# export OPENCODE_DISABLE_TELEMETRY=1
