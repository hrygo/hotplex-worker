---
type: spec
tags:
  - project/HotPlex
  - messaging/slack
  - cli
  - file-upload
  - canvas
  - multimedia
date: 2026-05-03
status: draft
priority: high
estimated_hours: 40
last_updated: 2026-05-03
---

# HotPlex Slack CLI 子命令规格书

> 版本: v2.0-draft
> 日期: 2026-05-03
> SDK: `github.com/slack-go/slack@v0.22.0`
> 架构原则: Agent 主动调用 > Gateway 被动解析

---

## 1. 背景与动机

### 1.1 问题

Oracle（Claude Code）生成播客、视频、报告等内容后，需要通过 Slack 发送给用户。当前有两种路径：

| 路径 | 问题 |
|------|------|
| Gateway 自动检测 Claude 输出中的文件路径 | 不可靠：LLM 输出不确定，正则解析是猜测，无反馈环 |
| Claude Code 直接 curl 调 Slack API | 可用但无封装：token 管理散落，错误处理粗糙，不可复用 |

### 1.2 方案

在 `hotplex` CLI 中新增 `slack` 子命令组，提供文件上传、消息发送等能力。Claude Code 通过 Bash 工具调用，与 `notebooklm`、`lark-cli`、`obsidian-cli` 同一范式。

**为什么是 CLI 子命令而非独立工具：**
- 复用已有的 `slack-go/slack` SDK（已在 go.mod）
- 复用已有的 `~/.hotplex/.env` 配置加载逻辑
- 复用已有的 Cobra CLI 框架
- 单二进制，无额外运行时依赖
- Gateway 可自动注入上下文（channel/thread）到 Worker 环境

### 1.3 依赖

| 依赖 | 版本 | 说明 |
|------|------|------|
| `slack-go/slack` | v0.22.0 | 已在 go.mod，含 `UploadFileContext` 三步式便捷方法 |
| `spf13/cobra` | v1.10.2 | 已在 go.mod |
| `spf13/viper` | v1.19.0 | 已在 go.mod |

---

## 2. 命令设计

### 2.1 命令树

```
hotplex slack
  ├── send-message       发送文本消息
  ├── update-message     更新已发送的消息
  ├── schedule-message   定时发送消息
  ├── upload-file        上传文件（全类型，新 API 三步式）
  ├── download-file      下载文件
  ├── search             搜索消息和文件
  ├── list-channels      列出频道/DM
  ├── canvas             Canvas 文档管理
  │   ├── create         创建 Canvas
  │   ├── edit           编辑 Canvas 内容
  │   └── list-sections  查看 Canvas 章节
  ├── bookmark           书签管理
  │   ├── add            添加书签
  │   ├── list           列出书签
  │   └── remove         删除书签
  └── react              添加/移除表情反应
```

### 2.2 `hotplex slack send-message`

```bash
hotplex slack send-message --text "消息内容" [--channel <id>] [--thread-ts <ts>]
```

| 参数 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `--text` / `-t` | 是 | — | 消息文本（支持 mrkdwn） |
| `--channel` / `-ch` | 否* | `$HOTPLEX_SLACK_CHANNEL_ID` | 目标频道/DM ID |
| `--thread-ts` | 否 | `$HOTPLEX_SLACK_THREAD_TS` | 线程回复时间戳 |
| `--config` / `-c` | 否 | `~/.hotplex/config.yaml` | 配置文件路径 |

> \* `--channel` 和环境变量二选一，都无则报错。

**输出：**

```
ok  channel=D0AQJ5CLZN0  ts=1777797319.120439
```

JSON 模式（`--json`）：

```json
{"ok": true, "channel": "D0AQJ5CLZN0", "ts": "1777797319.120439"}
```

### 2.3 `hotplex slack upload-file`

```bash
hotplex slack upload-file --file ./podcast.mp3 [--title "标题"] [--channel <id>] [--thread-ts <ts>] [--comment "说明"]
```

| 参数 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `--file` / `-f` | 是 | — | 文件路径（本地） |
| `--title` | 否 | 文件名 | 文件标题 |
| `--comment` | 否 | — | 文件说明（随文件显示） |
| `--channel` / `-ch` | 否* | `$HOTPLEX_SLACK_CHANNEL_ID` | 目标频道/DM |
| `--thread-ts` | 否 | `$HOTPLEX_SLACK_THREAD_TS` | 线程回复 |
| `--config` / `-c` | 否 | `~/.hotplex/config.yaml` | 配置文件路径 |
| `--json` | 否 | — | JSON 输出 |

**输出：**

```
ok  file_id=F0B1BL1SBK4  title="OpenClaw 播客"  size=42552573  channel=D0AQJ5CLZN0
```

JSON 模式：

```json
{"ok": true, "file_id": "F0B1BL1SBK4", "title": "OpenClaw 播客", "size": 42552573, "channel": "D0AQJ5CLZN0"}
```

**文件大小限制：**
- 默认最大 50MB（Slack Plus+ 计划支持）
- 超过限制时返回错误，建议用户通过云存储分享链接
- `--max-size` 参数可覆盖默认限制

### 2.4 `hotplex slack list-channels`

```bash
hotplex slack list-channels [--types im,public_channel,private_channel] [--limit 100]
```

| 参数 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `--types` | 否 | `im` | 频道类型（逗号分隔） |
| `--limit` / `-n` | 否 | 100 | 最大返回数 |
| `--json` | 否 | — | JSON 输出 |

**输出（表格模式）：**

```
ID              NAME                TYPE
D0AQJ5CLZN0    黄飞虹              DM
C0ABC12345     general             Public
```

### 2.5 `hotplex slack download-file`

```bash
hotplex slack download-file --file-id <id> --output ./save/path.mp3
```

| 参数 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `--file-id` | 是 | — | Slack 文件 ID |
| `--output` / `-o` | 是 | — | 本地保存路径 |
| `--config` / `-c` | 否 | `~/.hotplex/config.yaml` | 配置文件 |

### 2.6 `hotplex slack update-message`

更新已发送的消息（用于修正或追加内容）。

```bash
hotplex slack update-message --channel <id> --ts <timestamp> --text "新内容"
```

| 参数 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `--channel` / `-ch` | 是 | — | 频道 ID |
| `--ts` | 是 | — | 消息时间戳 |
| `--text` / `-t` | 是 | — | 新消息文本 |
| `--json` | 否 | — | JSON 输出 |

**SDK:** `client.UpdateMessageContext(ctx, channel, ts, MsgOptionText(text))`

### 2.7 `hotplex slack schedule-message`

定时发送消息。

```bash
hotplex slack schedule-message --text "提醒内容" --at "2026-05-04T09:00:00+08:00" [--channel <id>]
```

| 参数 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `--text` / `-t` | 是 | — | 消息文本 |
| `--at` | 是 | — | 发送时间（ISO 8601 或 Unix timestamp） |
| `--channel` / `-ch` | 否* | `$HOTPLEX_SLACK_CHANNEL_ID` | 目标频道 |
| `--json` | 否 | — | JSON 输出（返回 scheduled_message_id） |

**SDK:** `client.ScheduleMessageContext(ctx, channelID, postAt, options...)`

### 2.8 `hotplex slack search`

搜索 Slack 中的消息和文件。

```bash
hotplex slack search --query "关键词" [--type messages|files|all] [--limit 20]
```

| 参数 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `--query` / `-q` | 是 | — | 搜索关键词 |
| `--type` | 否 | `all` | 搜索范围：messages / files / all |
| `--limit` / `-n` | 否 | 20 | 最大返回数 |
| `--json` | 否 | — | JSON 输出 |

**输出：**

```
[MESSAGE] 2026-05-03 #general "讨论了 OpenClaw 架构..."
[FILE]    2026-05-02 report.pdf (2.3MB)
```

**SDK:** `client.SearchContext()`, `client.SearchMessagesContext()`, `client.SearchFilesContext()`

### 2.9 `hotplex slack canvas`

Canvas 是 Slack 的原生文档系统（类 Notion），适合存储结构化知识。

#### 2.9.1 `hotplex slack canvas create`

```bash
hotplex slack canvas create --title "文档标题" --content "# 标题\n正文内容" [--channel <id>]
```

| 参数 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `--title` | 是 | — | Canvas 标题 |
| `--content` | 否 | — | 初始内容（Markdown） |
| `--file` | 否 | — | 从文件读取内容（与 --content 二选一） |
| `--channel` / `-ch` | 否 | — | 关联到频道的 Canvas |
| `--json` | 否 | — | JSON 输出（返回 canvas_id） |

**SDK:** `client.CreateCanvasContext(ctx, title, documentContent)`

**返回的 canvas_id** 可通过 `https://hotplex.slack.com/docs/<canvas_id>` 访问。

#### 2.9.2 `hotplex slack canvas edit`

```bash
hotplex slack canvas edit --canvas-id <id> --content "新内容" [--section-id <sid>]
```

| 参数 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `--canvas-id` | 是 | — | Canvas ID |
| `--content` | 否 | — | 新内容（Markdown） |
| `--file` | 否 | — | 从文件读取（与 --content 二选一） |
| `--section-id` | 否 | — | 仅编辑指定章节 |
| `--operation` | 否 | `replace` | 操作：replace / insert_after / insert_before / delete |

**SDK:** `client.EditCanvasContext(ctx, EditCanvasParams)`

#### 2.9.3 `hotplex slack canvas list-sections`

```bash
hotplex slack canvas list-sections --canvas-id <id>
```

**SDK:** `client.LookupCanvasSectionsContext(ctx, params)`

### 2.10 `hotplex slack bookmark`

频道书签管理。

#### 2.10.1 `hotplex slack bookmark add`

```bash
hotplex slack bookmark add --channel <id> --title "标题" [--url <url>] [--emoji "🔗"]
```

| 参数 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `--channel` / `-ch` | 是 | — | 频道 ID |
| `--title` | 是 | — | 书签标题 |
| `--url` | 否 | — | 链接 URL |
| `--emoji` | 否 | `🔗` | 图标 |

**SDK:** `client.AddBookmarkContext(ctx, channelID, params)`

**Oracle 场景：** 生成研究报告后，自动在频道添加书签指向 Canvas 文档或附件。

#### 2.10.2 `hotplex slack bookmark list`

```bash
hotplex slack bookmark list --channel <id>
```

**SDK:** `client.ListBookmarksContext(ctx, channelID)`

#### 2.10.3 `hotplex slack bookmark remove`

```bash
hotplex slack bookmark remove --channel <id> --bookmark-id <id>
```

**SDK:** `client.RemoveBookmarkContext(ctx, channelID, bookmarkID)`

### 2.11 `hotplex slack react`

```bash
hotplex slack react add --channel <id> --ts <timestamp> --emoji "white_check_mark"
hotplex slack react remove --channel <id> --ts <timestamp> --emoji "white_check_mark"
```

| 参数 | 必填 | 说明 |
|------|------|------|
| `--channel` / `-ch` | 是 | 频道 ID |
| `--ts` | 是 | 消息时间戳 |
| `--emoji` / `-e` | 是 | 表情名称（不带冒号） |

**SDK:** `client.AddReactionContext()`, `client.RemoveReactionContext()`

**Oracle 场景：** 任务完成后在消息上标记 ✅ 或 🎯，提供视觉反馈。

---

## 3. 技术实现

### 3.1 文件结构

```
cmd/hotplex/
  slack_cmd.go           # newSlackCmd() — slack 父命令 + 子命令注册
  slack_message.go       # send-message / update-message / schedule-message
  slack_upload.go        # upload-file 子命令实现
  slack_download.go      # download-file 子命令实现
  slack_channels.go      # list-channels 子命令实现
  slack_search.go        # search 子命令实现
  slack_canvas.go        # canvas create/edit/list-sections
  slack_bookmark.go      # bookmark add/list/remove
  slack_react.go         # react add/remove

internal/cli/slack/
  client.go              # NewClient() — 从配置创建 slack.Client
  message.go             # sendMessage() — 消息发送/更新/定时
  upload.go              # uploadFile() — 文件上传核心逻辑
  download.go            # downloadFile() — 文件下载
  channels.go            # listChannels() — 频道列表
  search.go              # search() — 消息/文件搜索
  canvas.go              # canvas CRUD
  bookmark.go            # bookmark CRUD
  react.go               # reaction add/remove
```

### 3.2 核心代码模式

#### 3.2.1 Slack Client 创建 (`internal/cli/slack/client.go`)

复用现有配置加载，创建轻量客户端（不需要 Socket Mode）：

```go
package slackcli

import (
    "context"
    "fmt"
    "github.com/slack-go/slack"
    "github.com/hrygo/hotplex/internal/config"
)

func NewClient(cfg *config.Config) (*slack.Client, error) {
    token := cfg.Messaging.Slack.BotToken
    if token == "" {
        return nil, fmt.Errorf("slack bot token not configured (HOTPLEX_MESSAGING_SLACK_BOT_TOKEN)")
    }
    return slack.New(token), nil
}

// resolveChannel 从参数或环境变量获取 channel ID
func ResolveChannel(flagChannel string) (string, error) {
    if flagChannel != "" {
        return flagChannel, nil
    }
    if env := os.Getenv("HOTPLEX_SLACK_CHANNEL_ID"); env != "" {
        return env, nil
    }
    return "", fmt.Errorf("--channel is required (or set HOTPLEX_SLACK_CHANNEL_ID env var)")
}

// resolveThreadTS 从参数或环境变量获取 thread_ts
func ResolveThreadTS(flagTS string) string {
    if flagTS != "" {
        return flagTS
    }
    return os.Getenv("HOTPLEX_SLACK_THREAD_TS")
}
```

#### 3.2.2 文件上传核心 (`internal/cli/slack/upload.go`)

使用 SDK 的 `UploadFileContext` 便捷方法（内部已封装三步式新 API）：

```go
package slackcli

import (
    "context"
    "fmt"
    "os"
    "github.com/slack-go/slack"
)

func UploadFile(ctx context.Context, client *slack.Client, params *UploadParams) (*UploadResult, error) {
    file, err := os.Open(params.FilePath)
    if err != nil {
        return nil, fmt.Errorf("open file: %w", err)
    }
    defer file.Close()

    stat, err := file.Stat()
    if err != nil {
        return nil, fmt.Errorf("stat file: %w", err)
    }

    if params.MaxSize > 0 && stat.Size() > params.MaxSize {
        return nil, fmt.Errorf("file size %d exceeds limit %d", stat.Size(), params.MaxSize)
    }

    uploadParams := slack.UploadFileParameters{
        Filename:        filepath.Base(params.FilePath),
        File:            params.FilePath,
        FileSize:        int(stat.Size()),
        Title:           params.Title,
        InitialComment:  params.Comment,
        Channel:         params.Channel,
        ThreadTimestamp:  params.ThreadTS,
    }

    result, err := client.UploadFileContext(ctx, uploadParams)
    if err != nil {
        return nil, fmt.Errorf("upload failed: %w", err)
    }

    return &UploadResult{
        FileID:  result.ID,
        Title:   result.Title,
        Size:    stat.Size(),
        Channel: params.Channel,
    }, nil
}
```

#### 3.2.3 命令注册 (`cmd/hotplex/slack_cmd.go`)

```go
package main

import "github.com/spf13/cobra"

func newSlackCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "slack",
        Short: "Slack messaging operations",
        Long:  "Send messages, upload files, and interact with Slack workspaces.\n" +
               "Uses the same configuration as the gateway (~/.hotplex/.env).",
    }
    cmd.AddCommand(
        newSlackSendMessageCmd(),
        newSlackUpdateMessageCmd(),
        newSlackScheduleMessageCmd(),
        newSlackUploadFileCmd(),
        newSlackDownloadFileCmd(),
        newSlackListChannelsCmd(),
        newSlackSearchCmd(),
        newSlackCanvasCmd(),       // canvas create/edit/list-sections
        newSlackBookmarkCmd(),     // bookmark add/list/remove
        newSlackReactCmd(),        // react add/remove
    )
    return cmd
}
```

### 3.3 main.go 注册

在 `cmd/hotplex/main.go` 的 `rootCmd.AddCommand(...)` 中新增：

```go
rootCmd.AddCommand(
    // ... 现有命令 ...
    newSlackCmd(),      // 新增
)
```

### 3.4 配置加载策略

CLI 子命令需要加载配置但**不启动 Gateway**。复用现有 `loadEnvFile()` + `config.Load()`：

```go
func loadSlackConfig(configPath string) (*config.Config, error) {
    dir := filepath.Dir(configPath)
    loadEnvFile(dir)  // 加载 ~/.hotplex/.env
    cfg, err := config.Load(configPath, config.LoadOptions{SkipSecrets: true})
    if err != nil {
        return nil, err
    }
    if !cfg.Messaging.Slack.Enabled {
        return nil, fmt.Errorf("slack is not enabled in configuration")
    }
    return cfg, nil
}
```

---

## 4. 环境变量上下文注入

### 4.1 Gateway → Worker 注入

修改 `internal/worker/claudecode/worker.go`，启动 Claude Code 子进程时注入 Slack 上下文：

```go
// 在构建 cmd.Env 时追加
if conn, ok := conn.(*messaging.SlackConn); ok {
    cmd.Env = append(cmd.Env,
        "HOTPLEX_SLACK_CHANNEL_ID="+conn.ChannelID(),
        "HOTPLEX_SLACK_THREAD_TS="+conn.ThreadTS(),
    )
}
```

### 4.2 CLI 自动读取

`internal/cli/slack/client.go` 中的 `ResolveChannel()` 和 `ResolveThreadTS()` 读取这些环境变量作为默认值。

**效果：** Claude Code 在当前会话中直接调用 `hotplex slack upload-file --file ./x.mp3` 即可，无需指定 channel。

---

## 5. 配套 Skill 设计

### 5.1 Skill 文件

路径: `skills/slack/` 或 `~/.claude/skills/slack/SKILL.md`

```markdown
# Slack Integration

通过 hotplex CLI 向 Slack 发送消息、文件，管理 Canvas 和书签。

## 何时使用

- 用户说"发给我"、"发送到 Slack"、"slack 发"
- 内容生成后需推送（播客、报告、图片、视频等）
- 需要创建持久化文档（Canvas）
- 需要在频道添加书签/链接

## 命令速查

| 场景 | 命令 |
|------|------|
| 发消息 | `hotplex slack send-message --text "内容"` |
| 上传文件 | `hotplex slack upload-file --file <path> --title "标题"` |
| 创建文档 | `hotplex slack canvas create --title "标题" --content "# 内容"` |
| 添加书签 | `hotplex slack bookmark add --channel <id> --title "标题" --url <url>` |
| 搜索 | `hotplex slack search --query "关键词"` |
| 定时消息 | `hotplex slack schedule-message --text "提醒" --at "09:00"` |
| 标记反应 | `hotplex slack react add --channel <id> --ts <ts> --emoji white_check_mark` |

## 默认行为

- 不指定 --channel 时自动发到当前对话（环境变量注入）
- 不指定 --title 时使用文件名

## 工作流示例

### 播客生成 → Slack 推送
notebooklm download audio ./podcast.mp3 -n <notebook_id>
hotplex slack upload-file --file ./podcast.mp3 --title "OpenClaw 播客"

### 研究报告 → Canvas 持久化 + 频道书签
hotplex slack canvas create --title "OpenClaw 技术研究" --file ./report.md --channel C0ABC
hotplex slack bookmark add --channel C0ABC --title "OpenClaw 研究报告" --url "https://hotplex.slack.com/docs/<canvas_id>"

### 任务完成 → 反应标记
hotplex slack react add --channel <id> --ts <ts> --emoji white_check_mark

### 搜索历史消息
hotplex slack search --query "OpenClaw 架构" --type messages
```

### 5.2 CLAUDE.md 更新

在 Toolchain 表中新增行：

```markdown
| `hotplex slack` | v1.4.0+ | Slack 输出 | 文件上传、消息发送、频道查询 |
```

---

## 6. 错误处理

| 场景 | 退出码 | 输出 |
|------|--------|------|
| Token 未配置 | 1 | `error: slack bot token not configured` |
| Channel 未指定且无环境变量 | 1 | `error: --channel is required` |
| 文件不存在 | 1 | `error: open file: no such file or directory` |
| 文件超限 | 1 | `error: file size 52428800 exceeds limit 52428800` |
| Slack API 限流 | 2 | `error: upload failed: rate_limited` |
| Slack API 错误 | 1 | `error: upload failed: <slack_error>` |
| 成功 | 0 | `ok  file_id=xxx  title=xxx  size=xxx  channel=xxx` |

---

## 7. 测试计划

### 7.1 单元测试

| 测试 | 文件 | 说明 |
|------|------|------|
| `TestNewClient` | `client_test.go` | 配置加载 + client 创建 |
| `TestResolveChannel` | `client_test.go` | 参数 > 环境变量 > 报错 |
| `TestUploadFile` | `upload_test.go` | 使用 mock client 验证参数传递 |
| `TestUploadFileTooLarge` | `upload_test.go` | 超限拒绝 |
| `TestSendMessage` | `message_test.go` | 使用 mock client 验证 |

### 7.2 集成测试

```bash
# 前提：~/.hotplex/.env 中有有效 token

# 1. 发送消息
./hotplex slack send-message --text "集成测试" --channel D0AQJ5CLZN0

# 2. 上传小文件
echo "test" > /tmp/test.txt
./hotplex slack upload-file --file /tmp/test.txt --title "集成测试文件" --channel D0AQJ5CLZN0

# 3. 列出 DM
./hotplex slack list-channels --types im --json

# 4. 上下文注入测试
HOTPLEX_SLACK_CHANNEL_ID=D0AQJ5CLZN0 ./hotplex slack send-message --text "无需指定 channel"
```

---

## 8. 实施阶段

| 阶段 | 内容 | 预估工时 | 优先级 |
|------|------|----------|--------|
| **P1** | `upload-file` 命令 | 6h | P0 — 满足 Oracle 播客发送需求 |
| **P1** | `send-message` 命令 | 3h | P0 |
| **P1** | main.go 注册 + make build | 1h | P0 |
| **P2** | 环境变量上下文注入（Gateway → Worker） | 2h | P1 — 免指定 channel |
| **P2** | `update-message` 命令 | 2h | P1 |
| **P2** | `react` 命令 | 2h | P1 — 视觉反馈 |
| **P2** | `list-channels` 命令 | 2h | P2 |
| **P3** | `canvas create/edit/list-sections` | 4h | P1 — 知识持久化核心 |
| **P3** | `bookmark add/list/remove` | 3h | P2 — 频道资源管理 |
| **P3** | `search` 命令 | 3h | P2 — 消息/文件搜索 |
| **P3** | `schedule-message` 命令 | 2h | P2 |
| **P3** | `download-file` 命令 | 2h | P2 |
| **P4** | 配套 Skill 编写 | 3h | P1 — 引导 Claude Code 行为 |
| **P4** | CLAUDE.md 更新 | 0.5h | P1 |
| **总计** | | **37.5h** | |

---

## 9. SDK API 参考（v0.22.0 已验证）

所有方法均有 Context 版本（`xxxContext(ctx, ...)`），以下仅列非 Context 版本。

### 9.1 文件上传（三步式 — `UploadFileContext` 封装）

```go
func (api *Client) UploadFile(params UploadFileParameters) (*FileSummary, error)
// 内部: GetUploadURLExternal → UploadToURL → CompleteUploadExternal

type UploadFileParameters struct {
    File, Content, Reader                          // 三选一输入
    Filename, Title, InitialComment                // 元数据
    FileSize int                                   // 必填
    Channel, ThreadTimestamp                        // 分享目标
    AltTxt, SnippetType                            // 可选
    Blocks  Blocks                                 // 富文本
}
```

### 9.2 消息

```go
func (api *Client) PostMessage(channelID string, options ...MsgOption) (string, string, error)
func (api *Client) UpdateMessage(channelID, timestamp string, options ...MsgOption) (string, string, string, error)
func (api *Client) DeleteMessage(channel, messageTimestamp string) (string, string, error)
func (api *Client) ScheduleMessage(channelID, postAt string, options ...MsgOption) (string, string, error)
func (api *Client) GetPermalink(params *PermalinkParameters) (string, error)
func (api *Client) GetScheduledMessages(params *GetScheduledMessagesParameters) ([]ScheduledMessage, string, error)
func (api *Client) DeleteScheduledMessage(params *DeleteScheduledMessageParameters) (bool, error)
func (api *Client) PostEphemeral(channelID, userID string, options ...MsgOption) (string, error)
```

### 9.3 文件

```go
func (api *Client) GetFileInfo(fileID string, count, page int) (*File, []Comment, *Paging, error)
func (api *Client) ListFiles(params ListFilesParameters) ([]File, error)
func (api *Client) DeleteFile(fileID string) error
func (api *Client) ShareFilePublicURL(fileID string, params ShareFilePublicURLParameters) (*File, error)
```

### 9.4 搜索

```go
func (api *Client) Search(query string, params SearchParameters) (*SearchMessages, *SearchFiles, error)
func (api *Client) SearchMessages(query string, params SearchParameters) (*SearchMessages, error)
func (api *Client) SearchFiles(query string, params SearchParameters) (*SearchFiles, error)
```

### 9.5 Canvas

```go
func (api *Client) CreateCanvas(title string, documentContent DocumentContent) (string, error)
func (api *Client) EditCanvas(params EditCanvasParams) error
func (api *Client) DeleteCanvas(canvasID string) error
func (api *Client) SetCanvasAccess(params SetCanvasAccessParams) error
func (api *Client) DeleteCanvasAccess(params DeleteCanvasAccessParams) error
func (api *Client) LookupCanvasSections(params LookupCanvasSectionsParams) ([]CanvasSection, error)

type DocumentContent struct {
    Type     string `json:"type"`     // "markdown"
    Markdown string `json:"markdown"`
}

type EditCanvasParams struct {
    CanvasID   string `json:"canvas_id"`
    Changes    []CanvasChange `json:"changes"`
}

type CanvasChange struct {
    Operation   string `json:"operation"`    // replace/insert_after/insert_before/delete
    SectionID   string `json:"section_id,omitempty"`
    DocumentContent `json:"document_content,omitempty"`
}
```

### 9.6 Bookmarks

```go
func (api *Client) AddBookmark(channelID string, params AddBookmarkParameters) (Bookmark, error)
func (api *Client) ListBookmarks(channelID string) ([]Bookmark, error)
func (api *Client) EditBookmark(channelID, bookmarkID string, params EditBookmarkParameters) (Bookmark, error)
func (api *Client) RemoveBookmark(channelID, bookmarkID string) error

type AddBookmarkParameters struct {
    Title  string `json:"title"`
    Type   string `json:"type"`    // "link" / "text"
    URL    string `json:"url,omitempty"`
    Emoji  string `json:"emoji,omitempty"`
    EntityID string `json:"entity_id,omitempty"` // 关联 Canvas
}
```

### 9.7 Reactions

```go
func (api *Client) AddReaction(name string, item ItemRef) error
func (api *Client) RemoveReaction(name string, item ItemRef) error
func (api *Client) GetReactions(item ItemRef, params GetReactionsParameters) (ReactedItem, error)

type ItemRef struct {
    Channel   string `json:"channel"`
    Timestamp string `json:"timestamp"`
    File      string `json:"file,omitempty"`
}
```

### 9.8 频道/对话

```go
func (api *Client) GetAllConversations(options ...GetConversationsOption) ([]Channel, error)
func (api *Client) GetConversationsForUser(params *GetConversationsForUserParameters) ([]Channel, string, error)
```

### 9.9 Stars / Pins

```go
// Stars
func (api *Client) AddStar(channel string, item ItemRef) error
func (api *Client) RemoveStar(channel string, item ItemRef) error
func (api *Client) ListStars(params StarsParameters) ([]Item, string, error)

// Pins
func (api *Client) AddPin(channel string, item ItemRef) error
func (api *Client) RemovePin(channel string, item ItemRef) error
func (api *Client) ListPins(channel string) ([]Item, *Paging, error)
```

### 9.10 Assistant API（AI 相关，SDK v0.22.0 已支持）

```go
func (api *Client) SetAssistantThreadsTitle(params AssistantThreadsSetTitleParameters) error
func (api *Client) SetAssistantThreadsStatus(params AssistantThreadsSetStatusParameters) error
func (api *Client) SetAssistantThreadsSuggestedPrompts(params AssistantThreadsSetSuggestedPromptsParameters) error
func (api *Client) SearchAssistantContext(params AssistantSearchContextParameters) (*AssistantSearchContextResponse, error)
```

> 注：Assistant API 主要由 Gateway 的 Socket Mode 直接调用，CLI 层面暂不暴露。

---

## 10. 风险与缓解

| 风险 | 概率 | 缓解 |
|------|------|------|
| Slack API 限流 | 中 | 重试机制（`--retry 3`），错误信息清晰 |
| 大文件上传超时 | 中 | 默认 5 分钟 timeout，`--timeout` 可配置 |
| Token 过期 | 低 | Bot token 不过期，但需 scopes 正确 |
| `files.upload` 完全关闭 | 低 | SDK v0.22.0 已用新 API，不依赖旧接口 |
| Claude Code 不知道何时调用 | 中 | Skill + CLAUDE.md 引导行为 |
| Canvas API 需要付费计划 | 中 | 免费计划可能不支持 Canvas，graceful degradation |
| Bot token 权限不足 | 中 | `doctor` 命令检查所需 scopes，给出提示 |
| 搜索 API 需要特定 scope | 低 | `search:read` scope，`doctor` 检查 |

---

## 11. Oracle 场景映射

| Oracle 工作流 | hotplex slack 命令组合 |
|---------------|----------------------|
| 播客生成 → 发送 | `upload-file --file podcast.mp3` |
| 报告生成 → 持久化 | `canvas create --title "报告" --file report.md` |
| 研究摘要 → 定时推送 | `schedule-message --text "摘要" --at "09:00"` |
| 知识卡片 → 频道书签 | `bookmark add --title "TIL" --url <link>` |
| 任务完成 → 视觉反馈 | `react add --emoji white_check_mark` |
| 历史消息检索 | `search --query "关键词"` |
| 消息修正 | `update-message --ts <ts> --text "修正后"` |

---

## 12. Token 权限要求

Bot token (`xoxb-`) 需要以下 OAuth scopes：

| Scope | 用途 | 优先级 |
|-------|------|--------|
| `chat:write` | 发送/更新/删除消息 | P0 |
| `files:write` | 上传文件 | P0 |
| `files:read` | 下载文件 | P2 |
| `channels:read` | 列出频道 | P2 |
| `groups:read` | 列出私有频道 | P2 |
| `im:read` | 列出 DM | P2 |
| `search:read` | 搜索消息/文件 | P2 |
| `reactions:write` | 添加表情反应 | P1 |
| `bookmarks:write` | 管理书签 | P2 |
| `canvases:write` | 创建/编辑 Canvas | P3 |
| `canvases:read` | 读取 Canvas | P3 |
| `pins:write` | 固定消息 | P3 |
| `stars:write` | 收藏消息 | P3 |

`hotplex doctor` 命令应检查这些 scopes 是否已授权。
