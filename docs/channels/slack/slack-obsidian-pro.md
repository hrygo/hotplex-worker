---
name: Oracle
version: 1.0
type: AI Knowledge Agent
platform: Slack
tags: [obsidian, pkm, ai-agent, slack-bot]
app_id: A01ORACLE
background: "#7a3ce8"
socket_mode: true
---

# Oracle: Obsidian 专业管理 AI 助手

## 产品定位
Oracle 是基于 HotPlex 底层架构重构的、专为 **Obsidian (PKM)** 打造的专业知识库管理 Bot。它将 Slack 的沟通便捷性与 Obsidian 的本地双链知识体系无缝结合，帮助知识工作者在聊天界面中实现极速捕获、语义检索与双链重构。

## 核心专业能力
- **极速捕获 (Quick Capture)**：在 Slack 聊天中随时记录灵感、待办，自动写入指定的 Obsidian Daily Note 或 Inbox 收件箱。
- **语义检索与对话 (Chat with Vault)**：拥有整个本地 Vault 的上下文，可以直接询问：“我上周关于微服务改造的想法是什么？”
- **双链与拓扑分析 (Link & Graph)**：自动分析对话中提及的概念，推荐建立双向链接 `[[Concept]]`，甚至识别孤儿节点（Orphan Notes）。
- **Bases (结构化视图) 支持**：深度集成 Obsidian Bases 能力，在 Slack 中通过自然语言增删改查卡片视图、表格视图。

## 挂载技能库 (Supported Skills)
Oracle 自动发现并挂载本机底层的 **Obsidian 专属技能引擎 (Skills)**。无需额外配置，开箱即用：

| 技能引擎 (Skill) | 建议指令 | 核心应用场景 |
| :--- | :--- | :--- |
| `obsidian-capture` | `/obsidian-capture` | **极速捕获**：快速捕获灵感、想法，自动归档 |
| `obsidian-til` | `/obsidian-til` | **每日复盘**：管理 TIL (Today I Learned) 笔记 |
| `obsidian-markdown`| `/obsidian-markdown`| **笔记编辑**：创建和编辑 Obsidian Markdown 笔记 |
| `obsidian-graph` | `/obsidian-graph` | **图谱探索**：Obsidian 知识图谱深度分析 |
| `obsidian-bases` | `/obsidian-bases` | **结构视图**：创建和编辑 Bases 数据视图与筛选 |
| `obsidian-connect` | `/obsidian-connect` | **双链拓扑**：智能分析并重构笔记间的连接关系 |
| `obsidian-brainstorm`| `/obsidian-brainstorm`| **灵感激发**：调用 Obsidian 专属头脑风暴引擎 |
| `obsidian-project` | `/obsidian-project` | **项目驱动**：基于笔记体系的项目管理工具 |
| `obsidian-maintain`| `/obsidian-maintain`| **系统维保**：Obsidian 定期维护与垃圾清理 |
| `obsidian-rename` | `/obsidian-rename` | **安全重命名**：安全重命名 Vault 中的笔记及所有引用 |
| `obsidian-cli` | `/obsidian-cli` | **底层执行**：与 Obsidian 交互的 CLI 底层工具 |
| `(Core System)` | `/new`, `/park`| 框架控制流指令：重置检索上下文与安全释放内存 |

## 创意实验室 (Suggested Prompts)
高频场景触发语：

- **📅 每日复盘**: `调用 obsidian-til 技能，帮我汇总今天创建和修改的所有笔记，并生成一份 TIL (Today I Learned) 格式的复盘报告`
- **🕸️ 图谱探索**: `使用 obsidian-graph 技能，分析当前 Vault 中指定主题的核心连接关系，并指出可能的知识孤岛`
- **💡 灵感激发**: `运行 obsidian-brainstorm 技能，基于近期新增的笔记内容，帮我发散几个高价值的潜在探索方向`
- **🛠️ 系统维保**: `执行 obsidian-maintain 技能，清理库中未被引用的无效附件，并检查所有的断链错误`

## 权限与安全边界 (OAuth & Settings)

### 读写权限清单
- **文件系统**: `files:read`, `files:write`, `remote_files:read`, `remote_files:write`
- **消息与会话**: `chat:write`, `chat:write.customize`, `chat:write.public`, `im:read`, `im:write`, `im:history`, `channels:read`, `channels:history`, `channels:manage`, `groups:read`, `groups:history`, `mpim:read`, `mpim:write`, `mpim:history`
- **画布、书签与资源**: `canvases:read`, `canvases:write`, `bookmarks:read`, `bookmarks:write`, `links:read`, `links:write`, `pins:read`, `pins:write`
- **互动、成员与命令**: `assistant:write`, `commands`, `app_mentions:read`, `reactions:read`, `reactions:write`, `dnd:read`, `emoji:read`, `metadata.message:read`, `team:read`, `usergroups:read`, `users:read`

### 运行配置 (System Settings)
- **Socket Mode**: `Enabled`
- **Interactivity**: `Enabled`
- **Org Deploy**: `Enabled`
- **Event Subscriptions**: `app_home_opened`, `app_mention`, `assistant_thread_context_changed`, `assistant_thread_started`, `message.channels`, `message.groups`, `message.im`, `message.mpim`

## 原始配置参考 (Raw JSON)

```json
{
  "_metadata": {
    "major_version": 2,
    "minor_version": 1
  },
  "display_information": {
    "name": "Oracle",
    "long_description": "Oracle 是你的 Obsidian 知识库专业大管家。它将 Obsidian 本地 Vault 的强大能力延伸到了 Slack 聊天界面中。无论是随时闪现的灵感、会议纪要的快速整理、还是跨笔记的语义检索与双链网络分析，Oracle 都能自动完成。它支持创建和管理 Bases 数据视图，通过自然语言查询你的个人知识图谱。只需在频道中 @提及，即可让你的第二大脑随时待命。",
    "description": "专业的 Obsidian 知识管理搭档，在 Slack 中连接你的第二大脑",
    "background_color": "#7a3ce8"
  },
  "features": {
    "assistant_view": {
      "assistant_description": "专业的 Obsidian 知识管理搭档。支持在 Slack 中极速捕获灵感、全文语义搜索、构建双向链接，以及操作 Bases 结构化视图。",
      "suggested_prompts": [
        {
          "title": "📅 每日复盘",
          "message": "调用 obsidian-til 技能，帮我汇总今天创建和修改的所有笔记，并生成一份 TIL (Today I Learned) 格式的复盘报告"
        },
        {
          "title": "🕸️ 图谱探索",
          "message": "使用 obsidian-graph 技能，分析当前 Vault 中指定主题的核心连接关系，并指出可能的知识孤岛"
        },
        {
          "title": "💡 灵感激发",
          "message": "运行 obsidian-brainstorm 技能，基于近期新增的笔记内容，帮我发散几个高价值的潜在探索方向"
        },
        {
          "title": "🛠️ 系统维保",
          "message": "执行 obsidian-maintain 技能，清理库中未被引用的无效附件，并检查所有的断链错误"
        }
      ]
    },
    "app_home": {
      "home_tab_enabled": false,
      "messages_tab_enabled": true,
      "messages_tab_read_only_enabled": false
    },
    "bot_user": {
      "display_name": "Oracle",
      "always_online": true
    },
    "slash_commands": [
      {
        "command": "/obsidian-capture",
        "description": "极速捕获灵感或碎片信息到 Inbox，支持带上下文归档",
        "should_escape": false
      },
      {
        "command": "/obsidian-til",
        "description": "管理和记录今日所学 (Today I Learned)，每日自动复盘",
        "should_escape": false
      },
      {
        "command": "/obsidian-markdown",
        "description": "使用标准化 Markdown 格式创建或编辑 Obsidian 本地笔记",
        "should_escape": false
      },
      {
        "command": "/obsidian-graph",
        "description": "深度分析知识图谱，发现核心概念关系与知识孤岛",
        "should_escape": false
      },
      {
        "command": "/obsidian-bases",
        "description": "通过自然语言增删改查 Bases 的表格与卡片结构化视图",
        "should_escape": false
      },
      {
        "command": "/obsidian-connect",
        "description": "自动分析上下文，智能建立或重构笔记间的双向链接网络",
        "should_escape": false
      },
      {
        "command": "/obsidian-brainstorm",
        "description": "调用头脑风暴引擎，基于现有笔记发散新的思考和探索方向",
        "should_escape": false
      },
      {
        "command": "/obsidian-project",
        "description": "以笔记为核心进行项目管理、进度追踪与任务推进",
        "should_escape": false
      },
      {
        "command": "/obsidian-maintain",
        "description": "定期执行系统维保：清理冗余附件、修复断链与元数据",
        "should_escape": false
      },
      {
        "command": "/obsidian-rename",
        "description": "安全重命名 Vault 中的笔记及其所有相关的反向链接引用",
        "should_escape": false
      },
      {
        "command": "/obsidian-cli",
        "description": "执行与 Obsidian 插件系统交互的底层 CLI 指令",
        "should_escape": false
      },
      {
        "command": "/park",
        "description": "休眠当前会话，安全停止底层 Worker 进程以释放内存",
        "should_escape": false
      },
      {
        "command": "/gc",
        "description": "休眠会话（停止 Worker，保留上下文，同 /park）",
        "should_escape": false
      },
      {
        "command": "/new",
        "description": "重置当前知识检索的对话上下文，开启一段全新对话",
        "should_escape": false
      },
      {
        "command": "/reset",
        "description": "重置上下文（全新开始，同 /new）",
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
        "assistant:write",
        "app_mentions:read",
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
        "users:read"
      ]
    }
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
