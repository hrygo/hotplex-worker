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

### 3.3 turn_summary.go

**新建** `internal/messaging/turn_summary.go`。

```go
package messaging

type TurnSummaryData struct {
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
}

func ExtractTurnSummary(env *events.Envelope) TurnSummaryData { ... }
func FormatTurnSummary(d TurnSummaryData) string { ... }
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

## 4. WebChat 前端

无需后端改动。前端从 `DoneData.Stats["_session"]` 渲染。

扩展后的 `_session` 数据供前端展示：

```
Context: 🟢 24% [██████░░░░] 48K/200K
Model: Sonnet 4 | Turn: 3
Input: ~12K tokens | Output: ~2K tokens
Tools: Read × 5, Bash × 3, Edit × 2, Grep × 2
Duration: 42s | Cost: $0.04 (Total: $0.12)
```

前端可自行决定渲染粒度和展示风格。

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
