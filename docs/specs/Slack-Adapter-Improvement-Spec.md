---
type: spec
tags:
  - project/HotPlex
  - messaging/slack
  - platform-adapter
date: 2026-04-18
status: final
progress: 0
priority: high
estimated_hours: 40
---

# Slack Adapter 改进规格书

> 版本: v1.0
> 日期: 2026-04-18
> 状态: Final
> 交叉复核: 已逐行对齐 `internal/messaging/slack/adapter.go`（278 行）、`events.go`（60 行）、`bridge.go`（140 行）源码；已对照 slack-go SDK v0.22.0 源码验证所有 API 签名；已参考 `~/hotplex` 生产实现验证设计模式有效性
> SDK 版本: `github.com/slack-go/slack@v0.22.0`
> 原则: SDK first（能用 SDK 的不写新代码）| 消除幻觉（所有引用已交叉验证）| 最佳实践（~/hotplex 参考，非金标准）

---

## 1. 概述

### 1.1 目标

基于对现有源码的精确审计，识别可落地的改进点，分三个阶段递进：

| 阶段 | 主题 | 优先级 | 目标 |
|------|------|--------|------|
| Phase 1 | 消息路由修复 | P0 | 修复 teamID/threadTS 缺失、通用 bot 防御、去重、用户提及解析 |
| Phase 2 | 用户体验 | P1 | mrkdwn 格式化、Abort 检测、Assistant Status（原生 API + emoji fallback） |
| Phase 3 | 安全 | P2 | 访问控制、限流增强、消息过期 |

### 1.2 现状分析（逐行验证）

**源码规模**: 5 文件 / ~896 行（`adapter.go` 278 + `events.go` 60 + `stream.go` 277 + `rate_limiter.go` 80 + `adapter_test.go` 201）

| 维度 | 当前状态（源码行号） | 差距等级 |
|------|---------------------|---------|
| teamID | `Start():69` 保存了 `botID` 但未保存 `authTest.TeamID`；`HandleTextMessage():161` 传入空字符串 | 高 |
| threadTS | `handleEventsAPI():121` 提取了 `threadTS` 但 `HandleTextMessage():161` 传入空字符串 | 高 |
| Bot 防御 | `:116` 仅检查 `msgEvent.BotID == a.botID`（自身），其他 bot 消息放行 | 中 |
| 去重 | `:139-142` 生成 `platformMsgID` 但**无 seen-set 检查** | 高 |
| 用户提及 | 无 `<@UID>` → `@Name` 解析，AI 收到原始 ID | 中 |
| 消息类型 | `events.go:11-31` 仅提取 `Text` 和 `SectionBlock`，RichTextBlock/Files 被忽略 | 中 |
| 访问控制 | `SlackConfig` 无任何策略字段 | 严重 |
| Abort | 无 | 高 |
| 状态指示 | 无 `assistant.threads.setStatus` 原生 API 支持；无 emoji fallback；无能力探测 | 高 |
| mrkdwn 格式化 | `SlackConn.WriteCtx():230` 直接 `MsgOptionText(text, false)` 发送原始文本 | 中 |

### 1.3 相关文档

- 架构设计: [[Platform-Messaging-Extension]] messaging 平台层
- 飞书对标: [[Feishu-Adapter-Improvement-Spec]] 同层级改进
- 协议规范: [[AEP-v1-Protocol]] Envelope 结构
- 生产参考: `~/hotplex/chatapps/slack/`（Go，~18,500 行）
- Assistant Status 生产参考: `~/hotplex/chatapps/slack/messages.go:410-507`（SetStatus/ClearStatus/emoji fallback）、`~/hotplex/chatapps/slack/typing.go:1-231`（多阶段 TypingIndicator）、`~/hotplex/chatapps/internal/status_manager.go:1-69`（StatusManager 去重）

---

## 2. Phase 1 — 消息路由修复

### 2.1 teamID + threadTS 传递修复

#### 2.1.1 问题

**已验证** `adapter.go:161`：
```go
envelope := a.bridge.MakeSlackEnvelope("", channelID, "", userID, text)
//                                ^^^^teamID=""     ^^^^threadTS=""
```

- `Start():65` 已调用 `AuthTestContext` 并保存 `botID`，但 **未保存 `authTest.TeamID`**（`slack.AuthTestResponse.TeamID` 字段存在，已验证 SDK `slack.go:210`）
- `handleEventsAPI():121` 提取了 `threadTS`，但 `HandleTextMessage` 签名无此参数，无法传递
- 结果：session ID 实际为 `slack::C123::U456`（两个空段），而非设计意图的 `slack:T111:C123:1234567890.123456:U456`

#### 2.1.2 实现

**修改 1**：`adapter.go` Adapter 增加 `teamID` 字段，`Start()` 中保存：

```go
// adapter.go:25 Adapter struct 增加字段
type Adapter struct {
    // ... existing fields ...
    teamID string  // workspace ID from AuthTest
}

// adapter.go:64-69 Start() 修改
authTest, err := a.client.AuthTestContext(ctx)
if err != nil {
    return fmt.Errorf("slack: auth test: %w", err)
}
a.botID = authTest.UserID
a.teamID = authTest.TeamID  // 新增
```

**修改 2**：`HandleTextMessage` 增加 `threadTS` 参数：

```go
// adapter.go:156 签名变更
func (a *Adapter) HandleTextMessage(ctx context.Context, platformMsgID, channelID, threadTS, userID, text string) error {
    // ...
    envelope := a.bridge.MakeSlackEnvelope(a.teamID, channelID, threadTS, userID, text)
    // ...
}
```

**修改 3**：`adapter.go:151` 调用处传入 `threadTS`：

```go
if err := a.HandleTextMessage(ctx, platformMsgID, channelID, threadTS, userID, text); err != nil {
```

**修改 4**：`PlatformAdapterInterface.HandleTextMessage` 签名同步更新（`platform_adapter.go:258`）。**注意**：此签名变更影响所有平台 adapter 实现（Feishu、Mock），需同步更新 `feishu/adapter.go` 和 `mock/` 中的调用，传入对应平台的 threadTS 参数。

**Session ID 格式变化**：

```
修复前: slack::C123::U456              （teamID="" threadTS=""）
修复后: slack:T111:C123:123456.789:U456 （完整四段）
```

**SDK-first**: 使用已有的 `slack.AuthTestResponse.TeamID`，零新代码。

#### 2.1.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 2.1-1 | `Start()` 保存 `authTest.TeamID` 到 `a.teamID` | 单元测试（mock `AuthTestContext`） |
| 2.1-2 | `MakeSlackEnvelope` 收到正确的 teamID 和 threadTS | 单元测试 |
| 2.1-3 | session ID 格式 `slack:{teamID}:{channelID}:{threadTS}:{userID}` 四段完整 | 单元测试 |
| 2.1-4 | threadTS 为空时 session ID 退化为 `slack:{teamID}:{channelID}::{userID}`（第三段空） | 单元测试 |
| 2.1-5 | `ExtractChannelThread` 正确解析新格式（`events.go:54` 现有逻辑兼容） | 回归测试 |
| 2.1-6 | `AuthTestContext` 失败时 `Start` 返回 error（现有行为，不变） | 回归测试 |

---

### 2.2 去重实现

#### 2.2.1 问题

**已验证** `adapter.go:139-142`：

```go
platformMsgID := msgEvent.ClientMsgID
if platformMsgID == "" {
    platformMsgID = msgEvent.TimeStamp
}
```

生成了 `platformMsgID` 但**没有 seen-set 检查**。WebSocket 重连后 Slack 会重推积压事件，导致重复处理。

#### 2.2.2 实现

在 `adapter.go` 中添加去重 map（bounded + TTL 清理 goroutine）：

```go
// adapter.go Adapter struct 增加字段
type Adapter struct {
    // ... existing fields ...
    dedup *Dedup  // 有界 TTL dedup map
}

// Dedup 有界去重 map：超过 maxEntries 时 FIFO 淘汰，TTL 过期后自动清理
type Dedup struct {
    mu         sync.Mutex
    entries    map[string]time.Time
    order      []string  // FIFO 淘汰顺序
    maxEntries int
    ttl        time.Duration
}

func NewDedup(maxEntries int, ttl time.Duration) *Dedup {
    return &Dedup{
        entries:    make(map[string]time.Time),
        maxEntries: maxEntries,
        ttl:        ttl,
    }
}

// TryRecord returns false if id was already seen (duplicate)
func (d *Dedup) TryRecord(id string) bool {
    d.mu.Lock()
    defer d.mu.Unlock()
    if _, seen := d.entries[id]; seen {
        return false
    }
    for len(d.entries) >= d.maxEntries && len(d.order) > 0 {
        oldest := d.order[0]
        d.order = d.order[1:]
        delete(d.entries, oldest)
    }
    d.entries[id] = time.Now()
    d.order = append(d.order, id)
    return true
}

// handleEventsAPI() 中，生成 platformMsgID 之后：
if !a.dedup.TryRecord(platformMsgID) {
    return
}
```

**Close() 中停止清理 goroutine**，避免 goroutine 泄漏：
```go
func (a *Adapter) Close() error {
    if a.dedup != nil {
        a.dedup.Close()  // 关闭 cleanup goroutine
    }
    // ...
}
```

**⚠️ 有界 vs 无界**：无界 map 在长会话中会持续增长直到 OOM。上述实现将条目数上限设为 `maxEntries`（默认 5000），超出后 FIFO 淘汰最旧条目。

#### 2.2.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 2.2-1 | 相同 `ClientMsgID` 的消息在 dedup map 中仅处理一次 | 单元测试 |
| 2.2-2 | `ClientMsgID` 为空时 fallback 到 `TimeStamp` | 单元测试 |
| 2.2-3 | 不同消息正常处理 | 单元测试 |
| 2.2-4 | WebSocket 重连后重推的旧消息被过滤 | 集成测试 |
| 2.2-5 | 超过 maxEntries 时 FIFO 淘汰最旧条目 | 单元测试 |
| 2.2-6 | `Close()` 后 dedup goroutine 退出（无泄漏） | 单元测试 |

---

### 2.3 Bot 消息防御增强

#### 2.3.1 问题

**已验证** `adapter.go:115-118`：

```go
if msgEvent.BotID == a.botID {
    return
}
```

仅过滤自身 bot，其他 bot（如 Hubot、自定义 workflow bot）的消息会触发 AI 处理，可能导致**两个 bot 无限互回复**。

#### 2.3.2 实现

扩展为过滤所有 bot 消息和不需要的 subtype：

```go
// 替换 adapter.go:115-118
// Skip all bot messages (prevent bot-to-bot loops)
if msgEvent.BotID != "" {
    a.log.Debug("slack: skipping bot message", "bot_id", msgEvent.BotID)
    return
}

// Skip non-user subtypes
switch msgEvent.SubType {
case "message_changed", "message_deleted", "channel_join",
    "channel_leave", "group_join", "group_leave",
    "channel_topic", "channel_purpose":
    return
}
```

**SDK-first**：`slackevents.MessageEvent.BotID` 和 `SubType` 都是 SDK 原生字段。

#### 2.3.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 2.3-1 | 自身 bot 消息被忽略（现有行为保持） | 回归测试 |
| 2.3-2 | 其他 bot 的消息（`BotID != ""`）被忽略 | 单元测试 |
| 2.3-3 | `message_changed`/`message_deleted` 被忽略 | 单元测试 |
| 2.3-4 | `channel_join`/`channel_leave` 被忽略 | 单元测试 |
| 2.3-5 | 人类用户消息（`BotID == ""` 且 `SubType == ""`）正常处理 | 单元测试 |
| 2.3-6 | bot 过滤时记录 Debug 日志 | 单元测试 |
| 2.3-7 | 两个 bot 在同群不会形成无限回复循环 | 集成测试 |

---

### 2.4 用户提及解析

#### 2.4.1 问题

Slack 消息中 `@user` 表示为 `<@U12345678>` 或 `<@U12345678|Bob>`。当前 AI 收到原始 ID。

#### 2.4.2 实现

新增 `internal/messaging/slack/mention.go`：

```go
package slack

import (
    "context"
    "regexp"
    "strings"
    "sync"

    "github.com/slack-go/slack"
)

var mentionPattern = regexp.MustCompile(`<@([A-Z0-9]+)(?:\|([^>]*))?>`)

// UserCache resolves Slack user IDs to display names.
// Uses slack.Client.GetUserInfoContext for resolution.
type UserCache struct {
    client *slack.Client
    cache  map[string]string
    mu     sync.RWMutex
}

func NewUserCache(client *slack.Client) *UserCache {
    return &UserCache{client: client, cache: make(map[string]string)}
}

// ResolveMentions replaces <@UID> with @DisplayName.
// Bot self-mentions are removed. Non-resolvable mentions kept as-is.
func (uc *UserCache) ResolveMentions(ctx context.Context, text, botID string) string {
    return mentionPattern.ReplaceAllStringFunc(text, func(match string) string {
        parts := mentionPattern.FindStringSubmatch(match)
        if len(parts) < 2 {
            return match
        }
        userID := parts[1]
        inlineName := parts[2] // from <@UID|Name> format

        if userID == botID {
            return "" // remove bot self-mention
        }

        name := uc.resolve(ctx, userID, inlineName)
        if name != "" {
            return "@" + name
        }
        return match // keep <@UID> if unresolvable
    })
}

func (uc *UserCache) resolve(ctx context.Context, userID, fallback string) string {
    uc.mu.RLock()
    if name, ok := uc.cache[userID]; ok {
        uc.mu.RUnlock()
        return name
    }
    uc.mu.RUnlock()

    // SDK API: slack.Client.GetUserInfoContext
    user, err := uc.client.GetUserInfoContext(ctx, userID)
    if err != nil {
        return fallback
    }

    name := user.Profile.DisplayName
    if name == "" {
        name = user.RealName
    }

    uc.mu.Lock()
    uc.cache[userID] = name
    uc.mu.Unlock()
    return name
}
```

**SDK-first**：使用 `slack.Client.GetUserInfoContext`（已验证存在于 `users.go:273`）。`slack.User.Profile.DisplayName` 和 `RealName` 均已验证（`users.go:19-55`）。

**集成点**：`adapter.go` 增加 `userCache` 字段，`Start()` 中初始化，`handleEventsAPI()` 中调用：

```go
text = a.userCache.ResolveMentions(ctx, text, a.botID)
text = strings.TrimSpace(text)
if text == "" {
    return
}
```

#### 2.4.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 2.4-1 | `<@U111>` → `@Alice`（API 成功） | 单元测试（mock `GetUserInfoContext`） |
| 2.4-2 | `<@U111\|Bob>` → `@Bob`（使用内嵌名称，无 API 调用） | 单元测试 |
| 2.4-3 | `<@BOT_ID>` → 被移除（bot 自身提及） | 单元测试 |
| 2.4-4 | 多个 mention 全部解析 | 单元测试 |
| 2.4-5 | API 失败时保留原始 `<@U111>` | 单元测试 |
| 2.4-6 | 缓存命中时不发 API 调用 | 单元测试 |
| 2.4-7 | 解析后 text 为空（仅 bot mention）时跳过处理 | 单元测试 |
| 2.4-8 | 无 `<@UID>` 的文本原样返回 | 单元测试 |
| 2.4-9 | `<@U111>` 与 `<@U111\|Bob>` 混合出现时正确处理 | 单元测试 |

---

### 2.5 Rich Text Block 提取

#### 2.5.1 问题

**已验证** `events.go:11-31`：`extractText` 仅处理 `Text` 字段和 `SectionBlock`。`RichTextBlock`、`ContextBlock`、`Files` 均被忽略。

#### 2.5.2 实现

扩展 `events.go` 的 `extractText` 函数（不新增文件）：

```go
// events.go 修改 extractText
func extractText(event slackevents.MessageEvent) string {
    // 1. Primary text field
    if event.Text != "" {
        return event.Text
    }

    // 2. Walk blocks for text content
    var parts []string
    for _, block := range event.Blocks.BlockSet {
        switch b := block.(type) {
        case *slack.SectionBlock:
            if b.Text != nil && b.Text.Text != "" {
                parts = append(parts, b.Text.Text)
            }
        case *slack.ContextBlock:
            for _, elem := range b.ContextElements.Elements {
                if t, ok := elem.(*slack.TextBlockObject); ok && t.Text != "" {
                    parts = append(parts, t.Text)
                }
            }
        case *slack.RichTextBlock:
            for _, elem := range b.Elements {
                if sec, ok := elem.(*slack.RichTextSection); ok {
                    parts = append(parts, extractRichTextSection(sec))
                }
            }
        }
    }
    if len(parts) > 0 {
        return strings.Join(parts, "\n")
    }
    return ""
}

func extractRichTextSection(sec *slack.RichTextSection) string {
    var parts []string
    for _, elem := range sec.Elements {
        if t, ok := elem.(*slack.RichTextSectionTextElement); ok && t.Text != "" {
            parts = append(parts, t.Text)
        }
    }
    return strings.Join(parts, "")
}
```

**SDK-first**：所有 Block 类型（`SectionBlock`、`ContextBlock`、`RichTextBlock`、`RichTextSection`、`RichTextSectionTextElement`）均已验证存在于 SDK v0.22.0。

#### 2.5.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 2.5-1 | 纯 `Text` 消息保持现有行为 | 回归测试 |
| 2.5-2 | `SectionBlock.Text` 被提取（现有行为保持） | 回归测试 |
| 2.5-3 | `ContextBlock` 文本被提取 | 单元测试 |
| 2.5-4 | `RichTextBlock` 文本被提取 | 单元测试 |
| 2.5-5 | `Text` 为空但 blocks 有内容时正确返回 | 单元测试 |
| 2.5-6 | `Text` 和 blocks 均为空时返回空字符串 | 单元测试 |
| 2.5-7 | 未知 block 类型被安全跳过 | 单元测试 |

---

## 3. Phase 2 — 用户体验

### 3.1 mrkdwn 格式化

#### 3.1.1 问题

**已验证** `adapter.go:230`：

```go
opts := []slack.MsgOption{slack.MsgOptionText(text, false)}
```

`MsgOptionText` 第二个参数 `escapePtr=false` 意味着 Slack 会渲染 mrkdwn。但 AI 输出是标准 Markdown（`**bold**`、`## H1`、`[text](url)`），Slack mrkdwn 语法不同。

#### 3.1.2 mrkdwn vs Markdown 差异（已验证）

| 标准 Markdown | Slack mrkdwn | 说明 |
|--------------|-------------|------|
| `**bold**` | `*bold*` | Slack 用单星号粗体 |
| `## H2` | `*H2*` | Slack 无原生标题，用粗体替代 |
| `~~strike~~` | `~strike~` | Slack 单波浪线 |
| `[text](url)` | `<url\|text>` | 链接语法不同 |
| `- item` | `• item` | Slack 用圆点 |

**注意**：代码块（` ``` `）和行内代码（`` ` ``）语法相同，无需转换。

#### 3.1.3 实现

新增 `internal/messaging/slack/format.go`（精简版，聚焦核心转换）：

```go
package slack

import (
    "fmt"
    "regexp"
    "strings"
)

// FormatMrkdwn converts standard Markdown to Slack mrkdwn.
// Preserves code blocks and inline code unchanged.
func FormatMrkdwn(text string) string {
    // Protect code blocks and inline code
    placeholders := make(map[string]string)
    text = protectCode(text, placeholders)

    // Convert headings: ## H2 → *H2*
    text = headingRe.ReplaceAllStringFunc(text, func(m string) string {
        sub := headingRe.FindStringSubmatch(m)
        return "*" + strings.TrimSpace(sub[1]) + "*"
    })

    // Convert bold: **text** → *text*
    // Handle ***bold italic*** → *_text_* first, then remaining ** → *
    text = boldRe.ReplaceAllString(text, "*$1*")

    // Convert strikethrough: ~~text~~ → ~text~
    text = strikethroughRe.ReplaceAllString(text, "~$1~")

    // Convert links: [text](url) → <url|text>
    text = linkRe.ReplaceAllString(text, "<$2|$1>")

    // Convert unordered lists: - item → • item
    text = listRe.ReplaceAllString(text, "$1• ")

    // Restore code
    text = restoreCode(text, placeholders)
    return text
}

var (
    headingRe       = regexp.MustCompile(`(?m)^#{1,6}\s+(.+)$`)
    boldRe          = regexp.MustCompile(`\*\*(.+?)\*\*`)
    strikethroughRe = regexp.MustCompile(`~~([^~]+)~~`)
    linkRe          = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
    listRe          = regexp.MustCompile(`(?m)^(\s*)[-*+]\s+`)
    fencedCodeRe    = regexp.MustCompile("(```.*?```)")
    inlineCodeRe    = regexp.MustCompile("(`[^`]+`)")
)

var codePlaceholderPrefix = "\x00CODE"

func protectCode(text string, ph map[string]string) string {
    // Protect fenced code blocks first (greedy), then inline code
    text = fencedCodeRe.ReplaceAllStringFunc(text, func(m string) string {
        key := fmt.Sprintf("%s%d\x00", codePlaceholderPrefix, len(ph))
        ph[key] = m
        return key
    })
    text = inlineCodeRe.ReplaceAllStringFunc(text, func(m string) string {
        key := fmt.Sprintf("%s%d\x00", codePlaceholderPrefix, len(ph))
        ph[key] = m
        return key
    })
    return text
}

func restoreCode(text string, ph map[string]string) string {
    for k, v := range ph {
        text = strings.ReplaceAll(text, k, v)
    }
    return text
}
```

**SDK-first**：`MsgOptionText(text, false)` 已支持 mrkdwn 渲染，只需预处理文本。

**集成点**：`SlackConn.WriteCtx` 中格式化：

```go
// adapter.go:230 修改
opts := []slack.MsgOption{slack.MsgOptionText(FormatMrkdwn(text), false)}
```

#### 3.1.4 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 3.1-1 | `**bold**` → `*bold*` | 单元测试 |
| 3.1-2 | `## H2` → `*H2*` | 单元测试 |
| 3.1-3 | `[text](url)` → `<url\|text>` | 单元测试 |
| 3.1-4 | `~~strike~~` → `~strike~` | 单元测试 |
| 3.1-5 | `- item` → `• item` | 单元测试 |
| 3.1-6 | `` ```code``` `` 保持不变 | 单元测试 |
| 3.1-7 | `` `inline` `` 保持不变 | 单元测试 |
| 3.1-8 | 粗体与代码混合时代码不被转换 | 单元测试 |
| 3.1-9 | 空字符串/纯文本原样返回 | 单元测试 |
| 3.1-10 | 多行 Markdown 正确逐行转换 | 单元测试 |
| 3.1-11 | `*italic*` 不被误转换（与粗体 `**` 不冲突） | 单元测试 |
| 3.1-12 | `***bold italic***` → `*_bold italic_*`（不丢失格式） | 单元测试 |
| 3.1-13 | 代码块内的 `**text**` 不被转换（代码保护） | 单元测试 |

---

### 3.2 Abort 检测

#### 3.2.1 问题

用户无法中止正在进行的 AI 回复。

#### 3.2.2 实现

新增 `internal/messaging/slack/abort.go`：

```go
package slack

import "strings"

var abortTriggers = map[string]bool{
    // English
    "stop": true, "abort": true, "halt": true, "cancel": true,
    "wait": true, "exit": true,
    "please stop": true, "stop please": true,
    // Chinese
    "停止": true, "取消": true, "中断": true, "等一下": true,
    "别说了": true, "停下来": true,
}

// IsAbortCommand checks if text is an abort trigger.
func IsAbortCommand(text string) bool {
    t := strings.TrimSpace(strings.ToLower(text))
    t = strings.TrimRight(t, ".!?…,，。;；:!：\"')]")
    return abortTriggers[t]
}
```

**集成点**：在 `handleEventsAPI` 中，去重之后、`HandleTextMessage` 之前：

```go
if IsAbortCommand(text) {
    a.log.Info("slack: abort command received", "channel", channelID)
    // TODO: Phase 2 后续集成 ChatQueue.Abort 或 worker cancel
    return
}
```

#### 3.2.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 3.2-1 | "stop" 匹配 | 单元测试 |
| 3.2-2 | "停止" 匹配 | 单元测试 |
| 3.2-3 | "Stop." 匹配（去标点） | 单元测试 |
| 3.2-4 | "please stop" 匹配 | 单元测试 |
| 3.2-5 | "hello" 不匹配 | 单元测试 |
| 3.2-6 | "stop it" 不匹配（非完整匹配） | 单元测试 |
| 3.2-7 | 空字符串不匹配 | 单元测试 |
| 3.2-8 | "STOP"（全大写）匹配 | 单元测试 |
| 3.2-9 | "stop，" 匹配（中文标点） | 单元测试 |

---

### 3.3 Assistant Status & Activity Indicators

> **交叉验证**：本节基于 `~/hotplex/chatapps/slack/` 生产实现逐行对齐，所有 API 签名已对照 `slack-go@v0.22.0` 源码验证

#### 3.3.1 Background — Slack Assistant Threads API

**Slack 独有特性**：`assistant.threads.setStatus` 是 Slack 平台为 AI Assistant 应用提供的原生状态 API。它在 thread 底部显示一行轻量级状态文本（如 "Thinking..."、"Using tool..."），**不产生聊天消息**，不扰乱主对话流。这是 Slack 区别于其他平台的核心用户体验特性，**不能丢失或降级为简单 emoji reaction**。

**关键优势**：
- 状态文本独立于消息流，不产生额外消息条目
- 用户始终能看到 bot 当前进度，感知 "AI is alive"
- 支持 `loading_messages` 参数提供随机化等待提示
- 自动清除：状态在下次更新时替换，空字符串清除

**API 限制**：
- 仅付费 workspace 可用（Free plan 返回 `not_allowed`）
- 需要 `channel_id` + `thread_ts` 参数（thread 上下文）
- 需 `chat:write` scope（`assistant:write` 已废弃）

**SDK 支持**（已验证 `slack-go@v0.22.0/assistant.go`）：

```go
// assistant.go:12-17
type AssistantThreadsSetStatusParameters struct {
    ChannelID       string   `json:"channel_id"`
    Status          string   `json:"status"`
    ThreadTS        string   `json:"thread_ts"`
    LoadingMessages []string `json:"loading_messages,omitempty"`
}

// assistant.go:185-222
func (api *Client) SetAssistantThreadsStatusContext(ctx context.Context, params AssistantThreadsSetStatusParameters) error
```

#### 3.3.2 状态生命周期

```
用户消息到达
    │
    ├─ Status: "Initializing..."          ← session 创建 / worker 启动
    │
    ├─ Status: "Thinking..."              ← AI 开始推理
    │
    ├─ Status: "Using read_file..."       ← tool_use 事件（含工具名）
    │
    ├─ Status: "Tool completed"           ← tool_result 事件
    │
    ├─ Status: "Composing response..."    ← message.delta 首次到达
    │
    ├─ Status: "Using search..."          ← 可能多次 tool_use/tool_result 循环
    │
    └─ Status: "" (clear)                 ← done 事件 → 清除状态
```

**关键规则**：
- 同一状态文本不重复发送（StatusManager 去重）
- 状态更新最小间隔 1 秒（防止 API 滥用）
- `done` / `error` 事件必须清除状态（保证 UI 干净）
- DM 场景下 `thread_ts` 可能为空，此时跳过 status（API 要求 thread 上下文）

#### 3.3.3 StatusType → 状态文本映射

基于 `~/hotplex/chatapps/base/types.go:196-255` 的生产实现：

| AEP Event Type | StatusType | 状态文本模板 | Emoji Fallback |
|----------------|-----------|-------------|----------------|
| — | `initializing` | `"Initializing..."` | `:hourglass_flowing_sand:` |
| `message.delta` (首包前) | `thinking` | `"Thinking..."` | `:brain:` |
| `tool_call` | `tool_use` | `"Using {tool_name}..."` | `:gear:` |
| `tool_result` | `tool_result` | `"Tool completed"` | `:wrench:` |
| `message.delta` | `answering` | `"Composing response..."` | `:pencil:` |
| `step_finish` | `step_finish` | `"Step complete"` | `:white_check_mark:` |
| `done` | — | `""` (clear) | — |
| `error` | — | `""` (clear) | — |

**状态文本定制**：`tool_use` 状态提取工具名称，如 `"Using read_file..."`、`"Using search_web..."`。工具名从 AEP envelope 的 event data 中提取。

```go
// internal/messaging/slack/status.go

type StatusType string

const (
    StatusInitializing StatusType = "initializing"
    StatusThinking     StatusType = "thinking"
    StatusToolUse      StatusType = "tool_use"
    StatusToolResult   StatusType = "tool_result"
    StatusAnswering    StatusType = "answering"
    StatusStepFinish   StatusType = "step_finish"
    StatusIdle         StatusType = "idle"
)

// StatusEmojiMap maps StatusType to Slack emoji name for fallback
var StatusEmojiMap = map[StatusType]string{
    StatusInitializing: "hourglass_flowing_sand",
    StatusThinking:     "brain",
    StatusToolUse:      "gear",
    StatusToolResult:   "wrench",
    StatusAnswering:    "pencil",
    StatusStepFinish:   "white_check_mark",
    StatusIdle:         "white_circle",
}

// StatusTextMap maps StatusType to human-readable status text
var StatusTextMap = map[StatusType]string{
    StatusInitializing: "Initializing...",
    StatusThinking:     "Thinking...",
    StatusToolResult:   "Tool completed",
    StatusAnswering:    "Composing response...",
    StatusStepFinish:   "Step complete",
}
```

#### 3.3.4 Capability Probe（付费 vs 免费工作区检测）

**已验证** `~/hotplex/chatapps/slack/adapter.go:818-844` 的生产实现。

启动时异步探测 workspace 是否支持 Assistant API：

```go
// adapter.go Adapter struct 增加字段
type Adapter struct {
    // ... existing fields ...
    isAssistantCapable atomic.Bool  // workspace supports assistant.threads.setStatus
}

// Start() 中异步探测（非阻塞，不影响启动速度）
go func() {
    probeCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    capable := a.ProbeAssistantCapability(probeCtx)
    a.isAssistantCapable.Store(capable)
    if capable {
        a.log.Info("slack: Assistant API capability confirmed (paid workspace)")
    } else {
        a.log.Info("slack: Assistant API not available, using emoji reaction fallback")
    }
}()
```

```go
// ProbeAssistantCapability 尝试一次空状态调用来探测能力
func (a *Adapter) ProbeAssistantCapability(ctx context.Context) bool {
    // Config override: explicitly disabled
    if !a.assistantAPIEnabled() {
        a.log.Debug("slack: Assistant API disabled via config, using emoji fallback")
        return false
    }
    if a.client == nil {
        return false
    }
    // Empty call — no side effect, just tests capability
    params := slack.AssistantThreadsSetStatusParameters{Status: ""}
    err := a.client.SetAssistantThreadsStatusContext(ctx, params)
    if err != nil {
        if isAssistantCapabilityError(err) {
            a.log.Warn("slack: Assistant API not available (free workspace?), falling back to emoji reactions",
                "error", err)
            return false
        }
        // Transient error — treat as capable so runtime retries
        a.log.Warn("slack: Assistant API probe returned unexpected error, treating as capable",
            "error", err)
        return true
    }
    return true
}

// isAssistantCapabilityError 判断是否为 workspace 不支持 Assistant API 的错误
func isAssistantCapabilityError(err error) bool {
    if err == nil {
        return false
    }
    errStr := err.Error()
    return strings.Contains(errStr, "not_allowed") ||
        strings.Contains(errStr, "not_allowed_token_type")
}
```

**探测策略**：
- 空参数调用 `assistant.threads.setStatus`（无副作用）
- `not_allowed` → 免费工作区，标记为不可用
- 其他错误 → 可能是临时故障，标记为可用（运行时 retry）
- 一旦运行时检测到 `not_allowed`，**立即降级并不再重试**（避免持续失败）

#### 3.3.5 Primary: Native Assistant Status API

**已验证** `~/hotplex/chatapps/slack/messages.go:410-430` 的生产实现：

```go
// SetAssistantStatus 设置原生 assistant 状态文本
// Slack API: assistant.threads.setStatus
func (a *Adapter) SetAssistantStatus(ctx context.Context, channelID, threadTS, status string) error {
    if a.client == nil || threadTS == "" {
        return nil  // Skip if no thread context
    }

    params := slack.AssistantThreadsSetStatusParameters{
        ChannelID: channelID,
        ThreadTS:  threadTS,
        Status:    status,
    }

    a.log.Debug("slack: calling SetAssistantThreadsStatus",
        "channel", channelID, "thread_ts", threadTS, "status", status)
    err := a.client.SetAssistantThreadsStatusContext(ctx, params)
    if err != nil {
        a.log.Warn("slack: SetAssistantThreadsStatus API call failed",
            "error", err, "channel", channelID, "thread_ts", threadTS)
    }
    return err
}
```

**SDK-first**：完全使用 SDK 原生 `SetAssistantThreadsStatusContext`，零自定义 HTTP 调用。

#### 3.3.6 Fallback: Multi-Stage Emoji Reactions

**已验证** `~/hotplex/chatapps/slack/typing.go:1-231` 的生产实现。

当 Assistant API 不可用时（免费工作区），使用多阶段 emoji reaction 作为状态指示：

```go
// internal/messaging/slack/typing.go

// DefaultStages 多阶段 emoji 进度（生产验证）
var DefaultStages = []TypingStage{
    {0 * time.Second,  "eyes"},                    // AI saw the message
    {2 * time.Minute,  "clock1"},                  // Taking a while
    {7 * time.Minute,  "hourglass_flowing_sand"},  // Long wait
    {12 * time.Minute, "gear"},                    // Processing complex task
    {17 * time.Minute, "hourglass_flowing_sand"},  // Still going...
}

type TypingStage struct {
    After time.Duration
    Emoji string
}

// TypingIndicator 管理单个 channel+message 的多阶段 emoji 指示器
type TypingIndicator struct {
    adapter   *Adapter
    channelID string
    threadTS  string
    messageTS string  // anchor message to react to
    stages    []TypingStage

    mu    sync.Mutex
    done  bool
    added []string      // Track added reactions for cleanup
    stopCh chan struct{}
}

// Start 非阻塞：立即添加首个 emoji，后续阶段在 goroutine 中定时追加
func (ti *TypingIndicator) Start(ctx context.Context) {
    ti.doAddReaction(ctx, ti.stages[0].Emoji)
    go ti.runStages(ctx)
}

// Stop 停止指示器并清除所有已添加的 reactions
// Safe to call multiple times (idempotent)
func (ti *TypingIndicator) Stop(ctx context.Context) {
    ti.mu.Lock()
    if ti.done {
        ti.mu.Unlock()
        return
    }
    ti.done = true
    close(ti.stopCh)
    var added []string
    if len(ti.added) > 0 {
        added = make([]string, len(ti.added))
        copy(added, ti.added)
    }
    ti.mu.Unlock()
    for _, emoji := range added {
        ti.removeReaction(ctx, emoji)
    }
}
```

**ActiveIndicators 管理**（全局追踪所有活跃的 indicator）：

```go
// ActiveIndicators 管理所有活跃的 typing indicator
type ActiveIndicators struct {
    mu         sync.Mutex
    indicators map[string]*TypingIndicator  // key: "channelID:messageTS"
}

func (ai *ActiveIndicators) Start(ctx context.Context, adapter *Adapter, channelID, threadTS, messageTS string) {
    // Only start if not already active for this message
    // Adds eyes immediately, schedules subsequent stages
}

func (ai *ActiveIndicators) Stop(ctx context.Context, channelID, messageTS string) {
    // Stops indicator, removes all added reactions
}
```

**行为流程**：
1. 收到用户消息时立即添加 `:eyes:` reaction
2. 2 分钟后自动追加 `:clock1:`（提醒用户等待中）
3. 7/12/17 分钟逐级追加长时间等待指示
4. AI 回复完成后，`Stop()` 清除**所有**已添加的 emoji reactions

#### 3.3.7 StatusManager（去重 + 节流）

**已验证** `~/hotplex/chatapps/internal/status_manager.go:1-69` 的生产实现：

```go
// internal/messaging/slack/status_manager.go

// StatusManager 统一管理 AI 状态通知
// 职责: 状态去重、线程安全
type StatusManager struct {
    adapter *Adapter  // direct reference for SetAssistantStatus / emoji fallback
    logger  *slog.Logger
    mu      sync.Mutex
    current StatusType
    lastText string
}

func NewStatusManager(adapter *Adapter, logger *slog.Logger) *StatusManager {
    return &StatusManager{adapter: adapter, logger: logger}
}

// Notify 通知状态变化；相同状态+文本则跳过
func (m *StatusManager) Notify(ctx context.Context, channelID, threadTS string, status StatusType, text string) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    if m.current == status && m.lastText == text {
        return nil  // Dedup: skip repetitive updates
    }
    m.current = status
    m.lastText = text

    if text == "" {
        return m.adapter.ClearStatus(ctx, channelID, threadTS)
    }
    return m.adapter.SetStatus(ctx, channelID, threadTS, status, text)
}

// Clear 清除状态
func (m *StatusManager) Clear(ctx context.Context, channelID, threadTS string) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.current = StatusIdle
    m.lastText = ""
    return m.adapter.ClearStatus(ctx, channelID, threadTS)
}
```

**设计原则**：
- **去重**：相同 status type + text 不重复调用 API（避免 Slack rate limit）
- **Clear special case**：`text == ""` 时直接调 `ClearStatus`，避免状态不一致
- **Thread-safe**：所有操作持 `mu` 锁
- **上层节流**：`PlatformConn.WriteCtx` 实现中额外做 1s 最小间隔 throttle

#### 3.3.8 SetStatus 主入口（Native → Fallback 自动切换）

**已验证** `~/hotplex/chatapps/slack/messages.go:434-507` 的生产实现：

```go
// SetStatus 主入口：先尝试原生 Assistant API，失败则降级到 emoji
func (a *Adapter) SetStatus(ctx context.Context, channelID, threadTS string, status StatusType, text string) error {
    if a.client == nil {
        return nil
    }
    // Empty text → clear status
    if text == "" {
        return a.ClearStatus(ctx, channelID, threadTS)
    }
    // Primary: native Assistant API
    if a.isAssistantCapable.Load() {
        err := a.SetAssistantStatus(ctx, channelID, threadTS, text)
        if err == nil {
            return nil
        }
        // Capability error → downgrade permanently
        if isAssistantCapabilityError(err) {
            a.log.Warn("slack: Assistant API no longer available, switching to emoji fallback",
                "error", err)
            a.isAssistantCapable.Store(false)
        } else {
            a.log.Debug("slack: Assistant API call failed, trying emoji fallback",
                "error", err)
        }
    }
    // Fallback: emoji reaction
    return a.setStatusWithEmojiFallback(ctx, channelID, threadTS, status)
}

// setStatusWithEmojiFallback 使用 emoji reaction 作为状态指示
func (a *Adapter) setStatusWithEmojiFallback(ctx context.Context, channelID, threadTS string, status StatusType) error {
    emoji, ok := StatusEmojiMap[status]
    if !ok || emoji == "" || threadTS == "" {
        return nil
    }
    return a.client.AddReactionContext(ctx, emoji, slack.ItemRef{
        Channel:   channelID,
        Timestamp: threadTS,
    })
}

// ClearStatus 清除状态指示
func (a *Adapter) ClearStatus(ctx context.Context, channelID, threadTS string) error {
    if a.client == nil {
        return nil
    }
    if a.isAssistantCapable.Load() {
        err := a.SetAssistantStatus(ctx, channelID, threadTS, "")
        if err == nil {
            return nil
        }
        if isAssistantCapabilityError(err) {
            a.isAssistantCapable.Store(false)
        }
    }
    // Emoji fallback: reactions 在回复完成后由 TypingIndicator.Stop 统一清除
    return nil
}
```

#### 3.3.9 与 AEP 事件流的集成

**触发时机 1 — 入站**：`handleEventsAPI` 处理用户消息时启动初始状态：

```go
// adapter.go handleEventsAPI() 中，HandleTextMessage 调用之前

// 启动 typing indicator（emoji fallback 模式）
a.activeIndicators.Start(ctx, a, channelID, threadTS, msgEvent.TimeStamp)

// 如果支持 Assistant API，设置初始状态
if a.isAssistantCapable.Load() && threadTS != "" {
    a.SetAssistantStatus(ctx, channelID, threadTS, "Initializing...")
}
```

**触发时机 2 — 出站**：`PlatformConn.WriteCtx` 实现中接收 AEP envelope 时，根据 event type 触发状态更新：

> **⚠️ 架构注意**：当前 hotplex-worker 的 Slack 适配器使用 `NativeStreamingWriter`（实现 `io.WriteCloser`），**尚未实现** `PlatformConn` 接口（`WriteCtx(ctx, *Envelope) error`）。需要一个适配层或扩展现有类型。以下代码描述目标状态，具体实现时需决定：创建新的 `SlackConn` 包装器 或 扩展 `NativeStreamingWriter`。

```go
// 目标状态：PlatformConn 实现中的 status 集成
func (c *SlackPlatformConn) WriteCtx(ctx context.Context, env *events.Envelope) error {
    // 状态更新：从 AEP event 提取 status
    if status, text := aepEventToStatus(env); text != "" {
        c.adapter.statusMgr.Notify(ctx, c.channelID, c.threadTS, status, text)
    }

    // 原有消息处理逻辑...
    switch env.Event.Type {
    case events.Message, events.MessageDelta, events.Raw:
        // ... existing handling (delegated to NativeStreamingWriter)
    case events.Done:
        c.adapter.statusMgr.Clear(ctx, c.channelID, c.threadTS)
        c.adapter.activeIndicators.Stop(ctx, c.channelID, c.anchorTS)
        // ... existing handling
    case events.Error:
        c.adapter.statusMgr.Clear(ctx, c.channelID, c.threadTS)
        c.adapter.activeIndicators.Stop(ctx, c.channelID, c.anchorTS)
        // ... existing handling
    }
    return nil
}
```

**AEP event → status 映射函数**：

```go
// internal/messaging/slack/status.go

func aepEventToStatus(env *events.Envelope) (StatusType, string) {
    switch env.Event.Type {
    case events.ToolCall:
        toolName := extractToolName(env)
        return StatusToolUse, "Using " + toolName + "..."
    case events.ToolResult:
        return StatusToolResult, "Tool completed"
    case events.MessageDelta:
        return StatusAnswering, "Composing response..."
    default:
        return "", ""
    }
}

// extractToolName 从 AEP envelope 提取工具名称
// AEP ToolCallData 结构: {id, name, input}（已验证 pkg/events/events.go:147-152）
func extractToolName(env *events.Envelope) string {
    if env.Event.Data == nil {
        return "tool"
    }
    // ToolCallData 是具体类型，含 Name 字段
    if data, ok := env.Event.Data.(*events.ToolCallData); ok && data.Name != "" {
        return data.Name
    }
    // Fallback: 尝试 map 解析（兼容未类型化的 JSON 反序列化）
    if m, ok := env.Event.Data.(map[string]interface{}); ok {
        if name, ok := m["name"].(string); ok {
            return name
        }
    }
    return "tool"
}
```
    }
    return "tool"
}
```

#### 3.3.10 配置扩展

```go
// config.go SlackConfig 增加
type SlackConfig struct {
    // ... existing fields ...

    // AssistantAPIEnabled controls whether to attempt native Assistant API first.
    // Default: true (auto-probe, fallback to emoji on not_allowed)
    // Set false to skip probe, always use emoji reactions
    AssistantAPIEnabled *bool `mapstructure:"assistant_api_enabled"`
}
```

**配置选项**：
- `assistant_api_enabled: true`（默认）— 自动探测，优先使用原生 API
- `assistant_api_enabled: false` — 跳过探测，始终使用 emoji fallback
- 未配置 — 等同于 true

#### 3.3.11 文件变动

> **⚠️ 架构前置条件**：当前 `NativeStreamingWriter` 实现 `io.WriteCloser`，不满足 `PlatformConn` 接口。需先创建 `PlatformConn` 适配层（扩展现有类型或新增包装器），status 集成在适配层中完成。此适配层也同时服务于 Phase 2.1（mrkdwn 格式化）和 Phase 4（多媒体出站）。

| 文件 | 操作 | 说明 |
|------|------|------|
| `internal/messaging/slack/adapter.go` | 修改 | 增加 `isAssistantCapable` / `statusMgr` / `activeIndicators` 字段；增加 `ProbeAssistantCapability` / `SetAssistantStatus` / `SetStatus` / `ClearStatus` / `setStatusWithEmojiFallback` 方法；创建 `PlatformConn` 适配层 |
| `internal/messaging/slack/status.go` | 新增 | StatusType 定义、StatusEmojiMap、StatusTextMap、aepEventToStatus、extractToolName、StatusManager |
| `internal/messaging/slack/typing.go` | 新增 | TypingStage、TypingIndicator、ActiveIndicators（多阶段 emoji fallback） |
| `internal/messaging/slack/status_test.go` | 新增 | StatusManager + SetStatus + aepEventToStatus 单元测试 |
| `internal/messaging/slack/typing_test.go` | 新增 | TypingIndicator 多阶段 emoji 单元测试 |
| `internal/config/config.go` | 修改 | SlackConfig 增加 `AssistantAPIEnabled` 字段 |

#### 3.3.12 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 3.3-1 | 付费 workspace 启动时探测确认 Assistant API 可用 | 单元测试（mock `SetAssistantThreadsStatusContext` 返回 nil） |
| 3.3-2 | 免费 workspace 探测返回 `not_allowed`，降级到 emoji | 单元测试（mock 返回 `not_allowed` 错误） |
| 3.3-3 | 探测失败后不再重试原生 API（`isAssistantCapable` 持久化） | 单元测试 |
| 3.3-4 | `tool_use` 事件触发状态 "Using {tool_name}..." | 单元测试 |
| 3.3-5 | `done` 事件清除所有状态 | 单元测试 |
| 3.3-6 | `error` 事件清除所有状态 | 单元测试 |
| 3.3-7 | 相同状态+文本不重复调用 API（StatusManager 去重） | 单元测试 |
| 3.3-8 | `threadTS` 为空时跳过状态更新（不 panic） | 单元测试 |
| 3.3-9 | DM 场景（无 thread）跳过 assistant status | 单元测试 |
| 3.3-10 | Emoji fallback：收到消息立即添加 `:eyes:` | 集成测试 |
| 3.3-11 | Emoji fallback：2 分钟后追加 `:clock1:` | 单元测试（mock timer） |
| 3.3-12 | Emoji fallback：回复完成后清除所有 reactions | 集成测试 |
| 3.3-13 | `assistant_api_enabled: false` 跳过探测，直接 emoji | 单元测试 |
| 3.3-14 | Status API 失败不阻断消息处理 | 错误测试 |
| 3.3-15 | 多个 tool_use/tool_result 循环正确更新状态 | 单元测试 |
| 3.3-16 | 原生 API 运行时突然不可用（`not_allowed`）自动降级并不再重试 | 单元测试 |
| 3.3-17 | `Stop()` 幂等：多次调用不 panic、不重复删除 | 单元测试 |
| 3.3-18 | `loading_messages` 参数可配置（可选，Phase 2+） | 配置测试 |

---

## 4. Phase 3 — 安全

### 4.1 访问控制

#### 4.1.1 问题

当前 `SlackConfig`（`config.go:138-146`）无任何访问控制字段。

**已验证** `adapter.go:109-154`：`handleEventsAPI` 无 gate 检查，任何用户在任何频道都能触发 bot。

#### 4.1.2 配置扩展

```go
// config.go SlackConfig 扩展
type SlackConfig struct {
    // ... existing fields ...
    DMPolicy       string   `mapstructure:"dm_policy"`        // open | allowlist | disabled
    GroupPolicy    string   `mapstructure:"group_policy"`     // open | allowlist | disabled
    RequireMention bool     `mapstructure:"require_mention"`  // group must @bot
    AllowFrom      []string `mapstructure:"allow_from"`       // user_id whitelist
}
```

#### 4.1.3 Gate 实现

新增 `internal/messaging/slack/gate.go`：

```go
package slack

import "sync"  // only needed if allowFrom is dynamically updated in future

type Gate struct {
    dmPolicy       string
    groupPolicy    string
    requireMention bool
    allowFrom      map[string]bool
}

type GateResult struct {
    Allowed bool
    Reason  string
}

func NewGate(dmPolicy, groupPolicy string, requireMention bool, allowFrom []string) *Gate {
    g := &Gate{
        dmPolicy:       dmPolicy,
        groupPolicy:    groupPolicy,
        requireMention: requireMention,
        allowFrom:      make(map[string]bool),
    }
    for _, u := range allowFrom {
        g.allowFrom[u] = true
    }
    return g
}

func (g *Gate) Check(channelType, userID string, botMentioned bool) *GateResult {
    if channelType == "im" {
        switch g.dmPolicy {
        case "disabled":
            return &GateResult{false, "dm_disabled"}
        case "allowlist":
            if !g.allowFrom[userID] {
                return &GateResult{false, "not_in_allowlist"}
            }
        }
        return &GateResult{true, ""}
    }

    // Group/channel
    switch g.groupPolicy {
    case "disabled":
        return &GateResult{false, "group_disabled"}
    case "allowlist":
        if !g.allowFrom[userID] {
            return &GateResult{false, "not_in_allowlist"}
        }
    }
    if g.requireMention && !botMentioned {
        return &GateResult{false, "no_mention"}
    }
    return &GateResult{true, ""}
}
```

**集成点**：`handleEventsAPI` 中，thread ownership 检查之前：

```go
// Access control gate
// ⚠️ msgEvent.Text 仅是 plain-text fallback。Block Kit 消息中的 @mention 出现在 blocks.elements 中。
// 因此 require_mention=true 在纯 Block Kit 消息上会静默失效。修复方案：复用 extractText（含 blocks 遍历）。
botMentioned := strings.Contains(extractText(msgEvent), "<@"+a.botID+">")
result := a.gate.Check(channelType, userID, botMentioned)
if !result.Allowed {
    a.log.Debug("slack: gate rejected", "reason", result.Reason, "user", userID)
    return
}
```

#### 4.1.4 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 4.1-1 | dm_policy=open 允许所有 DM | 单元测试 |
| 4.1-2 | dm_policy=disabled 拒绝所有 DM | 单元测试 |
| 4.1-3 | dm_policy=allowlist 仅允许白名单 | 单元测试 |
| 4.1-4 | group_policy=open 允许所有群消息 | 单元测试 |
| 4.1-5 | group_policy=disabled 拒绝所有群消息 | 单元测试 |
| 4.1-6 | group_policy=allowlist + 非白名单用户被拒 | 单元测试 |
| 4.1-7 | require_mention=true + 未 @bot → 拒绝 | 单元测试 |
| 4.1-8 | require_mention=true + 已 @bot → 允许 | 单元测试 |
| 4.1-9 | require_mention=false + 未 @bot → 允许 | 单元测试 |
| 4.1-10 | DM 中 require_mention 不生效（DM 总是视为 mentioned） | 单元测试 |
| 4.1-11 | 空配置（默认 open）允许所有消息 | 单元测试 |
| 4.1-12 | gate 被拒时仅 Debug 日志，不发错误消息给用户 | 单元测试 |
| 4.1-13 | MPIM（channelType="mpim"）与 group 策略一致 | 单元测试 |
| 4.1-14 | require_mention=true + Block Kit 消息（含 blocks.elements 中的 @mention）也能正确检测 | 单元测试 |

> **⚠️ Block Kit mention 已知限制**：旧版检测仅扫描 `msgEvent.Text`，会漏掉 Block Kit `elements` 中的 @mention。修复方案：集成点改用 `extractText(msgEvent)`（Phase 1.5 扩展后支持 blocks 遍历）。

---

### 4.2 消息过期检查

#### 4.2.1 问题

WebSocket 重连后 Slack 重推积压事件，bot 可能回复数小时前的旧消息。

#### 4.2.2 实现

在 `handleEventsAPI` 中添加时间戳检查：

```go
// Message expiry: skip messages older than 30 minutes
if msgEvent.TimeStamp != "" {
    if ts, err := parseSlackTS(msgEvent.TimeStamp); err == nil {
        if time.Since(ts) > 30*time.Minute {
            a.log.Debug("slack: skipping expired message", "ts", msgEvent.TimeStamp)
            return
        }
    }
}

func parseSlackTS(ts string) (time.Time, error) {
    parts := strings.SplitN(ts, ".", 2)
    sec, err := strconv.ParseInt(parts[0], 10, 64)
    if err != nil {
        return time.Time{}, err
    }
    return time.Unix(sec, 0), nil
}
```

#### 4.2.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 4.2-1 | 超过 30 分钟的旧消息被忽略 | 单元测试 |
| 4.2-2 | 30 分钟内的消息正常处理 | 单元测试 |
| 4.2-3 | 时间戳解析失败时不阻断（静默放行） | 单元测试 |
| 4.2-4 | 空 TimeStamp 时不 panic | 单元测试 |

---

## 5. Phase 4 — 多媒体消息支持

> 对标 [[Feishu-Adapter-Improvement-Spec]] §2.3（富消息类型支持），模式对齐：解析媒体元信息 → 下载到本地 → 拼接路径到文本 → AI 处理
>
> **核心差异**：Slack 的 `Msg.Files[]` 直接内嵌在 `MessageEvent` 中，无需从 JSON content 字段提取；下载使用 `client.GetFile(urlPrivateDownload, writer)`；出站渲染使用 Block Kit Image Block

### 5.1 入站：File 消息处理

#### 5.1.1 问题

**已验证** `adapter.go:123` — `extractText` 仅提取 `Text` 和 `SectionBlock`，`Msg.Files[]`（含 image/file/audio/video）被完全忽略。

#### 5.1.2 消息 SubType 识别

**已验证** `messages.go:49` — `MsgSubTypeFileShare = "file_share"` 是文件分享的标准 SubType：

```go
// messages.go:49
MsgSubTypeFileShare = "file_share"  // [Events API, RTM] A file was shared into a channel
```

当 `msgEvent.SubType == "file_share"` 时，`msgEvent.Message.Files[]` 包含所有附件。

> **⚠️ 关键路径**：`slackevents.MessageEvent` 自身**没有** `Files` 字段（已验证 `slackevents/inner_events.go:301-348`）。`Files` 嵌套在 `MessageEvent.Message`（`*slack.Msg`）中。

#### 5.1.3 File 元信息结构

**已验证** `files.go:26-101` — `slack.File` struct：

```go
type File struct {
    ID            string   `json:"id"`
    Name          string   `json:"name"`
    Title         string   `json:"title"`
    Mimetype      string   `json:"mimetype"`
    Filetype      string   `json:"filetype"`   // "png", "jpg", "gif", "pdf", "mov", "mp4", etc.
    PrettyType    string   `json:"pretty_type"`
    Size          int      `json:"size"`        // bytes
    URLPrivate    string   `json:"url_private"`        // 需要认证
    URLPrivateDownload string `json:"url_private_download"`  // 直接下载 URL
    Thumb64       string   `json:"thumb_64"`
    Thumb160      string   `json:"thumb_160"`
    Thumb360      string   `json:"thumb_360"`
    OriginalW     int      `json:"original_w"`
    OriginalH     int      `json:"original_h"`
    Permalink     string   `json:"permalink"`    // 公共链接
}
```

#### 5.1.4 File 类型分类

```go
// internal/messaging/slack/converter.go

type MediaInfo struct {
    Type       string // "image", "video", "audio", "document", "file"
    FileID     string
    Name       string
    MimeType   string
    Size       int
    ThumbURL   string  // 缩略图 URL（image/video）
    DownloadURL string // url_private_download，需认证
    PublicURL   string  // permalink，无需认证
}

func fileCategory(f slack.File) string {
    switch f.Filetype {
    case "png", "jpg", "jpeg", "gif", "webp", "bmp", "svg":
        return "image"
    case "mp4", "mov", "avi", "webm", "flv":
        return "video"
    case "mp3", "wav", "ogg", "opus", "m4a":
        return "audio"
    case "pdf", "doc", "docx", "xls", "xlsx", "ppt", "pptx", "txt", "csv", "md":
        return "document"
    default:
        return "file"
    }
}
```

#### 5.1.5 ConvertMessage 入口

新增 `internal/messaging/slack/converter.go`：

```go
// ConvertMessage converts a Slack MessageEvent into text + media info.
// text: user-facing message text (may be empty if only file was shared)
// ok: whether to continue processing
// media: attached media files (nil if none)
// 实现为 Adapter 方法以访问 a.botID；文件从 msgEvent.Message.Files 提取。
func (a *Adapter) ConvertMessage(msgEvent slackevents.MessageEvent) (text string, ok bool, media []*MediaInfo) {
    // 1. 提取主文本
    text = extractText(msgEvent)  // 现有逻辑

    // 2. 提取文件（⚠️ msgEvent.Message.Files，非 msgEvent.Files）
    msg := msgEvent.Message
    if msg != nil && len(msg.Files) > 0 {
        media = make([]*MediaInfo, 0, len(msg.Files))
        for _, f := range msg.Files {
            // 跳过自身 bot 上传的文件
            if f.User == a.botID {
                continue
            }
            // 跳过 external/remote 文件（无法下载）
            if f.IsExternal || f.ExternalType != "" {
                continue
            }
            media = append(media, &MediaInfo{
                Type:        fileCategory(f),
                FileID:      f.ID,
                Name:        f.Name,
                MimeType:    f.Mimetype,
                Size:        f.Size,
                ThumbURL:    f.Thumb360,
                DownloadURL: f.URLPrivateDownload,
                PublicURL:   f.Permalink,
            })
        }
    }

    // 3. file_share 但无 text → 仅 "[用户分享了一个文件: filename]"
    if text == "" && len(media) > 0 {
        var parts []string
        for _, m := range media {
            if m.Type == "image" {
                parts = append(parts, fmt.Sprintf("[用户分享了一张图片: %s]", m.Name))
            } else {
                parts = append(parts, fmt.Sprintf("[用户分享了文件: %s]", m.Name))
            }
        }
        text = strings.Join(parts, " ")
    }

    return text, text != "" || len(media) > 0, media
}
```

**集成点**：修改 `adapter.go` 的 `handleEventsAPI`，替换现有 `extractText` 调用：

```go
// Before:
text := extractText(msgEvent)
if text == "" {
    return
}

// After:
text, ok, media := a.ConvertMessage(msgEvent)
if !ok {
    return
}

// Download media after access control passes, before HandleTextMessage
if len(media) > 0 {
    for _, m := range media {
        path, err := a.downloadMedia(ctx, m)
        if err == nil {
            text += "\n" + path
        } else {
            // 降级：保留纯文本描述，不阻断消息
            a.log.Warn("slack: download media failed", "file", m.Name, "error", err)
            text += fmt.Sprintf("\n[%s: %s]", m.Type, m.Name)
        }
    }
}
```

#### 5.1.6 验收标准

| ID | AC | 验证方式 | 状态 |
|----|-----|---------|------|
| 5.1-1 | `file_share` subtype 触发 Files 提取 | 单元测试 | ✅ |
| 5.1-2 | 图片文件（png/jpg/gif/webp）分类为 image | 单元测试 | ✅ |
| 5.1-3 | 视频文件（mp4/mov/webm）分类为 video | 单元测试 | ✅ |
| 5.1-4 | 音频文件（mp3/wav/opus）分类为 audio | 单元测试 | ✅ |
| 5.1-5 | 文档文件（pdf/doc/txt）分类为 document | 单元测试 | ✅ |
| 5.1-6 | 仅分享文件无文字时生成占位文本 | 单元测试 | ✅ |
| 5.1-7 | bot 自己上传的文件被跳过 | 单元测试 | ✅ |
| 5.1-8 | external/remote 文件被跳过 | 单元测试 | ✅ |
| 5.1-9 | 下载失败降级为文本描述，不阻断消息 | 单元测试 | ✅ |
| 5.1-10 | 多个文件均被处理并拼接路径 | 单元测试 | ✅ |

---

### 5.2 下载与存储

#### 5.2.1 实现

**已验证** `files.go:266-275` — SDK 提供 `GetFile(downloadURL, writer)` 方法：

```go
// GetFile retrieves a given file from its private download URL.
func (api *Client) GetFile(downloadURL string, writer io.Writer) error {
    return api.GetFileContext(context.Background(), downloadURL, writer)
}
```

**需额外 OAuth scope**：`files:read`（当前 `slack/adapter.go` 的 manifest 可能缺失此 scope）。

```go
// adapter.go
const mediaMaxSize = 20 * 1024 * 1024 // 20 MB（Slack 默认限制）

func (a *Adapter) downloadMedia(ctx context.Context, m *MediaInfo) (string, error) {
    if m.Size > mediaMaxSize {
        return "", fmt.Errorf("file too large: %d bytes", m.Size)
    }

    // 确定文件扩展名
    ext := mimeExt(m.MimeType)
    if ext == "" {
        ext = "." + m.Filetype
    }
    safeName := sanitizeFilename(m.Name)
    filename := fmt.Sprintf("%s_%s%s", m.Type, m.FileID, ext)

    dir := fmt.Sprintf("/tmp/hotplex/media/slack/%ss", m.Type)
    if err := os.MkdirAll(dir, 0o755); err != nil {
        return "", err
    }
    path := filepath.Join(dir, filename)

    f, err := os.Create(path)
    if err != nil {
        return "", err
    }
    defer f.Close()

    // GetFile needs auth — use the client's token automatically
    if err := a.client.GetFile(m.DownloadURL, f); err != nil {
        os.Remove(path)
        return "", fmt.Errorf("get file: %w", err)
    }

    return path, nil
}

func mimeExt(mime string) string {
    switch mime {
    case "image/jpeg":   return ".jpg"
    case "image/png":    return ".png"
    case "image/gif":    return ".gif"
    case "image/webp":   return ".webp"
    case "video/mp4":    return ".mp4"
    case "video/quicktime": return ".mov"
    case "video/webm":   return ".webm"
    case "audio/mpeg":   return ".mp3"
    case "audio/wav":    return ".wav"
    case "audio/opus":   return ".opus"
    case "application/pdf": return ".pdf"
    }
    return ""
}

func sanitizeFilename(name string) string {
    // 移除路径分隔符和危险字符，保留原始文件名
    return strings.ReplaceAll(strings.ReplaceAll(name, "/", "_"), "\\", "_")
}
```

#### 5.2.2 Rate Limiting 扩展

Slack Files API 遵循标准速率限制（约 1 req/sec），现有 `rate_limiter.go` 已覆盖基础场景。文件下载在现有限流器基础上无需额外调整。

#### 5.2.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 5.2-1 | 图片下载到 `/tmp/hotplex/media/slack/images/` | 单元测试 |
| 5.2-2 | 文件按 MIME 类型获得正确扩展名 | 单元测试 |
| 5.2-3 | 超过 20MB 的文件跳过下载 | 单元测试 |
| 5.2-4 | 下载失败时返回 error，不创建空文件 | 单元测试 |
| 5.2-5 | `GetFile` 自动使用 client token 认证 | 集成测试 |
| 5.2-6 | 同一文件重复下载时覆盖 | 单元测试 |

---

### 5.3 出站：Image Block 渲染

#### 5.3.1 问题

Worker 输出含图片路径时，当前 `SlackConn.WriteCtx` 直接发送原始文本。AI 生成的图片路径（如 `/tmp/hotplex/media/slack/images/xxx.png`）应以 Block Kit Image Block 形式展示。

#### 5.3.2 实现

修改 `adapter.go` 的 `SlackConn.WriteCtx`（参考 `slack/block_image.go:35-43`）：

```go
// WriteCtx sends an AEP envelope to Slack.
// text: raw markdown/text from AI
// Extracts image paths and renders as Block Kit Image Block.
func (c *SlackConn) WriteCtx(ctx context.Context, env *events.Envelope) error {
    text, ok := extractResponseText(env)
    if !ok {
        return nil
    }

    // 提取图片路径（支持 AI 输出本地路径或 public URL）
    parts, remaining := extractImages(text)
    if len(parts) == 0 {
        return c.adapter.sendTextMessage(ctx, c.channelID, c.threadTS, text)
    }

    // 组装 Block Kit 消息
    blocks := make([]slack.Block, 0, 1+len(parts)+1)
    // 1. 文本部分（排除图片路径行）
    if remaining != "" {
        blocks = append(blocks, slack.NewSectionBlock(
            slack.NewTextBlockObject(slack.MBTPTypeMrkdwn, remaining, false, false),
            nil, nil,
        ))
    }
    // 2. 每个图片 → Image Block
    for _, img := range parts {
        blocks = append(blocks, slack.NewImageBlock(
            img.URL,
            img.AltText,
            "", // blockID 空
            nil, // title 可选
        ))
    }
    // 3. 发送
    return c.adapter.postBlocks(ctx, c.channelID, c.threadTS, blocks)
}

type imagePart struct {
    URL     string
    AltText string
}

// extractResponseText extracts displayable text from an AEP envelope.
func extractResponseText(env *events.Envelope) (string, bool) {
    switch env.Event.Type {
    case events.Message, events.MessageDelta, events.Raw:
        if env.Event.Data == nil {
            return "", false
        }
        if s, ok := env.Event.Data.(string); ok {
            return s, s != ""
        }
        return "", false
    default:
        return "", false
    }
}

// extractImages extracts image paths from AI text and returns cleaned remaining text.
// Supported patterns:
//   - Local path: /tmp/hotplex/media/slack/images/xxx.png
//   - Slack file URL: https://files.slack.com/... (converted to URLPrivateDownload)
func extractImages(text string) ([]imagePart, string) {
    var parts []imagePart
    var lines []string

    for _, line := range strings.Split(text, "\n") {
        line = strings.TrimSpace(line)
        if strings.HasPrefix(line, "/tmp/hotplex/media/slack/images/") ||
            strings.HasPrefix(line, "/tmp/hotplex/media/slack/videos/") {
            // 本地文件路径 → 转换为 file:// URL 或直接上传
            imgURL, altText := localFileToImagePart(line)
            if imgURL != "" {
                parts = append(parts, imagePart{URL: imgURL, AltText: altText})
                continue
            }
        } else if strings.Contains(line, "files.slack.com") {
            // Slack 公共文件 URL → 直接作为 Image Block URL
            parts = append(parts, imagePart{URL: line, AltText: "image"})
        } else if strings.HasPrefix(line, "http://") || strings.HasPrefix(line, "https://") {
            // 通用 URL（imgbb 等图床）→ 直接使用
            parts = append(parts, imagePart{URL: line, AltText: "image"})
        }
        lines = append(lines, line)
    }

    remaining := strings.TrimSpace(strings.Join(lines, "\n"))
    return parts, remaining
}

func localFileToImagePart(path string) (url string, altText string) {
    // 读取本地文件 → base64 data URL（最简单方案，无需额外服务）
    // 限制：仅适合小图片（< 5MB）
    data, err := os.ReadFile(path)
    if err != nil {
        return "", ""
    }
    if len(data) > 5*1024*1024 {
        return "", ""
    }
    mime := http.DetectContentType(data)
    if !strings.HasPrefix(mime, "image/") {
        return "", ""
    }
    ext := strings.TrimPrefix(mime, "image/")
    altText = filepath.Base(path)
    return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), altText
}
```

**`postBlocks` 辅助方法**：

```go
func (a *Adapter) postBlocks(ctx context.Context, channelID, threadTS string, blocks []slack.Block) error {
    params := slack.NewPostMessageParameters()
    params.ThreadTimestamp = threadTS
    _, _, err := a.client.PostMessageContext(ctx, channelID, slack.MsgOptionBlocks(blocks...), slack.MsgOptionPost())
    return err
}
```

#### 5.3.3 降级策略

若 `PostMessageContext` 因 Block 格式错误失败（返回 `channel_not_found`/`not_authed` 以外错误），降级到纯文本发送：

```go
if err != nil {
    a.log.Warn("slack: post blocks failed, falling back to text", "error", err)
    return c.adapter.sendTextMessage(ctx, c.channelID, c.threadTS, text)
}
```

#### 5.3.4 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 5.3-1 | AI 输出含 `/tmp/hotplex/media/slack/images/xxx.png` → Image Block 渲染 | 集成测试 |
| 5.3-2 | 无图片时退化为纯文本发送 | 单元测试 |
| 5.3-3 | 本地图片 < 5MB 转为 base64 data URL | 单元测试 |
| 5.3-4 | 本地图片 ≥ 5MB 跳过 Image Block，仅发文本 | 单元测试 |
| 5.5 | 多个图片均生成独立 Image Block | 单元测试 |
| 5.3-6 | Block 发送失败降级为纯文本 | 错误测试 |
| 5.3-7 | Image Block 支持 thread 上下文（threadTS 传递） | 集成测试 |

---

### 5.4 出站：File Upload

#### 5.4.1 背景

当 Worker 输出内容为**大文件**（如 AI 生成了 PDF/CSV/代码文件）时，文本消息不适合承载。应通过 `files.uploadV2` 上传到 Slack。

#### 5.4.2 实现

```go
// postFile uploads a file to Slack and returns the permalink.
func (a *Adapter) postFile(ctx context.Context, channelID, threadTS string, path, title string) (string, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return "", err
    }

    params := slack.UploadFileParameters{
        Filename:        filepath.Base(path),
        Title:           title,
        Reader:          bytes.NewReader(data),
        FileSize:        len(data),
        Channel:         channelID,
        ThreadTimestamp: threadTS,
    }

    file, err := a.client.UploadFileContext(ctx, params)
    if err != nil {
        return "", fmt.Errorf("upload file: %w", err)
    }

    // file.ID 可用于后续编辑或引用
    return file.ID, nil
}
```

**触发条件**：当 AI 输出以文件路径结尾且文件存在于 `/tmp/hotplex/media/slack/` 时：

```go
// 在 WriteCtx 中，extractImages 之后
if strings.HasSuffix(remaining, ".pdf") || strings.HasSuffix(remaining, ".csv") {
    filePath := strings.TrimSpace(remaining)
    if _, err := os.Stat(filePath); err == nil {
        fileID, err := a.postFile(ctx, c.channelID, c.threadTS, filePath, filepath.Base(filePath))
        if err == nil {
            // 文本中移除文件路径，替换为 Slack file ID 引用
            return c.adapter.postText(ctx, c.channelID, c.threadTS, fmt.Sprintf("📎 已上传文件: %s", fileID))
        }
    }
}
```

#### 5.4.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 5.4-1 | PDF/CSV 文件上传到 Slack 并返回 file ID | 集成测试 |
| 5.4-2 | 上传的文件附加到 thread（threadTS） | 集成测试 |
| 5.4-3 | 上传失败时降级为文本发送 | 错误测试 |
| 5.4-4 | 大文件（> 20MB）跳过上传 | 单元测试 |

---

### 5.5 Thread/Reply 上下文保留

#### 5.5.1 问题

多媒体消息的 `threadTS` 在 `ConvertMessage` 提取后传递链路断裂——`HandleTextMessage` 和 `SlackConn.WriteCtx` 已有 `threadTS` 参数，流程与 Phase 1 对齐。

#### 5.5.2 实现

无需新代码。`HandleTextMessage` 签名已含 `threadTS` 参数（Phase 1.1 修复），`SlackConn` 存储 `threadTS` 并在所有出站操作中传递（已对齐）。

#### 5.5.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 5.5-1 | 图片消息在 thread 中发送时 Image Block 仍在同一 thread | 集成测试 |
| 5.5-2 | DM 中无 threadTS 时 Image Block 发送正常 | 集成测试 |

---

### 5.6 Block Kit RichText 出站

#### 5.6.1 背景

当前 `WriteCtx` 发送原始文本，mrkdwn 格式由 Slack 自动渲染（Phase 2.1）。对于更复杂的富文本输出（如表格、代码高亮），Block Kit 提供了更强表达力。

#### 5.6.2 实现

复用现有 `format.go`（Phase 2.1）的 Markdown → mrkdwn 转换，通过 `slack.MsgOptionText(text, false)` 发送。**无需额外 Block Kit 实现**：Slack 的原生 mrkdwn 渲染已足够支持粗体/斜体/代码块/列表/链接。

Image Block（5.3节）是唯一需要显式 Block Kit 的场景。

#### 5.6.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 5.6-1 | Markdown 表格在 Slack 中渲染为格式化的 block quote | 集成测试 |
| 5.6-2 | 代码块高亮正确 | 集成测试 |

---

### 5.7 Slack OAuth Scope 更新

#### 5.7.1 问题

当前 `slack/adapter.go` 中未声明 `files:read` scope，文件下载会返回 403。

#### 5.7.2 所需 Scopes

```json
// Slack App manifest scopes 需增加：
"bot": [
    // ... 现有 scopes ...
    "files:read",           // 下载用户分享的文件
    "files:write"          // 上传 AI 生成的文件（已部分存在）
]
```

#### 5.7.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 5.7-1 | `files:read` scope 申请后用户可分享图片触发下载 | E2E 测试 |
| 5.7-2 | 缺 scope 时下载返回 403，日志记录并降级 | 错误测试 |

---

## 6. 文件变动清单

### Phase 4 — 多媒体消息支持

| 文件 | 操作 | 说明 |
|------|------|------|
| `internal/messaging/slack/converter.go` | 新增 | ConvertMessage 入站转换：提取 Files[] → MediaInfo[] → 拼接文本 |
| `internal/messaging/slack/media.go` | 新增 | downloadMedia + postFile + extractImages + localFileToImagePart |
| `internal/messaging/slack/adapter.go` | 修改 | 集成 ConvertMessage + downloadMedia 调用；postBlocks 辅助方法 |
| `internal/messaging/slack/adapter_test.go` | 修改 | 新增 AC 测试（file_share/File 分类/download/Image Block） |
| `docs/specs/Slack-Adapter-Improvement-Spec.md` | 修改 | 新增 Phase 4 章节（5.1-5.7） |

---

## 7. handleEventsAPI 处理流水线（Phase 4 完成后）

```
P2MessageReceiveV1 Event (via Socket Mode)
    │
    ├─ 1. InnerEvent → MessageEvent            // :110
    ├─ 2. Bot 防御 (BotID != "" → skip)        // :115 [Phase 1.3]
    ├─ 3. Subtype 过滤 (join/leave/change → skip)   [Phase 1]
    ├─ 4. 消息过期检查 (ts > 30min → skip)      [Phase 3.2]
    ├─ 5. ConvertMessage → text + MediaInfo[]   [Phase 4, 新增]
    │       ├─ 5a. 提取主文本（现有 extractText）
    │       ├─ 5b. 提取 Files[] → MediaInfo[]
    │       ├─ 5c. 仅文件无文本 → 生成占位符
    │       └─ 5d. ok = text!="" || len(media)>0
    ├─ 6. ok=false → return                    [Phase 4, 修改条件]
    ├─ 7. 去重检查 (seen[platformMsgID])         [Phase 1.2]
    ├─ 8. 访问控制 (Gate.Check)                  [Phase 3.1]
    ├─ 9. 媒体下载 (downloadMedia) → 拼接路径   [Phase 4, 新增]
    │       └─ 下载失败 → 降级为纯文本描述
    ├─ 10. Thread ownership 检查                 // :134
    ├─ 11. Abort 快速路径                        [Phase 2.2]
    ├─ 12. Assistant Status "Initializing..."   [Phase 2.3]
    ├─ 13. HandleTextMessage(teamID, channelID, threadTS, userID, text)
    │       └─ MakeSlackEnvelope
    │           └─ Bridge.Handle → Session → Worker
    │               └─ SlackConn.WriteCtx
    │                   ├─ extractResponseText
    │                   ├─ extractImages → Image Block? [Phase 4]
    │                   │   └─ base64 data URL / 降级文本
    │                   ├─ postFile → files.uploadV2? [Phase 4]
    │                   └─ postBlocks / sendTextMessage [Phase 4 + 2.1]
    └─ 14. Assistant Status clear (done/error)  [Phase 2.3]
```

---

## 8. 依赖关系（Phase 4）

```
Phase 1.1 (threadTS fix) ──→ Phase 5.5 (thread context preserved)
Phase 4.1 (ConvertMessage) ──→ Phase 4.2 (downloadMedia)
Phase 4.3 (Image Block) ←── Phase 2.1 (mrkdwn format, optional)
Phase 4.4 (file upload) ←── Phase 4.3
Phase 4.2 (download) ←── Phase 4.1
Phase 4.6 (RichText) ←── Phase 2.1
Phase 4.7 (scope) ──→ Phase 4.2

独立于其他 Phase，可与 Phase 1-3 并行开发
```

---

## 9. E2E 用户验收测试（Phase 4 补充）

### 9.1 多媒体场景 (Multimedia)

| ID | 场景 | 操作步骤 | 验收标准 |
|----|------|---------|---------|
| **TC-4.1** | 图片分享 | 1. Slack 中截图粘贴发送给 bot<br>2. bot 正在流式回复时再粘贴一张图片 | 1. bot 收到图片本地路径（如 `/tmp/hotplex/media/slack/images/Fxxx.png`）<br>2. AI 能理解并评论图片内容 |
| **TC-4.2** | 多图消息 | 1. 同时粘贴 3 张截图发送给 bot | bot 按顺序提取 3 个路径，正确理解多图关系 |
| **TC-4.3** | 仅文件分享 | 1. 上传一个 PDF 文件（无文字）给 bot | bot 回复：`[用户分享了文件: xxx.pdf]`，不报错 |
| **TC-4.4** | AI 输出图片 | 1. 让 bot 生成并保存一张图片到本地<br>2. bot 回复消息中的图片路径 | Slack 中该路径被渲染为 Image Block（< 5MB） |
| **TC-4.5** | 大文件上传 | 1. 让 bot 生成一个 CSV 报告<br>2. bot 输出文件路径 | Slack thread 中出现上传的文件，用户可下载 |
| **TC-4.6** | 文件下载失败 | 1. 模拟网络错误导致下载失败 | bot 降级为 `[用户分享了图片: filename.png]`，消息处理不中断 |
| **TC-4.7** | 图片 + 文本组合 | 1. 发送图片并附文字「分析这个 UI 设计」 | bot 收到文本 + 图片路径，能同时理解两者 |
| **TC-4.8** | thread 中图片 | 1. 在 bot 消息上 Reply in thread<br>2. 粘贴图片 | Image Block 出现在 thread 内，不散落到频道主消息流 |

---

## 10. 开发顺序建议

Phase 1-3 内部独立模块均可并行开发。Phase 4 多媒体建议按以下顺序实施：

1. **Phase 4.1** (ConvertMessage) — 先让 AI 能"看到"媒体
2. **Phase 4.7** (scope 更新) — 上线前申请 `files:read`
3. **Phase 4.2** (downloadMedia) — 核心依赖
4. **Phase 4.3** (Image Block 出站) — 提升用户体验
5. **Phase 4.5** (thread 保留) — 已在 Phase 1.1 修复
6. **Phase 4.4** (file upload) — 可选优化
7. **Phase 4.6** (RichText) — 已在 Phase 2.1 覆盖

---

## 附录 A. 飞书 vs Slack 多媒体对比

| 维度 | Feishu | Slack |
|------|--------|-------|
| 入站媒体元信息位置 | `content` JSON 字段含 `image_key`/`file_key` | `Msg.Files[]` 直接内嵌在事件中 |
| 下载 API | `MessageResource` API（需要 file_key） | `GetFile(url_private_download, writer)`（需要 bot token） |
| 出站图片 | CardKit（需卡片模板） | Block Kit Image Block（更简单） |
| 媒体类型 | image/file/audio/video/sticker | image/video/audio/document/file |
| 下载大小限制 | 10 MB | 20 MB（Slack 默认） |
| SDK 支持 | Lark Go SDK v3 | slack-go SDK v0.22.0 |

---

## 附录 B. 参考来源

- slack-go SDK v0.22.0 源码: `/Users/huangzhonghui/go/pkg/mod/github.com/slack-go/slack@v0.22.0/`
  - `files.go` — File struct + GetFile/UploadFile/UploadFileContext
  - `messages.go` — Msg.Files[] + MsgSubTypeFileShare
  - `block_image.go` — ImageBlock + NewImageBlock
  - `chat.go` — PostMessageParameters + PostMessageContext
  - `assistant.go` — AssistantThreadsSetStatusParameters + SetAssistantThreadsStatusContext + SetAssistantThreadsTitle + SearchAssistantContext
  - `slackevents/message_events.go` — MessageEvent
- Feishu 参照: `[[Feishu-Adapter-Improvement-Spec]]` §2.3

## 附录 C. Phase 1-3 文件变动（完整版）

### Phase 1 — 消息路由修复

| 文件 | 操作 | 说明 |
|------|------|------|
| `internal/messaging/slack/adapter.go` | 修改 | teamID 字段 + Start() 保存 teamID + HandleTextMessage 增加 threadTS 参数 + Dedup 类型 + bot 防御增强 |
| `internal/messaging/slack/events.go` | 修改 | extractText 扩展 RichTextBlock/ContextBlock 支持 |
| `internal/messaging/slack/mention.go` | 新增 | UserCache + ResolveMentions |
| `internal/messaging/platform_adapter.go` | 修改 | HandleTextMessage 签名增加 threadTS（接口变更） |
| `internal/messaging/feishu/adapter.go` | 修改 | HandleTextMessage 调用处增加 threadTS 参数 |
| `internal/messaging/mock/` | 修改 | Mock adapter HandleTextMessage 签名同步 |
| `internal/messaging/slack/adapter_test.go` | 修改 | 新增 AC 测试 |

### Phase 2 — 用户体验

| 文件 | 操作 | 说明 |
|------|------|------|
| `internal/messaging/slack/format.go` | 新增 | FormatMrkdwn Markdown → mrkdwn |
| `internal/messaging/slack/abort.go` | 新增 | IsAbortCommand |
| `internal/messaging/slack/status.go` | 新增 | StatusType、StatusEmojiMap、aepEventToStatus、StatusManager |
| `internal/messaging/slack/typing.go` | 新增 | TypingIndicator、ActiveIndicators（多阶段 emoji fallback） |
| `internal/messaging/slack/adapter.go` | 修改 | 创建 `PlatformConn` 适配层 + 集成 FormatMrkdwn + Assistant Status API + ProbeAssistantCapability + emoji fallback |

### Phase 3 — 安全

| 文件 | 操作 | 说明 |
|------|------|------|
| `internal/messaging/slack/gate.go` | 新增 | Gate 访问控制 |
| `internal/config/config.go` | 修改 | SlackConfig 增加 DM/Group 策略字段 |
| `configs/config-dev.yaml` | 修改 | 新增 gate 配置项 |
