---
type: spec
tags:
  - project/HotPlex
  - messaging/feishu
  - cardkit-v2
date: 2026-05-08
status: draft
progress: 0
---

# 飞书卡片 Header 增强规格书

**版本**: v2.0
**日期**: 2026-05-08
**状态**: 草案
**范围**: `internal/messaging/feishu/` 下所有卡片构建器
**SDK**: `github.com/larksuite/oapi-sdk-go/v3@v3.5.3`
**参考文档**: [卡片 JSON 2.0 结构](https://open.feishu.cn/document/feishu-cards/card-json-v2-structure)、[标题组件](https://open.feishu.cn/document/uAjLw4CM/ukzMukzMukzM/feishu-cards/card-json-v2-components/content-components/title)、[2.0 不兼容变更](https://open.feishu.cn/document/feishu-cards/card-json-v2-breaking-changes-release-notes)

---

## 1. 概述

### 1.1 目标

为飞书适配器的所有卡片添加 **CardKit v2 官方 `header` 组件**，实现：

- 视觉锚点：彩色标题栏区分卡片类型/状态
- 身份标识：header title 显示 Bot 名称（从飞书 API 懒加载）
- 状态传达：`template` 颜色表达语义（生成中/完成/错误/交互）
- 元信息标签：`text_tag_list` 显示模型名、耗时等

### 1.2 当前状态

所有 5 种卡片构建器均 **未使用 `header` 字段**，内容直接裸露在 `body.elements` 中：

| 构建器 | 文件:行号 | Header | Footer |
|--------|-----------|--------|--------|
| `buildCardContent` | `adapter.go:1313` | 无 | 无 |
| `buildTurnSummaryCard` | `adapter.go:1336` | 无 | 无 |
| `buildInteractionCard` | `interaction.go:328` | 无 | `hr` + `markdown` 模拟 |
| 流式卡片 (sendCardMessage) | `streaming.go:512` | 无 | 无 |
| IM Patch fallback | `streaming.go:710` | 无 | 无 |

### 1.3 限制

Card JSON 2.0 **没有原生的 footer/note 组件**（`note` 是 1.0 组件，2.0 已明确废弃，官方建议用 `div` + `notation` 字号 + `grey` 颜色替代）。当前 `hr` + `markdown` 的 footer 模拟方式是 2.0 的标准做法，**不做变更**。

---

## 2. 飞书 Card JSON 2.0 Header 技术规格

### 2.1 Header 完整结构

```json
{
  "header": {
    "title": {
      "tag": "plain_text",
      "content": "主标题"
    },
    "subtitle": {
      "tag": "plain_text",
      "content": "副标题"
    },
    "template": "blue",
    "icon": {
      "tag": "standard_icon",
      "token": "icon_token",
      "color": "blue"
    },
    "text_tag_list": [
      {
        "tag": "text_tag",
        "text": { "tag": "plain_text", "content": "标签" },
        "color": "neutral"
      }
    ],
    "padding": "12px"
  }
}
```

### 2.2 字段说明

| 字段 | 必填 | 类型 | 约束 |
|------|------|------|------|
| `title` | **是** | object | `{"tag": "plain_text"\|"lark_md", "content": "..."}`。最大 4 行，超长 `...` 截断 |
| `subtitle` | 否 | object | 同 title 格式。**单行**超长 `...`。不能脱离 title 单独存在 |
| `template` | 否 | string | 13 种主题色，默认 `"default"` |
| `icon` | 否 | object | `"standard_icon"`（token + color）或 `"custom_icon"`（img_key） |
| `text_tag_list` | 否 | array | 最多 **3 个**标签，超出静默丢弃，顺序与数组一致 |
| `padding` | 否 | string | 默认 `"12px"`，范围 `[0, 99]px`，支持 CSS 简写 |

### 2.3 Template 颜色值（13 种）

| 值 | 语义 | 适用场景 |
|----|------|---------|
| `"default"` | 无色 | 默认卡片 |
| `"blue"` | 蓝色 | 信息/正常状态/完成 |
| `"wathet"` | 浅蓝 | 进行中/生成中 |
| `"turquoise"` | 青色 | 信息展示 |
| `"green"` | 绿色 | 完成/成功 |
| `"yellow"` | 黄色 | 注意/等待 |
| `"orange"` | 橙色 | 警告/需要操作 |
| `"red"` | 红色 | 错误/异常 |
| `"carmine"` | 胭脂红 | 严重错误 |
| `"violet"` | 紫罗兰 | MCP 请求 |
| `"purple"` | 紫色 | 特殊标记 |
| `"indigo"` | 靛蓝 | 特殊标记 |
| `"grey"` | 灰色 | 过期/取消/失效 |

官方设计建议：
- 群聊：彩色标题提供视觉锚点
- 单聊：颜色匹配卡片状态，避免每张卡片无差别使用同一颜色

### 2.4 Text Tag 颜色值（13 种）

`neutral` | `blue` | `turquoise` | `lime` | `orange` | `violet` | `indigo` | `wathet` | `green` | `yellow` | `red` | `purple` | `carmine`

### 2.5 Streaming 下的 Header 更新

| 操作 | 方式 | API |
|------|------|-----|
| 创建时设置 header | 卡片 JSON 中包含 `header` | `im.v1.message.create/reply` |
| 流式期间更新 header | `Cardkit.V1.Card.Update` | `PUT /cardkit/v1/cards/:card_id` |
| 流式结束后更新 header | `Cardkit.V1.Card.Update` | 同上 |
| 更新 config（streaming_mode） | `Cardkit.V1.Card.Settings` | 仅支持 `config` + `card_link` |

**关键约束**：
- `Settings` API **只能**更新 `config` 和 `card_link`，**不支持更新 header**
- 更新 header 必须使用 `Card.Update` API，需传入完整 card JSON（`card.type="card_json"`, `card.data="<JSON string>"`）
- `streaming_mode: true` 期间，`CardElement.Content` 文本更新不受 QPS 限制；`Card.Update` 和 `Card.Settings` 仍受 10 QPS/card 限制
- `streaming_mode: true` 期间，卡片不响应交互回调
- `Card.Update` 的 `sequence` 必须严格递增，否则报错 `300317`
- 卡片 JSON 大小限制：30KB；元素上限：200 个

### 2.6 SDK 能力确认

当前项目已导入 `github.com/larksuite/oapi-sdk-go/v3/service/cardkit/v1`，可用 API：

| API | 方法 | 当前使用 |
|-----|------|---------|
| `Card.IdConvert` | msg_id → card_id | ✅ `streaming.go:498` |
| `Card.Settings` | 更新 config/card_link | ✅ `streaming.go:611,648` |
| `CardElement.Content` | 流式更新 body 元素 | ✅ `streaming.go:675` |
| **`Card.Update`** | **更新完整卡片 JSON** | ❌ **未使用，本次新增** |
| `Card.BatchUpdate` | 批量局部更新 | ❌ 未使用 |
| `CardElement.Create/Delete/Patch/Update` | 元素级 CRUD | ❌ 未使用 |

`Card.Update` 请求体：

```go
type UpdateCardReqBody struct {
    Card     *Card   // {Type: "card_json", Data: "<card JSON string>"}
    Uuid     *string // 幂等 ID
    Sequence *int    // 必须严格递增
}

type Card struct {
    Type *string  // 固定 "card_json"
    Data *string  // 完整 card JSON 字符串
}
```

---

## 3. 设计方案

### 3.1 Header 语义映射

| 卡片类型 | title | subtitle | template | text_tag_list |
|---------|-------|----------|----------|---------------|
| 普通文本 | Bot 名称 | — | `default` | — |
| 流式（生成中） | Bot 名称 | `"生成中..."` | `wathet` | — |
| 流式（完成） | Bot 名称 | 内容首行摘要 | `blue` | — |
| 流式（取消） | Bot 名称 | `"已取消"` | `grey` | — |
| 权限请求 | `"工具执行授权"` | `data.ToolName` | `orange` | `[pending]` |
| 问答请求 | `"用户输入请求"` | — | `yellow` | — |
| MCP 请求 | `"MCP Server 请求"` | `data.MCPServerName` | `violet` | — |
| Turn 摘要 | `"Turn 摘要"` | — | `blue` | — |

### 3.2 Bot 名称获取：懒加载 + sync.Once 缓存

Adapter 不在 `Start()` 启动阶段获取 bot 名称（避免增加启动依赖）。改为 **首次发送卡片时按需获取，`sync.Once` 缓存到进程退出**。

**来源**：飞书 `/open-apis/bot/v3/info` API 返回的 `app_name` 字段（与现有 `fetchBotOpenID` 调用同一 API，但 botName 单独懒加载）。

**fallback 链**：`app_name` → `"HotPlex"`

```go
// Adapter 新增字段
type Adapter struct {
    // ... existing fields
    botName     string
    botNameOnce sync.Once
}

// resolveBotName 懒加载 bot 显示名，sync.Once 保证仅请求一次 API
func (a *Adapter) resolveBotName(ctx context.Context) string {
    a.botNameOnce.Do(func() {
        if a.larkClient == nil {
            a.botName = "HotPlex"
            return
        }
        botCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
        defer cancel()

        resp, err := a.larkClient.Get(botCtx, "/open-apis/bot/v3/info", nil, "tenant_access_token")
        if err != nil {
            a.botName = "HotPlex"
            a.Log.Debug("feishu: bot name fallback (API error)", "err", err)
            return
        }
        var result struct {
            Code int    `json:"code"`
            Msg  string `json:"msg"`
            Bot  struct {
                AppName string `json:"app_name"`
            } `json:"bot"`
        }
        if err := json.Unmarshal(resp.RawBody, &result); err != nil || result.Code != 0 || result.Bot.AppName == "" {
            a.botName = "HotPlex"
            if result.Code != 0 {
                a.Log.Debug("feishu: bot name fallback (API error)", "code", result.Code, "msg", result.Msg)
            }
            return
        }
        a.botName = result.Bot.AppName
        a.Log.Info("feishu: bot name resolved", "name", a.botName)
    })
    if a.botName == "" {
        return "HotPlex"
    }
    return a.botName
}
```

调用时机：在 `sendTextMessage`、`replyMessage`、`NewStreamingCardController` 构造时调用。

### 3.3 统一卡片构建函数

新增 `card_template.go` 集中管理 header 类型和构建函数，替代 4 处散落的 `map[string]any` 手工构建。

#### cardHeader 类型

```go
type cardHeader struct {
    Title    string     // 必填
    Subtitle string     // 可选
    Template string     // 可选，空值省略
    Tags     []cardTag  // 可选，最多 3 个
}

type cardTag struct {
    Text  string
    Color string
}

func (h cardHeader) toMap() map[string]any {
    // 零值省略规则：Template 空 → 省略；Tags 空 → 省略；Subtitle 空 → 省略
    // 仅输出非零字段，保持 JSON 精简
}
```

#### 统一构建入口

```go
// buildCard 构建标准 CardKit v2 卡片（非流式）
func buildCard(header cardHeader, config map[string]any, elements []map[string]any) string {
    card := map[string]any{
        "schema": "2.0",
        "config": config,
        "body":   map[string]any{"elements": elements},
    }
    if hm := header.toMap(); hm != nil {
        card["header"] = hm
    }
    return encodeCard(card)
}

// buildStreamingCard 构建流式卡片（streaming_mode + element_id + summary）
func buildStreamingCard(header cardHeader, summary, content string) string {
    card := map[string]any{
        "schema": "2.0",
        "config": map[string]any{
            "streaming_mode": true,
            "summary":        map[string]any{"content": summary},
        },
        "body": map[string]any{
            "elements": []any{
                map[string]any{
                    "tag": "markdown", "element_id": streamingElementID, "content": content,
                },
            },
        },
    }
    if hm := header.toMap(); hm != nil {
        card["header"] = hm
    }
    return encodeCard(card)
}
```

#### 现有构建器退化为参数适配层

每个现有函数只做参数组装 + 委托 `buildCard`：

```go
// buildCardContent — 普通 text 卡片
func buildCardContent(text string, header cardHeader) string {
    return buildCard(header,
        map[string]any{"wide_screen_mode": true},
        []map[string]any{{"tag": "markdown", "content": text}},
    )
}

// buildInteractionCard — 交互卡片（footer 逻辑不变）
func buildInteractionCard(body, footer string, header cardHeader) string {
    elements := []map[string]any{{"tag": "markdown", "content": body}}
    if footer != "" {
        elements = append(elements, map[string]any{"tag": "hr"})
        elements = append(elements, map[string]any{"tag": "markdown", "content": footer})
    }
    return buildCard(header, map[string]any{"wide_screen_mode": true}, elements)
}

// buildTurnSummaryCard — 摘要卡片（tableRow 逻辑不变）
func buildTurnSummaryCard(d messaging.TurnSummaryData, header cardHeader) string {
    fields := d.Fields()
    if len(fields) == 0 { return "" }
    elements := make([]map[string]any, len(fields))
    for i, f := range fields {
        elements[i] = tableRow(f.Label, f.Value)
    }
    return buildCard(header, map[string]any{"wide_screen_mode": true}, elements)
}
```

`encodeCard`、`tableRow` 不变。

### 3.4 调用方变更

所有调用方加一行 `cardHeader` 构造：

| 调用位置 | header 参数 |
|---------|-------------|
| `sendTextMessage` (adapter.go:1112) | `cardHeader{Title: a.resolveBotName(ctx)}` |
| `replyMessage` (adapter.go:1144) | `cardHeader{Title: a.resolveBotName(ctx)}` |
| `sendTurnSummaryCard` (adapter.go:929) | `cardHeader{Title: "Turn 摘要", Template: "blue"}` |
| `sendPermissionRequest` (interaction.go:45) | `cardHeader{Title: "工具执行授权", Subtitle: data.ToolName, Template: "orange", Tags: []cardTag{{Text: "pending", Color: "orange"}}}` |
| `sendQuestionRequest` (interaction.go:104) | `cardHeader{Title: "用户输入请求", Template: "yellow"}` |
| `sendElicitationRequest` (interaction.go:134) | `cardHeader{Title: "MCP Server 请求", Subtitle: data.MCPServerName, Template: "violet"}` |

交互卡片的 body markdown 相应精简——移除现有类型标题行（如 `**⚠️ 工具执行授权**`），类型信息已由 header 承载。

### 3.5 Header 状态流转（流式卡片）

```
PhaseCreating → PhaseStreaming → PhaseCompleted
                              ↘ PhaseAborted
```

```
创建 → [wathet "生成中..."] → 流式更新 body（header 不变） → 完成 → [blue "内容摘要"]
                                                          ↘ 取消 → [grey "已取消"]
```

流式更新阶段（`flushCardKit`）只更新 body element content，header 保持 wathet 不变。状态转换只在 `Close` / `Abort` 时发生。

#### 状态 1：创建 — wathet（浅蓝）

**触发**: `sendCardMessage` 创建卡片

```go
contentJSON := buildStreamingCard(
    cardHeader{Title: c.agentName, Subtitle: "生成中...", Template: "wathet"},
    truncateForSummary(content),
    content,
)
```

#### 状态 2a：完成 — blue（蓝色）

**触发**: `Close()` 被 bridge 调用，流式输出正常结束

**调用时序**（在 Close 方法内部）：
1. 现有的最终 flush（CardKit → IM Patch fallback）
2. `disableStreaming` — 关闭 streaming_mode
3. **`updateHeader`** — header 转蓝

```go
// Close 末尾新增
c.updateHeader(ctx, cardHeader{
    Title:    c.agentName,
    Subtitle: truncateForSummary(content),
    Template: "blue",
})
```

#### 状态 2b：取消 — grey（灰色）

**触发**: `Abort()` 被 bridge 调用

**调用时序**（在 Abort 方法内部）：
1. `disableStreaming`
2. `sendAbortMessage`（现有逻辑不变）
3. **`updateHeader`** — header 转灰

```go
// Abort 末尾新增
c.updateHeader(ctx, cardHeader{
    Title:    c.agentName,
    Subtitle: "已取消",
    Template: "grey",
})
```

#### updateHeader 实现细节

使用 `Cardkit.V1.Card.Update` API（SDK 已有，项目未使用）：

```go
func (c *StreamingCardController) updateHeader(ctx context.Context, header cardHeader) {
    if c.cardID == "" {
        return // CardKit 不可用，跳过
    }

    c.mu.Lock()
    body := c.lastFlushed
    c.mu.Unlock()

    // 重建完整卡片 JSON（新 header + 当前 body content + 关闭 streaming_mode）
    cardJSON := buildCard(header,
        map[string]any{
            "streaming_mode": false,
            "summary":        map[string]any{"content": truncateForSummary(body)},
        },
        []map[string]any{
            {"tag": "markdown", "element_id": streamingElementID, "content": body},
        },
    )

    reqBody := larkcardkit.NewUpdateCardReqBodyBuilder().
        Card(&larkcardkit.Card{
            Type: ptrStr("card_json"),
            Data: ptrStr(cardJSON),
        }).
        Sequence(int(c.sequence.Add(1))).  // 严格递增
        Build()

    req := larkcardkit.NewUpdateCardReqBuilder().
        CardId(c.cardID).
        Body(reqBody).
        Build()

    resp, err := c.client.Cardkit.V1.Card.Update(ctx, req)
    if err != nil || !resp.Success() {
        c.log.Warn("feishu: header update failed (non-fatal)", "err", err)
    }
}

func ptrStr(s string) *string { return &s }
```

**Sequence 递增保证**：所有 CardKit API 操作共享 `c.sequence atomic.Int64`。现有调用链：
1. `enableStreaming`: seq=N
2. `flushCardKit` (多次): seq=N+1, N+2, ...
3. `disableStreaming`: seq=M
4. **`updateHeader`**: seq=M+1 ← 始终在 disableStreaming 之后

#### IM Patch fallback 路径

当 CardKit 整体降级时（`flushIMPatch` / `flushIMPatchWithConfig`），header 通过 `im.v1.message.patch` 自然带入——IM patch 替换整个卡片 JSON：

- `flushIMPatch`（中间 fallback）: `template: "wathet"`（还在生成中）
- `flushIMPatchWithConfig`（最终关闭）: `template: "blue"`（完成状态）

### 3.6 StreamingCardController 新增字段

```go
type StreamingCardController struct {
    // ... existing fields
    agentName string // bot 显示名，从 Adapter 传入
}
```

`NewStreamingCardController` 签名扩展：

```go
func NewStreamingCardController(client *lark.Client, limiter *FeishuRateLimiter, log *slog.Logger, agentName string) *StreamingCardController
```

调用方 `adapter.go:473` 同步更新：

```go
ctrl := NewStreamingCardController(a.larkClient, a.rateLimiter, a.Log, a.resolveBotName(ctx))
```

---

## 4. JSON 结构对比

### 4.1 普通文本卡片

**Before**:
```json
{
  "schema": "2.0",
  "config": {"wide_screen_mode": true},
  "body": {"elements": [{"tag": "markdown", "content": "Hello"}]}
}
```

**After**:
```json
{
  "schema": "2.0",
  "config": {"wide_screen_mode": true},
  "header": {
    "title": {"tag": "plain_text", "content": "HotPlex Bot"}
  },
  "body": {"elements": [{"tag": "markdown", "content": "Hello"}]}
}
```

### 4.2 流式卡片（生成中）

**Before**:
```json
{
  "schema": "2.0",
  "config": {"streaming_mode": true, "summary": {"content": "生成中"}},
  "body": {"elements": [{"tag": "markdown", "element_id": "streaming_content", "content": "..."}]}
}
```

**After**:
```json
{
  "schema": "2.0",
  "config": {"streaming_mode": true, "summary": {"content": "生成中"}},
  "header": {
    "title": {"tag": "plain_text", "content": "HotPlex Bot"},
    "subtitle": {"tag": "plain_text", "content": "生成中..."},
    "template": "wathet"
  },
  "body": {"elements": [{"tag": "markdown", "element_id": "streaming_content", "content": "..."}]}
}
```

### 4.3 流式卡片（完成）

```json
{
  "schema": "2.0",
  "config": {"streaming_mode": false, "summary": {"content": "完成了XX功能..."}},
  "header": {
    "title": {"tag": "plain_text", "content": "HotPlex Bot"},
    "subtitle": {"tag": "plain_text", "content": "完成了XX功能..."},
    "template": "blue"
  },
  "body": {"elements": [{"tag": "markdown", "element_id": "streaming_content", "content": "..."}]}
}
```

### 4.4 权限请求卡片

**Before**:
```json
{
  "schema": "2.0",
  "config": {"wide_screen_mode": true},
  "body": {"elements": [
    {"tag": "markdown", "content": "**⚠️ 工具执行授权**\nClaude Code 请求：\n📝 **Bash**\n..."},
    {"tag": "hr"},
    {"tag": "markdown", "content": "📋 请求ID: `...`\n💬 回复 允许/拒绝"}
  ]}
}
```

**After**（类型信息从 body 提升到 header，body 更聚焦内容）:
```json
{
  "schema": "2.0",
  "config": {"wide_screen_mode": true},
  "header": {
    "title": {"tag": "plain_text", "content": "工具执行授权"},
    "subtitle": {"tag": "plain_text", "content": "Bash"},
    "template": "orange",
    "text_tag_list": [
      {"tag": "text_tag", "text": {"tag": "plain_text", "content": "pending"}, "color": "orange"}
    ]
  },
  "body": {"elements": [
    {"tag": "markdown", "content": "Claude Code 请求：\n> 描述...\n```\nargs\n```"},
    {"tag": "hr"},
    {"tag": "markdown", "content": "📋 请求ID: `...`\n💬 回复 **允许/同意/ok** 或 **拒绝/取消/no**"}
  ]}
}
```

---

## 5. 风险与缓解

### 5.1 Token 开销

每条消息增加 ~150-300 bytes header JSON。在流式场景中，header 只在创建时编码一次，不参与每轮 flush。

**缓解**: 可接受。单条消息 JSON 通常 1-50KB，header 占比 < 1%。

### 5.2 `Card.Update` API 可用性

流式完成/取消时更新 header 依赖 `Cardkit.V1.Card.Update`。

**缓解**: header 更新失败为 **non-fatal**。卡片 body 内容已完整，只是 header 停留在 `"wathet"` 状态。日志 warn 即可，不影响用户体验。

### 5.3 IM Patch fallback 路径

当 CardKit API 整体降级时，`flushIMPatch` 使用 `im.v1.message.patch` 更新整个卡片 JSON。此路径本身已包含 header（因为 patch 替换整个卡片内容），无需额外处理。

### 5.4 向后兼容

- 飞书 Card JSON 2.0 的 `header` 字段是 **可选的**，不影响无 header 的旧卡片
- 已发送的旧卡片不会被更新（只有新创建的卡片带 header）
- 不需要数据迁移

### 5.5 `wide_screen_mode` 兼容性

当前代码使用 `"wide_screen_mode": true`（1.0 字段）。经核实，此字段在 2.0 中仍可工作（向后兼容），无需立即迁移到 `"width_mode"`。作为后续优化可单独处理。

### 5.6 Card JSON 大小限制

`Card.Update` API 要求 card JSON 不超过 30KB。header 增加的 ~300 bytes 远低于此限制。对于超大 body 内容（接近 30KB），`toMap()` 的零值省略规则可进一步压缩。

---

## 6. 测试计划

### 6.1 单元测试

| 测试 | 文件 | 验证内容 |
|------|------|---------|
| `TestCardHeaderToMap` | `card_template_test.go` | 零值省略规则：空 Template/Tags/Subtitle 不输出 |
| `TestCardHeaderToMap_AllFields` | `card_template_test.go` | 全字段输出完整 JSON |
| `TestBuildCard` | `card_template_test.go` | 统一构建函数输出正确 schema 2.0 + header + body |
| `TestBuildStreamingCard` | `card_template_test.go` | 流式卡片包含 streaming_mode + summary + element_id + header |
| `TestBuildCardContent_WithHeader` | `adapter_helper_test.go` | 更新现有 case，传入 cardHeader |
| `TestBuildInteractionCard_WithHeader` | `interaction_test.go` | 更新现有 case，header + body + footer 共存 |
| `TestResolveBotName` | `adapter_helper_test.go` | 懒加载 + sync.Once 缓存 + fallback |

### 6.2 流式 Header 状态测试

| 测试 | 文件 | 验证内容 |
|------|------|---------|
| `TestUpdateHeader_Success` | `streaming_card_test.go` | mock `Card.Update` 被调用，sequence 递增 |
| `TestUpdateHeader_CardKitUnavailable` | `streaming_card_test.go` | cardID 为空时静默跳过 |
| `TestStreamingCard_ContainsHeader` | `streaming_card_test.go` | 创建的卡片 JSON 包含 wathet header |
| `TestClose_UpdatesHeaderToBlue` | `streaming_card_test.go` | Close 后 updateHeader 收到 `template: "blue"` |
| `TestAbort_UpdatesHeaderToGrey` | `streaming_card_test.go` | Abort 后 updateHeader 收到 `template: "grey"` |

---

## 7. 影响文件清单

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `internal/messaging/feishu/card_template.go` | **新建** | `cardHeader` 类型 + `toMap()` + `buildCard` + `buildStreamingCard` |
| `internal/messaging/feishu/card_template_test.go` | **新建** | header 和卡片模板测试 |
| `internal/messaging/feishu/adapter.go` | 修改 | `botName`/`botNameOnce` 字段、`resolveBotName()` 懒加载、`buildCardContent`/`buildTurnSummaryCard` 签名 + 委托 `buildCard`、`sendTextMessage`/`replyMessage` 调用方更新 |
| `internal/messaging/feishu/streaming.go` | 修改 | `agentName` 字段、`NewStreamingCardController` 签名扩展、`sendCardMessage` 委托 `buildStreamingCard`、新增 `updateHeader` + `ptrStr`、`Close`/`Abort` 状态流转、`flushIMPatch`/`flushIMPatchWithConfig` header 注入 |
| `internal/messaging/feishu/interaction.go` | 修改 | `buildInteractionCard` 签名 + 委托 `buildCard`、交互请求 header 参数 + body 精简 |
| `internal/messaging/feishu/adapter_helper_test.go` | 修改 | `buildCardContent` 测试更新 |
| `internal/messaging/feishu/interaction_test.go` | 修改 | `buildInteractionCard` 测试更新 |
| `internal/messaging/feishu/streaming_card_test.go` | 修改 | 新增 header 状态流转测试 |

---

## 8. 实施顺序

```
Step 1: card_template.go ────────────────────────────────
  ├─ 定义 cardHeader / cardTag 类型 + toMap()
  ├─ 实现 buildCard() + buildStreamingCard()
  ├─ card_template_test.go 单元测试
  └─ make test 验证

Step 2: Adapter botName 懒加载 ─────────────────────────
  ├─ Adapter 新增 botName + botNameOnce 字段
  ├─ 实现 resolveBotName()（sync.Once + API fallback）
  └─ make test 验证

Step 3: 非流式卡片构建器签名变更 ─────────────────────────
  ├─ buildCardContent 签名 + 委托 buildCard
  ├─ buildInteractionCard 签名 + 委托 buildCard
  ├─ buildTurnSummaryCard 签名 + 委托 buildCard
  ├─ 所有调用方传入 cardHeader
  ├─ 交互卡片 body 精简（移除类型标题行）
  ├─ 更新 adapter_helper_test.go / interaction_test.go
  └─ make quality 验证

Step 4: 流式卡片 header 状态流转 ─────────────────────────
  ├─ StreamingCardController 新增 agentName 字段
  ├─ NewStreamingCardController 签名扩展
  ├─ sendCardMessage 委托 buildStreamingCard（wathet header）
  ├─ 新增 updateHeader + ptrStr
  ├─ Close 末尾 updateHeader(blue)
  ├─ Abort 末尾 updateHeader(grey)
  ├─ flushIMPatch / flushIMPatchWithConfig header 注入
  ├─ streaming_card_test.go 新增状态流转测试
  └─ make quality 验证

Step 5: 端到端验证 ─────────────────────────────────────
  ├─ make check（完整 CI）
  └─ 手动 dev 环境验证卡片 header 显示
```

---

## 9. Card JSON 2.0 补充规格（调研备忘）

### 9.1 note 组件废弃说明

2.0 不兼容变更文档明确指出：**备注（note）组件和交互模块（"tag" 为 "action"）已废弃**。替代方案：
- note → `div` + `notation` 字号 + `grey` 字体颜色
- action → `button` 或 `overflow` 组件 + 合适的 `vertical_spacing` / `horizontal_spacing`

### 9.2 Body 布局属性（2.0 新增）

```json
{
  "body": {
    "direction": "vertical",
    "padding": "12px 8px 12px 8px",
    "horizontal_spacing": "3px",
    "horizontal_align": "left",
    "vertical_spacing": "4px",
    "vertical_align": "center",
    "elements": [...]
  }
}
```

元素级新增 `margin`（`[-99, 99]px`）和 `element_id`。

### 9.3 Config 补充字段

| 字段 | 默认值 | 说明 |
|------|--------|------|
| `width_mode` | `"default"` | `"default"` (600px) / `"compact"` (400px) / `"fill"` (撑满) |
| `streaming_config.print_strategy` | `"fast"` | `"fast"`: 新内容到达时立即刷完旧缓冲 / `"delay"`: 继续打字机效果 |
| `streaming_config.print_frequency_ms` | `70` | 渲染间隔 ms |
| `streaming_config.print_step` | `1` | 每次渲染字符数 |
| `style.text_size` | — | 自定义字号（命名引用） |
| `style.color` | — | 自定义颜色（RGBA + 明暗主题） |
