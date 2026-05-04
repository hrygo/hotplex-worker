---
type: spec
tags:
  - project/HotPlex
  - gateway/bridge
  - eventstore
  - performance
date: 2026-05-04
status: proposed
priority: high
estimated_hours: 8
---

# Delta 三渠道积累优化规格书

> 版本: v2.0
> 日期: 2026-05-04
> 状态: Proposed
> 分支: refactor/bridge-decompose

---

## 1. 概述

### 1.1 背景

Gateway 的 delta（message.delta）事件在三个渠道中流动：WebChat (Hub)、Platform (PlatformWriter)、EventStore (Collector)。前两个渠道已完成调优，EventStore Collector 存在三个问题：性能浪费、crash 丢数据窗口、retry 后 accumulator 残留。

**核心设计原则**: Delta 归并不要求一个 turn 必须归并为一条 Message。它是时间（timer）、大小（size）、性能（DB 写入压力）、回放 UX（渐进呈现）的综合权衡。参数需适配 EventStore 的消费者特征（磁盘写入、无实时观看、全量持久化）。

### 1.2 三渠道架构对比

| 维度 | WebChat (Hub) | Platform (pcEntry) | EventStore (Collector) |
|------|--------------|-------------------|----------------------|
| 策略 | 直通 | Timer+Size 合并 | **三重触发** Size+Timer+Event 积累 |
| 延迟 | ~0ms | 0–120ms | size 达标即时 / timer 2s / event MessageEnd |
| 丢容忍 | 高 (droppable) | 高 (droppable) | 零 (必须持久化) |
| 缓冲 | 256 (broadcast) | 64 (write) | 2048 (capture) |
| 丢弃阈值 | channel 满 | ≥56/64 (87.5%) | channel 满 (captureC) |
| 合并参数 | 无 | 120ms / 200 runes | 4096 bytes / 2s / MessageEnd |
| 消费者 | Browser WS | Slack/飞书 API | SQLite (replay/recovery) |
| 调优状态 | 已调优 | 已调优 (UX 调研) | **待修复** |

### 1.3 WebChat 渠道（不变）

Delta 实时直传到浏览器，打字效果依赖低延迟。Hub backpressure 正确标记 MessageDelta 为 droppable，channel 满时静默丢弃，Done 时通过 `dropped` 标志触发 fallback 全文补偿。

### 1.4 Platform 渠道（不变）

pcEntry writeLoop 的合并参数经 UX 调研确定：
- 120ms 间隔 — 自然流畅的视觉更新节奏
- 200 runes — 一段完整表达，避免碎片化
- 56/64 丢弃水位 (87.5%) — 给 Slack/飞书 API 限流留足余量

---

## 2. EventStore Collector 问题分析

### 2.1 问题 A: 每个 Delta 多余 2× JSON 往返

**现状**: `bridge_forward.go:188` 对所有事件（含 MessageDelta）调用 `captureEvent`：

```
captureEvent
  → json.Marshal(Event.Data)        // 序列化完整 Event.Data
  → Collector.Capture
    → json.Unmarshal(data, &delta)  // 反序列化提取 content
    → appendRaw(seq, delta.Content)
```

`extractMessageContent` (行 111-115) 已经提取了 `content` 字符串用于 `turnText`。Collector 有 `CaptureDeltaString` 快路径（零序列化），却从未被调用。

**影响**: 典型 LLM 响应 ~200 个 delta × 2 次 marshal/unmarshal = 400 次多余序列化。

### 2.2 问题 B: 无 Timer/Size 触发，全量积累有界风险

**现状**: Delta 积累仅在下一个 storable event 时 flush。一个长 LLM 响应（如生成 500 行代码 ≈ 15KB）全部积累在内存中，直到 Done 或 ToolCall 才一次性写入 DB。

- 内存无上界（长响应 → 大 accumulator）
- Crash 时全部丢失（未 flush 的内容无法恢复）
- DB 写入集中在 turn 结束时（单条巨大行，查询和传输效率低）

### 2.3 问题 C: Retry 不重置 Collector accumulator

**现状** (`bridge_forward.go:200-206`):

```go
turnText.Reset()    // convStore 视角：已重置
lastError = nil
// collector accumulator 未重置！
```

Retry 后 Collector 仍持有旧 delta 积累（重试前的错误输出文本），被后续事件 flush 后写入 eventStore。eventStore 中存储的是错误内容。

---

## 3. Seq 赋值时序（关键约束）

`bridge_forward.go` 中的执行顺序：

```
line 108: env = events.Clone(env)        // env.Seq = 0
line 111: extractMessageContent(env)      // 提取 content，但 Seq 尚未赋值
line 184: hub.SendToSession(env)          // Hub 在此赋值 env.Seq
line 188: captureEvent(env.Seq, ...)      // 此时 Seq 才可用
```

**结论**: `CaptureDeltaString` 不能在 line 111 调用（seq=0）。必须在 line 184 SendToSession 之后调用。

---

## 4. 修复方案

### 4.1 Fix A: CaptureDeltaString 快路径（尊重 seq 时序）

**文件**: `internal/gateway/bridge_forward.go`

**变更 1** — 行 111-116 保存 delta content（seq 尚未赋值，不可调用 Collector）：

```go
var capturedDeltaContent string
if env.Event.Type == events.MessageDelta || env.Event.Type == events.Message {
    if content := extractMessageContent(env); content != "" {
        turnText.WriteString(content)
        if env.Event.Type == events.MessageDelta {
            capturedDeltaContent = content
        }
    }
}
```

**变更 2** — 行 184-191 SendToSession 赋值 seq 后使用快路径：

```go
if err := b.hub.SendToSession(context.Background(), env); err != nil { ... }

if capturedDeltaContent != "" && b.collector != nil {
    b.collector.CaptureDeltaString(sessionID, env.Seq, capturedDeltaContent)
} else if env.Event.Type != events.MessageDelta {
    b.captureEvent(sessionID, env.Seq, env.Event.Type, env.Event.Data)
}
```

**收益**: 消除每个 delta 2× JSON 往返。典型 turn 节省 ~400 次序列化。

### 4.2 Fix B: 三重 Flush 触发机制

**文件**: `internal/eventstore/collector.go`

Collector accumulator 从单一 event 触发改为三重触发：

#### 4.2.1 Size 触发（热路径，同步）

在 `CaptureDeltaString` 中 append 后检查累积内容大小：

```go
func (c *Collector) CaptureDeltaString(sessionID string, seq int64, content string) {
    c.accumMu.Lock()
    acc := c.accum[sessionID]
    if acc == nil { acc = newDeltaAccumulator(); c.accum[sessionID] = acc }
    acc.appendRaw(seq, content)

    if acc.content.Len() >= deltaFlushSize {
        delete(c.accum, sessionID)
        c.accumMu.Unlock()
        c.send(acc.toRequest(sessionID))
        return
    }
    c.accumMu.Unlock()
}
```

`deltaFlushSize = 4096` bytes — 一段完整段落或代码块，DB 行大小合理，replay 单行有意义。

#### 4.2.2 Timer 触发（runWriter goroutine，周期扫描）

在已有的 runWriter ticker (100ms) 中增加 accumulator 扫描。**绕过 captureC channel**，直接写入 batch（避免死锁风险）：

```go
case <-ticker.C:
    c.flushTimedOutAccumulators(&batch)
    flush()
```

```go
func (c *Collector) flushTimedOutAccumulators(batch *[]*captureRequest) {
    now := time.Now()
    c.accumMu.Lock()
    for sid, acc := range c.accum {
        if now.Sub(acc.firstSeenAt) >= deltaFlushInterval {
            delete(c.accum, sid)
            *batch = append(*batch, acc.toRequest(sid))
        }
    }
    c.accumMu.Unlock()
}
```

`deltaFlushInterval = 2s` — Claude 生成速度 ~50 token/s ≈ 200 bytes/s，2s 内 ~400 bytes（远低于 4096 size 阈值）。Timer 主要在 tool 调用等待期间触发（delta 流暂停，但 accumulator 中有待 flush 内容）。

#### 4.2.3 Event 触发（MessageEnd + storable event）

`Capture` 方法中保留现有 flushDelta 逻辑，并增加 MessageEnd 路径：

```go
func (c *Collector) Capture(sessionID string, seq int64, eventType events.Kind, data json.RawMessage, direction string) {
    if eventType == events.MessageDelta {
        // 仍走 Capture 路径（兼容直接调用），但 Fix A 后桥端已切换到 CaptureDeltaString
        c.accumMu.Lock()
        acc := c.accum[sessionID]
        if acc == nil { acc = newDeltaAccumulator(); c.accum[sessionID] = acc }
        acc.append(seq, data)
        c.accumMu.Unlock()
        return
    }

    // MessageEnd triggers flush but is not stored itself.
    if eventType == events.MessageEnd {
        c.flushDelta(sessionID)
        return
    }

    if !IsStorable(eventType) { return }

    c.flushDelta(sessionID)
    // ... store current event
}
```

#### 4.2.4 accumulator 增加 firstSeenAt + toRequest

```go
type deltaAccumulator struct {
    content     strings.Builder
    seq         int64
    firstSeq    int64
    lastSeq     int64
    count       int
    firstSeenAt time.Time
}

func (a *deltaAccumulator) appendRaw(seq int64, content string) {
    a.content.WriteString(content)
    a.lastSeq = seq
    if a.count == 0 {
        a.firstSeq = seq
        a.seq = seq
        a.firstSeenAt = time.Now()
    }
    a.count++
}

func (a *deltaAccumulator) toRequest(sessionID string) *captureRequest {
    merged, seq, firstSeq, lastSeq := a.flush()
    mergedData, _ := json.Marshal(map[string]any{
        "content":      merged,
        "merged_count": lastSeq - firstSeq + 1,
        "seq_range":    []int64{firstSeq, lastSeq},
    })
    return &captureRequest{event: &StoredEvent{
        SessionID: sessionID,
        Seq:       seq,
        Type:      string(events.Message),
        Data:      mergedData,
        Direction: "outbound",
        CreatedAt: a.firstSeenAt.UnixMilli(),
    }}
}
```

`created_at` 使用 `firstSeenAt`（第一个 delta 到达时间）而非 `time.Now()`（flush 时间），确保 replay 的时序准确性。

### 4.3 Fix C: Retry 重置 Collector accumulator

**文件 1**: `internal/eventstore/collector.go` — 新增方法：

```go
func (c *Collector) ResetSession(sessionID string) {
    c.accumMu.Lock()
    delete(c.accum, sessionID)
    c.accumMu.Unlock()
}
```

**文件 2**: `internal/gateway/bridge_forward.go` 行 204 — retry 块中加入：

```go
if shouldRetry, attempt := b.retryCtrl.ShouldRetry(sessionID, lastError); shouldRetry {
    pendingError = nil
    b.autoRetry(context.Background(), w, sessionID, attempt)
    turnText.Reset()
    if b.collector != nil {
        b.collector.ResetSession(sessionID)
    }
    lastError = nil
    continue
}
```

**收益**: Retry 后 eventStore 不含重试前的错误 delta 内容。

---

## 5. Replay 时序正确性证明

### 5.1 完整事件流追踪

典型 tool-use turn（含 partial message 拆分）：

```
Handler goroutine:
T=0ms   Input         → CaptureInbound(seq=1) → store Input(seq=1, created_at=T0, dir=inbound)

forwardEvents goroutine (sequential):
T=1ms   State(running)    → SendToSession(seq=2) → store State(seq=2, created_at=T1)
T=2ms   MessageStart      → SendToSession(seq=3) → NOT storable → skip
T=3ms   MessageDelta      → SendToSession(seq=4) → CaptureDeltaString → accumulated
T=4ms   MessageDelta      → SendToSession(seq=5) → CaptureDeltaString → accumulated
T=5ms   MessageEnd        → SendToSession(seq=6) → Capture → MessageEnd → flushDelta
                              → store Message(seq=4, created_at=T3, seq_range=[4,5])
T=6ms   ToolCall          → SendToSession(seq=7) → store ToolCall(seq=7)
T=7ms   ToolResult        → SendToSession(seq=8) → store ToolResult(seq=8)
T=8ms   MessageDelta      → SendToSession(seq=9) → CaptureDeltaString → accumulated
T=9ms   MessageDelta      → SendToSession(seq=10) → CaptureDeltaString → accumulated
T=10ms  MessageEnd        → SendToSession(seq=11) → flushDelta
                              → store Message(seq=9, created_at=T8, seq_range=[9,10])
T=11ms  Done              → SendToSession(seq=12) → store Done(seq=12)
```

**Replay ORDER BY seq**:
```
Input(1) → State(2) → Message(4,"Hello world") → ToolCall(7) → ToolResult(8) → Message(9,"Result: done") → Done(12)
```

### 5.2 长响应 + Timer/Size 触发

```
T=0ms    Input(seq=1) → stored
T=1ms    State(seq=2) → stored
T=3ms    MessageDelta(seq=4) → accumulated (firstSeenAt=T3)
...
T=2003ms MessageDelta(seq=N) → accumulated (size=4000)

runWriter ticker at ~T=2100ms:
         → flushTimedOutAccumulators: age=2100-T3=2097ms > 2s → timer trigger
         → store Message(seq=4, created_at=T3, seq_range=[4,N])

T=2105ms MessageDelta(seq=N+1) → accumulated (NEW accum, firstSeenAt=T2105)
...
T=5000ms MessageDelta(seq=M) → size=4100 > 4096 → size trigger
         → store Message(seq=N+1, created_at=T2105, seq_range=[N+1,M])

T=5005ms MessageDelta(seq=M+1) → accumulated (NEW accum, firstSeenAt=T5005)
T=6000ms MessageEnd(seq=X) → flush remaining
         → store Message(seq=M+1, created_at=T5005, seq_range=[M+1,X-1])
T=6001ms Done(seq=X+1) → stored
```

**Replay ORDER BY seq**:
```
Input(1) → State(2) → Message(4,partial) → Message(N+1,partial) → Message(M+1,final) → Done(X+1)
```

### 5.3 时序正确性保证

对任意事件 A(seq=a, created_at=ta) 和 B(seq=b, created_at=tb)：

1. **Input vs Message**: Input 由 Handler 立即存储（created_at=T_input），Message 的 firstSeenAt 是第一个 delta 到达时间（T_first_delta > T_input）。**answer 不可能在 question 之前。**

2. **Partial Messages 之间**: seq 严格递增（[4,N] < [N+1,M] < [M+1,...]）。**ORDER BY seq 保证正确顺序。**

3. **Message vs ToolCall**: Message 的 seq 来自 delta（总在 ToolCall 之前产生），ToolCall 的 seq 更大。**不可能交叉。**

4. **created_at vs seq 一致性**: 两者在 forwardEvents 中由同一 goroutine 严格递增赋值，天然单调。Message 使用 firstSeenAt（delta 生成时刻），进一步保证 created_at 反映真实时序。

---

## 6. 并发安全分析

### 6.1 双 goroutine 访问

| Goroutine | 操作 | 锁 |
|-----------|------|----|
| forwardEvents | CaptureDeltaString → append + size flush | accumMu |
| runWriter | flushTimedOutAccumulators → timer flush | accumMu |

### 6.2 竞态场景

**场景 1 — size 和 timer 同时触发同一 accumulator**:
```
forwardEvents: lock → append → size≥4096 → delete acc → unlock → send request
runWriter:     lock → acc == nil (已删) → 无操作 → unlock
```
forwardEvents 优先 flush，runWriter 找不到 accumulator。正确。

**场景 2 — timer 先触发**:
```
runWriter:     lock → find acc → delete → add to batch → unlock
forwardEvents: lock → acc == nil → create NEW acc → append → unlock
```
Timer flush 旧 batch，forwardEvents 开始新 accumulator（seq 继续递增）。时序正确。

**场景 3 — timer flush 绕过 channel 的必要性**:
runWriter 既读 captureC 又执行 timer flush。若 timer flush 写 captureC，channel 满时产生死锁（runWriter 等待自己消费）。直接写入 batch 是唯一安全方式。

---

## 7. 参数推导

| 参数 | 值 | 推导 |
|------|-----|------|
| `deltaFlushSize` | **4096 bytes** | ~1365 中文字符 / ~1000 英文单词。一段完整段落或代码块。DB 行大小合理，replay 单行有意义。大于 PlatformWriter 的 200 runes 因为磁盘写入无实时约束，减少行数提升查询性能。 |
| `deltaFlushInterval` | **2s** | Claude 生成速度 ~50 token/s ≈ 200 bytes/s，2s 内 ~400 bytes（远低于 4096）。Timer 主要在 tool 调用等待期间触发（delta 流暂停但 accum 中有内容）。长于 PlatformWriter 的 120ms 因为无人类实时观看。 |
| `collectorFlushInterval` | **100ms** (不变) | Batch writer 的 tick 间隔，同时作为 timer flush 的扫描周期。 |

### 为什么不更小？

- **Size=512**: 典型 LLM 响应 ~200 delta → 拆成 ~50 行。DB 写入频繁，replay 结果碎片化，一个自然段被拆成多行。
- **Timer=500ms**: Claude 正常生成 ~100 bytes/500ms，timer 几乎每次都触发，与 size 触发重叠，无意义。

### 为什么不更大？

- **Size=64KB**: Crash 时最大丢失 64KB。Replay 时单行巨大，传输和渲染效率低。
- **Timer=10s**: Worker crash 后最多 10s 的内容丢失，不可接受。

---

## 8. 影响分析

### 8.1 性能影响

| 指标 | 修复前 | 修复后 | 改善 |
|------|--------|--------|------|
| Delta 序列化次数/turn | ~400 (200×2) | 0 | -100% |
| Collector 内存峰值 | 无上界 (长响应全量积累) | ≤4096 bytes/session | 有界 |
| DB 写入模式 | turn 结束时单条巨大行 | 多条合理大小行 | 更均衡 |
| Crash 数据损失 | 整个 turn 的 delta | ≤4096 bytes 或 ≤2s | 大幅减少 |

### 8.2 Replay UX 影响

| 场景 | 修复前 | 修复后 |
|------|--------|--------|
| 短响应 (<4KB) | 1 行 Message | 1 行 Message (不变) |
| 长响应 (>4KB) | 1 行巨大 Message | 多行 partial Message，渐进呈现 |
| Tool 调用间隙 | 不拆分 | Timer 触发拆分，减少 crash 损失 |
| Replay 排序 | ORDER BY seq 正确 | ORDER BY seq 正确 (不变) |

### 8.3 风险评估

三个 fix 有依赖关系：
- Fix A 独立，可单独应用
- Fix B 的 size/timer 触发依赖 Fix A 的 CaptureDeltaString 调用（否则走 Capture 的 append 路径，无 size 检查）
- Fix C 独立，可单独应用

建议应用顺序: A → B → C

---

## 9. 测试计划

### 9.1 单元测试

| 测试 | 验证 |
|------|------|
| `TestCaptureDeltaStringFastPath` | CaptureDeltaString 积累正确，flush 输出完整 |
| `TestCaptureDeltaStringSizeFlush` | 累积超过 4096 bytes 时自动 flush |
| `TestMessageEndFlushWithoutStore` | MessageEnd 触发 flush 但自身不入库 |
| `TestTimerFlushInRunWriter` | runWriter ticker 扫描并 flush 超时 accumulator |
| `TestTimerFlushBypassesChannel` | timer flush 直接写入 batch，不经过 captureC |
| `TestResetSessionClearsAccumulator` | ResetSession 后 flush 输出为空 |
| `TestRetryResetCorrectness` | Retry 后 Capture 新 delta，flush 不含旧内容 |
| `TestReplaySeqOrdering` | 多 partial Message + ToolCall + Done 的 ORDER BY seq 正确 |
| `TestCreatedAtUsesFirstSeenAt` | flushed Message 的 created_at 是首个 delta 时刻 |
| `TestConcurrentSizeAndTimerFlush` | size 和 timer 同时触发不丢不重 |

### 9.2 集成验证

```bash
make quality  # fmt + lint + test (含 -race)
```

### 9.3 回归检查

- [ ] WebChat 流式输出正常
- [ ] Slack/飞书卡片更新节奏不变
- [ ] EventStore events 表有数据写入
- [ ] 长响应产生多条 partial Message，seq 递增
- [ ] LLM auto-retry 后 eventStore 数据正确（不含重试前内容）

---

## 10. 实施清单

| # | Fix | 文件 | 改动量 | 优先级 |
|---|-----|------|--------|--------|
| 1 | A: 快路径 (seq 时序修正) | bridge_forward.go | ~10 行 | P0 |
| 2 | B: 三重 flush (size+timer+event) | collector.go | ~40 行 | P0 |
| 3 | C: Retry reset | collector.go + bridge_forward.go | ~7 行 | P0 |

总改动量：~57 行。预估 8 小时（含测试编写和验证）。
