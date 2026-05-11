---
version: 4
description: "HotPlex platform capabilities and tools"
---

# SKILLS.md

## 架构

    用户 → 消息平台 (Slack/飞书/WebChat) → HotPlex 网关 → Worker (你)

网关管理连接、路由、心跳。Session 跨交互持久化，直到空闲超时或用户终止。

## 你可以使用的工具

### Slack CLI

通过 `hotplex slack` 命令操作 Slack：

| 命令 | 用途 |
|------|------|
| `hotplex slack send-message --channel <id> --text "..."` | 发送消息（支持 mrkdwn） |
| `hotplex slack upload-file --file <path> --title "..."` | 上传文件 |
| `hotplex slack list-channels --types im,public_channel` | 列出频道 |
| `hotplex slack react add --channel <id> --ts <ts> --emoji <name>` | 添加 Emoji 反应 |
| `hotplex slack bookmark add/list/remove` | 书签管理 |
| `hotplex slack schedule-message --text "..." --at <RFC3339>` | 定时发送 |

### 飞书 CLI

通过 `lark-cli` 操作飞书：

| 命令 | 用途 |
|------|------|
| `lark-cli im +messages-send --chat-id <id> --markdown "..."` | 发送消息 |
| `lark-cli im +messages-reply --message-id <id> --text "..."` | 回复消息 |
| `lark-cli im +chat-search --query "..."` | 搜索群组 |
| `lark-cli docs` / `lark-cli drive` | 文档与云盘操作 |
| `lark-cli base` | 多维表格操作 |

### Cron 定时任务

通过 `hotplex cron` 创建定时/延迟/周期任务。详见 `~/.hotplex/skills/cron.md`。

### 语音

STT 自动转写语音为文本，等同文本处理。TTS 支持语音合成输出。

## 平台特性

| 平台 | 输出特点 |
|------|---------|
| Slack | 消息分块，Markdown 转换，限流流式 |
| 飞书 | 流式卡片，交互按钮，卡片 TTL |
| WebChat | 完整 Markdown，实时流式 |

## 网关命令（无需你处理）

`/gc` `/park` — 休眠 | `/reset` `/new` — 重置 | `/cd <path>` — 切换目录

## 配置层级

此文件支持 3 级 fallback，高优先级完整替换低优先级：
- 全局级：~/.hotplex/agent-configs/SKILLS.md（本文件）
- 平台级：~/.hotplex/agent-configs/slack/SKILLS.md
- Bot 级：~/.hotplex/agent-configs/slack/U12345/SKILLS.md

使用 `hotplex-setup` skill 进行交互式个性化配置。修改后对新会话生效。
