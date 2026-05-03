# Slack App 接入 HotPlex 完整教程

> 本教程覆盖从创建 Slack App 到完成接入的全部步骤。

---

## 核心流程

1. 在 Slack 开发者后台通过 Manifest 一键创建 App。
2. 启用 Socket Mode，获取 App-Level Token（`xapp-...`）。
3. 安装 App 到 Workspace，获取 Bot Token（`xoxb-...`）。
4. 将 Token 配置到 HotPlex 并启动。

---

## 第 1 步：通过 Manifest 创建 Slack App

1. 登录 [https://api.slack.com/apps](https://api.slack.com/apps)。
2. 点击 **"Create New App"**。
3. 选择 **"From an app manifest"**。

![步骤 1：通过 Manifest 创建](./guides/slack_step1_manifest.png)

4. 选择你的 Workspace，点击 **"Next"**。
5. 选择 **"JSON"** 标签，清空文本框，粘贴下方 [App Manifest](#app-manifest-json) 中的 JSON，点击 **"Next"** → **"Create"**。

---

## 第 2 步：启用 Socket Mode

1. 在左侧菜单找到 **"Socket Mode"**，点击进入。
2. 将 **"Enable Socket Mode"** 开关拨到 **On**。

![步骤 2：启用 Socket Mode](./guides/slack_step2_socketmode.png)

3. 在弹出的窗口中输入 Token 名称（如 `hotplex_socket`），点击 **"Generate"**。
4. 复制生成的 `xapp-...` 字符串（App Token），妥善保存。点击 "Done"。

---

## 第 3 步：安装 App 并获取 Bot Token

1. 在左侧菜单点击 **"Install App"**。
2. 点击 **"Install to Workspace"**，授权访问权限。
3. 安装成功后，在 **"Bot User OAuth Token"** 栏复制 `xoxb-...` 字符串（Bot Token）。

![步骤 3：获取 Bot Token](./guides/slack_step3_tokens.png)

---

## 第 4 步：配置 HotPlex

将两把 Token 写入 `.env` 文件（从 `configs/env.example` 复制）：

```env
# 启用 Slack 适配器
HOTPLEX_MESSAGING_SLACK_ENABLED=true

# Bot Token（xoxb- 开头）
HOTPLEX_MESSAGING_SLACK_BOT_TOKEN=xoxb-your-bot-token

# App Token（xapp- 开头）
HOTPLEX_MESSAGING_SLACK_APP_TOKEN=xapp-your-app-token
```

Socket Mode 默认在 `configs/config.yaml` 中已开启（`socket_mode: true`），无需额外配置。

更多可选项（也可通过环境变量覆盖）：

```env
# Worker 类型：claude_code（默认）或 opencode_server
HOTPLEX_MESSAGING_SLACK_WORKER_TYPE=claude_code

# 工作目录（默认使用 HotPlex workspace）
HOTPLEX_MESSAGING_SLACK_WORK_DIR=

# 访问控制策略：allowlist（默认）或 allow
HOTPLEX_MESSAGING_SLACK_DM_POLICY=allowlist
HOTPLEX_MESSAGING_SLACK_GROUP_POLICY=allowlist
HOTPLEX_MESSAGING_SLACK_REQUIRE_MENTION=true

# 用户白名单（逗号分隔的 Slack User ID）
HOTPLEX_MESSAGING_SLACK_ALLOW_FROM=
HOTPLEX_MESSAGING_SLACK_ALLOW_DM_FROM=
HOTPLEX_MESSAGING_SLACK_ALLOW_GROUP_FROM=
```

---

## 第 5 步：启动并测试

```bash
make run    # 构建并启动 Gateway
# 或
make dev    # 同时启动 Gateway + Webchat
```

在 Slack 中：
1. 在任意频道发送 `@HotPlex`，邀请机器人进群。
2. 发送 `?` 或 `/help` 查看全部可用命令。
3. 直接发送消息即可开始对话。

---

## 可用命令

### 会话控制

| 命令               | 说明                                  |
| ------------------ | ------------------------------------- |
| `/gc` 或 `/park`   | 休眠会话（停止 Worker，保留会话状态） |
| `/reset` 或 `/new` | 重置上下文（全新开始，同 Session ID） |
| `/cd <目录>`       | 切换工作目录（创建新 Session）        |

### 信息与状态

| 命令       | 说明                 |
| ---------- | -------------------- |
| `/context` | 查看上下文窗口使用量 |
| `/skills`  | 查看已加载的技能列表 |
| `/mcp`     | 查看 MCP 服务器状态  |

### 配置

| 命令             | 说明         |
| ---------------- | ------------ |
| `/model <名称>`  | 切换 AI 模型 |
| `/perm <模式>`   | 设置权限模式 |
| `/effort <级别>` | 设置推理力度 |

### 对话

| 命令       | 说明           |
| ---------- | -------------- |
| `/compact` | 压缩对话历史   |
| `/clear`   | 清空对话       |
| `/rewind`  | 撤销上一轮对话 |
| `/commit`  | 创建 Git 提交  |

所有命令也支持 `$` 前缀的自然语言触发（如 `$休眠`、`$上下文`、`$切换模型`）。

---

## 交互式审批

HotPlex 在 Slack 中提供三种交互式审批界面：

- **工具审批**：Claude Code 请求运行工具时，显示 Allow / Deny 按钮。
- **问答请求**：Agent 提出问题时，显示选项按钮供选择。
- **MCP 引导请求**：MCP Server 需要用户输入时，显示 Accept / Decline 按钮。

所有交互请求在 5 分钟内无响应将自动拒绝。

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
    "long_description": "HotPlex 是深度集成于 Slack 的高性能 AI 编程搭档。它将 Claude Code 的工程能力带入聊天界面，支持在对话中完成代码编写、Bug 修复、自动化代码审查、以及创建 PR 和 Issue 等研发任务。系统内置进程级隔离与 WAF 安全保护，确保执行环境稳健。支持全双工会话流、工具运行审批确认、多模态语音输入、MCP Server 协议集成及自定义技能动态扩展。只需在频道中 @提及即可开启，让开发者无需离开 Slack 即可掌控从架构设计到代码提交的完整研发全生命周期工作流。",
    "description": "AI 编程搭档，在 Slack 中完成编码、审查、修复和提交",
    "background_color": "#1e293b"
  },
  "features": {
    "assistant_view": {
      "assistant_description": "你的 AI 编程搭档。支持代码编写与审查、Bug 修复、PR 和 Issue 创建、工作目录切换等开发工作流。直接发消息即可开始。",
      "suggested_prompts": [
        {
          "title": "💡 创意激发",
          "message": "以头脑风暴模式，帮我分析当前项目架构，识别三个可以改进的方向，说明改进价值和实现思路"
        },
        {
          "title": "📝 创建 Issue",
          "message": "创建一个 GitHub Issue，必须使用项目定义的 Issue 模板，描述项目中一个重要的 bug 或功能需求"
        },
        {
          "title": "🔀 创建 PR",
          "message": "基于当前代码改动创建 pull request，必须使用项目定义的 PR 模板"
        },
        {
          "title": "🔍 代码审查",
          "message": "对当前分支进行全面的代码审查，包括 DRY 原则、SOLID 原则、整洁架构、代码质量、安全漏洞和性能优化"
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
        "description": "休眠会话（停止 Worker，保留上下文）",
        "should_escape": false
      },
      {
        "command": "/park",
        "description": "休眠会话（同 /gc）",
        "should_escape": false
      },
      {
        "command": "/reset",
        "description": "重置上下文（全新开始）",
        "should_escape": false
      },
      {
        "command": "/new",
        "description": "重置上下文（同 /reset）",
        "should_escape": false
      },
      {
        "command": "/cd",
        "description": "切换工作目录",
        "usage_hint": "/cd /path/to/project",
        "should_escape": false
      },
      {
        "command": "/context",
        "description": "查看上下文窗口使用量",
        "should_escape": false
      },
      {
        "command": "/skills",
        "description": "查看已加载的技能列表",
        "should_escape": false
      },
      {
        "command": "/mcp",
        "description": "查看 MCP 服务器状态",
        "should_escape": false
      },
      {
        "command": "/model",
        "description": "切换 AI 模型",
        "usage_hint": "/model claude-sonnet-4-6",
        "should_escape": false
      },
      {
        "command": "/perm",
        "description": "设置权限模式",
        "usage_hint": "/perm bypassPermissions",
        "should_escape": false
      },
      {
        "command": "/effort",
        "description": "设置推理力度",
        "usage_hint": "/effort high",
        "should_escape": false
      },
      {
        "command": "/compact",
        "description": "压缩对话历史",
        "should_escape": false
      },
      {
        "command": "/clear",
        "description": "清空对话",
        "should_escape": false
      },
      {
        "command": "/rewind",
        "description": "撤销上一轮对话",
        "should_escape": false
      },
      {
        "command": "/commit",
        "description": "创建 Git 提交",
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

## 权限说明 (OAuth Scopes)

### 消息与会话

| Scope                               | 用途                       | 必要性 |
| ----------------------------------- | -------------------------- | ------ |
| `chat:write`                        | 发送消息                   | 基础   |
| `chat:write.public`                 | 在未加入的频道发消息       | 推荐   |
| `chat:write.customize`              | 自定义消息发送者名称和头像 | 推荐   |
| `im:read` / `im:write`              | 私聊读写                   | 核心   |
| `im:history`                        | 读取私聊历史               | 核心   |
| `channels:read` / `channels:manage` | 频道信息与管理             | 推荐   |
| `channels:history`                  | 读取频道消息历史           | 核心   |
| `groups:read`                        | 群组信息               | 推荐   |
| `groups:history`                    | 读取群组消息历史           | 核心   |
| `mpim:read` / `mpim:write`          | 多人私聊读写               | 推荐   |
| `mpim:history`                      | 读取多人私聊历史           | 核心   |
| `metadata.message:read`             | 读取消息元数据             | 推荐   |

### 文件与媒体

| Scope                                      | 用途         | 必要性 |
| ------------------------------------------ | ------------ | ------ |
| `files:read` / `files:write`               | 文件上传处理 | 推荐   |
| `remote_files:read` / `remote_files:write` | 远程文件管理 | 可选   |

### 交互与通知

| Scope                                | 用途                                     | 必要性 |
| ------------------------------------ | ---------------------------------------- | ------ |
| `assistant:write`                    | AI Assistant 状态更新（Thinking 指示器） | 关键   |
| `app_mentions:read`                  | 监听 `@HotPlex` 消息                     | 关键   |
| `commands`                           | 注册斜杠命令                             | 推荐   |
| `reactions:read` / `reactions:write` | Emoji 反应读写                           | 推荐   |

### 用户与组织

| Scope              | 用途                           | 必要性 |
| ------------------ | ------------------------------ | ------ |
| `users:read`       | 用户信息查询（用于 @提及解析） | 推荐   |
| `usergroups:read`  | 用户组信息                     | 可选   |
| `team:read`        | 团队信息                       | 推荐   |

### 搜索与标记

| Scope                                | 用途           | 必要性 |
| ------------------------------------ | -------------- | ------ |
| `search:read.files`                 | 搜索文件         | 推荐   |
| `search:read.im`                    | 搜索私聊消息       | 推荐   |
| `search:read.users`                 | 搜索用户         | 推荐   |
| `search:read.public`                | 搜索公开频道消息     | 推荐   |
| `pins:read` / `pins:write`           | 消息置顶       | 可选   |
| `bookmarks:read` / `bookmarks:write` | 频道书签       | 可选   |
| `links:read` / `links:write`         | 链接展开       | 可选   |

### 功能扩展

| Scope                              | 用途              | 必要性 |
| ---------------------------------- | ----------------- | ------ |
| `canvases:read` / `canvases:write` | Slack Canvas 读写 | 可选   |
| `emoji:read`                       | 自定义 Emoji 列表 | 可选   |
| `dnd:read`                         | 免打扰状态        | 可选   |

---

## 高级配置

在 `configs/config.yaml` 的 `messaging.slack` 段下配置：

```yaml
messaging:
  slack:
    enabled: true
    socket_mode: true
    worker_type: "claude_code"       # 或 "opencode_server"
    work_dir: ""                      # 默认使用 HotPlex workspace
    assistant_api_enabled: true       # 启用原生 Assistant API（需付费 Workspace）
    dm_policy: "allowlist"            # 私聊策略：allowlist / allow
    group_policy: "allowlist"         # 群聊策略：allowlist / allow
    require_mention: true             # 群聊中是否需要 @提及
    allow_from: []                    # 全局用户白名单（Slack User ID）
    allow_dm_from: []                 # 私聊用户白名单
    allow_group_from: []              # 群聊用户白名单
    reconnect_base_delay: 1s          # 断线重连基础延迟
    reconnect_max_delay: 60s          # 断线重连最大延迟
    stt_provider: "local"             # 语音转文字：local / 空（禁用）
    stt_local_cmd: "python3 ~/.hotplex/scripts/stt_server.py"
    stt_local_mode: "persistent"      # ephemeral / persistent
    stt_local_idle_ttl: 1h            # 持久模式空闲超时
```

所有配置项均可通过环境变量覆盖（`HOTPLEX_MESSAGING_SLACK_*` 前缀），参见 `configs/env.example`。

技术实现细节参见 [Slack Adapter 改进规格书](../../specs/Slack-Adapter-Improvement-Spec.md)。
