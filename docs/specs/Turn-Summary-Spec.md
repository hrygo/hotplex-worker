---
type: spec
tags:
  - project/HotPlex
date: 2026-05-01
status: draft
progress: 70
---
# Turn Summary Spec — Issue #117

每轮 Done 时，向用户发送本轮摘要信息。

---

## 1. 数据源

### 1.1 `_session` 快照扩展

在 `sessionAccumulator.snapshot()` 输出中新增以下 key：

| Key | 类型 | 来源 | 说明 |
|-----|------|------|------|
| `turn_duration_ms` | int64 | `time.Since(turnStartTime)` | 本轮耗时（毫秒） |
| `turn_input_tok` | int64 | `PerTurnInput` | 本轮输入 token（delta） |
| `turn_output_tok` | int64 | `PerTurnOutput` | 本轮输出 token（delta） |
| `turn_cost_usd` | float64 | `PerTurnCost` | 本轮花费（delta） |
| `tool_names` | map[string]int | `ToolNames` | 工具名 → 调用次数 |

已有字段不变：

| Key | 类型 | 说明 |
|-----|------|------|
| `turn_count` | int | 累计轮次 |
| `tool_call_count` | int | 本轮工具调用总数 |
| `total_input_tok` | int64 | 累计输入 token |
| `total_output_tok` | int64 | 累计输出 token |
| `context_window` | int64 | context 窗口大小（0 = 未知） |
| `context_pct` | float64 | context 使用率 0-100 |
| `total_cost_usd` | float64 | 累计花费 |
| `model_name` | string | 模型短名 |
| `duration` | string | session 累计耗时 |
| `duration_seconds` | float64 | session 累计耗时（秒） |

### 1.2 提取函数

```go
// ExtractTurnSummary 从 Done envelope 提取 _session 数据。
// 处理 events.Clone JSON round-trip 后的 map[string]any 类型。
func ExtractTurnSummary(env *events.Envelope) TurnSummaryData
```

Envelope 经 `events.Clone` 后，`Event.Data` 为 `map[string]any`。
提取路径：`env.Event.Data.(map[string]any)["stats"].(map[string]any)["_session"].(map[string]any)`。

数值类型统一用 `toInt64` / `toFloat64` 辅助函数转换（JSON 反序列化后为 float64）。

### 1.3 Worker 兼容性

| Worker | `context_pct` | `model_name` | `tool_call_count` | `turn_duration_ms` |
|--------|---------------|--------------|-------------------|---------------------|
| Claude Code | ✅ 完整 | ✅ "Sonnet"/"Opus"/"Haiku" | ✅ | ✅ |
| OCS | ⚠️ 0（无 contextWindow） | ⚠️ 可能为空 | ✅ | ✅ |
| Pi | ❌ noop worker | — | — | — |

字段缺失时格式化函数跳过该段，不显示占位符。

---

## 2. 格式化规则

### 2.1 统一格式（Slack / 飞书）

```
{icon} Context {pct}% ({used}/{max}) | {model} | 🛠 {count} tools | ⏱ {duration} | ${cost}
```

**示例输出**：

```
🟢 Context 24% (48K/200K) | Sonnet | 🛠 12 tools | ⏱ 42s | $0.04
```

**Severity icon 映射**（复用 `context_format.go`）：

| Context % | Icon | 级别 |
|-----------|------|------|
| 0-49% | 🟢 | Comfortable |
| 50-75% | 🟡 | Moderate |
| 76-90% | 🟠 | High |
| 91-100% | 🔴 | Critical |
| 无数据 | ⚪ | — |

**各段格式规则**：

| 段 | 条件 | 格式 |
|----|------|------|
| Context | `context_window > 0` | `{icon} Context {pct}% ({used}/{max})` |
| Context（无 window） | `total_input_tok > 0 && context_window == 0` | `{icon} Context {used} tokens` |
| Model | `model_name != ""` | `{model_name}` |
| Tools | `tool_call_count > 0` | `🛠 {count} tools` |
| Duration | `turn_duration_ms > 0` | `⏱ {human_duration}` |
| Cost | `turn_cost_usd >= 0.01` | `${cost}` |

**时长格式化**：

| 范围 | 格式 | 示例 |
|------|------|------|
| < 1s | `{ms}ms` | `420ms` |
| < 60s | `{s}s` | `42s` |
| < 60m | `{m}m{s}s` | `3m42s` |
| ≥ 60m | `{h}h{m}m` | `1h23m` |

**Token 格式化**（复用 `context_format.go.FormatTokenCount`）：

| 值 | 格式 |
|----|------|
| < 1000 | `999` |
| 整千 | `48K` |
| 非整千 | `~48.4K` |

**Cost 格式化**：

| 值 | 格式 |
|----|------|
| < 0.01 | 不显示 |
| < 1 | `$0.04` |
| ≥ 1 | `$1.23` |

### 2.2 降级场景

**OCS（无 context window）**：
```
⚪ Context ~48K tokens | Sonnet | 🛠 5 tools | ⏱ 12s
```

**无 context 数据、有 model**：
```
Sonnet | 🛠 3 tools | ⏱ 8s
```

**全部无数据（Pi noop）**：
```
（空字符串，不发送消息）
```

---

## 3. 后端变更

### 3.1 session_stats.go

**新增字段**：

```go
type sessionAccumulator struct {
    // ... existing fields ...
    TurnDurationMs int64  // 本轮耗时（毫秒）
}
```

**snapshot() 新增 key**：

```go
func (a *sessionAccumulator) snapshot() map[string]any {
    // ... existing ...
    "turn_duration_ms": a.TurnDurationMs,
    "turn_input_tok":   a.PerTurnInput,
    "turn_output_tok":  a.PerTurnOutput,
    "turn_cost_usd":    a.PerTurnCost,
    "tool_names":       a.ToolNames,
}
```

### 3.2 bridge.go

**Done case 内，提前计算 per-turn 数据**：

```go
case events.Done:
    if turnTimer != nil {
        turnTimer.Stop()
    }
    acc := b.getOrInitAccum(sessionID)
    if dd, ok := asDoneData(env.Event.Data); ok {
        acc.mergePerTurnStats(dd)
    }
    acc.TurnCount++
    acc.TurnDurationMs = time.Since(turnStartTime).Milliseconds()  // ← 新增
    acc.computePerTurnDeltas()                                       // ← 提前调用
    b.injectSessionStats(env, acc)                                   // snapshot 包含完整数据
    // ... rest unchanged ...
```

`resetPerTurn()` 保持不变（在 conversation store 写入后调用）。
`computePerTurnDeltas()` 是纯计算，提前调用安全。

### 3.3 turn_summary.go ✅ Implemented

**文件**: `internal/messaging/turn_summary.go`

实际 `TurnSummaryData` 包含比 spec 更多的字段：

```go
type TurnSummaryData struct {
    // Spec 定义字段
    ContextPct     float64
    ContextWindow  int64
    TotalInputTok  int64
    ModelName      string
    ToolCallCount  int
    ToolNames      map[string]int
    TurnDurationMs int64
    TurnCount      int
    TurnInputTok   int64
    TurnOutputTok  int64
    TurnCostUSD    float64
    TotalCostUSD   float64

    // 额外实现字段
    ContextFill     int64
    TotalOutputTok  int64
    SessionDuration string
    WorkDir         string
    GitBranch       string
}

func ExtractTurnSummary(env *events.Envelope) TurnSummaryData { ... }
func FormatTurnSummary(d TurnSummaryData) string { ... }      // 紧凑单行
func FormatTurnSummaryRich(d TurnSummaryData) string { ... }  // 多行（Slack fallback）
func TruncatePath(p string) string { ... }
func FormatSessionDuration(d string) string { ... }
```

**复用已有工具**（`context_format.go`）：
- `SeverityLevel(pct)` → severity
- `SeverityIcon(severity)` → emoji
- `FormatTokenCount(tokens)` → "48K"

### 3.4 Slack adapter

**WriteCtx Done case**：

```go
case events.Done, events.Error:
    c.clearStatus(ctx)
    c.adapter.Interactions.CancelAll(env.SessionID)
    c.closeStreamWriter()
    if env.Event.Type == events.Done {
        go c.sendTurnSummary(ctx, env)
    }
    if env.Event.Type == events.Error {
        // ... existing error handling ...
    }
    return nil
```

**sendTurnSummary 方法**（遵循 `sendContextUsage` 模式）：

```go
func (c *SlackConn) sendTurnSummary(ctx context.Context, env *events.Envelope) {
    d := messaging.ExtractTurnSummary(env)
    text := messaging.FormatTurnSummary(d)
    if text == "" {
        return
    }
    opts := []slack.MsgOption{slack.MsgOptionText(text, false)}
    if c.threadTS != "" {
        opts = append(opts, slack.MsgOptionTS(c.threadTS))
    }
    _, _, _ = c.adapter.client.PostMessageContext(ctx, c.channelID, opts...)
}
```

### 3.5 Feishu adapter

**WriteCtx Done case**（重构，保留 Close 错误传播）：

```go
case events.Done:
    streamCtrl := c.clearActiveIndicators(ctx)
    c.adapter.Interactions.CancelAll(env.SessionID)
    var closeErr error
    if streamCtrl != nil && streamCtrl.IsCreated() {
        closeErr = streamCtrl.Close(ctx)
    }
    go c.sendTurnSummary(ctx, env)
    return closeErr
```

**sendTurnSummary 方法**（遵循 `sendContextUsage` 模式）：

```go
func (c *FeishuConn) sendTurnSummary(ctx context.Context, env *events.Envelope) {
    d := messaging.ExtractTurnSummary(env)
    text := messaging.FormatTurnSummary(d)
    if text == "" {
        return
    }
    c.mu.RLock()
    replyToMsgID := c.replyToMsgID
    chatID := c.chatID
    c.mu.RUnlock()
    if replyToMsgID != "" {
        _ = c.adapter.replyMessage(ctx, replyToMsgID, text, false)
    } else {
        _ = c.adapter.sendTextMessage(ctx, chatID, text)
    }
}
```

---

## 4. WebChat 前端 ✅ Implemented

### 4.1 数据流

后端无需额外改动。`_session` 数据通过 AEP `done` 事件到达前端：

```
Gateway → done envelope → BrowserHotPlexClient._routeEvent()
  → handleDone(data) → data.stats._session → TurnSummaryCard
```

### 4.2 类型定义

`webchat/lib/ai-sdk-transport/client/types.ts` 扩展 `DoneStats`：

```typescript
export interface DoneStats {
  // ... existing fields ...
  _session?: TurnSessionStats;
}

export interface TurnSessionStats {
  turn_count: number;
  tool_call_count: number;
  model_name: string;
  context_pct: number;
  context_window: number;
  context_fill: number;
  total_input_tok: number;
  total_output_tok: number;
  total_cost_usd: number;
  turn_duration_ms: number;
  turn_input_tok: number;
  turn_output_tok: number;
  turn_cost_usd: number;
  tool_names: Record<string, number> | null;
  duration: string;
  duration_seconds: number;
  work_dir: string;
  git_branch: string;
}
```

### 4.3 Adapter 注入

`webchat/lib/adapters/hotplex-runtime-adapter.ts`：
- `MessagePart` union 包含 `TurnSummaryPart`（type: `turn-summary`, data: `TurnSessionStats`）
- `handleDone` 中提取 `_session` 数据，追加 `turn-summary` part 到最后一条 assistant message
- `adapterMessages` filter 过滤 `turn-summary` 类型（同 `context-usage` 逻辑）
- `convertToThreadMessage` 通过 `metadata.turnSummary` 传递数据

### 4.4 TurnSummaryCard 组件

`webchat/components/assistant-ui/TurnSummaryCard.tsx`：
- 遵循 `ContextUsageCard.tsx` 的动画和样式模式
- severity coloring（复用 context-format severity 逻辑）
- 紧凑布局：Model · Context bar · Tools · Duration · Cost

### 4.5 渲染集成

`webchat/components/assistant-ui/thread.tsx` `AssistantMessage` 中，在 `ContextUsageCard` 之后渲染 `TurnSummaryCard`。

---

## 5. WebChat Dedup & Filtering

`webchat/lib/adapters/hotplex-runtime-adapter.ts` 中存在三层去重/过滤机制：

### 5.1 Server History Merge Dedup

加载服务端历史消息时，与本地 live 消息进行双层去重：

- **ID-based**：以服务端消息 ID 集合为准，丢弃匹配的本地消息
- **Content-signature-based**：对 `role:text` 内容签名匹配的本地消息丢弃（处理本地 `user-${Date.now()}` ID 与服务端 ID 不一致的场景）

合并顺序：`[...serverMessages, ...liveOnly]`，服务端消息优先。

### 5.2 Output Dedup for assistant-ui

`adapterMessages` 使用 `useMemo` + `Set<string>` 过滤重复 ID 消息，防止 assistant-ui `MessageRepository` "same id already exists" 错误。`handleMessage` 和 `handleDelta+handleDone` 可为同一逻辑内容创建消息，此去重层确保不重复。

### 5.3 Context-usage Message Filtering

纯 `context-usage` 类型 part 的消息从 adapter 输出中过滤。`context_usage` 事件通过 `handleContextUsage` 创建独立消息，`ContextUsageCard` 通过 `metadata.contextUsage` 渲染（不作为可见对话消息）。不过滤会导致空 assistant 消息出现。

---

## 5. 测试覆盖

### 5.1 turn_summary_test.go

| 测试 | 场景 |
|------|------|
| `TestExtractTurnSummary_Full` | 全部字段有值，验证每个字段正确提取 |
| `TestExtractTurnSummary_NilSession` | `_session` 不存在，返回零值 |
| `TestExtractTurnSummary_NilStats` | `Stats` 为 nil |
| `TestFormatTurnSummary_Full` | 完整输出：`🟢 Context 24% (48K/200K) \| Sonnet \| 🛠 12 tools \| ⏱ 42s \| $0.04` |
| `TestFormatTurnSummary_NoContext` | 无 context window：`Sonnet \| 🛠 5 tools \| ⏱ 12s` |
| `TestFormatTurnSummary_NoModel` | 有 context 无 model |
| `TestFormatTurnSummary_NoTools` | 零工具调用，tools 段不显示 |
| `TestFormatTurnSummary_Minimal` | 仅 duration |
| `TestFormatTurnSummary_Empty` | 全部无数据，返回空字符串 |
| `TestFormatTurnSummary_DurationFormats` | 覆盖 ms/s/m+s/h+m 格式 |
| `TestFormatTurnSummary_CostThreshold` | cost < $0.01 不显示 |

### 5.2 session_stats 验证

`snapshot()` 包含新增 key，`computePerTurnDeltas` 提前调用结果一致。

---

## 6. 实施顺序

1. `session_stats.go` — 新增字段 + 扩展 snapshot
2. `bridge.go` — 提前计算 turnDuration + perTurnDeltas
3. `turn_summary.go` + `turn_summary_test.go` — 提取 + 格式化 + 测试
4. `slack/adapter.go` — Done case + sendTurnSummary
5. `feishu/adapter.go` — Done case + sendTurnSummary
6. `make lint && make test` 验证

---

## 7. 配置开关

Turn summary 可通过配置开关控制是否发送，默认开启。

### 7.1 配置方式

**YAML**（`configs/config.yaml`）：

```yaml
messaging:
  turn_summary_enabled: false  # 关闭 turn summary 输出
```

**环境变量**：

```bash
HOTPLEX_MESSAGING_TURN_SUMMARY_ENABLED=false
```

### 7.2 实现位置

| 文件 | 说明 |
|------|------|
| `internal/config/config.go` | `MessagingConfig.TurnSummaryEnabled` 字段定义、默认值 `true`、环境变量映射 |
| `cmd/hotplex/messaging_init.go` | 注入到 `AdapterConfig.Extras["turn_summary_enabled"]` |
| `internal/messaging/slack/adapter.go` | `Adapter.turnSummaryEnabled` + WriteCtx 门控 |
| `internal/messaging/feishu/adapter.go` | `Adapter.turnSummaryEnabled` + WriteCtx 门控 |

关闭时，Slack 不触发 `sendTurnSummary` goroutine；飞书保留 `Log.Info` 遥测日志但不发送卡片。
