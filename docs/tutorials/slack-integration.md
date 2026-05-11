---
title: Slack 集成教程
weight: 1
description: 从零配置 HotPlex Gateway 的 Slack 接入，5 分钟完成双向对话
persona: developer
difficulty: beginner
---

# Slack 集成教程

本教程引导你完成 HotPlex Gateway 与 Slack 的集成，实现 AI Agent 在 Slack 中实时响应。

**前置条件**：HotPlex 已安装（`make quickstart`）、Slack Workspace 管理权限。

## 1. 创建 Slack App

访问 [api.slack.com/apps](https://api.slack.com/apps)，点击 **Create New App**。

### 方式 A：一键 Manifest（推荐）

选择 **From an app manifest** → 粘贴以下 JSON，一次性完成权限、事件、交互、斜杠命令的全部配置：

```json
{
  "_metadata": { "major_version": 2, "minor_version": 1 },
  "display_information": {
    "name": "HotPlex",
    "long_description": "HotPlex is your AI coding partner in Slack. Write code, fix bugs, review PRs, create issues, and manage your entire development workflow directly in conversations.",
    "description": "AI coding partner — write, review, fix, and ship code in Slack",
    "background_color": "#1e293b"
  },
  "features": {
    "assistant_view": {
      "assistant_description": "Your AI coding partner. Supports code writing & review, bug fixes, PR and issue creation, directory switching, and more.",
      "suggested_prompts": [
        { "title": "💡 Creative Sparks", "message": "Use brainstorm mode to analyze current project architecture, identify three areas for improvement, and explain their value and implementation ideas." },
        { "title": "📝 Create Issue", "message": "Create a GitHub Issue using the project's defined template, describing an important bug or feature requirement." },
        { "title": "🔀 Create PR", "message": "Create a pull request based on current code changes using the project's defined PR template." },
        { "title": "🔍 Code Review", "message": "Perform a comprehensive code review of the current branch, covering DRY, SOLID, clean architecture, security vulnerabilities, and performance optimization." }
      ]
    },
    "app_home": {
      "home_tab_enabled": false,
      "messages_tab_enabled": true,
      "messages_tab_read_only_enabled": false
    },
    "bot_user": { "display_name": "HotPlex", "always_online": true },
    "slash_commands": [
      { "command": "/gc", "description": "Sleep session (stop Worker, preserve context)", "should_escape": false },
      { "command": "/park", "description": "Sleep session (same as /gc)", "should_escape": false },
      { "command": "/reset", "description": "Reset context (fresh start)", "should_escape": false },
      { "command": "/new", "description": "Reset context (same as /reset)", "should_escape": false },
      { "command": "/cd", "description": "Switch working directory", "usage_hint": "/cd /path/to/project", "should_escape": false },
      { "command": "/context", "description": "View context window usage", "should_escape": false },
      { "command": "/skills", "description": "List loaded skills", "should_escape": false },
      { "command": "/mcp", "description": "Show MCP server status", "should_escape": false },
      { "command": "/model", "description": "Switch AI model", "usage_hint": "/model claude-sonnet-4-6", "should_escape": false },
      { "command": "/perm", "description": "Set permission mode", "usage_hint": "/perm bypassPermissions", "should_escape": false },
      { "command": "/effort", "description": "Set reasoning effort level", "usage_hint": "/effort high", "should_escape": false },
      { "command": "/compact", "description": "Compact conversation history", "should_escape": false },
      { "command": "/clear", "description": "Clear conversation", "should_escape": false },
      { "command": "/rewind", "description": "Undo last conversation turn", "should_escape": false },
      { "command": "/commit", "description": "Create a Git commit", "should_escape": false }
    ]
  },
  "oauth_config": {
    "scopes": {
      "bot": [
        "app_mentions:read", "assistant:write", "bookmarks:read", "bookmarks:write",
        "canvases:read", "canvases:write", "channels:history", "channels:manage",
        "channels:read", "chat:write", "chat:write.customize", "chat:write.public",
        "commands", "dnd:read", "emoji:read", "files:read", "files:write",
        "groups:history", "groups:read", "im:history", "im:read", "im:write",
        "links:read", "links:write", "metadata.message:read", "mpim:history",
        "mpim:read", "mpim:write", "pins:read", "pins:write", "reactions:read",
        "reactions:write", "remote_files:read", "remote_files:write", "team:read",
        "usergroups:read", "users:read", "search:read.files", "search:read.im",
        "search:read.users", "search:read.public"
      ]
    }
  },
  "settings": {
    "event_subscriptions": {
      "bot_events": [
        "app_home_opened", "app_mention", "assistant_thread_context_changed",
        "assistant_thread_started", "message.channels", "message.groups",
        "message.im", "message.mpim"
      ]
    },
    "interactivity": { "is_enabled": true },
    "org_deploy_enabled": true,
    "socket_mode_enabled": true
  }
}
```

创建后跳到 [步骤 1.3 生成 Token](#13-生成-token)。

### 方式 B：手动配置

选择 **From scratch**，按以下步骤逐项配置。

### 1.1 启用 Socket Mode

Settings → **Socket Mode** → 打开 **Enable Socket Mode**。

Socket Mode 让 Gateway 通过 WebSocket 与 Slack 通信，无需公网地址。

### 1.2 配置 Bot Token Scopes

**OAuth & Permissions** → **Bot Token Scopes**，添加以下权限：

| Scope | 用途 |
|-------|------|
| `chat:write` | 发送消息 |
| `chat:write.public` | 在未加入的频道发消息 |
| `channels:history` | 读取公开频道消息 |
| `groups:history` | 读取私有频道消息 |
| `im:history` | 读取私聊消息 |
| `files:write` | 上传文件 |
| `files:read` | 读取文件 |
| `reactions:write` | 添加表情回应 |
| `bookmarks:write` | 管理频道书签 |
| `users:read` | 查询用户信息 |
| `users:read.email` | 读取邮箱 |
| `commands` | 斜杠命令 |

### 1.3 生成 Token

需要两个 Token：

- **Bot Token** (`xoxb-...`)：OAuth & Permissions → **Bot User OAuth Token**
- **App-Level Token** (`xapp-...`)：Basic Information → **App-Level Tokens** → Generate，Scope 勾选 `connections:write`

> 记录这两个 Token，下一步需要用到。

### 1.4 订阅事件

**Event Subscriptions** → 打开 Enable → **Subscribe to bot events**：

- `message.channels`
- `message.groups`
- `message.im`
- `app_mention`

### 1.5 启用交互

**Interactivity & Shortcuts** → 打开 Enable Interactivity。

权限请求和问答按钮需要此功能。

### 1.6 安装到 Workspace

**OAuth & Permissions** → **Install to Workspace** → 授权。

**验证**：Settings → **Install App** 页面显示 Bot Token，Slack 侧栏 Apps 中出现你的 Bot。

## 2. 配置 HotPlex

在项目根目录编辑 `.env`（首次使用：`cp configs/env.example .env`）：

```bash
# 启用 Slack
HOTPLEX_MESSAGING_SLACK_ENABLED=true

# 填入步骤 1.3 获取的 Token
HOTPLEX_MESSAGING_SLACK_BOT_TOKEN=xoxb-your-bot-token
HOTPLEX_MESSAGING_SLACK_APP_TOKEN=xapp-your-app-token
```

> 也可以运行 `hotplex onboard` 向导交互式配置。

**验证**：确认 `.env` 中三个变量均已取消注释且值非空。

## 3. 启动 Gateway

```bash
hotplex gateway start -d
```

`-d` 表示后台运行。查看状态：

```bash
hotplex status
```

输出中应包含 health endpoint 返回 200 的状态，表示 Socket Mode 已连接。

**验证**：`hotplex status` 显示 Slack 连接正常。

## 4. 测试

### 4.1 基本对话

1. 在 Slack 中找到你的 Bot（侧栏 Apps → 点击进入私聊）
2. 发送 `你好`
3. 等待流式回复完成

**验证**：Bot 回复消息，内容为 AI 生成的响应。

### 4.2 权限请求

1. 发送一条需要执行命令的请求，例如 `帮我看一下当前目录的文件`
2. Slack 中应出现权限确认按钮

**验证**：出现 Allow / Deny 按钮，点击后 Bot 执行相应操作。

## 5. 进阶配置

<details>
<summary>DM / 群组访问策略</summary>

默认仅白名单用户可触发 Bot。在 `.env` 或 `config.yaml` 中配置：

```bash
# 私聊策略：open（所有人）| allowlist（白名单）| disabled（禁止）
HOTPLEX_MESSAGING_SLACK_ALLOW_DM_FROM=U12345678,U87654321

# 群组策略：同上
HOTPLEX_MESSAGING_SLACK_ALLOW_GROUP_FROM=U12345678
```

对应 `config.yaml`：

```yaml
messaging:
  slack:
    dm_policy: allowlist      # open | allowlist | disabled
    group_policy: allowlist
    allow_from: ["U12345678"]
    allow_dm_from: []
    allow_group_from: []
```

</details>

<details>
<summary>群组 @提及 触发</summary>

群组中默认需要 `@Bot` 才触发响应，避免每条消息都创建会话：

```bash
# 群组需要 @提及（默认 true）
HOTPLEX_MESSAGING_SLACK_REQUIRE_MENTION=true
```

私聊始终触发，不受此设置影响。

</details>

<details>
<summary>语音功能（TTS / STT）</summary>

```bash
# 文字转语音（Edge TTS，免费）
HOTPLEX_MESSAGING_TTS_ENABLED=true
HOTPLEX_MESSAGING_TTS_PROVIDER=edge
HOTPLEX_MESSAGING_TTS_VOICE=zh-CN-XiaoxiaoNeural

# 语音转文字
HOTPLEX_MESSAGING_STT_PROVIDER=local
```

</details>

<details>
<summary>斜杠命令与状态指示</summary>

Bot 注册了两个斜杠命令：

| 命令 | 功能 |
|------|------|
| `/reset` | 清空上下文，重新开始对话 |
| `/dc` | 断开会话，保留上下文供下次继续 |

状态指示：
- **Typing indicator**：Agent 思考时自动显示
- **Emoji reaction**：消息处理状态标记

</details>

---

**下一步**：配置 [Agent 人格](../reference/configuration.md) 或探索 [飞书集成](./feishu-integration.md)。
