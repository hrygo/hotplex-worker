---
type: spec
tags:
  - project/HotPlex
  - gateway/stats
  - messaging/feishu
  - messaging/slack
  - worker/claude-code
date: 2026-04-19
status: draft
progress: 0
priority: medium
estimated_hours: 16
---

# Session Stats 展示规格书

> 版本: v1.0
> 日期: 2026-04-19
> 状态: Draft
> 源码验证: 已交叉验证 `claude-code-src/src/` 源码中 context window 计算公式、ModelUsage schema、SDK result 事件结构
> 目标: 在 Slack/Feishu 平台的 done 回复中展示会话级统计信息

---

## 1. 概述

### 1.1 目标

在每轮对话完成（done 事件）时，于 Slack/Feishu 平台展示当前会话的聚合统计信息，帮助用户了解资源消耗和上下文使用情况。

**展示内容**：

| 统计项 | 示例 | 数据来源 |
|--------|------|---------|
| 模型名称 | Sonnet 4.6 | `modelUsage` key |
| 会话时长 | 3m42s | `time.Since(startedAt)` |
| 对话轮数 | 5轮 | Bridge 累加 `TurnCount` |
| 工具调用次数 | 12次 | Bridge 累加 `ToolCallCount` |
| Token 消耗 | 45.2K in / 3.8K out | Worker `usage` 字段 |
| Context 窗口占用 | 24% ctx | `usage.input_tokens / modelUsage.contextWindow` |
| 累计费用 | $0.042 | Worker `total_cost_usd` / `cost` |

### 1.2 展示效果

**Feishu**（流式卡片底部）：

```
回复内容...
---
📊 Sonnet 4.6 · 🕐 3m42s · 🔄 5轮 · 🔧 12次工具
🪙 45.2K in / 3.8K out (24% ctx) · 💰 $0.042
```

**Slack**（done 后追加 Context Block）：

```
回复内容...

🕐 3m42s · 🔄 5 turns · 🔧 12 tools · 🪙 45.2K/3.8K tok (24% ctx) · 💰 $0.042
```

### 1.3 相关文档

- 用户交互: `docs/specs/Worker-User-Interaction-Spec.md` — 权限请求/问题询问（与本 spec 正交）
- Worker 会话控制: `docs/specs/Worker-Session-Control-Spec.md` — stdio 直达命令（context 查询等）
- 架构设计: `docs/specs/Worker-Common-Protocol.md` — Worker 公共协议层
- Claude Code 集成: `docs/specs/Worker-ClaudeCode-Spec.md` — SDK 输出格式
- Feishu 改进: `docs/specs/Feishu-Adapter-Improvement-Spec.md` — 卡片结构
- Slack 改进: `docs/specs/Slack-Adapter-Improvement-Spec.md` — 消息格式
- Claude Code 源码: `~/claude-code-src/src/utils/context.ts` — context window 计算公式

### 1.4 与 Worker-User-Interaction-Spec 的关系

本 spec（Session Stats）与 Worker-User-Interaction-Spec **完全正交**，解决不同问题：

| 维度 | Worker-User-Interaction-Spec | 本 Spec (Session Stats) |
|------|------------------------------|------------------------|
| 核心问题 | Agent **阻塞等待**用户输入（审批/问答） | **只读展示**资源消耗信息 |
| 交互方向 | 双向（Agent → 用户 → Agent） | 单向（Agent → 用户） |
| 触发时机 | 工具执行前 / Agent 提问时 | 每轮 done 事件时 |
| 用户行为 | 需主动操作（点击按钮/输入） | 被动接收，无需操作 |
| 不实施的后果 | Agent 无法执行需授权的工具，请求被丢弃 | 用户无法感知 token/费用消耗 |

实施顺序：**User-Interaction 优先**（功能性阻塞），本 spec 随后（体验增强）。两者改不同文件/不同逻辑路径，代码冲突风险极低。

---

## 2. 源码分析

### 2.1 Claude Code SDK Result 事件完整结构

> 来源: `~/claude-code-src/src/QueryEngine.ts:618-638`
> Schema: `~/claude-code-src/src/entrypoints/sdk/coreSchemas.ts:1407-1450`

Claude Code CLI 在 `--output-format stream-json` 模式下输出的 `result` 事件：

```json
{
  "type": "result",
  "subtype": "success",
  "duration_ms": 12450,
  "duration_api_ms": 11200,
  "is_error": false,
  "num_turns": 3,
  "result": "...",
  "session_id": "abc-123",
  "total_cost_usd": 0.042,
  "usage": {
    "input_tokens": 15234,
    "cache_creation_input_tokens": 8200,
    "cache_read_input_tokens": 0,
    "output_tokens": 3821,
    "server_tool_use": {
      "web_search_requests": 0,
      "web_fetch_requests": 0
    },
    "service_tier": "standard",
    "cache_creation": {
      "ephemeral_1h_input_tokens": 0,
      "ephemeral_5m_input_tokens": 0
    },
    "speed": "standard"
  },
  "modelUsage": {
    "claude-sonnet-4-6": {
      "inputTokens": 15234,
      "outputTokens": 3821,
      "cacheReadInputTokens": 0,
      "cacheCreationInputTokens": 8200,
      "webSearchRequests": 0,
      "costUSD": 0.042,
      "contextWindow": 200000,
      "maxOutputTokens": 16384
    }
  }
}
```

**关键发现**：`modelUsage` 中已包含 `contextWindow`（模型上下文窗口大小），无需硬编码。

### 2.2 OpenCode 源码深度分析

> 源码路径: `~/opencode/packages/opencode/src/`

#### 2.2.1 Token 计算方式（与 Claude Code 不同）

> 来源: `~/opencode/packages/opencode/src/session/session.ts:261-324`

OpenCode 对 input tokens 做了**预调整**，将 cache tokens 从 input 中分离：

```typescript
// OpenCode 的 tokens.input 不含 cache！
const adjustedInputTokens = inputTokens - cacheReadInputTokens - cacheWriteInputTokens

const tokens = {
  total,
  input: adjustedInputTokens,        // 已减去 cache
  output: outputTokens - reasoningTokens, // 已减去 reasoning
  reasoning: reasoningTokens,
  cache: {
    write: cacheWriteInputTokens,
    read: cacheReadInputTokens,
  },
}
```

**关键差异**：OpenCode 的 `tokens.input` 是**净 input**（不含 cache），而 Claude Code 的 `usage.input_tokens` 是**原始 input**。计算 context window 占用时，需要把 cache 加回去。

#### 2.2.2 CLI 模式 — NDJSON stdout

> 来源: `~/opencode/packages/opencode/src/cli/cmd/run.ts:429-490`

```typescript
// CLI emit 函数 (L429-435)
function emit(type: string, data: Record<string, unknown>) {
  if (args.format === "json") {
    process.stdout.write(
      JSON.stringify({ type, timestamp: Date.now(), sessionID, ...data }) + EOL
    )
    return true
  }
  return false
}

// step_finish 事件 (L488-490)
if (part.type === "step-finish") {
  if (emit("step_finish", { part })) continue
}
```

**CLI 输出格式**：

```json
{
  "type": "step_finish",
  "timestamp": 1234567890,
  "sessionID": "session-uuid",
  "part": {
    "id": "part-id",
    "type": "step-finish",
    "reason": "stop",
    "cost": 0.0234,
    "tokens": {
      "total": 12034,
      "input": 8400,
      "output": 3634,
      "reasoning": 0,
      "cache": {
        "read": 2000,
        "write": 500
      }
    }
  }
}
```

**事件类型列表**：`step_start`, `step_finish`, `text`, `reasoning`, `tool_use`, `error`

**注意**：CLI 的 `tokens` 结构使用**嵌套对象** `cache.read` / `cache.write`（hotplex parser 将其展平为 `cache_read` / `cache_write`）。

#### 2.2.3 Server 模式 — HTTP + SSE

> 来源: `~/opencode/packages/opencode/src/server/routes/instance/event.ts:12-88`

Server 模式使用 Bus 事件系统，通过 SSE `/event` 端点广播所有事件：

```typescript
// SSE 事件流
const unsub = Bus.subscribeAll((event) => {
  q.push(JSON.stringify(event))
})
for await (const data of q) {
  await stream.writeSSE({ data })
}
```

**Session 输入端点**：`POST /sessions/:sessionID/prompt_async`

Server 模式与 CLI 模式产生**完全相同的 Bus 事件**，包含相同的 `step_finish` 结构（tokens + cost）。hotplex 的 `opencodeserver` worker 通过 SSE 接收这些事件。

#### 2.2.4 Context Window 信息

> 来源: `~/opencode/packages/opencode/src/provider/provider.ts:858-862`

```typescript
const ProviderLimit = Schema.Struct({
  context: Schema.Number,    // 总 context window 大小
  output: Schema.Number,     // 最大 output tokens
})
```

**Context Window 大小不通过 step_finish 事件上报**。它在 OpenCode 内部使用（`session/overflow.ts` 中用于判断是否需要 compaction），但不序列化到 NDJSON 输出。

**Context Overflow 检测逻辑** (`session/overflow.ts:8-22`)：

```typescript
export function isOverflow(input) {
  const context = input.model.limit.context
  const count = input.tokens.total ||
    input.tokens.input + input.tokens.output +
    input.tokens.cache.read + input.tokens.cache.write
  const usable = context - maxOutputTokens(input.model)
  return count >= usable
}
```

**OpenCode 不计算 context window 使用百分比**。它只做 overflow 检测（布尔值），不报告具体比例。

#### 2.2.5 Cost 计算

> 来源: `~/opencode/packages/opencode/src/session/session.ts:300-320`

```typescript
const cost = new Decimal(0)
  .add(new Decimal(tokens.input).mul(costInfo.input).div(1_000_000))
  .add(new Decimal(tokens.output).mul(costInfo.output).div(1_000_000))
  .add(new Decimal(tokens.cache.read).mul(costInfo.cache.read).div(1_000_000))
  .add(new Decimal(tokens.cache.write).mul(costInfo.cache.write).div(1_000_000))
  .add(new Decimal(tokens.reasoning).mul(costInfo.output).div(1_000_000))
  .toNumber()
```

OpenCode 自行计算 cost（基于模型定价表），cost 值在 `step_finish.part.cost` 中上报。而 Claude Code 在 `result.total_cost_usd` 中上报。

#### 2.2.6 两种模式对比

| 维度 | CLI 模式 | Server 模式 |
|------|---------|------------|
| 传输 | stdout NDJSON (`--format json`) | HTTP + SSE (`/event`) |
| 事件来源 | `emit()` 函数直接写 stdout | `Bus.subscribeAll()` → SSE |
| step_finish | 含 tokens + cost | 相同事件，通过 SSE |
| context window | 不上报 | 不上报 |
| 输入端 | stdin 纯文本 | HTTP POST |

### 2.3 Context Window 百分比计算公式

#### Claude Code 公式

> 来源: `~/claude-code-src/src/utils/context.ts:118-144`

```typescript
const totalInputTokens =
  currentUsage.input_tokens +
  currentUsage.cache_creation_input_tokens +
  currentUsage.cache_read_input_tokens

const usedPercentage = Math.round(
  (totalInputTokens / contextWindowSize) * 100
)
const clampedUsed = Math.min(100, Math.max(0, usedPercentage))
```

**只算 input tokens（含 cache），不算 output_tokens**。

#### OpenCode 适配

由于 OpenCode 的 `tokens.input` 是调整后的值（已减去 cache），计算公式需要加回：

```
totalInput = tokens.input + tokens.cache.read + tokens.cache.write
contextPct = totalInput / contextWindowSize * 100
```

#### 统一公式

在 hotplex Bridge 层，两种 Worker 使用统一的计算逻辑：

```go
// 已在 mergePerTurnStats 中正确累加了各 Worker 的 TotalInput
// Claude Code: input_tokens + cache_creation + cache_read
// OpenCode:    input + cache.read + cache.write
// 因此 TotalInput 已经是 total input tokens（含 cache）

func (a *sessionAccumulator) computeContextPct() float64 {
    if a.ContextWindow <= 0 { return 0 }
    pct := float64(a.TotalInput) / float64(a.ContextWindow) * 100
    if pct > 100 { pct = 100 }
    if pct < 0 { pct = 0 }
    return pct
}
```

### 2.4 Context Window 大小来源汇总

| Worker | 来源 | 字段路径 | 精确度 |
|--------|------|---------|--------|
| **Claude Code** | `modelUsage[modelName].contextWindow` | `DoneData.Stats["model_usage"][name]["contextWindow"]` | 精确值 |
| **** | 不上报 | 无 | 使用内置映射表 |
| **OpenCode Server** | 不上报 | 无 | 使用内置映射表 |

内置映射表（兜底）：

```go
var defaultContextWindows = map[string]int64{
    "claude-sonnet-4-6":   200_000,
    "claude-opus-4-6":     200_000,
    "claude-haiku-4-5":    200_000,
    "claude-3.5-sonnet":   200_000,
    "gpt-4o":              128_000,
    "gpt-4.1":             1_047_576,
    "gemini-2.5-pro":      1_048_576,
    "default":             200_000,
}
```

### 2.5 当前数据流失点

| 层级 | 文件 | 问题 |
|------|------|------|
| **Mapper** | `claudecode/mapper.go:224` | 只传 `p.Stats` 到 `DoneData.Stats`，`p.Usage` 和 `p.ModelUsage` 被丢弃 |
| **Bridge** | `bridge.go:222-349` | `forwardEvents` 透传所有事件，无会话级聚合累加 |
| **Platform** | `feishu/adapter.go:488-508` | done 只清理 typing/reaction/关闭卡片，不渲染 stats |
| **Platform** | `slack/adapter.go:460-466` | done 只清除 status indicator，不渲染 stats |

---

## 3. 架构设计

### 3.1 数据流

```
┌──────────────────────────────────────────────────────────────┐
│                     Worker 进程                               │
│  result/step_finish 事件                                      │
│  Claude Code: usage + modelUsage (含 contextWindow)          │
│: tokens + cost                                  │
└──────────────────────┬───────────────────────────────────────┘
                       │ NDJSON stdout
                       ▼
┌──────────────────────────────────────────────────────────────┐
│              Worker Adapter (Parser + Mapper)                  │
│                                                                │
│  claudecode/mapper.go:mapResult                                │
│    [改动] 合并 Usage + ModelUsage 到 DoneData.Stats           │
│                                                                │
│  opencodeserver/worker.go:emitDone                                │
│    [不变] 已将 tokens + cost 放入 Stats                        │
│                                                                │
│  opencodeserver/worker.go                                      │
│    [不变] SSE 事件透传，DoneData 结构相同                      │
└──────────────────────┬───────────────────────────────────────┘
                       │ Envelope (events.Done)
                       ▼
┌──────────────────────────────────────────────────────────────┐
│              Bridge.forwardEvents                              │
│                                                                │
│  [新增] sessionAccumulator (per sessionID)                     │
│  ┌──────────────────────────────────────────────┐             │
│  │  on ToolCall:   ToolCallCount++               │             │
│  │  on Done:       mergePerTurnStats()           │             │
│  │                  TurnCount++                   │             │
│  │                injectSessionStats() → Stats   │             │
│  └──────────────────────────────────────────────┘             │
└──────────────────────┬───────────────────────────────────────┘
                       │ Enriched Envelope
                       ▼
┌──────────────────────────────────────────────────────────────┐
│              PlatformConn.WriteCtx                             │
│                                                                │
│  Feishu: 追加 stats footer 到流式卡片 → Close                  │
│  Slack:  done 后追加 Context Block 消息                        │
└──────────────────────────────────────────────────────────────┘
```

### 3.2 Stats Schema

使用 `DoneData.Stats` 中的约定 key `_session` 注入聚合统计。不需要新增事件类型。

```go
// DoneData.Stats["_session"] 的值结构
type SessionStatsSnapshot struct {
    // 会话元信息
    TurnCount     int     `json:"turn_count"`
    ToolCallCount int     `json:"tool_call_count"`
    Duration      string  `json:"duration"`          // "3m42s"
    DurationSecs  float64 `json:"duration_seconds"`  // 222.5

    // Token 统计
    TotalInputTokens  int64 `json:"total_input_tokens"`
    TotalOutputTokens int64 `json:"total_output_tokens"`

    // Context Window（精确值，来自 modelUsage.contextWindow）
    ContextWindow int64   `json:"context_window"`   // 200000
    ContextPct    float64 `json:"context_pct"`      // 24.1

    // 费用
    TotalCostUSD float64 `json:"total_cost_usd"`

    // 模型
    ModelName string `json:"model_name"` // "Sonnet 4.6"
}
```

### 3.3 Context Window 计算规则

严格对齐 Claude Code 源码公式：

```go
func (a *sessionAccumulator) computeContextPct() float64 {
    // 公式: (input_tokens + cache_creation + cache_read) / contextWindow * 100
    // 与 claude-code-src/src/utils/context.ts:calculateContextPercentages 完全一致
    totalInput := a.TotalInputTokens // 已含 cache tokens
    if a.ContextWindow <= 0 {
        return 0
    }
    pct := float64(totalInput) / float64(a.ContextWindow) * 100
    if pct > 100 { pct = 100 }
    if pct < 0 { pct = 0 }
    return pct
}
```

**Context Window 大小来源优先级**：

| 优先级 | 来源 | 适用 Worker |
|--------|------|-------------|
| 1 | `modelUsage[modelName].contextWindow` | Claude Code（精确值） |
| 2 | 内置模型窗口映射表 | / 兜底 |

内置映射表：

```go
var defaultContextWindows = map[string]int64{
    "claude-sonnet-4-6": 200_000,
    "claude-opus-4-6":   200_000,
    "claude-haiku-4-5":  200_000,
    "default":           200_000,
}
```

---

## 4. 详细设计

### 4.1 Phase 1: Mapper 修复 — 保留完整 Usage

**文件**: `internal/worker/claudecode/mapper.go`

**当前代码** (`mapResult`, 约第 218-229 行)：

```go
// 只传 p.Stats，丢了 p.Usage 和 p.ModelUsage
return []*events.Envelope{{
    Event: events.Event{
        Type: events.Done,
        Data: events.DoneData{Success: true, Stats: p.Stats},
    },
}}, nil
```

**改为**：

```go
func (m *Mapper) mapResult(p *ResultPayload) ([]*events.Envelope, error) {
    // 合并所有可用数据到 Stats
    stats := make(map[string]any, len(p.Stats)+2)
    for k, v := range p.Stats {
        stats[k] = v
    }
    if p.Usage != nil {
        stats["usage"] = p.Usage
    }
    if p.ModelUsage != nil {
        stats["model_usage"] = p.ModelUsage
    }

    if !p.Success {
        return []*events.Envelope{
            { /* error event (unchanged) */ },
            {
                Event: events.Event{
                    Type: events.Done,
                    Data: events.DoneData{Success: false, Stats: stats},
                },
            },
        }, nil
    }

    return []*events.Envelope{{
        Event: events.Event{
            Type: events.Done,
            Data: events.DoneData{Success: true, Stats: stats},
        },
    }}, nil
}
```

**影响范围**: 仅 `claudecode/mapper.go` 的 `mapResult` 方法

### 4.2 Phase 2: Bridge 累加器

**文件**: `internal/gateway/bridge.go`

#### 4.2.1 新增类型

```go
// sessionAccumulator tracks session-level statistics across turns.
type sessionAccumulator struct {
    TurnCount     int
    ToolCallCount int
    TotalCostUSD  float64
    TotalInput    int64 // input_tokens + cache_creation + cache_read
    TotalOutput   int64
    ContextWindow int64  // from modelUsage.contextWindow (0 = unknown)
    ModelName     string // first model seen
    StartedAt     time.Time
}
```

#### 4.2.2 Bridge 新增字段

```go
type Bridge struct {
    // ...existing fields...
    accum   map[string]*sessionAccumulator
    accumMu sync.Mutex
}
```

`NewBridge` 中初始化 `accum: make(map[string]*sessionAccumulator)`。

#### 4.2.3 累加逻辑

在 `forwardEvents` 的事件循环中，**在 Clone 之后、SendToSession 之前**插入：

```go
for env := range w.Conn().Recv() {
    // ...existing firstEvent/clone logic...

    // === Stats accumulation ===
    switch env.Event.Type {
    case events.ToolCall:
        acc := b.getOrInitAccum(sessionID)
        acc.ToolCallCount++

    case events.Done:
        acc := b.getOrInitAccum(sessionID)
        acc.mergePerTurnStats(env.Event.Data)
        acc.TurnCount++
        b.injectSessionStats(env, acc)
    }
    // ===========================

    // ...existing dropped delta / forward logic...
}
```

#### 4.2.4 mergePerTurnStats

从不同 Worker 格式中提取标准字段：

```go
func (a *sessionAccumulator) mergePerTurnStats(data any) {
    dd, ok := data.(events.DoneData)
    if !ok || dd.Stats == nil {
        return
    }

    // === Claude Code format ===
    if usage, ok := dd.Stats["usage"].(map[string]any); ok {
        a.TotalInput += toInt64(usage["input_tokens"]) +
            toInt64(usage["cache_creation_input_tokens"]) +
            toInt64(usage["cache_read_input_tokens"])
        a.TotalOutput += toInt64(usage["output_tokens"])
    }
    if modelUsage, ok := dd.Stats["model_usage"].(map[string]any); ok {
        for modelName, v := range modelUsage {
            if mu, ok := v.(map[string]any); ok {
                if a.ModelName == "" {
                    a.ModelName = shortModelName(modelName)
                }
                if cw := toInt64(mu["contextWindow"]); cw > 0 {
                    a.ContextWindow = cw
                }
            }
        }
    }

    // === format ===
    // OpenCode 源码 tokens.input 是调整后的值（已减去 cache），
    // 因此需加回 cache.read + cache.write 才是真正的 total input。
    // hotplex parser 已将 "cache.read"/"cache.write" 展平为 "cache_read"/"cache_write"
    // (参见 opencodeserver types)
    if tokens, ok := dd.Stats["tokens"].(map[string]any); ok {
        a.TotalInput += toInt64(tokens["input"]) +
            toInt64(tokens["cache_read"]) +
            toInt64(tokens["cache_write"])
        a.TotalOutput += toInt64(tokens["output"])
    }

    // === OpenCode Server SSE format ===
    // Server 模式通过 SSE 推送相同的 Bus 事件，hotplex opencodeserver worker
    // 解析后产生相同的 DoneData.Stats 结构。无需额外处理。
    // (参见 ~/opencode/packages/opencode/src/server/routes/instance/event.ts)

    // === Cost (both formats) ===
    // Claude Code: "total_cost_usd"
    ///Server: "cost" (自行计算，基于模型定价表)
    a.TotalCostUSD += toFloat64(dd.Stats["total_cost_usd"])
    a.TotalCostUSD += toFloat64(dd.Stats["cost"])
}
```

#### 4.2.5 injectSessionStats

将聚合 stats 注入 DoneData.Stats["_session"]：

```go
func (b *Bridge) injectSessionStats(env *events.Envelope, acc *sessionAccumulator) {
    dd, ok := env.Event.Data.(events.DoneData)
    if !ok {
        return
    }
    if dd.Stats == nil {
        dd.Stats = make(map[string]any)
    }

    ctxPct := acc.computeContextPct()
    dd.Stats["_session"] = map[string]any{
        "turn_count":       acc.TurnCount,
        "tool_call_count":  acc.ToolCallCount,
        "duration":         time.Since(acc.StartedAt).Round(time.Second).String(),
        "duration_seconds": time.Since(acc.StartedAt).Seconds(),
        "total_input_tok":  acc.TotalInput,
        "total_output_tok": acc.TotalOutput,
        "context_window":   acc.ContextWindow,
        "context_pct":      ctxPct,
        "total_cost_usd":   acc.TotalCostUSD,
        "model_name":       acc.ModelName,
    }
    env.Event.Data = dd
}
```

#### 4.2.6 生命周期管理

```go
func (b *Bridge) getOrInitAccum(sessionID string) *sessionAccumulator {
    b.accumMu.Lock()
    defer b.accumMu.Unlock()
    if acc, ok := b.accum[sessionID]; ok {
        return acc
    }
    acc := &sessionAccumulator{StartedAt: time.Now()}
    b.accum[sessionID] = acc
    return acc
}
```

在 `cleanupCrashedWorker` 或 session 终止时清理 accumulator：

```go
func (b *Bridge) deleteAccum(sessionID string) {
    b.accumMu.Lock()
    delete(b.accum, sessionID)
    b.accumMu.Unlock()
}
```

### 4.3 Phase 3: Feishu 渲染

**文件**: `internal/messaging/feishu/adapter.go`

#### 4.3.1 Done 事件处理增强

当前 `WriteCtx` (`adapter.go:488-508`) 在 done 时只清理资源。改为先追加 stats 再关闭：

```go
if env.Event.Type == events.Done {
    // ...existing typing/reaction cleanup...

    // 追加 stats footer 到流式卡片
    if streamCtrl != nil && streamCtrl.IsCreated() {
        if ss := extractSessionStats(env); ss != nil {
            footer := formatFeishuStatsFooter(ss)
            _ = streamCtrl.AppendContent(footer)
        }
        return streamCtrl.Close(ctx)
    }
    return nil
}
```

#### 4.3.2 Stats Footer 格式化

```go
func formatFeishuStatsFooter(ss map[string]any) string {
    modelName, _ := ss["model_name"].(string)
    duration, _ := ss["duration"].(string)
    turnCount := toInt(ss["turn_count"])
    toolCount := toInt(ss["tool_call_count"])
    inputTok := toInt64(ss["total_input_tok"])
    outputTok := toInt64(ss["total_output_tok"])
    ctxPct, _ := ss["context_pct"].(float64)
    cost, _ := ss["total_cost_usd"].(float64)

    return fmt.Sprintf("\n\n---\n📊 **%s** · 🕐 %s · 🔄 %d轮 · 🔧 %d次工具\n"+
        "🪙 %s in / %s out (%.0f%% ctx) · 💰 $%.4f",
        modelName, duration, turnCount, toolCount,
        formatTokenCount(inputTok), formatTokenCount(outputTok),
        ctxPct, cost)
}
```

#### 4.3.3 Streaming Controller 扩展

**文件**: `internal/messaging/feishu/streaming.go`

需确认 `streamCtrl` 是否已有追加内容的方法。若没有，需新增 `AppendContent(content string) error`，在关闭前向卡片 markdown element 追加内容。

### 4.4 Phase 4: Slack 渲染

**文件**: `internal/messaging/slack/adapter.go`

#### 4.4.1 Done 事件处理增强

当前 `WriteCtx` (`adapter.go:460-466`) 在 done 时只清除 status。改为追加 stats 消息：

```go
case events.Done, events.Error:
    _ = c.adapter.statusMgr.Clear(ctx, c.channelID, c.threadTS)
    c.adapter.activeIndicators.Stop(ctx, c.channelID, c.messageTS)

    // done 时追加 stats block
    if env.Event.Type == events.Done {
        if ss := extractSessionStats(env); ss != nil {
            _ = c.postStatsBlock(ctx, ss)
        }
    }
    return nil
```

#### 4.4.2 Stats Block 消息

```go
func (c *SlackConn) postStatsBlock(ctx context.Context, ss map[string]any) error {
    duration, _ := ss["duration"].(string)
    turnCount := toInt(ss["turn_count"])
    toolCount := toInt(ss["tool_call_count"])
    inputTok := toInt64(ss["total_input_tok"])
    outputTok := toInt64(ss["total_output_tok"])
    ctxPct, _ := ss["context_pct"].(float64)
    cost, _ := ss["total_cost_usd"].(float64)

    text := fmt.Sprintf(
        "🕐 %s · 🔄 %d turns · 🔧 %d tools · 🪙 %s/%s tok (%.0f%% ctx) · 💰 $%.4f",
        duration, turnCount, toolCount,
        formatTokenCount(inputTok), formatTokenCount(outputTok),
        ctxPct, cost,
    )

    blocks := []slack.Block{
        slack.NewContextBlock(
            "stats",
            slack.NewTextBlockObject("mrkdwn", text, false, nil),
        ),
    }

    _, _, err := c.adapter.client.PostMessageContext(ctx, c.channelID,
        slack.MsgOptionBlocks(blocks...),
        slack.MsgOptionTS(c.threadTS),
    )
    return err
}
```

### 4.5 辅助函数

在 `internal/gateway/` 或 `pkg/events/` 下新增工具函数（可选提取到独立文件）：

```go
func toInt64(v any) int64 {
    switch n := v.(type) {
    case float64:
        return int64(n)
    case int:
        return int64(n)
    case int64:
        return n
    case json.Number:
        i, _ := n.Int64()
        return i
    default:
        return 0
    }
}

func toFloat64(v any) float64 {
    switch n := v.(type) {
    case float64:
        return n
    case int:
        return float64(n)
    case int64:
        return float64(n)
    default:
        return 0
    }
}

func shortModelName(full string) string {
    switch {
    case strings.Contains(full, "sonnet"):
        return "Sonnet"
    case strings.Contains(full, "opus"):
        return "Opus"
    case strings.Contains(full, "haiku"):
        return "Haiku"
    default:
        return full
    }
}

func formatTokenCount(n int64) string {
    switch {
    case n >= 1_000_000:
        return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
    case n >= 1_000:
        return fmt.Sprintf("%.1fK", float64(n)/1_000)
    default:
        return fmt.Sprintf("%d", n)
    }
}

func extractSessionStats(env *events.Envelope) map[string]any {
    dd, ok := env.Event.Data.(events.DoneData)
    if !ok {
        return nil
    }
    ss, ok := dd.Stats["_session"]
    if !ok {
        return nil
    }
    m, ok := ss.(map[string]any)
    if !ok {
        return nil
    }
    return m
}
```

---

## 5. 改动范围

| 文件 | 改动类型 | 描述 |
|------|---------|------|
| `internal/worker/claudecode/mapper.go` | 修改 | `mapResult` 合并 Usage + ModelUsage |
| `internal/gateway/bridge.go` | 修改 | 新增 `sessionAccumulator` + 累加 + 注入 |
| `internal/gateway/stats.go` | 新增 | 辅助函数 (toInt64, formatTokenCount, extractSessionStats 等) |
| `worker.go` | 不变 | 已将 tokens + cost 放入 Stats |
| `internal/worker/opencodeserver/worker.go` | 不变 | SSE 透传，DoneData 结构相同 |
| `internal/messaging/feishu/adapter.go` | 修改 | done 处理增加 stats footer |
| `internal/messaging/feishu/streaming.go` | 可能修改 | `AppendContent` 方法（如不存在） |
| `internal/messaging/slack/adapter.go` | 修改 | done 处理增加 stats block 消息 |

**不需要改动的文件**：
- `pkg/events/events.go` — `DoneData.Stats` 已是 `map[string]any`，无需新增类型
- `internal/worker/claudecode/parser.go` — 已正确提取 Usage/ModelUsage
- `*` — 已将 tokens/cost 放入 Stats
- `internal/session/manager.go` — 不持久化 stats，纯内存聚合

---

## 6. 测试策略

### 6.1 单元测试

| 测试 | 文件 | 描述 |
|------|------|------|
| `TestMapResultStatsMerge` | `claudecode/mapper_test.go` | 验证 Usage/ModelUsage 合入 DoneData.Stats |
| `TestMergePerTurnStats` | `bridge_test.go` | Claude Code 格式 + 格式提取 |
| `TestComputeContextPct` | `bridge_test.go` | 对齐 Claude Code 源码公式：只算 input，clamped 0-100 |
| `TestFormatTokenCount` | `stats_test.go` | K/M 格式化边界 |
| `TestExtractSessionStats` | `stats_test.go` | 从 Envelope 提取 _session map |
| `TestFormatFeishuStatsFooter` | `feishu/adapter_test.go` | 渲染格式正确 |
| `TestPostStatsBlock` | `slack/adapter_test.go` | Slack block 构造正确 |

### 6.2 Context Window 计算对齐测试

```go
func TestContextPctMatchesClaudeCode(t *testing.T) {
    // 使用 Claude Code 源码相同的测试数据
    tests := []struct {
        name           string
        inputTokens    int64
        cacheCreation  int64
        cacheRead      int64
        contextWindow  int64
        wantPct        float64
    }{
        {"zero usage", 0, 0, 0, 200000, 0},
        {"50% usage", 100000, 0, 0, 200000, 50},
        {"with cache", 50000, 30000, 20000, 200000, 50},
        {"over 100%", 150000, 80000, 0, 200000, 100}, // clamped
        {"no window", 50000, 0, 0, 0, 0},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            acc := &sessionAccumulator{
                TotalInput:   tt.inputTokens + tt.cacheCreation + tt.cacheRead,
                ContextWindow: tt.contextWindow,
            }
            got := acc.computeContextPct()
            require.Equal(t, tt.wantPct, got)
        })
    }
}
```

### 6.3 集成测试

通过 noop worker 端到端验证：
1. 创建 session → 发送 input → 模拟 worker 返回带 usage 的 done
2. 验证 forwardEvents 注入了 `_session` 到 DoneData.Stats
3. 验证 Feishu/Slack mock conn 收到正确的 stats 格式

---

## 7. 实施顺序

```
Phase 1: Mapper 修复 (1h)
  ├─ claudecode/mapper.go: 合并 Usage + ModelUsage
  └─ mapper_test.go: 验证合并正确

Phase 2: Bridge 累加器 (4h)
  ├─ bridge.go: sessionAccumulator + merge + inject
  ├─ stats.go: 辅助函数
  ├─ bridge_test.go: 聚合 + context pct 测试
  └─ 确保 noop worker 测试通过

Phase 3: Feishu 渲染 (4h)
  ├─ streaming.go: AppendContent (如需)
  ├─ adapter.go: done → stats footer
  └─ adapter_test.go: 格式化测试

Phase 4: Slack 渲染 (3h)
  ├─ adapter.go: done → stats block
  └─ adapter_test.go: block 构造测试

Phase 5: 集成验证 (2h)
  ├─ make test (race)
  ├─ make lint
  └─ E2E 验证
```

---

## 8. 风险与缓解

| 风险 | 影响 | 缓解 |
|------|------|------|
| `modelUsage` 为空（极旧版本 Claude Code） | 无 context window 值 | 使用内置映射表兜底 |
| OpenCode `tokens.input` 含义不同 | 误算 context 占用 | `mergePerTurnStats` 已加回 cache.read + cache.write |
| `Stats["_session"]` key 冲突 | 覆盖用户数据 | `_` 前缀为约定私有命名空间，Worker 不会使用 |
| 流式卡片追加内容失败 | Feishu 不显示 stats | 非关键路径，`_ = streamCtrl.AppendContent()` |
| Slack rate limit | stats block 发送失败 | 非关键路径，仅 log warn |
| OpenCode Server 无 stats | 全部显示为 0 | 可接受，OpenCode Server 当前无统计上报 |

---

## 9. 未来扩展

- **WebSocket 客户端**：webchat/SDK 客户端同样可从 `_session` 提取 stats，在前端展示
- **持久化**：将 `sessionAccumulator` 快照持久化到 SQLite `sessions` 表，支持 admin API 查询
- **OpenCode Server**：如后续版本支持 stats 上报，在 `mergePerTurnStats` 中新增对应格式解析。OpenCode 源码 `provider/provider.ts:ProviderLimit.context` 已有 context window 大小，未来可能通过 SSE 事件暴露
- **`tool_use_summary` 事件**：Claude Code SDK 已定义此事件类型，未来可解析以获取按工具分类的调用统计
