# Slack App Integration Guide

> Covers the full process from creating a Slack App to connecting it with HotPlex.

---

## Overview

1. Create a Slack App via Manifest (one-click setup).
2. Enable Socket Mode and get the App-Level Token (`xapp-...`).
3. Install the App to your Workspace and get the Bot Token (`xoxb-...`).
4. Configure HotPlex with the tokens and start the gateway.

---

## Step 1: Create the Slack App via Manifest

1. Log in to [https://api.slack.com/apps](https://api.slack.com/apps).
2. Click **"Create New App"**.
3. Select **"From an app manifest"**.

![Step 1: Create from Manifest](./guides/slack_step1_manifest.png)

4. Select your Workspace, click **"Next"**.
5. Select the **"JSON"** tab, clear the text box, and paste the JSON from the [App Manifest](#app-manifest-json) section below. Click **"Next"** → **"Create"**.

---

## Step 2: Enable Socket Mode

1. In the left sidebar, find and click **"Socket Mode"**.
2. Toggle **"Enable Socket Mode"** to **On**.

![Step 2: Enable Socket Mode](./guides/slack_step2_socketmode.png)

3. Enter a token name (e.g., `hotplex_socket`) and click **"Generate"**.
4. Copy the `xapp-...` string (App Token). Click "Done".

---

## Step 3: Install the App and Get the Bot Token

1. In the left sidebar, click **"Install App"**.
2. Click **"Install to Workspace"** and authorize.
3. After installation, copy the `xoxb-...` string from **"Bot User OAuth Token"**.

![Step 3: Get Bot Token](./guides/slack_step3_tokens.png)

---

## Step 4: Configure HotPlex

Add the tokens to your `.env` file (copy from `configs/env.example`):

```env
# Enable Slack adapter
HOTPLEX_MESSAGING_SLACK_ENABLED=true

# Bot Token (starts with xoxb-)
HOTPLEX_MESSAGING_SLACK_BOT_TOKEN=xoxb-your-bot-token

# App Token (starts with xapp-)
HOTPLEX_MESSAGING_SLACK_APP_TOKEN=xapp-your-app-token
```

Socket Mode is enabled by default in `configs/config.yaml` (`socket_mode: true`), no extra configuration needed.

Additional options (also overridable via environment variables):

```env
# Worker type: claude_code (default) or opencode_server
HOTPLEX_MESSAGING_SLACK_WORKER_TYPE=claude_code

# Working directory (default: HotPlex workspace)
HOTPLEX_MESSAGING_SLACK_WORK_DIR=

# Access control: allowlist (default) or allow
HOTPLEX_MESSAGING_SLACK_DM_POLICY=allowlist
HOTPLEX_MESSAGING_SLACK_GROUP_POLICY=allowlist
HOTPLEX_MESSAGING_SLACK_REQUIRE_MENTION=true

# User allowlists (comma-separated Slack User IDs)
HOTPLEX_MESSAGING_SLACK_ALLOW_FROM=
HOTPLEX_MESSAGING_SLACK_ALLOW_DM_FROM=
HOTPLEX_MESSAGING_SLACK_ALLOW_GROUP_FROM=
```

---

## Step 5: Start and Test

```bash
make run    # Build and start the Gateway
# or
make dev    # Start Gateway + Webchat together
```

In Slack:
1. Send `@HotPlex` in any channel to invite the bot.
2. Send `?` or `/help` to see all available commands.
3. Send a message to start a conversation.

---

## Available Commands

### Session Control

| Command | Description |
|---------|-------------|
| `/gc` or `/park` | Sleep session (stop Worker, preserve session state) |
| `/reset` or `/new` | Reset context (fresh start, same Session ID) |
| `/cd <path>` | Switch working directory (creates new Session) |

### Info & Status

| Command | Description |
|---------|-------------|
| `/context` | View context window usage |
| `/skills` | List loaded skills |
| `/mcp` | Show MCP server status |

### Configuration

| Command | Description |
|---------|-------------|
| `/model <name>` | Switch AI model |
| `/perm <mode>` | Set permission mode |
| `/effort <level>` | Set reasoning effort level |

### Conversation

| Command | Description |
|---------|-------------|
| `/compact` | Compact conversation history |
| `/clear` | Clear conversation |
| `/rewind` | Undo last conversation turn |
| `/commit` | Create a Git commit |

All commands also support `$`-prefix natural language triggers (e.g., `$休眠`, `$上下文`, `$切换模型`).

---

## Interactive Approvals

HotPlex provides three types of interactive UI in Slack:

- **Tool Approval**: When Claude Code requests to run a tool, Allow / Deny buttons are displayed.
- **Question Requests**: When the Agent asks a question, option buttons are shown for selection.
- **MCP Elicitation**: When an MCP Server needs user input, Accept / Decline buttons appear.

All interactive requests auto-deny after 5 minutes of inactivity.

---

## App Manifest (JSON)

```json
{
  "_metadata": {
    "major_version": 2,
    "minor_version": 1
  },
  "display_information": {
    "name": "HotPlex",
    "long_description": "HotPlex is your AI coding partner in Slack. Write code, fix bugs, review PRs, create issues, and manage your entire development workflow directly in conversations. Features persistent session context, interactive tool approval, voice input, MCP Server integration, and skills extensions. @mention in channels or DM to get started — complete coding workflows without leaving Slack.",
    "description": "AI coding partner — write, review, fix, and ship code in Slack",
    "background_color": "#1e293b"
  },
  "features": {
    "assistant_view": {
      "assistant_description": "Your AI coding partner. Supports code writing & review, bug fixes, PR and issue creation, directory switching, and more. Just send a message to get started.",
      "suggested_prompts": [
        {
          "title": "💡 Creative Sparks",
          "message": "Use brainstorm mode to analyze current project architecture, identify three areas for improvement, and explain their value and implementation ideas."
        },
        {
          "title": "📝 Create Issue",
          "message": "Create a GitHub Issue using the project's defined template, describing an important bug or feature requirement."
        },
        {
          "title": "🔀 Create PR",
          "message": "Create a pull request based on current code changes using the project's defined PR template."
        },
        {
          "title": "🔍 Code Review",
          "message": "Perform a comprehensive code review of the current branch, covering DRY, SOLID principles, clean architecture, code quality, security vulnerabilities, and performance optimization."
        }
      ]
    },
    "app_home": {
      "home_tab_enabled": false,
      "messages_tab_enabled": true,
      "messages_tab_read_only_enabled": false
    },
    "bot_user": {
      "display_name": "HotPlex",
      "always_online": true
    },
    "slash_commands": [
      {
        "command": "/gc",
        "description": "Sleep session (stop Worker, preserve context)",
        "should_escape": false
      },
      {
        "command": "/park",
        "description": "Sleep session (same as /gc)",
        "should_escape": false
      },
      {
        "command": "/reset",
        "description": "Reset context (fresh start)",
        "should_escape": false
      },
      {
        "command": "/new",
        "description": "Reset context (same as /reset)",
        "should_escape": false
      },
      {
        "command": "/cd",
        "description": "Switch working directory",
        "usage_hint": "/cd /path/to/project",
        "should_escape": false
      },
      {
        "command": "/context",
        "description": "View context window usage",
        "should_escape": false
      },
      {
        "command": "/skills",
        "description": "List loaded skills",
        "should_escape": false
      },
      {
        "command": "/mcp",
        "description": "Show MCP server status",
        "should_escape": false
      },
      {
        "command": "/model",
        "description": "Switch AI model",
        "usage_hint": "/model claude-sonnet-4-6",
        "should_escape": false
      },
      {
        "command": "/perm",
        "description": "Set permission mode",
        "usage_hint": "/perm bypassPermissions",
        "should_escape": false
      },
      {
        "command": "/effort",
        "description": "Set reasoning effort level",
        "usage_hint": "/effort high",
        "should_escape": false
      },
      {
        "command": "/compact",
        "description": "Compact conversation history",
        "should_escape": false
      },
      {
        "command": "/clear",
        "description": "Clear conversation",
        "should_escape": false
      },
      {
        "command": "/rewind",
        "description": "Undo last conversation turn",
        "should_escape": false
      },
      {
        "command": "/commit",
        "description": "Create a Git commit",
        "should_escape": false
      }
    ]
  },
  "oauth_config": {
    "scopes": {
      "bot": [
        "search:read.files",
        "app_mentions:read",
        "assistant:write",
        "bookmarks:read",
        "bookmarks:write",
        "canvases:read",
        "canvases:write",
        "channels:history",
        "channels:manage",
        "channels:read",
        "chat:write",
        "chat:write.customize",
        "chat:write.public",
        "commands",
        "dnd:read",
        "emoji:read",
        "files:read",
        "files:write",
        "groups:history",
        "groups:read",
        "im:history",
        "im:read",
        "im:write",
        "links:read",
        "links:write",
        "metadata.message:read",
        "mpim:history",
        "mpim:read",
        "mpim:write",
        "pins:read",
        "pins:write",
        "reactions:read",
        "reactions:write",
        "remote_files:read",
        "remote_files:write",
        "team:read",
        "usergroups:read",
        "users:read",
        "search:read.im",
        "search:read.users",
        "search:read.public"
      ]
    },
    "pkce_enabled": false
  },
  "settings": {
    "event_subscriptions": {
      "bot_events": [
        "app_home_opened",
        "app_mention",
        "assistant_thread_context_changed",
        "assistant_thread_started",
        "message.channels",
        "message.groups",
        "message.im",
        "message.mpim"
      ]
    },
    "interactivity": {
      "is_enabled": true
    },
    "org_deploy_enabled": true,
    "socket_mode_enabled": true
  }
}
```

---

## Permission Scopes (OAuth)

### Messaging & Conversations

| Scope | Purpose | Necessity |
|-------|---------|-----------|
| `chat:write` | Send messages | Base |
| `chat:write.public` | Send to channels the bot hasn't joined | Recommended |
| `chat:write.customize` | Customize message sender name and avatar | Recommended |
| `im:read` / `im:write` | DM read/write | Core |
| `im:history` | Read DM history | Core |
| `channels:read` / `channels:manage` | Channel info and management | Recommended |
| `channels:history` | Read channel message history | Core |
| `groups:read` | Group info | Recommended |
| `groups:history` | Read group message history | Core |
| `mpim:read` / `mpim:write` | Multi-party IM read/write | Recommended |
| `mpim:history` | Read multi-party IM history | Core |
| `metadata.message:read` | Read message metadata | Recommended |

### Files & Media

| Scope | Purpose | Necessity |
|-------|---------|-----------|
| `files:read` / `files:write` | File upload handling | Recommended |
| `remote_files:read` / `remote_files:write` | Remote file management | Optional |

### Interaction & Notifications

| Scope | Purpose | Necessity |
|-------|---------|-----------|
| `assistant:write` | AI Assistant status updates (Thinking indicator) | Critical |
| `app_mentions:read` | Listen for `@HotPlex` messages | Critical |
| `commands` | Register slash commands | Recommended |
| `reactions:read` / `reactions:write` | Emoji reactions read/write | Recommended |

### Users & Organization

| Scope | Purpose | Necessity |
|-------|---------|-----------|
| `users:read` | User info lookup (for @mention resolution) | Recommended |
| `usergroups:read` | User group info | Optional |
| `team:read` | Team info | Recommended |

### Search & Bookmarks

| Scope | Purpose | Necessity |
|-------|---------|-----------|
| `search:read.files` | Search files | Recommended |
| `search:read.im` | Search DM messages | Recommended |
| `search:read.users` | Search users | Recommended |
| `search:read.public` | Search public channel messages | Recommended |
| `pins:read` / `pins:write` | Pin messages | Optional |
| `bookmarks:read` / `bookmarks:write` | Channel bookmarks | Optional |
| `links:read` / `links:write` | Link unfurling | Optional |

### Extended Features

| Scope | Purpose | Necessity |
|-------|---------|-----------|
| `canvases:read` / `canvases:write` | Slack Canvas read/write | Optional |
| `emoji:read` | Custom emoji list | Optional |
| `dnd:read` | Do not disturb status | Optional |

---

## Advanced Configuration

Configure in `configs/config.yaml` under the `messaging.slack` section:

```yaml
messaging:
  slack:
    enabled: true
    socket_mode: true
    worker_type: "claude_code"       # or "opencode_server"
    work_dir: ""                      # default: HotPlex workspace
    assistant_api_enabled: true       # native Assistant API (requires paid Workspace)
    dm_policy: "allowlist"            # DM policy: allowlist / allow
    group_policy: "allowlist"         # group policy: allowlist / allow
    require_mention: true             # require @mention in group chats
    allow_from: []                    # global user allowlist (Slack User IDs)
    allow_dm_from: []                 # DM user allowlist
    allow_group_from: []              # group user allowlist
    reconnect_base_delay: 1s          # reconnect base delay
    reconnect_max_delay: 60s          # reconnect max delay
    stt_provider: "local"             # speech-to-text: local / empty (disabled)
    stt_local_cmd: "python3 ~/.hotplex/scripts/stt_server.py"
    stt_local_idle_ttl: 1h            # persistent mode idle timeout
```

All settings can be overridden via environment variables (`HOTPLEX_MESSAGING_SLACK_*` prefix). See `configs/env.example` for the full list.

For technical implementation details, refer to the [Slack Adapter Improvement Spec](../../specs/Slack-Adapter-Improvement-Spec.md).
