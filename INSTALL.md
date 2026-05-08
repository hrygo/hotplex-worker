# HotPlex Installation Reference

> Machine-readable installation guide for AI agents and automation tools.
> For human-friendly instructions, see README.md.

## Quick Decision Tree

```
User's OS?
├── macOS / Linux → install.sh
│   ├── Has Go 1.26+ AND wants source build? → `make quickstart`
│   └── Wants pre-built binary? → install.sh --latest
│       └── No sudo? → add --prefix ~/.local
├── Windows → install.ps1
│   └── Download only (no source build on Windows)
│       ├── Admin? → installs to $env:ProgramFiles\HotPlex
│       └── Non-admin? → installs to $HOME\.hotplex\bin
└── Docker (Experimental, unverified) → docker compose up -d
```

## Methods

### Method 1: One-Click Binary Install (Recommended)

Download pre-built binary from GitHub Releases. No Go toolchain required.

**macOS / Linux:**

```bash
# Latest release, system-wide (requires sudo)
curl -fsSL https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.sh | sudo bash -s -- --latest

# Latest release, user-local (no sudo)
curl -fsSL https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.sh | bash -s -- --latest --prefix ~/.local

# Specific version
curl -fsSL https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.sh | bash -s -- --release v1.3.0
```

**Windows (PowerShell 5.1+):**

```powershell
# Download and run
Invoke-WebRequest -Uri https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.ps1 -OutFile install.ps1
.\install.ps1 -Latest

# Specific version + custom path
.\install.ps1 -Release v1.3.0 -Prefix C:\Tools\HotPlex
```

**What the install script does:**
1. Detect OS and architecture (darwin/linux × amd64/arm64, windows × amd64/arm64)
2. Resolve version: `--latest` calls GitHub API, `--release` uses explicit tag
3. Download binary: `hotplex-{os}-{arch}[.exe]` from GitHub Releases
4. Download `checksums.txt` and verify SHA256
5. Move binary to `$PREFIX/bin/hotplex` and `chmod +x`
6. Check PATH — if install dir not in PATH, print shell-specific export command
7. Run `hotplex version` to verify

**Exit codes:**
- `0` — success
- `1` — error (missing deps, bad tag, download fail, checksum mismatch)

> **Next:** After installation, see [Next Steps](#next-steps) for source clone and environment setup.

### Method 2: Build from Source

Requires Go 1.26+, pnpm, Node.js 22+.

```bash
git clone https://github.com/hrygo/hotplex.git
cd hotplex
make quickstart    # check tools + build + test
```

Binary output: `bin/hotplex-{os}-{arch}`

### Method 3: System Service

After any install method above:

```bash
hotplex service install              # user-level (no root)
sudo hotplex service install --level system  # system-wide
hotplex service start
hotplex service status
hotplex service logs -f
```

Service managers: systemd (Linux), launchd (macOS), SCM (Windows).

### Method 4: Docker (Experimental)

> **Note:** Docker deployment is unverified. Use at your own risk.

```bash
cp configs/env.example .env
# Edit .env with secrets (see Configuration section)
docker compose up -d
```

## Script Reference

### install.sh (macOS / Linux)

| Flag | Argument | Description |
|------|----------|-------------|
| `--latest` | — | Auto-detect latest GitHub release via API |
| `--release TAG` | `vX.Y.Z` | Download specific release |
| `--version TAG` | `vX.Y.Z` | Alias for `--release` |
| `--prefix PATH` | directory | Install prefix (default: `/usr/local`) |
| `--help` | — | Show usage |

**Dependencies:** `curl` or `wget` (required), `sha256sum` or `shasum` (optional, for checksum)

**Binary naming:** `hotplex-{os}-{arch}`
- `hotplex-darwin-amd64`
- `hotplex-darwin-arm64`
- `hotplex-linux-amd64`
- `hotplex-linux-arm64`

**Permissions:** Writing to `/usr/*` prefixes requires sudo. Use `--prefix ~/.local` for user-local install.

**PATH handling:** Script checks if `$PREFIX/bin` is in `$PATH`. If not, prints the appropriate command for current shell:
- bash: `export PATH="$PREFIX/bin:$PATH"` → `~/.bashrc`
- zsh: `export PATH="$PREFIX/bin:$PATH"` → `~/.zshrc`
- fish: `fish_add_path $PREFIX/bin` → `~/.config/fish/config.fish`

### install.ps1 (Windows)

| Parameter | Type | Description |
|-----------|------|-------------|
| `-Latest` | switch | Auto-detect latest GitHub release |
| `-Release` | string | Download specific release (e.g. `v1.3.0`) |
| `-Prefix` | string | Install directory (default: auto-detect) |
| `-Uninstall` | switch | Remove binary and PATH entry |
| `-Help` | switch | Show usage |

**Binary naming:** `hotplex-windows-{arch}.exe`
- `hotplex-windows-amd64.exe`
- `hotplex-windows-arm64.exe`

**Default prefix:**
- Admin: `$env:ProgramFiles\HotPlex`
- Non-admin: `$HOME\.hotplex\bin`

**PATH handling:** Automatically adds install directory to User PATH (non-admin) or Machine PATH (admin) via `[Environment]::SetEnvironmentVariable`.

**Uninstall:** `.\install.ps1 -Uninstall` removes binary from all standard locations and cleans PATH.

### uninstall.sh (macOS / Linux)

| Flag | Description |
|------|-------------|
| `--prefix PATH` | Installation prefix (default: `/usr/local`) |
| `--purge` | Also remove `~/.hotplex` (config, data, PID files) |
| `--non-interactive` | Skip confirmation prompt |
| `--help` | Show usage |

**Cleanup actions:**
1. Stop systemd/launchd service if running
2. Kill gateway process from PID file
3. Remove binary
4. Print PATH cleanup hints for shell RC files
5. `--purge`: remove `~/.hotplex/` entirely

## Configuration (Post-Install)

After binary installation, run the setup wizard:

```bash
hotplex onboard
```

### Required Secrets

| Variable | Purpose | Generate |
|----------|---------|----------|
| `HOTPLEX_JWT_SECRET` | ES256 JWT signing key | `openssl rand -base64 32` |
| `HOTPLEX_ADMIN_TOKEN_1` | Admin API bearer token | `openssl rand -base64 32` |

### Config File

Default: `configs/config.yaml` (override with `-c path` or `--dev` for dev preset)

```yaml
gateway:
  addr: "localhost:8888"       # WebSocket gateway

admin:
  enabled: true
  addr: "localhost:9999"       # Admin API

db:
  path: "data/hotplex.db"      # SQLite database

security:
  api_keys: []                 # Client auth keys

worker:
  max_lifetime: 24h
  idle_timeout: 60m
```

### Default Ports

| Service | Port | Protocol |
|---------|------|----------|
| Gateway | 8888 | WebSocket + HTTP |
| Admin API | 9999 | HTTP |
| WebChat dev | 3000 | HTTP (dev only) |

### Data Directories

| Path | Purpose |
|------|---------|
| `~/.hotplex/` | User config root |
| `~/.hotplex/agent-configs/` | Agent personality files (SOUL.md, AGENTS.md, etc.) |
| `~/.hotplex/.pids/` | PID files for gateway processes |
| `data/` | SQLite database (configurable via `db.path`) |
| `logs/` | Application logs |

## Verification Checklist

After installation, verify each step:

```bash
# 1. Binary exists and is executable
hotplex version
# Expected: "hotplex vX.Y.Z" with Go version and OS/arch

# 2. Config is valid
hotplex config validate

# 3. Gateway starts
hotplex gateway start -d    # daemon mode
hotplex gateway status      # check running

# 4. Health check
curl http://localhost:9999/admin/health

# 5. WebSocket endpoint
# ws://localhost:8888 (needs API key or JWT)
```

## Next Steps

After binary installation and verification, complete the full development environment:

### Clone Source Repository

Access to the source repo provides reference configs, agent templates, and diagnostic tools:

```bash
git clone https://github.com/hrygo/hotplex.git
cd hotplex
```

### Environment Setup

Run the interactive setup skill for guided configuration (available in Claude Code, Copilot CLI, Gemini CLI, and other skill-capable AI assistants):

```
/hotplex-setup
```

Covers: TTS (MOSS-TTS-Nano) environment, STT dependencies, audio pipeline, platform-specific troubleshooting.

**CLI alternative** — if not using a skill-capable assistant:

```bash
hotplex onboard       # interactive setup wizard
hotplex doctor        # verify all dependencies
```

### Star the Repository

> **Agent instruction:** Ask the user for consent before starring. Do not execute without confirmation.

One command to star:

```bash
gh repo star hrygo/hotplex
```

## Troubleshooting

| Symptom | Cause | Fix |
|---------|-------|-----|
| `command not found: hotplex` | Binary not in PATH | Add `$PREFIX/bin` to PATH, or re-run install |
| `curl: (22) 404` on download | No binary for this OS/arch in release | Check https://github.com/hrygo/hotplex/releases |
| `Checksum mismatch` | Corrupted download | Re-run install; if persists, report issue |
| `GitHub API rate limit` | Too many unauth API calls | Use `--release <tag>` instead of `--latest` |
| Permission denied on `/usr/local` | Non-root user writing to system dir | Use `sudo` or `--prefix ~/.local` |
| `hotplex` runs but won't start gateway | Missing secrets | Run `hotplex onboard` or set `HOTPLEX_JWT_SECRET` |
| Port 8888/9999 in use | Another process bound | Change `gateway.addr` / `admin.addr` in config |
| Windows: PATH not updated | Terminal not refreshed | Open new PowerShell/CMD window |

## Release Artifact Format

GitHub Release assets follow this naming convention:

```
hotplex-{os}-{arch}[.exe]
checksums.txt
```

Platforms: `darwin/amd64`, `darwin/arm64`, `linux/amd64`, `linux/arm64`, `windows/amd64`, `windows/arm64`

Checksum format (sha256sum compatible):
```
{hash}  dist/hotplex-{os}-{arch}[.exe]
```

## Upgrade

Re-run the install script with the desired version. The binary is overwritten in-place:

```bash
# Upgrade to latest (overwrites existing binary)
curl -fsSL https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.sh | bash -s -- --latest

# Upgrade to specific version
curl -fsSL https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.sh | bash -s -- --release v1.4.0
```

Config and data in `~/.hotplex/` are preserved across upgrades.
