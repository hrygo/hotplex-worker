---
type: spec
tags:
  - project/HotPlex
  - messaging/feishu
  - platform-adapter
date: 2026-04-19
status: in-progress
progress: 50
priority: high
estimated_hours: 40
last_updated: 2026-04-21
---

# Feishu Adapter 改进规格书

> 版本: v1.1
> 日期: 2026-04-19
> 状态: In Progress
> ⚠️ **进度修正**（2026-04-21）：本文档 header 曾标记 `progress: 70`，实际 `internal/messaging/feishu/adapter.go` 已包含约 976 行实现代码，进度以估算值 `50` 记录，具体 Phase 进度以实现状态为准。
> 交叉复核: 已对齐 `internal/messaging/feishu/adapter.go`、`internal/messaging/bridge.go`、`internal/config/config.go` 源码，已对照 OpenClaw Lark 官方插件 (`@larksuite/openclaw-lark@2026.4.1`) 源码验证所有 API 调用
> SDK 版本: `github.com/larksuite/oapi-sdk-go/v3@v3.5.3`

---

## 1. 概述

### 1.1 目标

基于 OpenClaw Lark 官方插件的架构实践，对当前 Feishu adapter 进行系统性改进，分三个阶段按优先级递进：

| 阶段 | 主题 | 优先级 | 目标 |
|------|------|--------|------|
| Phase 1 | DM/群消息基础处理 | P0 | 消息路由正确——区分 DM/群聊、线程回复、多消息类型 |
| Phase 2 | 用户体验 | P1 | 流式卡片、abort、typing indicator、reply-to |
| Phase 3 | 安全 | P2 | 访问控制、限流、去重增强、消息过期 |

### 1.2 现状分析

| 维度 | 当前状态 | OpenClaw 参照 | 差距 |
|------|---------|--------------|------|
| 源码规模 | 3 文件 / ~310 行 | ~80+ 文件 / ~8000+ 行 | 25x |
| 消息类型 | 仅 `text` | 24 种 converter | 严重 |
| 回复方式 | 纯文本 `im.message.create` | CardKit 流式 + IM patch + 静态 | 严重 |
| 访问控制 | 无 | DM/Group 策略 + allowlist + @mention | 严重 |
| 线程回复 | threadTS 始终为空 | root_id + parent_id + replyInThread | 高 |
| @提及 | 不处理 | @_user_N 占位符解析 | 高 |
| Abort | 无 | 65 语言触发词 + AbortController | 高 |
| 限流 | 无 | CardKit 100ms / IM patch 1500ms | 高 |
| Chat 队列 | 无 | per-chat 串行执行 | 高 |

### 1.3 相关文档

- 高阶设计: [[Worker-Gateway-Design]] messaging 平台层
- 协议规范: [[AEP-v1-Protocol]] Envelope 结构
- 安全设计: [[Security-Authentication]] 平台访问控制
- 对标参考: `@larksuite/openclaw-lark` 源码 (`/Users/huangzhonghui/tmp/openclaw-lark`)

---

## 2. Phase 1 — DM/群消息基础处理

### 2.1 Thread/Reply 支持

#### 2.1.1 问题

`adapter.go:157` 的 `MakeFeishuEnvelope` 始终传入空 `threadTS`；出站消息不支持回复引用。

#### 2.1.2 入站：提取线程信息

**已验证数据源** — `service/im/v1/model.go` 的 `EventMessage` struct：

```go
type EventMessage struct {
    MessageId   *string          // line ~34
    RootId      *string          // line ~36 话题根消息 ID
    ParentId    *string          // line ~38 父消息 ID (回复)
    ChatId      *string          // line ~44 群组 ID
    ThreadId    *string          // line ~46 话题 ID
    ChatType    *string          // line ~48 "p2p" | "group" | "topic_group"
    MessageType *string          // line ~50
    Content     *string          // line ~52 JSON 内容
    Mentions    []*MentionEvent  // line ~54
}
```

**实现**：修改 `adapter.go` 的 `handleMessage`

```go
func (a *Adapter) handleMessage(ctx context.Context, event *larkim.P2MessageReceiveV1) error {
    // ... existing nil checks ...

    msg := event.Event.Message
    chatType := ptrStr(msg.ChatType)    // "p2p" | "group" | "topic_group"
    chatID := ptrStr(msg.ChatId)
    rootID := ptrStr(msg.RootId)        // 话题根消息
    parentID := ptrStr(msg.ParentId)    // 回复的父消息

    // thread key: 优先 root_id，其次 thread_id
    threadKey := rootID
    if threadKey == "" {
        threadKey = ptrStr(msg.ThreadId)
    }

    envelope := a.bridge.MakeFeishuEnvelope(chatID, threadKey, userID, text)
    // 将 chatType、rootID、parentID、messageID 注入 envelope metadata
}
```

**session ID 格式**（`bridge.go:117` 现有格式兼容）：

```
feishu:{chat_id}:{root_id}:{user_id}
```

当 `root_id` 为空时，session ID 退化为 `feishu:{chat_id}::{user_id}`，与当前行为兼容。

#### 2.1.3 出站：Reply API

**已验证 API** — `service/im/v1/resource.go:1468`：

```go
func (m *message) Reply(ctx context.Context, req *ReplyMessageReq, ...) (*ReplyMessageResp, error)
// POST /open-apis/im/v1/messages/:message_id/reply
```

**已验证请求体** — `model.go` 的 `ReplyMessageReqBody`：

```go
type ReplyMessageReqBody struct {
    Content       *string  // JSON 消息内容
    MsgType       *string  // 消息类型
    ReplyInThread *bool    // 是否以话题形式回复
}
```

**实现**：`FeishuConn` 增加 `replyToMsgID` 字段

```go
type FeishuConn struct {
    adapter      *Adapter
    chatID       string
    replyToMsgID string  // 新增：回复目标消息 ID
}

func (c *FeishuConn) WriteCtx(ctx context.Context, env *events.Envelope) error {
    // ... extract text ...

    if c.replyToMsgID != "" {
        return c.adapter.replyMessage(ctx, c.replyToMsgID, text, false)
    }
    return c.adapter.sendTextMessage(ctx, c.chatID, text)
}
```

新增 `replyMessage` 方法：

```go
func (a *Adapter) replyMessage(ctx context.Context, messageID, content string, replyInThread bool) error {
    body := larkim.NewReplyMessageReqBodyBuilder().
        MsgType(larkim.MsgTypeText).
        Content(content).
        ReplyInThread(replyInThread).
        Build()
    req := larkim.NewReplyMessageReqBuilder().
        MessageId(messageID).
        Body(body).
        Build()
    resp, err := a.larkClient.Im.V1.Message.Reply(ctx, req)
    if err != nil {
        return fmt.Errorf("feishu: reply message: %w", err)
    }
    if !resp.Success() {
        return fmt.Errorf("feishu: reply message failed: code=%d msg=%s", resp.Code, resp.Msg)
    }
    return nil
}
```

#### 2.1.4 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 2.1-1 | DM 消息（chat_type=p2p）正确路由，session ID 格式 `feishu:{chat_id}::{user_id}` | 单元测试 |
| 2.1-2 | 群消息中的话题消息提取 root_id 作为 threadKey | 单元测试 |
| 2.1-3 | 回复消息提取 parent_id，出站使用 Reply API | 单元测试 |
| 2.1-4 | bridge.MakeFeishuEnvelope 正确传递 threadKey | 单元测试 |
| 2.1-5 | FeishuConn 在有 replyToMsgID 时使用 Reply API 而非 Create | 集成测试 |

---

### 2.2 @提及解析

#### 2.2.1 问题

飞书消息中 `@user` 表示为 `@_user_1` 占位符 + `Mentions` 数组。当前不处理，导致 AI 收到原始占位符。

**已验证数据源** — `MentionEvent` struct (`model.go`)：

```go
type MentionEvent struct {
    Key  *string  // "@_user_1" 占位符
    Id   *UserId  // 用户 ID (含 OpenId, UserId, UnionId)
    Name *string  // 显示名称
}
```

**OpenClaw 实现** — `src/messaging/converters/content-converter-helpers.ts:68-81`：
- 替换 `@_user_N` → `@DisplayName`
- bot 自身 @mention 被移除（不是替换）
- `@_all` 特殊处理

#### 2.2.2 实现

新增 `internal/messaging/feishu/mention.go`：

```go
package feishu

import (
    "strings"
    larkim "github.com/larksuite/oapi-sdk-go/v3/service/im/v1"
)

// ResolveMentions replaces @_user_N placeholders with @DisplayName
// and strips bot self-mentions.
func ResolveMentions(text string, mentions []*larkim.MentionEvent, botOpenID string) string {
    if len(mentions) == 0 {
        return text
    }
    for _, m := range mentions {
        if m.Key == nil || m.Id == nil {
            continue
        }
        key := *m.Key
        openID := ptrStr(m.Id.OpenId)
        if openID == botOpenID {
            // 移除 bot 自身 @mention
            text = strings.ReplaceAll(text, key+" ", "")
            text = strings.ReplaceAll(text, key, "")
        } else {
            name := ptrStr(m.Name)
            if name != "" {
                text = strings.ReplaceAll(text, key, "@"+name)
            }
        }
    }
    return strings.TrimSpace(text)
}
```

**集成点**：在 `handleMessage` 的 `extractTextFromContent` 之后调用：

```go
mentions := event.Event.Message.Mentions
text := ResolveMentions(rawText, mentions, a.botOpenID)
```

#### 2.2.3 前置条件

需要获取 bot 自身 open_id。在 `Start` 中获取：

```go
// 获取 bot identity (类似 Slack adapter 的 AuthTest)
// 方法: 调用 bot info API 或从第一条 P2 事件的 app_id 中提取
```

#### 2.2.4 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 2.2-1 | `@_user_1` 被替换为 `@Alice` | 单元测试 |
| 2.2-2 | bot 自身 @mention 被移除 | 单元测试 |
| 2.2-3 | 多个 mention 全部被解析 | 单元测试 |
| 2.2-4 | 无 mentions 时原样返回 | 单元测试 |
| 2.2-5 | `@_all` 保留原样（不做替换） | 单元测试 |

---

### 2.3 富消息类型支持

#### 2.3.1 问题

`adapter.go:114` 只处理 `msg_type == "text"`，post/image/file 等类型直接丢弃。

#### 2.3.2 支持的消息类型

| 类型 | 说明 | 优先级 | 转换策略 |
|------|------|--------|---------|
| `text` | 纯文本 | P0（已支持） | 现有 `extractTextFromContent` |
| `post` | 富文本 | P0 | 解析 JSON → markdown |
| `image` | 图片 | P0（✅已实现） | 下载到 `/tmp/hotplex/media/images/{key}.jpg`，拼接路径 |
| `file` | 文件 | P0（✅已实现） | 下载到 `/tmp/hotplex/media/files/{key}_{name}`，拼接路径 |
| `audio` | 语音 | P0（✅已实现） | 下载到 `/tmp/hotplex/media/audios/{key}.opus`，自动转录为文本（STT 引擎） |
| `video` | 视频 | P0（✅已实现） | 下载到 `/tmp/hotplex/media/videos/{key}.mp4`，拼接路径 |
| `sticker` | 表情 | P0（✅已实现） | 下载到 `/tmp/hotplex/media/stickers/{key}.gif`，拼接路径 |
| `interactive` | 交互式卡片 | P2 | 提取文本内容 |

#### 2.3.3 实现

新增 `internal/messaging/feishu/converter.go`：

```go
package feishu

import "encoding/json"

// MediaInfo carries metadata about a non-text media attachment.
type MediaInfo struct {
    Type      string // "image", "file", "audio", "video", "sticker"
    Key       string // image_key, file_key, etc.
    Name      string // Original filename (file type only).
    MessageID string // Message ID (for downloading user-sent media via MessageResource API).
}

// ConvertMessage converts Feishu raw content to AI-friendly text.
// Returns ("", false, nil) for unsupported types that should be silently ignored.
func ConvertMessage(msgType, rawContent string, mentions []*larkim.MentionEvent, botOpenID, messageID string) (string, bool, *MediaInfo) {
    switch msgType {
    case "text":
        text := extractTextFromContent(rawContent)
        return ResolveMentions(text, mentions, botOpenID), true, nil
    case "post":
        return convertPost(rawContent, mentions, botOpenID), true, nil
    case "image":
        return convertImage(rawContent, messageID)
    case "file":
        return convertFile(rawContent, messageID)
    case "audio":
        return convertAudio(rawContent, messageID)
    case "video":
        return convertVideo(rawContent, messageID)
    case "sticker":
        return convertSticker(rawContent, messageID)
    default:
        return "", false, nil
    }
}

// convertPost 解析飞书富文本为 markdown。
// Feishu post content 格式:
// {"title":"...", "content":[[{"tag":"text","text":"hello"},{"tag":"at","user_id":"ou_xxx"}]]}
func convertPost(rawContent string, mentions []*larkim.MentionEvent, botOpenID string) string {
    var post struct {
        Title   string           `json:"title"`
        Content [][]postElement  `json:"content"`
    }
    if err := json.Unmarshal([]byte(rawContent), &post); err != nil {
        return ""
    }
    // ... 遍历 content 数组，按 tag 类型转换为 markdown ...
}

type postElement struct {
    Tag      string `json:"tag"`
    Text     string `json:"text"`
    Href     string `json:"href"`
    UserID   string `json:"user_id"`
    ImageKey string `json:"image_key"`
}

func convertImage(rawContent string) string {
    var img struct {
        ImageKey string `json:"image_key"`
    }
    if err := json.Unmarshal([]byte(rawContent), &img); err != nil {
        return "[图片]"
    }
    return "[图片: " + img.ImageKey + "]"
}

func convertFile(rawContent string) string {
    var f struct {
        FileName string `json:"file_name"`
        FileKey  string `json:"file_key"`
    }
    if err := json.Unmarshal([]byte(rawContent), &f); err != nil {
        return "[文件]"
    }
    return "[文件: " + f.FileName + "]"
}
```

**集成点**：修改 `handleMessage`，替换硬编码的 text-only 检查：

```go
// Before:
if msg.MessageType == nil || *msg.MessageType != "text" {
    return nil
}
text := extractTextFromContent(ptrStr(msg.Content))

// After:
msgType := ptrStr(msg.MessageType)
text, ok, media := ConvertMessage(msgType, ptrStr(msg.Content), msg.Mentions, a.botOpenID, messageID)
if !ok || text == "" {
    return nil
}

// Download media and append path.
if media != nil {
    path, err := a.downloadMedia(ctx, media)
    if err == nil && path != "" {
        text = text + ": " + path
    }
}
```

#### 2.3.4 验收标准

| ID | AC | 验证方式 | 状态 |
|----|-----|---------|------|
| 2.3-1 | text 类型保持现有行为 | 回归测试 | ✅ |
| 2.3-2 | post 类型正确转换为 markdown | 单元测试 | ✅ |
| 2.3-3 | image 类型下载到本地并拼接路径 | 单元测试 | ✅ 已实现 |
| 2.3-4 | file 类型下载到本地并拼接路径 | 单元测试 | ✅ 已实现 |
| 2.3-5 | audio 类型下载到本地并拼接路径 | 单元测试 | ✅ 已实现 |
| 2.3-6 | video 类型下载到本地并拼接路径 | 单元测试 | ✅ 已实现 |
| 2.3-7 | sticker 类型下载到本地并拼接路径 | 单元测试 | ✅ 已实现 |
| 2.3-8 | 解析失败时降级为文本占位符（不 panic） | 单元测试 | ✅ |
| 2.3-9 | post 中的 @mention 被正确解析 | 单元测试 | ✅ |
| 2.3-10 | 下载失败时保留纯文本（降级不阻断消息） | 单元测试 | ✅ 已实现 |
| 2.3-11 | 文件超 10MB 跳过下载，保留纯文本 | 单元测试 | ✅ 已实现 |
| 2.3-12 | 不支持的类型静默忽略（不报错） | 单元测试 | ✅ |

#### 2.3.5 Speech-to-Text (STT) 语音转录

音频消息（`msg_type == "audio"`）在下载后自动经过 STT 引擎转录为文本，然后注入到 Worker 输入中。用户可以在飞书中发送语音消息与 AI 对话。

**STT 提供者**:

| 提供者 | 配置值 | 说明 | RequiresDisk |
|--------|--------|------|-------------|
| 飞书云端 | `stt_provider: "feishu"` | 调用飞书 `speech_to_text` API | No |
| 本地引擎 | `stt_provider: "local"` | SenseVoice-Small ONNX FP32 | Yes |
| 混合模式 | `stt_provider: "feishu+local"` | 云端优先，失败降级到本地 | Yes |
| 禁用 | `stt_provider: ""` | 不转录，音频文件路径直接传递 | N/A |

**本地 STT 引擎**:

- **模型**: SenseVoice-Small (`iic/SenseVoiceSmall`)，~900MB ONNX
- **推理**: `funasr-onnx` ONNX Runtime，FP32 非量化
- **性能**: ~0.35s/file，CER ~2%（中文）
- **语言**: 中文、英文、日语、韩语、粤语（自动检测）
- **持久模式** (`stt_local_mode: "persistent"`): 长驻子进程 `stt_server.py`，模型常驻内存，避免冷启动
- **ONNX 补丁**: `fix_onnx_model.py` 自动修复 ModelScope 预导出模型的 `Less` 节点类型不匹配

**配置示例**:

```yaml
messaging:
  feishu:
    stt_provider: "local"
    stt_local_cmd: "python3 scripts/stt_server.py --model iic/SenseVoiceSmall"
    stt_local_mode: "persistent"
    stt_local_idle_ttl: "10m"
```

**实现文件**: `internal/messaging/feishu/stt.go`（Transcriber 接口 + 4 种实现）

---

### 2.4 Chat 队列序列化

#### 2.4.1 问题

同一 chat 的消息无串行保证。并发消息可能导致回复乱序或重复创建 session。

**OpenClaw 实现** — `src/channel/chat-queue.ts`：
- 使用 `Map<string, Promise<void>>` 做链式串行
- 队列 key: `{accountId}:{chatId}[:thread:{threadId}]`
- 活跃调度器注册表，支持 abort fast-path

#### 2.4.2 实现

新增 `internal/messaging/feishu/chat_queue.go`：

```go
package feishu

import (
    "context"
    "sync"
)

// ChatQueue serializes per-chat message processing.
// Different chats process in parallel.
type ChatQueue struct {
    mu     sync.Mutex
    queues map[string]*chatWorker
}

type chatWorker struct {
    mu      sync.Mutex
    pending chan func()
    cancel  context.CancelFunc  // abort fast-path
    done    chan struct{}
}

func NewChatQueue() *ChatQueue {
    return &ChatQueue{queues: make(map[string]*chatWorker)}
}

// Enqueue adds a task to the per-chat serialized queue.
func (q *ChatQueue) Enqueue(chatID string, task func(context.Context) error) error {
    // 获取或创建 worker，串行执行 task
    // ...
}

// Abort cancels the currently active task for the given chat.
func (q *ChatQueue) Abort(chatID string) {
    // 调用 active worker 的 cancel()
}
```

#### 2.4.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 2.4-1 | 同一 chatID 的消息串行处理 | 并发测试 |
| 2.4-2 | 不同 chatID 的消息并行处理 | 并发测试 |
| 2.4-3 | Abort 能取消正在执行的任务 | 单元测试 |
| 2.4-4 | worker 空闲后自动清理 | 泄漏测试 |

---

### 2.5 Bot 自身消息防御

#### 2.5.1 问题

虽然 Feishu `im.message.receive_v1` 事件理论上仅在用户发送消息时触发，但需防御性检查 sender_type。

**OpenClaw 做法** — `src/messaging/inbound/parse.ts:74`：标记 `isBot` 但不直接过滤。OpenClaw 依赖 gate 策略控制，而非硬编码过滤。

#### 2.5.2 实现

在 `handleMessage` 中添加防御性检查：

```go
// 防御性检查：忽略应用消息
if event.Event.Sender != nil {
    senderType := ptrStr(event.Event.Sender.SenderType)
    if senderType == "app" {
        return nil
    }
}
```

#### 2.5.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 2.5-1 | sender_type == "app" 的消息被忽略 | 单元测试 |
| 2.5-2 | sender_type == "user" 的消息正常处理 | 单元测试 |
| 2.5-3 | sender 为 nil 的消息正常处理（防御性不阻断） | 单元测试 |

---

## 3. Phase 2 — 用户体验

### 3.1 流式卡片回复

#### 3.1.1 问题

`FeishuConn.WriteCtx`（`adapter.go:210-221`）每收到一个 AEP envelope 就调 `sendTextMessage` 发一条新消息，导致消息洪水。

#### 3.1.2 CardKit Go SDK API 链路

**已全部验证存在于 SDK v3.5.3**：

| 步骤 | API | SDK 路径 | HTTP |
|------|-----|---------|------|
| 创建卡片实体 | `card.create` | `client.Cardkit.V1.Card.Create` | POST `/open-apis/cardkit/v1/cards` |
| 发送卡片消息 | `im.message.create` | `client.Im.V1.Message.Create` | POST `/open-apis/im/v1/messages` |
| 流式更新内容 | `cardElement.content` | `client.Cardkit.V1.CardElement.Content` | PUT `/open-apis/cardkit/v1/cards/:card_id/elements/:element_id/content` |
| 关闭流式模式 | `card.settings` | `client.Cardkit.V1.Card.Settings` | — |
| 更新卡片 | `card.update` | `client.Cardkit.V1.Card.Update` | — |
| IM patch 降级 | `im.message.patch` | `client.Im.V1.Message.Patch` | PATCH `/open-apis/im/v1/messages/:message_id` |

**已验证请求体结构** — `service/cardkit/v1/model.go`：

```go
// card.create
type CreateCardReqBody struct {
    Type *string  // "card_json"
    Data *string  // JSON 卡片模板
}

// cardElement.content
type ContentCardElementReqBody struct {
    Content  *string  // 更新后的 markdown 文本
    Sequence *int     // 递增序号
    Uuid     *string  // 幂等 ID
}

// card.settings (流式开关)
type SettingsCardReqBody struct {
    Settings  *string  // JSON: {"streaming_mode": true/false}
    Sequence  *int     // 递增序号
    Uuid      *string
}
```

#### 3.1.3 状态机

来自 OpenClaw `src/card/reply-dispatcher-types.ts`，已验证：

```
idle → creating → streaming → completed
                ↘ creation_failed → (降级到静态)
                ↘ aborted
                ↘ terminated
```

合法转换集合（OpenClaw `PHASE_TRANSITIONS`）：

```
idle:       {creating}
creating:   {streaming, creation_failed, terminated}
streaming:  {completed, aborted, terminated}
completed:  {} (终态)
aborted:    {} (终态)
terminated: {} (终态)
creation_failed: {} (终态，触发降级)
```

#### 3.1.4 降级策略

来自 OpenClaw `src/card/reply-dispatcher-types.ts:107-113`，已验证：

| 路径 | 限流间隔 | 说明 |
|------|---------|------|
| CardKit `cardElement.content` | 100ms | 低延迟，打字机效果 |
| IM `message.patch` | 1500ms | CardKit 失败时的降级路径 |
| 纯文本 `message.create` | — | 最终降级 |

错误处理策略（OpenClaw `streaming-card-controller.ts`）：

| 错误码 | 处理 | 来源 |
|--------|------|------|
| 230020 (速率限制) | 跳过当前帧，不降级 | `isCardRateLimitError` |
| 230099/11310 (表格超限) | 禁用 CardKit 流式，等终态用 CardKit 收尾 | `isCardTableLimitError` |
| 其他错误 | 禁用 CardKit 流式，降级到 IM patch | `cardkit.ts` fallback |

#### 3.1.5 实现

新增 `internal/messaging/feishu/streaming.go`：

```go
package feishu

type CardPhase int

const (
    PhaseIdle           CardPhase = iota
    PhaseCreating
    PhaseStreaming
    PhaseCompleted
    PhaseAborted
    PhaseTerminated
    PhaseCreationFailed
)

var phaseTransitions = map[CardPhase]map[CardPhase]bool{
    PhaseIdle:          {PhaseCreating: true},
    PhaseCreating:      {PhaseStreaming: true, PhaseCreationFailed: true, PhaseTerminated: true},
    PhaseStreaming:     {PhaseCompleted: true, PhaseAborted: true, PhaseTerminated: true},
}

// StreamingCardController manages the lifecycle of a CardKit streaming card.
type StreamingCardController struct {
    phase     CardPhase
    cardID    string    // CardKit card_id (from card.create)
    elementID string    // streaming element_id (constant)
    msgID     string    // IM message_id
    sequence  int64

    mu          sync.Mutex
    buf         strings.Builder
    lastFlushed string

    client *lark.Client
    log    *slog.Logger
}

func NewStreamingCardController(client *lark.Client, log *slog.Logger) *StreamingCardController

// EnsureCard creates card entity + sends IM message.
// Falls back to IM card on CardKit failure.
func (c *StreamingCardController) EnsureCard(ctx context.Context, chatID string) error

// Write appends streaming content to the buffer.
func (c *StreamingCardController) Write(text string) error

// Flush pushes buffered content via cardElement.content.
func (c *StreamingCardController) Flush(ctx context.Context) error

// Close sets streaming_mode=false and updates final card.
func (c *StreamingCardController) Close(ctx context.Context) error

// Abort stops streaming and shows "Aborted" message.
func (c *StreamingCardController) Abort(ctx context.Context) error
```

**FeishuConn.WriteCtx 改造**：

```go
func (c *FeishuConn) WriteCtx(ctx context.Context, env *events.Envelope) error {
    // Phase 1 (静态): 直接发文本
    // Phase 2 (流式): 委托给 StreamingCardController
    if c.streamCtrl != nil {
        text, ok := extractResponseText(env)
        if !ok {
            return nil
        }
        if env.Event.Type == events.Done {
            return c.streamCtrl.Close(ctx)
        }
        return c.streamCtrl.Write(text)
    }
    // ... 现有静态逻辑 ...
}
```

#### 3.1.6 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 3.1-1 | 状态机仅允许合法转换 | 单元测试 |
| 3.1-2 | CardKit 创建成功后进入 streaming | 集成测试 |
| 3.1-3 | CardKit 创建失败降级到 IM patch | 单元测试 |
| 3.1-4 | IM patch 也失败则降级到纯文本 | 单元测试 |
| 3.1-5 | 流式内容以 100ms 间隔更新 | 限流测试 |
| 3.1-6 | 速率限制(230020)跳过帧不降级 | 错误测试 |
| 3.1-7 | 表格超限(230099)禁用流式等终态 | 错误测试 |
| 3.1-8 | Done 事件触发 Close 关闭流式 | 集成测试 |
| 3.1-9 | 同一 chat 不出现消息洪水 | 集成测试 |

---

### 3.2 Abort 检测

#### 3.2.1 问题

用户无法中止正在进行的流式回复。

**OpenClaw 实现** — `src/channel/abort-detect.ts:23-66`，已验证 65 个触发词。

#### 3.2.2 实现

新增 `internal/messaging/feishu/abort.go`：

```go
package feishu

import "strings"

// abortTriggers is a set of normalized trigger words for abort detection.
// Source: OpenClaw abort-detect.ts (65 triggers, pruned to ~30 core).
var abortTriggers = map[string]bool{
    // English
    "stop": true, "abort": true, "halt": true, "cancel": true,
    "wait": true, "exit": true, "interrupt": true,
    "please stop": true, "stop please": true,
    // Chinese
    "停止": true, "取消": true, "中断": true, "等一下": true,
    "别说了": true, "停下来": true,
    // Japanese
    "やめて": true, "止めて": true,
    // Russian
    "стоп": true,
}

// IsAbortCommand checks if the message text is an abort command.
// Normalization: trim → lowercase → strip trailing punctuation.
func IsAbortCommand(text string) bool {
    t := strings.TrimSpace(strings.ToLower(text))
    t = strings.TrimRight(t, ".!?…,，。;；:：\"')]")
    return abortTriggers[t]
}
```

**集成点**：在 `handleMessage` 中，去重之后、入队之前检测：

```go
text := ConvertMessage(...)
if IsAbortCommand(text) {
    a.chatQueue.Abort(chatID)  // 触发 abort fast-path
    return nil
}
a.chatQueue.Enqueue(chatID, func(ctx context.Context) error {
    return a.HandleTextMessage(ctx, ...)
})
```

#### 3.2.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 3.2-1 | "stop" 被识别为 abort | 单元测试 |
| 3.2-2 | "停止" 被识别为 abort | 单元测试 |
| 3.2-3 | "Stop." 去标点后匹配 | 单元测试 |
| 3.2-4 | "stop please" 匹配 | 单元测试 |
| 3.2-5 | "hello" 不匹配 | 单元测试 |
| 3.2-6 | abort 命令触发 StreamingCardController.Abort | 集成测试 |

---

### 3.3 Typing 指示器

#### 3.3.1 问题

用户发送消息后无反馈，不知道 bot 是否在处理。

**OpenClaw 实现** — `src/messaging/outbound/typing.ts`：使用 reaction API 添加/移除 emoji 模拟 typing。

#### 3.3.2 已验证 API

```go
// service/im/v1/resource.go:1612
func (m *messageReaction) Create(ctx, req *CreateMessageReactionReq) (*CreateMessageReactionResp, error)
// service/im/v1/resource.go:1640
func (m *messageReaction) Delete(ctx, req *DeleteMessageReactionReq) (*DeleteMessageReactionResp, error)

// service/im/v1/model.go — Emoji struct
type Emoji struct {
    EmojiType *string  // emoji 类型字符串
}
```

#### 3.3.3 实现

新增 `internal/messaging/feishu/typing.go`：

```go
package feishu

import "context"

// AddTypingIndicator adds a reaction to the user's message to indicate processing.
func (a *Adapter) AddTypingIndicator(ctx context.Context, messageID string) error {
    body := larkim.NewCreateMessageReactionReqBodyBuilder().
        ReactionType(larkim.NewEmojiBuilder().EmojiType("Typing").Build()).
        Build()
    req := larkim.NewCreateMessageReactionReqBuilder().
        MessageId(messageID).
        Body(body).
        Build()
    _, err := a.larkClient.Im.V1.MessageReaction.Create(ctx, req)
    return err
}

// RemoveTypingIndicator removes the typing reaction.
func (a *Adapter) RemoveTypingIndicator(ctx context.Context, messageID, reactionID string) error {
    // ... 使用 reactionID 删除 ...
}
```

**注意**：`EmojiType` 的具体合法值需查阅飞书 emoji 类型列表。OpenClaw TS 代码使用 `"Typing"` 字面量，需确认 Go 环境下是否相同。

#### 3.3.4 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 3.3-1 | 消息处理开始时添加 typing reaction | 集成测试 |
| 3.3-2 | 消息处理结束时移除 typing reaction | 集成测试 |
| 3.3-3 | reaction 失败不阻断消息处理 | 错误测试 |

---

### 3.4 Reply-to 出站支持

已在 2.1.3 中覆盖。FeishuConn 在有 `replyToMsgID` 时使用 `Reply` API 替代 `Create`。

---

## 4. Phase 3 — 安全

### 4.1 访问控制

#### 4.1.1 问题

当前 `config.go:147-153` 飞书配置仅有 `Enabled/AppID/AppSecret/WorkerType`，无任何访问控制。任何人在任何群都能触发 bot。

**OpenClaw 实现** — `src/core/config-schema.ts:154-197`，已验证：

- `dmPolicy`: `'open'` | `'pairing'` | `'allowlist'` | `'disabled'`
- `groupPolicy`: `'open'` | `'allowlist'` | `'disabled'`
- `requireMention`: boolean
- `allowFrom`: string | string[]
- `respondToMentionAll`: boolean
- `groups`: per-group 覆盖配置

#### 4.1.2 配置扩展

修改 `internal/config/config.go`：

```go
type FeishuConfig struct {
    Enabled   bool   `mapstructure:"enabled"`
    AppID     string `mapstructure:"app_id"`
    AppSecret string `mapstructure:"app_secret"`
    WorkerType string `mapstructure:"worker_type"`

    // 访问控制
    DMPolicy       string   `mapstructure:"dm_policy"`        // open | allowlist | disabled
    GroupPolicy    string   `mapstructure:"group_policy"`     // open | allowlist | disabled
    RequireMention bool     `mapstructure:"require_mention"`  // 群内必须 @bot
    AllowFrom      []string `mapstructure:"allow_from"`       // open_id 白名单
}
```

对应 `configs/config-dev.yaml`：

```yaml
messaging:
  feishu:
    enabled: true
    app_id: "${FEISHU_APP_ID}"
    app_secret: "${FEISHU_APP_SECRET}"
    worker_type: "claude_code"
    dm_policy: "open"
    group_policy: "open"
    require_mention: true
    allow_from: []
```

#### 4.1.3 Gate 实现

新增 `internal/messaging/feishu/gate.go`：

```go
package feishu

import "sync"

type Gate struct {
    dmPolicy       string
    groupPolicy    string
    requireMention bool
    allowFrom      map[string]bool
    mu             sync.RWMutex
}

type GateResult struct {
    Allowed bool
    Reason  string
}

func NewGate(cfg FeishuConfig) *Gate

func (g *Gate) Check(chatType, userID string, botMentioned bool) *GateResult {
    if chatType == "p2p" {
        switch g.dmPolicy {
        case "disabled":
            return &GateResult{Allowed: false, Reason: "dm_disabled"}
        case "allowlist":
            if !g.isAllowed(userID) {
                return &GateResult{Allowed: false, Reason: "not_in_allowlist"}
            }
        }
        // "open" / "pairing" → allowed
    } else {
        switch g.groupPolicy {
        case "disabled":
            return &GateResult{Allowed: false, Reason: "group_disabled"}
        case "allowlist":
            if !g.isAllowed(userID) {
                return &GateResult{Allowed: false, Reason: "not_in_allowlist"}
            }
        }
        if g.requireMention && !botMentioned {
            return &GateResult{Allowed: false, Reason: "no_mention"}
        }
    }
    return &GateResult{Allowed: true}
}
```

**集成点**：在 `handleMessage` 中，去重之后、abort 检测之前：

```go
// 检查 bot 是否被提及
botMentioned := isBotMentioned(msg.Mentions, a.botOpenID)

// 访问控制
result := a.gate.Check(chatType, userID, botMentioned)
if !result.Allowed {
    a.log.Debug("feishu: gate rejected", "reason", result.Reason)
    return nil
}
```

#### 4.1.4 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 4.1-1 | dm_policy=disabled 拒绝所有 DM | 单元测试 |
| 4.1-2 | dm_policy=open 允许所有 DM | 单元测试 |
| 4.1-3 | dm_policy=allowlist 仅允许白名单用户 DM | 单元测试 |
| 4.1-4 | group_policy=disabled 拒绝所有群消息 | 单元测试 |
| 4.1-5 | require_mention=true 且未 @bot 时拒绝 | 单元测试 |
| 4.1-6 | require_mention=true 且已 @bot 时允许 | 单元测试 |
| 4.1-7 | topic_group 与 group 策略一致 | 单元测试 |

---

### 4.2 限流

#### 4.2.1 问题

飞书 API 有速率限制，当前无任何限流机制。

**OpenClaw 限流参数** — `src/card/reply-dispatcher-types.ts:107-113`：
- CardKit: 100ms
- IM patch: 1500ms

#### 4.2.2 实现

新增 `internal/messaging/feishu/rate_limiter.go`（参照 `slack/rate_limiter.go`）：

```go
package feishu

import (
    "sync"
    "time"
)

type FeishuRateLimiter struct {
    mu           sync.Mutex
    cardKitLimit time.Duration  // 100ms per card
    patchLimit   time.Duration  // 1500ms per message
    lastCardKit  map[string]time.Time
    lastPatch    map[string]time.Time
}

func NewFeishuRateLimiter() *FeishuRateLimiter

func (r *FeishuRateLimiter) AllowCardKit(cardID string) bool
func (r *FeishuRateLimiter) AllowPatch(msgID string) bool
```

#### 4.2.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 4.2-1 | CardKit 同一卡片 100ms 内只允许 1 次 | 单元测试 |
| 4.2-2 | IM patch 同一消息 1500ms 内只允许 1 次 | 单元测试 |
| 4.2-3 | 不同卡片/消息独立限流 | 单元测试 |

---

### 4.3 去重增强

#### 4.3.1 问题

当前 `adapter.go:73-76` 的 `map[string]time.Time` 无容量上限，理论上可 OOM。

**OpenClaw 实现** — `src/messaging/inbound/dedup.ts`：
- FIFO（非 LRU），ES2015 Map 保持插入序
- 默认 12h TTL，5000 max entries，5min sweep

#### 4.3.2 实现

新增 `internal/messaging/feishu/dedup.go`：

```go
package feishu

import (
    "sync"
    "time"
)

const (
    dedupDefaultTTL        = 12 * time.Hour
    dedupDefaultMaxEntries = 5000
    dedupSweepInterval     = 5 * time.Minute
)

type Dedup struct {
    mu         sync.Mutex
    entries    map[string]time.Time
    order      []string  // FIFO eviction order
    maxEntries int
    ttl        time.Duration
}

func NewDedup(maxEntries int, ttl time.Duration) *Dedup

func (d *Dedup) TryRecord(id string) bool {
    d.mu.Lock()
    defer d.mu.Unlock()

    if _, seen := d.entries[id]; seen {
        return false  // duplicate
    }

    // FIFO eviction when at capacity
    for len(d.entries) >= d.maxEntries && len(d.order) > 0 {
        oldest := d.order[0]
        d.order = d.order[1:]
        delete(d.entries, oldest)
    }

    d.entries[id] = time.Now()
    d.order = append(d.order, id)
    return true  // new
}
```

替换 `adapter.go` 中的 `dedup map[string]time.Time`。

#### 4.3.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 4.3-1 | 重复 message_id 被拒绝 | 单元测试 |
| 4.3-2 | 超过 maxEntries 时 FIFO 淘汰最旧条目 | 单元测试 |
| 4.3-3 | 过期条目被定期清理 | 单元测试 |
| 4.3-4 | 无容量无限增长 | 压力测试 |

---

### 4.4 消息过期检查

#### 4.4.1 问题

WebSocket 重连后可能重放旧消息，当前无过滤。

**OpenClaw 实现** — `src/messaging/inbound/dedup.ts:34-49`：30 分钟过期。

#### 4.4.2 实现

```go
const messageExpiry = 30 * time.Minute

func isMessageExpired(msg *larkim.EventMessage) bool {
    if msg.CreateTime == nil {
        return false
    }
    createTime, err := strconv.ParseInt(*msg.CreateTime, 10, 64)
    if err != nil {
        return false
    }
    return time.Since(time.UnixMilli(createTime)) > messageExpiry
}
```

#### 4.4.3 验收标准

| ID | AC | 验证方式 |
|----|-----|---------|
| 4.4-1 | 超过 30 分钟的旧消息被丢弃 | 单元测试 |
| 4.4-2 | create_time 为 nil 时不丢弃 | 单元测试 |
| 4.4-3 | 新鲜消息正常处理 | 单元测试 |

---

## 5. 文件变动清单

### Phase 1 — DM/群消息基础处理

| 文件 | 操作 | 说明 |
|------|------|------|
| `internal/messaging/feishu/adapter.go` | 修改 | handleMessage 重构：提取 chatType/rootID/parentID/mentions；bot 防御检查；downloadMedia() 方法（MessageResource API） |
| `internal/messaging/feishu/events.go` | 修改 | extractResponseText 保持不变（已在 2.1 中确认） |
| `internal/messaging/feishu/mention.go` | 新增 | ResolveMentions 提及解析 |
| `internal/messaging/feishu/converter.go` | 修改 | ConvertMessage 返回 `*MediaInfo`，支持 image/file/audio/video/sticker 下载元信息 |
| `internal/messaging/feishu/chat_queue.go` | 新增 | ChatQueue per-chat 串行队列 |
| `internal/messaging/feishu/adapter_test.go` | 修改 | 新增 AC 测试 |
| `internal/messaging/bridge.go` | 修改 | MakeFeishuEnvelope 支持新 metadata 字段 |

### Phase 2 — 用户体验

| 文件 | 操作 | 说明 |
|------|------|------|
| `internal/messaging/feishu/streaming.go` | 新增 | StreamingCardController + 状态机 + 降级 |
| `internal/messaging/feishu/abort.go` | 新增 | IsAbortCommand |
| `internal/messaging/feishu/typing.go` | 新增 | AddTypingIndicator / RemoveTypingIndicator |
| `internal/messaging/feishu/adapter.go` | 修改 | 集成 streaming/abort/typing |
| `internal/messaging/feishu/adapter_test.go` | 修改 | 新增 AC 测试 |

### Phase 3 — 安全

| 文件 | 操作 | 说明 |
|------|------|------|
| `internal/messaging/feishu/gate.go` | 新增 | Gate 访问控制 |
| `internal/messaging/feishu/rate_limiter.go` | 新增 | FeishuRateLimiter |
| `internal/messaging/feishu/dedup.go` | 新增 | FIFO Dedup |
| `internal/config/config.go` | 修改 | FeishuConfig 扩展 |
| `configs/config-dev.yaml` | 修改 | 新增 gate 配置项 |
| `configs/env.example` | 修改 | 新增环境变量 |
| `cmd/hotplex/main.go` | 修改 | 传递新配置到 adapter |

---

## 6. handleMessage 处理流水线（完成后）

```
P2MessageReceiveV1 Event
    │
    ├─ 1. nil check (Event, Message)
    ├─ 2. Bot 防御 (sender_type == "app" → skip)
    ├─ 3. 消息过期检查 (createTime > 30min → skip)
    ├─ 4. 去重 (Dedup.TryRecord)
    ├─ 5. 消息类型转换 (ConvertMessage)
    │       └─ 5a. 若为多媒体类型 → downloadMedia (MessageResource API) → 拼接本地路径
    │       └─ 5b. 若解析失败 → 降级为纯文本占位符
    ├─ 6. @提及解析 (ResolveMentions)
    ├─ 7. 访问控制 (Gate.Check)
    ├─ 8. Abort 快速路径 (IsAbortCommand → ChatQueue.Abort)
    └─ 9. Chat 队列入队 (ChatQueue.Enqueue → HandleTextMessage)
            │
            ├─ Typing indicator ON
            ├─ MakeFeishuEnvelope (chatID, threadKey, userID, text)
            ├─ Bridge.Handle → Session → Worker
            ├─ FeishuConn.WriteCtx
            │   ├─ Phase 1: sendTextMessage (静态)
            │   └─ Phase 2: StreamingCardController (流式)
            └─ Typing indicator OFF
```

---

## 7. 依赖关系

```
Phase 1.5 (mention) ←── Phase 1.3 (converter) ←── Phase 1.1 (thread)
         ↑                                              ↓
Phase 2.5 (botOpenID) ←── Phase 2.2 (abort)    Phase 1.4 (chat queue)
         ↓                         ↓                    ↓
Phase 3.2 (rate limiter)
Phase 3.3 (dedup)
Phase 3.4 (message expiry)
```

---

## 8. 完整的 E2E 用户验收测试 (UAT) 手册

为了确保项目落地时不仅逻辑完备，且测试团队可按照**真机黑盒（Black-box）**视角无歧义地重现验收操作，本手册将各类情况严格映射到了**飞书客户端（PC/移动端）的具体 UI 级操作步骤**。

### 8.1 核心业务交互测试 (Happy Paths)

| 用例编号 | 测试模块 | 飞书操作步骤 (Tester Action) | 验收标准 (QA Assert) |
|---------|---------|-------------------------|--------------------|
| **TC-1.1** | **单聊直连与基础格式** | 1. 在飞书顶部搜索栏搜索该 `机器人名字` 并进入单聊。<br>2. 键入测试文本：`你好，输出加粗、横线以及一段高亮的 Go 代码`，点击回车发送。 | 1. 机器人迅速弹出消息体。<br>2. 回复不仅有文本，且 Markdown 的**加粗**、~~横线~~、以及 `高亮代码块` 在飞书中均渲染正确。 |
| **TC-1.2** | **群聊显式唤醒** | 1. 在任意已含有机器人的沟通群，先发一句闲聊：`今天天气不错`。<br>2. 输入 `@` 唤出面板，选中 `该机器人`，接着输入：`翻译刚才这句话为英文`。 | 1. 闲聊语句不能被机器人理睬（防骚扰）。<br>2. 机器人被 `@` 的消息予以回复，且正确输出翻译，自身内容不携带多余的 `@占位符`。 |
| **TC-1.3** | **上下游话题链 (Thread)** | 1. 寻找一条机器人在群里发出的旧消息。<br>2. 鼠标悬停，点击快捷侧栏上的 **「回复 (Reply)」** 图标（或长按操作）。<br>3. 在下方的展开框中输入 `@机器人 请补充解释一下` 并发送。 | 机器人的第二次回答**必须折叠在原话题 (Thread) 的盖楼详情页内**，而不能以一条孤立新消息的形式散落在外围的群聊瀑布流中。 |
| **TC-1.4** | **复杂群内成员混转** | 1. 在群里发送：`@机器人 帮我评价一下 @张三（群内真人）的周报`。 | 机器人的推理和理解中能精准看到文字 `帮我评价一下 @张三 的周报`，不会被错误隔断。 |
| **TC-1.5** | **原生媒体与富控件混排** | 1. 对屏幕使用截图功能 (如 `Command+Ctrl+Shift+4`)，直接粘贴在飞书输入框。<br>2. 随后同时输入 `帮我看看这个UI图`，点击发送。<br>3. 发送一个文件（如 PDF/TXT）。<br>4. 发送飞书内置的默认 Emoji 或动态表情大图 (Sticker)。<br>5. 发送一段语音或短视频。 | 1. 图片/文件/语音/视频/表情均被下载到 `/tmp/hotplex/media/` 下对应子目录。<br>2. AI 收到的消息文本中携带文件本地路径（如 `[用户发送了一张图片: /tmp/hotplex/media/images/img_xxx.jpg]`）。<br>3. AI 能读取并理解媒体内容。<br>4. 下载失败不影响消息处理——降级为纯文本占位符。<br>5. 动态表情被成功下载到本地 GIF，AI 能理解表情语义。 |

### 8.2 极致交互体验测试 (UX Tests)

| 用例编号 | 测试模块 | 飞书操作步骤 (Tester Action) | 验收标准 (QA Assert) |
|---------|---------|-------------------------|--------------------|
| **TC-2.1** | **思考焦虑消除器 (Typing)** | 1. 发送需要漫长推演的开放性话题：`帮我总结世界经济100年历史`。<br>2. 发送后刻意不眨眼盯着自己刚才发出的绿色消息气泡。 | 发出后约半秒内，用户绿色气泡下方会出现飞书系统自带的 **`[...]打字中 / Typing` 或者动画表情小贴纸 (Reaction)**。当流式打字卡片浮现第一句话后，该小贴纸必须自动消失。 |
| **TC-2.2** | **沉浸式字元输出 (Streaming)** | 1. 让机器人输出长篇大论的代码或故事。<br>2. 观察新跳出的交互消息卡片框。 | 卡片不应处于长达几秒的白板静止态。用户应肉眼可见字词以类似**电影打字机**（~100毫秒）的频率顺畅向下滑溜延展，不可经常卡顿。 |
| **TC-2.3** | **超限容灾无缝降级** | 1. 对该机器人发出刁钻请求：`请生成至少有两百行、涵盖五十列随机字母元素的超大型 Markdown 表格，现在就开始`。 | 关注飞书客户端展现表现：此时表格体积过巨一定将触发飞书 CardKit 渲染报错（API 230099）。**界面绝对不能向用户抛出红色报错代码**。系统底层静默挂起并在最终直接下发一大块完整的纯文本表格进行托底展示，文字完全没有丢失。 |
| **TC-2.4** | **急速刹车态 (Abort)** | 1. 在体会上一用例“长篇大论打字”流动的半道上。<br>2. 直接在输入框敲入单词 `stop` 或 `停止` 并火速发送。 | 飞书客户端上的打字流动应**在 1-2 秒内戛然而止**。由于底层推理已被成功强杀退潮，前端不可再出现延宕出来的文字，避免出现停止失灵现象。 |

### 8.3 极端容错与黑盒边界测试 (Edge Cases & Resilience)

| 用例编号 | 测试模块 | 飞书操作步骤 (Tester Action) | 验收标准 (QA Assert) |
|---------|---------|-------------------------|--------------------|
| **TC-3.1** | **高频暴力输入防竞态** | 1. 准备大段文本分 5 句。<br>2. 不等机器人回复，利用飞书热键以**最快手速**“狂点回车”，连发 5 句话（1秒内抛出）。 | 测试机器人绝不可产生幻觉并发 5 份同时闪烁在飞书屏幕里交错互说。必须保证当前飞书屏幕**只有一张**卡片在专心思考与渲染回复；渲染结束后再回答你第二句话。 |
| **TC-3.2** | **死循环左脚踩右脚防御** | 1. （需协助）新建一个会自动机械化群回复闲聊话语的传统运维机器人（Robot B），拉入大群。<br>2. 你人工发出引战言论，触发 Robot B 的回复。 | Robot B 的广播将推送给群内包括本机器人在内的所有人。但本网关内置基于来源判定 (`sender_type`)，必须静默无视 Robot B 的发言，从而**杜绝两个机器人在群内触发无限文字轰炸**灾难。 |
| **TC-3.3** | **长链接断网与积压消息重放** | 1. 模拟弱网丢包，在 50ms 的极小时间窗内向 WebSocket 通道注入两帧带有相同 `message_id` 的模拟下发包。<br>2. 强杀切断网关进程 1 小时后再重启，此时 WebSocket 刚建连成功，立刻会接收到飞书服务端由于用户之前发信而重推过来的十几个旧消息包。 | 1. 网关必须有且仅有一次有效响应，极速并发下的冗余发包被 `Dedup` 锁精准销毁。<br>2. 重推下来的大量过期飞书老旧消息包一旦时间戳被判定超过 `30 min` 立即遭到引擎清道夫回收屏蔽，严防机器人复活后像发了疯一样疯狂群发历史信息。|
| **TC-3.4** | **群越权渗透攻击** | 1. 找一个不在后台网关 `AllowList / 限充群名单` 内且规模庞大的无关大百人群。<br>2. 以低级别用户权限强行拉扯机器人进群，并且 `@该机器人 测试`。 | 无论怎么呼叫，机器人以完全**已读不回**的状态应对此异常请求，不浪费 GPU Token。即便被绕过拉起，业务层仍处于全额闭锁状。 |

Phase 1 内部可并行开发（1.1 ~ 1.5 相互独立），Phase 2 依赖 Phase 1 完成，Phase 3 可部分并行。
