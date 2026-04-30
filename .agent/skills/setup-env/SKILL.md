---
name: setup-env
description: Interactive .env configuration for HotPlex Gateway. Use this skill whenever the user wants to set up, configure, or update the .env file for HotPlex — including first-time setup, adding new messaging adapters (Slack/Feishu), configuring STT, updating tokens, switching access policies, tuning resource limits, or enabling observability. Also trigger when the user mentions "configure slack", "configure feishu", "setup messaging", "update .env", "add allowlist", "get user IDs", "setup OTel", "configure workers", or "tune resource limits". Do NOT trigger for general .env edits unrelated to HotPlex.
---

# HotPlex .env Configuration

Interactive workflow to configure `.env` for HotPlex Gateway with Slack and/or Feishu messaging adapters.

## Prerequisites

- Project root: current working directory
- Example file: `configs/env.example`
- Target file: `.env` (project root, gitignored)

## Workflow

### Step 1: Assess Current State

1. Check if `.env` exists. If not, `cp configs/env.example .env`.
2. Read current `.env` content.
3. Build a status map, presenting only missing or misconfigured items:

| Section | Key Fields |
|---------|-----------|
| Secrets | `HOTPLEX_JWT_SECRET`, `HOTPLEX_ADMIN_TOKEN_1` |
| Client Auth | `HOTPLEX_SECURITY_API_KEY_1..N` |
| WorkDir | `SLACK_WORK_DIR`, `FEISHU_WORK_DIR` |
| Slack | `BOT_TOKEN`, `APP_TOKEN` |
| Feishu | `APP_ID`, `APP_SECRET` |
| Access Policy | `DM_POLICY`, `GROUP_POLICY`, `ALLOW_FROM`, `ALLOW_DM_FROM`, `ALLOW_GROUP_FROM` |
| STT | `STT_PROVIDER`, `STT_LOCAL_MODE` |
| Agent Config | `AGENT_CONFIG_ENABLED`, `AGENT_CONFIG_DIR` |
| Worker | `WORKER_CLAUDE_CODE_COMMAND`, `WORKER_OPENCODE_SERVER_IDLE_DRAIN_PERIOD` |
| Observability | `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_SERVICE_NAME` |
| Resource Limits | `SESSION_MAX_CONCURRENT`, `POOL_MAX_SIZE`, `POOL_MAX_MEMORY_PER_USER` |

### Step 2: Collect Missing Credentials

Use `AskUserQuestion` to gather only what's missing. Batch related questions (max 4 per call).

**Slack** (if tokens missing):
- `HOTPLEX_MESSAGING_SLACK_BOT_TOKEN` (xoxb-...)
- `HOTPLEX_MESSAGING_SLACK_APP_TOKEN` (xapp-...)

**Feishu** (if credentials missing):
- `HOTPLEX_MESSAGING_FEISHU_APP_ID` (cli_xxx)
- `HOTPLEX_MESSAGING_FEISHU_APP_SECRET`

Ask the user to paste values via "Other" input. Never guess or fabricate tokens.

### Step 3: Validate Tokens

After collecting credentials, validate them via API calls in parallel.

**Slack:**
```bash
curl -s -H "Authorization: Bearer <bot_token>" "https://slack.com/api/auth.test"
```
- `ok: true` → valid, record `user_id` and `team`
- `ok: false` → report error, re-ask

**Feishu:**
```bash
curl -s -X POST "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal" \
  -H "Content-Type: application/json" \
  -d '{"app_id":"<app_id>","app_secret":"<app_secret>"}'
```
- `code: 0` → valid, record `tenant_access_token` for Step 4
- `code != 0` → report error, re-ask

Do NOT proceed with invalid tokens.

### Step 3.5: Configure Work Directory

Worker processes need a working directory. Priority: session-level > platform-level > global default (`~/.hotplex/workspace`).

Ask the user for each enabled platform:

```
Where should the Worker run for Slack sessions? (default: ~/.hotplex/workspace)
Where should the Worker run for Feishu sessions? (default: ~/.hotplex/workspace)
```

Set the env vars only if the user specifies a non-default path:
```
HOTPLEX_MESSAGING_SLACK_WORK_DIR=/path/to/project
HOTPLEX_MESSAGING_FEISHU_WORK_DIR=/path/to/project
```

If the user accepts the default, leave the variable unset — `worker.default_work_dir` applies automatically.

### Step 4: Auto-fetch User IDs

Use validated tokens to retrieve workspace user IDs automatically.

**Slack** — list human users:
```bash
curl -s -H "Authorization: Bearer <bot_token>" "https://slack.com/api/users.list?limit=50"
```
Filter: skip `is_bot: true` and `id == "USLACKBOT"`. Present for selection.

**Feishu** — list contacts (requires tenant_access_token from Step 3):
```bash
curl -s -H "Authorization: Bearer <tenant_access_token>" \
  "https://open.feishu.cn/open-apis/contact/v3/users?page_size=50&user_id_type=open_id"
```
Present for selection.

If API calls fail, fall back to manual instructions:
- Slack: Profile → three dots → "Copy member ID"
- Feishu: Admin console → Organization → find Open ID

### Step 5: Configure Access Policy

Ask the user to choose using `AskUserQuestion`:

| Option | DM Policy | Group Policy | ALLOW_FROM |
|--------|-----------|-------------|------------|
| Open (dev only) | `open` | `open` | (empty) |
| Allowlist (recommended) | `allowlist` | `allowlist` | user IDs from Step 4 |
| DM only | `allowlist` | `disabled` | user IDs from Step 4 |

Default recommendation: **allowlist** with the user's own ID.

For fine-grained control, also set per-channel allowlists:
- `ALLOW_DM_FROM` — users who can DM the bot directly
- `ALLOW_GROUP_FROM` — users who can use the bot in group channels
- `ALLOW_FROM` — blanket allowlist (applies to both DM and group if the specific lists are empty)

If "open" is chosen, warn that anyone in the workspace can use the bot. Warn once only.

### Step 6: Configure STT

Both platforms support speech-to-text:

| Platform | Recommended | Reason |
|----------|------------|--------|
| Slack | `local` | No cloud STT API for Slack |
| Feishu | `feishu+local` | Native cloud API with local fallback |

Set environment variables:
```
HOTPLEX_MESSAGING_SLACK_STT_PROVIDER=local
HOTPLEX_MESSAGING_SLACK_STT_LOCAL_MODE=ephemeral
HOTPLEX_MESSAGING_FEISHU_STT_PROVIDER=feishu+local
HOTPLEX_MESSAGING_FEISHU_STT_LOCAL_MODE=ephemeral
```

Local mode options: `ephemeral` (per-request process, default) or `persistent` (long-lived subprocess, lower latency after warmup).

If the user explicitly doesn't want STT, skip this step.

### Step 7: Configure Worker & Agent

**Worker type** — ask which runtime the user wants:
- `claude_code` (default) — Claude Code CLI
- `opencode_server` — OpenCode Server (singleton process, shared across sessions)
- `pi` — Pi-mono

Set per-platform:
```
HOTPLEX_MESSAGING_SLACK_WORKER_TYPE=claude_code
HOTPLEX_MESSAGING_FEISHU_WORKER_TYPE=claude_code
```

For `opencode_server`, set idle drain period (default 30m):
```
HOTPLEX_WORKER_OPENCODE_SERVER_IDLE_DRAIN_PERIOD=30m
```

For `claude_code`, optionally set custom command path:
```
HOTPLEX_WORKER_CLAUDE_CODE_COMMAND=claude
```

**Agent Config** — enabled by default, loads persona files from `~/.hotplex/agent-configs/`:
```
# HOTPLEX_AGENT_CONFIG_ENABLED=true
# HOTPLEX_AGENT_CONFIG_DIR=~/.hotplex/agent-configs/
```

Only set these if the user wants to disable or use a custom directory.

### Step 8: Write .env

Assemble the complete `.env` file. Structure:

```
# ── Required Secrets ──
HOTPLEX_JWT_SECRET=<generated or existing>
HOTPLEX_ADMIN_TOKEN_1=<generated or existing>

# ── Client Authentication ──
# HOTPLEX_SECURITY_API_KEY_1=<generated>

# ── Core Overrides ──
HOTPLEX_LOG_LEVEL=debug
HOTPLEX_LOG_FORMAT=text

# ── Resource Limits ──
# HOTPLEX_SESSION_MAX_CONCURRENT=1000
# HOTPLEX_POOL_MAX_SIZE=100
# HOTPLEX_POOL_MAX_MEMORY_PER_USER=8589934592

# ── Observability ──
# OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318
# OTEL_SERVICE_NAME=hotplex-gateway

# ── Slack ──
HOTPLEX_MESSAGING_SLACK_ENABLED=true
HOTPLEX_MESSAGING_SLACK_BOT_TOKEN=<token>
HOTPLEX_MESSAGING_SLACK_APP_TOKEN=<token>
HOTPLEX_MESSAGING_SLACK_WORKER_TYPE=claude_code
HOTPLEX_MESSAGING_SLACK_WORK_DIR=<path>
HOTPLEX_MESSAGING_SLACK_DM_POLICY=<policy>
HOTPLEX_MESSAGING_SLACK_GROUP_POLICY=<policy>
HOTPLEX_MESSAGING_SLACK_REQUIRE_MENTION=true
HOTPLEX_MESSAGING_SLACK_ALLOW_FROM=<user_id>
# HOTPLEX_MESSAGING_SLACK_ALLOW_DM_FROM=<user_id>
# HOTPLEX_MESSAGING_SLACK_ALLOW_GROUP_FROM=<user_id>

# ── Slack STT ──
# HOTPLEX_MESSAGING_SLACK_STT_PROVIDER=local
# HOTPLEX_MESSAGING_SLACK_STT_LOCAL_MODE=ephemeral

# ── Feishu ──
HOTPLEX_MESSAGING_FEISHU_ENABLED=true
HOTPLEX_MESSAGING_FEISHU_APP_ID=<app_id>
HOTPLEX_MESSAGING_FEISHU_APP_SECRET=<secret>
HOTPLEX_MESSAGING_FEISHU_WORKER_TYPE=claude_code
HOTPLEX_MESSAGING_FEISHU_WORK_DIR=<path>
HOTPLEX_MESSAGING_FEISHU_DM_POLICY=<policy>
HOTPLEX_MESSAGING_FEISHU_GROUP_POLICY=<policy>
HOTPLEX_MESSAGING_FEISHU_REQUIRE_MENTION=true
HOTPLEX_MESSAGING_FEISHU_ALLOW_FROM=<open_id>
# HOTPLEX_MESSAGING_FEISHU_ALLOW_DM_FROM=<open_id>
# HOTPLEX_MESSAGING_FEISHU_ALLOW_GROUP_FROM=<open_id>

# ── Feishu STT ──
# HOTPLEX_MESSAGING_FEISHU_STT_PROVIDER=feishu+local
# HOTPLEX_MESSAGING_FEISHU_STT_LOCAL_MODE=ephemeral

# ── Agent Config ──
# HOTPLEX_AGENT_CONFIG_ENABLED=true
# HOTPLEX_AGENT_CONFIG_DIR=~/.hotplex/agent-configs/

# ── Worker Config ──
# HOTPLEX_WORKER_CLAUDE_CODE_COMMAND=claude
# HOTPLEX_WORKER_OPENCODE_SERVER_IDLE_DRAIN_PERIOD=30m
```

For secret generation (if missing):
```bash
openssl rand -base64 32 | tr -d '\n'                # JWT secret
openssl rand -base64 32 | tr -d '/+=' | head -c 43  # Admin token / API key
```

Preserve existing valid values. Only fill missing fields or update what the user explicitly wants to change.

### Step 9: Summary

Present a final configuration summary:

| Setting | Value |
|---------|-------|
| Slack Bot | xoxb-...xxxx (validated) |
| Slack User ID | U0XXXXX |
| Slack WorkDir | /path/to/project |
| Slack Worker | claude_code |
| Feishu App | cli_xxx (validated) |
| Feishu User ID | ou_xxx |
| Feishu WorkDir | /path/to/project |
| Feishu Worker | claude_code |
| STT | Slack: local, Feishu: feishu+local |
| Access Policy | allowlist |
| Agent Config | enabled (~/.hotplex/agent-configs/) |

Suggest next step: `make dev` to start the gateway and verify connectivity.

## Idempotent Re-runs

This skill is designed for repeated use. On subsequent runs:
- Skip steps where valid configuration already exists
- Only re-process sections the user wants to update
- Always validate tokens before trusting them
- Never regenerate secrets that already exist
