# P2P 聊天进入事件处理设计

**日期**: 2026-05-15
**状态**: Draft
**范围**: Feishu + Slack 双平台

---

## 背景与动机

用户打开与 HotPlex bot 的 P2P 聊天窗口时，飞书会发送 `bot_p2p_chat_entered_v1` 事件，但当前 HotPlex 未注册 handler，导致：
- SDK 日志持续产生 "not found handler" ERROR 噪音
- 错失在最佳时机（用户刚打开聊天）展示引导的机会
- 无法追踪"打开聊天 → 发送首条消息"的转化漏斗

Slack 平台虽无完全等价事件，但 `app_home_opened(tab=messages)` 提供了近似能力。

## 目标

1. 注册飞书 `bot_p2p_chat_entered_v1` handler，消除 ERROR 日志
2. 在用户进入 P2P 聊天时发送欢迎/引导卡片（简洁信息卡）
3. 区分新用户与冷启动回归用户，发送差异化内容
4. 记录进入事件到 SQLite，提供转化漏斗数据基础
5. Slack 端通过 `app_home_opened` 实现等价功能

## 平台事件映射

| 平台 | 事件 | 触发时机 | Bot token 可订阅 | 可用字段 |
|------|------|---------|-----------------|---------|
| Feishu | `im.chat.access_event.bot_p2p_chat_entered_v1` | 用户打开 P2P 聊天 | YES | chat_id, operator_id, last_message_id, last_message_create_time |
| Slack | `app_home_opened` (tab=messages) | 用户打开 App Home 消息 tab | YES | user, channel, tab, view |

**Slack 限制说明**：`im_open` 语义完全匹配但仅支持 user token，bot 无法订阅。`app_home_opened` 是 Slack 平台上最好的可用替代方案。

## 架构

```
平台事件触发层
  ├── Feishu: bot_p2p_chat_entered_v1  →  ws.go handler
  └── Slack:  app_home_opened          →  adapter.go type switch
        │
        ▼
chat_access 公共层（chat_access_store.go）
  ├── 去重（event_id 唯一约束）
  ├── 防抖（同 chat_id < 1h 跳过发送）
  ├── 新老用户判断
  └── 写入 chat_access_events 表
        │
        ▼
平台消息发送层
  ├── Feishu: Interactive Card via larkim API
  └── Slack:  Block Kit via PostMessage API
```

## 触发策略

### 去重

`chat_access_events.event_id` 列有 UNIQUE 约束，INSERT 失败时静默跳过。

### 防抖

同一 `(platform, chat_id, bot_id)` 组合最近一条记录的 `created_at` 距今 < 1 小时 → 仅写埋点，不发欢迎消息。

### 新老用户判断

**Feishu**：利用事件 payload 中的 `last_message_create_time`：
- 值为 0 或 nil → 新用户
- 值距今 > 24h → 冷启动回归用户
- 值距今 ≤ 24h → 活跃用户

**Slack**：`app_home_opened` 不含历史消息时间，需查 SQLite：
- 该 `(platform, user_id, bot_id)` 组合无记录 → 新用户
- 最新记录 `created_at` 距今 > 24h → 冷启动回归用户
- 最新记录 `created_at` 距今 ≤ 24h → 活跃用户

### 发送规则

| 用户类型 | 发送欢迎 | welcome_sent |
|---------|---------|-------------|
| 新用户 | YES | TRUE |
| 冷启动回归（>24h） | YES（welcome_back 变体） | TRUE |
| 活跃用户（≤24h） | NO | FALSE |

## 数据模型

### chat_access_events 表

```sql
CREATE TABLE IF NOT EXISTS chat_access_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    event_id TEXT NOT NULL UNIQUE,
    platform TEXT NOT NULL,
    chat_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    bot_id TEXT NOT NULL,
    last_message_at INTEGER DEFAULT 0,
    welcome_sent BOOLEAN NOT NULL DEFAULT FALSE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_ca_event ON chat_access_events(event_id);
CREATE INDEX idx_ca_chat_bot ON chat_access_events(platform, chat_id, bot_id);
```

`platform` 取值：`"feishu"` | `"slack"`。表由现有 SQLite 连接管理（`internal/session/store.go` 或通过 `sqlutil` 模块），在 gateway 启动时 auto-migrate。

## 欢迎卡片

### 飞书（Interactive Card）

简洁信息卡，包含欢迎语 + 能力概览 + 快捷命令提示。卡片内容模板：

```
👋 {welcome_text}

我可以帮你：
• 💻 编写、审查、调试代码
• 📁 管理项目文件和目录
• 🔍 搜索代码库和分析架构

快捷命令：/help /reset /cd
直接发消息即可开始 ✨
```

通过 phrases 系统的 `welcome` / `welcome_back` category 定制欢迎语，支持三级 fallback（全局 → 平台 → bot）。

### Slack（Block Kit）

等价布局，使用 section + divider + context blocks：

```json
{
  "blocks": [
    {
      "type": "section",
      "text": {
        "type": "mrkdwn",
        "text": "👋 {welcome_text}\n\n我可以帮你：\n• 💻 编写、审查、调试代码\n• 📁 管理项目文件和目录\n• 🔍 搜索代码库和分析架构"
      }
    },
    { "type": "divider" },
    {
      "type": "context",
      "elements": [
        {
          "type": "mrkdwn",
          "text": "快捷命令：`/help` `/reset` `/cd`  ·  直接发消息即可开始 ✨"
        }
      ]
    }
  ]
}
```

## phrases 扩展

在 `Phrases` 结构体新增两个 category：

### welcome（新用户）

```
{Weight: 3, Text: "Hi，我是 {bot_name}，你的 AI 编程助手！"}
{Weight: 2, Text: "欢迎！直接发消息给我，我们可以开始写代码了。"}
```

### welcome_back（冷启动回归用户）

```
{Weight: 3, Text: "好久不见！有什么我可以帮你的？"}
{Weight: 2, Text: "欢迎回来～随时继续。"}
```

`{bot_name}` 由各平台 adapter 在发送时替换为实际 bot 名称。

## 文件变更清单

| 文件 | 类型 | 说明 |
|------|------|------|
| `internal/messaging/feishu/ws.go` | 修改 | 在 dispatcher 链追加 `.OnP2ChatAccessEventBotP2pChatEnteredV1()` |
| `internal/messaging/feishu/chat_access.go` | 新增 | `handleChatEntered()` + 欢迎卡片构建 + 飞书 API 发送 |
| `internal/messaging/slack/adapter.go` | 修改 | `handleEventsAPI()` 改为 type switch，新增 `AppHomeOpenedEvent` 分支 |
| `internal/messaging/slack/chat_access.go` | 新增 | `handleAppHomeOpened()` + Block Kit 欢迎消息构建 |
| `internal/messaging/chat_access_store.go` | 新增 | 公共存储层：表创建、去重查询、记录写入 |
| `internal/messaging/phrases/phrases.go` | 修改 | 新增 `Welcome` / `WelcomeBack` 字段和默认短语池 |

## 关键实现细节

### 飞书 handler 注册

在 `ws.go` 的 `newEventHandler()` 链末尾追加：

```go
dispatcher.NewEventDispatcher("", "").
    // ... 现有 handlers ...
    OnP2ChatAccessEventBotP2pChatEnteredV1(a.handleChatEntered)
```

handler 直接调用飞书 `larkim.CreateMessage` API 发送欢迎卡，不经 bridge（因为此时无 session）。adapter 已持有 lark client，可复用。

### Slack handleEventsAPI 改造

当前是硬类型断言：

```go
msgEvent, ok := event.InnerEvent.Data.(*slackevents.MessageEvent)
if !ok { return }
```

改为 type switch：

```go
switch e := event.InnerEvent.Data.(type) {
case *slackevents.MessageEvent:
    a.handleMessageEvent(ctx, e)
case *slackevents.AppHomeOpenedEvent:
    a.handleAppHomeOpened(ctx, e)
}
```

现有 `handleMessageEvent` 逻辑不变，仅从 `handleEventsAPI` 提取为独立方法。

### 存储层初始化

`chat_access_store.go` 接受 `*sql.DB`（与 session store 共享同一 SQLite 连接）。在 gateway 启动时调用 `EnsureChatAccessSchema(db)` 执行 CREATE TABLE IF NOT EXISTS。

## 风险与边界

- **飞书 API 调用**：handler 中直接调用消息发送 API（绕过 bridge），因为无 session。adapter 已持有 lark client，无新增依赖。
- **Slack app_home_opened 覆盖率**：部分用户可能直接发消息而不经过 App Home。此场景下首条消息仍走现有流程，欢迎消息不会触发（可接受的退化）。
- **速率限制**：冷却机制（1h 防抖 + 24h 冷启动判断）规避飞书/Slack 消息发送频率限制。
- **多 bot 场景**：各 adapter 实例独立注册 handler，bot_id 从 adapter 配置获取，天然隔离。
- **向后兼容**：纯新增 handler 和存储表，不改任何现有消息处理流程，零回归风险。
- **SQLite 并发**：与现有 session store 共享连接，已有 WAL 模式和互斥保护，无需额外处理。
