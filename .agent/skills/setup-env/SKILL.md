---
name: setup-env
description: Interactive .env configuration for HotPlex Worker Gateway. Use this skill whenever the user wants to set up, configure, or update the .env file for HotPlex Worker — including first-time setup, adding new messaging adapters (Slack/Feishu), configuring STT, updating tokens, or switching access policies. Also trigger when the user mentions "configure slack", "configure feishu", "setup messaging", "update .env", "add allowlist", or "get user IDs". Do NOT trigger for general .env edits unrelated to HotPlex Worker.
---

# HotPlex Worker .env Configuration Skill

Standardized workflow to configure the `.env` file for both Slack and Feishu messaging adapters with full functionality.

## Prerequisites

- Project root: current working directory (the skill runs inside the HotPlex Worker repo)
- Example file: `configs/env.example`
- Target file: `.env` (project root, gitignored)

## Workflow

### Step 1: Assess Current State

1. Check if `.env` exists. If not, `cp configs/env.example .env`.
2. Read current `.env` content.
3. Build a status map for each section:

| Section | Required Fields | Status |
|---------|----------------|--------|
| Secrets | `HOTPLEX_JWT_SECRET`, `HOTPLEX_ADMIN_TOKEN_1` | Present / Missing |
| WorkDir | `SLACK_WORK_DIR`, `FEISHU_WORK_DIR` | Configured / Missing |
| Slack | `BOT_TOKEN`, `APP_TOKEN` | Present / Missing |
| Feishu | `APP_ID`, `APP_SECRET` | Present / Missing |
| Slack STT | `STT_PROVIDER` | Configured / Missing |
| Feishu STT | `STT_PROVIDER` | Configured / Missing |
| Access Policy | `DM_POLICY`, `GROUP_POLICY`, `ALLOW_FROM` | Configured / Missing |

4. Present a concise status summary to the user, highlighting only missing items.

### Step 2: Collect Missing Credentials

Use `AskUserQuestion` to gather only what's missing. Batch related questions together (max 4 per call).

**For Slack** (if tokens missing or invalid):
- `HOTPLEX_MESSAGING_SLACK_BOT_TOKEN` (xoxb-...)
- `HOTPLEX_MESSAGING_SLACK_APP_TOKEN` (xapp-...)

**For Feishu** (if credentials missing):
- `HOTPLEX_MESSAGING_FEISHU_APP_ID` (cli_xxx)
- `HOTPLEX_MESSAGING_FEISHU_APP_SECRET`

Ask the user to paste values via the "Other" input option. Never guess or fabricate tokens.

### Step 3: Validate Tokens

After collecting credentials, validate them immediately via API calls. Do this in parallel for both platforms.

**Slack validation:**
```bash
curl -s -H "Authorization: Bearer <bot_token>" "https://slack.com/api/auth.test"
```
- `ok: true` → valid, record `user_id` and `team` from response
- `ok: false` → report error to user, ask for correct token

**Feishu validation:**
```bash
curl -s -X POST "https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal" \
  -H "Content-Type: application/json" \
  -d '{"app_id":"<app_id>","app_secret":"<app_secret>"}'
```
- `code: 0` → valid, record `tenant_access_token` for Step 4
- `code != 0` → report error to user, ask for correct credentials

If validation fails, report the exact error and re-ask. Do NOT proceed with invalid tokens.

### Step 3.5: Configure Work Directory

Worker processes (e.g. `claude` CLI) need a working directory for each session. WorkDir follows a 3-tier priority: session-level > platform-level > global default (`~/.hotplex/workspace`).

Ask the user where their project code lives for each enabled platform:

```
Where should the Worker run for Slack sessions? (default: ~/.hotplex/workspace)
Where should the Worker run for Feishu sessions? (default: ~/.hotplex/workspace)
```

Typical values:
- A specific project repo: `/home/user/my-project`
- A shared workspace: `~/projects`
- Platform-default (empty = use `worker.default_work_dir`)

Set the corresponding env vars only if the user specifies a non-default path:
```
HOTPLEX_MESSAGING_SLACK_WORK_DIR=/path/to/project    # only if Slack enabled and user specified
HOTPLEX_MESSAGING_FEISHU_WORK_DIR=/path/to/project   # only if Feishu enabled and user specified
```

If the user accepts the default, leave the variable commented out or unset — the global `worker.default_work_dir` (`~/.hotplex/workspace`) applies automatically.

### Step 4: Auto-fetch User IDs

Use validated tokens to automatically retrieve workspace user IDs. This eliminates manual lookup.

**Slack** — list human users:
```bash
curl -s -H "Authorization: Bearer <bot_token>" "https://slack.com/api/users.list?limit=50"
```
Filter: skip `is_bot: true` and `id == "USLACKBOT"`. Present the list to the user for selection.

**Feishu** — list contacts (requires tenant_access_token from Step 3):
```bash
curl -s -H "Authorization: Bearer <tenant_access_token>" \
  "https://open.feishu.cn/open-apis/contact/v3/users?page_size=50&user_id_type=open_id"
```
Present the list to the user for selection.

If API calls fail (permissions not granted, etc.), fall back to manual instructions:
- Slack: Profile → three dots → "Copy member ID"
- Feishu: Admin console → Organization → find Open ID

### Step 5: Configure Access Policy

Ask the user to choose access policy using `AskUserQuestion`:

| Option | DM Policy | Group Policy | ALLOW_FROM |
|--------|-----------|-------------|------------|
| Open (dev only) | `open` | `open` | (empty) |
| Allowlist (recommended) | `allowlist` | `allowlist` | user IDs from Step 4 |
| DM only | `allowlist` | `disabled` | user IDs from Step 4 |

Default recommendation: **allowlist** with the user's own ID from Step 4.

If "open" is chosen, warn that anyone in the workspace can use the bot. Do not warn more than once.

### Step 6: Configure STT

Both platforms support speech-to-text. Configure based on platform capability:

| Platform | Recommended Provider | Reason |
|----------|---------------------|--------|
| Slack | `local` | No cloud STT API available for Slack |
| Feishu | `feishu+local` | Native cloud API with local fallback |

Set these environment variables:
```
HOTPLEX_MESSAGING_SLACK_STT_PROVIDER=local
HOTPLEX_MESSAGING_SLACK_STT_LOCAL_MODE=persistent
HOTPLEX_MESSAGING_FEISHU_STT_PROVIDER=feishu+local
```

If the user explicitly doesn't want STT, skip this step.

### Step 7: Write .env

Assemble the complete `.env` file with all collected values. Structure:

```
# ── Required Secrets ──
HOTPLEX_JWT_SECRET=<generated or existing>
HOTPLEX_ADMIN_TOKEN_1=<generated or existing>

# ── Core Overrides ──
HOTPLEX_LOG_LEVEL=debug
HOTPLEX_LOG_FORMAT=text

# ── Slack ──
HOTPLEX_MESSAGING_SLACK_ENABLED=true
HOTPLEX_MESSAGING_SLACK_BOT_TOKEN=<token>
HOTPLEX_MESSAGING_SLACK_APP_TOKEN=<token>
HOTPLEX_MESSAGING_SLACK_WORKER_TYPE=claude_code
HOTPLEX_MESSAGING_SLACK_WORK_DIR=<path>  # project dir for worker sessions
HOTPLEX_MESSAGING_SLACK_DM_POLICY=<policy>
HOTPLEX_MESSAGING_SLACK_GROUP_POLICY=<policy>
HOTPLEX_MESSAGING_SLACK_REQUIRE_MENTION=true
HOTPLEX_MESSAGING_SLACK_ALLOW_FROM=<user_id>  # only if allowlist

# ── Slack STT ──
HOTPLEX_MESSAGING_SLACK_STT_PROVIDER=local
HOTPLEX_MESSAGING_SLACK_STT_LOCAL_MODE=persistent

# ── Feishu ──
HOTPLEX_MESSAGING_FEISHU_ENABLED=true
HOTPLEX_MESSAGING_FEISHU_APP_ID=<app_id>
HOTPLEX_MESSAGING_FEISHU_APP_SECRET=<secret>
HOTPLEX_MESSAGING_FEISHU_WORKER_TYPE=claude_code
HOTPLEX_MESSAGING_FEISHU_WORK_DIR=<path>  # project dir for worker sessions
HOTPLEX_MESSAGING_FEISHU_DM_POLICY=<policy>
HOTPLEX_MESSAGING_FEISHU_GROUP_POLICY=<policy>
HOTPLEX_MESSAGING_FEISHU_REQUIRE_MENTION=true
HOTPLEX_MESSAGING_FEISHU_ALLOW_FROM=<open_id>  # only if allowlist

# ── Feishu STT ──
HOTPLEX_MESSAGING_FEISHU_STT_PROVIDER=feishu+local
```

For secrets generation (if missing):
```bash
openssl rand -base64 32 | tr -d '\n'          # JWT secret
openssl rand -base64 32 | tr -d '/+=' | head -c 43  # Admin token
```

**Important**: Preserve existing valid values. Only fill in missing fields or update fields the user explicitly wants to change. Never overwrite secrets that are already present and valid.

### Step 8: Summary

Present a final configuration summary table:

| Setting | Value |
|---------|-------|
| Slack Bot | xoxb-...xxxx (validated) |
| Slack User ID | U0XXXXX |
| Slack WorkDir | /path/to/project |
| Feishu App | cli_xxx (validated) |
| Feishu User ID | ou_xxx |
| Feishu WorkDir | /path/to/project |
| Slack STT | local (persistent) |
| Feishu STT | feishu+local |
| Access Policy | allowlist |

Suggest next step: `make dev` to start the gateway and verify connectivity.

## Idempotent Re-runs

This skill is designed for repeated use. On subsequent runs:
- Skip steps where valid configuration already exists
- Only re-process sections the user wants to update
- Always validate tokens before trusting them
- Never regenerate secrets that already exist
