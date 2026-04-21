# Hub Platform Conn Async Writer + Delta Coalescing — Integrated Design

> **BUG-1 + PERF-1**: Hub.Run() broadcast loop 阻塞 + Delta 高频 API 调用优化
> **Status**: Proposed · **Priority**: P0 · **Date**: 2026-04-21

## 1. Problem

### 1.1 BUG-1: Hub 阻塞

Hub.Run() 是单 goroutine 广播循环，所有 session 的事件投递在 `routeMessage` 中串行执行。
Platform conn (Slack/Feishu) 的 `WriteCtx` 触发 HTTP API 调用时，整个 Hub 事件分发被阻塞。

```
Hub.Run()
  └─ routeMessage(msg)
       ├─ *Conn.WriteMessage()          ← WebSocket, ~μs, 不阻塞
       └─ pcEntry.WriteCtx()            ← Platform HTTP API 调用
            ├─ Slack: PostMessage / AppendStream / StopStream
            │   └─ HTTP round-trip: 50ms ~ 5s (p99 可达 10s+)
            └─ Feishu: PatchMessage / UpdateCard
                └─ HTTP round-trip: 30ms ~ 3s
```

### 1.2 PERF-1: Delta 高频 API 调用

Worker 输出 `message.delta` 的速率可达 100-200 tokens/sec，每个 delta 独立穿透全链路：

| 层 | 每秒操作 (100 tok/s) | 瓶颈 |
|----|---------------------|------|
| Hub.broadcast channel | 100 slots/sec | 256 容量，2.5s 填满 |
| pcEntry.WriteCtx | 100 次/sec | mutex + chan ops |
| Slack NSW.Write (mutex) | 100 次/sec | mutex 竞争 |
| Feishu Write+Flush | **100 API 调用/sec** | **超出 rate limit (~30-50/sec)** |

**关键差异**：
- Slack NativeStreamingWriter 内部已有 150ms/20rune 批处理，实际 API 调用 ~7 次/sec
- Feishu StreamingCardController **无批处理**，每个 delta = Write + Flush = 1 次 CardKit API 调用

### 1.3 阻塞时序图

```
                    Hub.Run goroutine
                    │
    ┌───────────────┼───────────────┐
    │               │               │
  Session A      Session B      Session C
  (WebSocket)    (Slack)        (WebSocket)
    │               │               │
    │◄── write ─────┤               │   ← ~μs
    │               │               │
    │               ├── PostMessage ─┤   ← HTTP, 500ms
    │               │   (blocking)  │
    │               │               │   ← Session C waits!
    │               │◄──────────────┤   ← 500ms later
    │               │               │
    │               │               │◄── write ── delayed 500ms
```

## 2. Solution: Per-Conn Async Writer + Delta Coalescing

### 2.1 架构总览

```
Hub.Run() → routeMessage()
  │
  ├── *Conn (WebSocket): WriteMessage(encoded)     ← 同步, ~μs
  │
  └── *pcEntry (Platform): WriteCtx(env)            ← 异步 channel send, ~μs
         │
         └── writeLoop goroutine
              │
              ├── MessageDelta → accumulate + timer  ← 合并
              │       │
              │       ├── timer fires (30ms)
              │       ├── size threshold (200 runes)
              │       └── non-delta arrives
              │               │
              │               ▼
              │         flush merged delta
              │         PlatformConn.WriteCtx(mergedEnvelope)
              │               │
              │               ├── Slack: NSW.Write(merged) → buf → flushLoop 150ms
              │               └── Feishu: StreamCtrl.Write(merged) + Flush()
              │
              └── non-delta → flush pending → PlatformConn.WriteCtx(env)
                    (state/done/error/interaction)
```

### 2.2 数据结构

```go
const (
    platformWriteBufSize  = 64                    // per-conn channel 容量
    platformDropThreshold = 56                    // delta 开始丢弃的水位线
    coalesceInterval      = 30 * time.Millisecond // delta 合并窗口
    coalesceSizeThreshold = 200                   // runes — 超过立即 flush
)

type pcEntry struct {
    pc   messaging.PlatformConn
    ch   chan *events.Envelope // buffered
    done chan struct{}         // writeLoop exit signal
}

func newPCEntry(pc messaging.PlatformConn) *pcEntry {
    e := &pcEntry{
        pc:   pc,
        ch:   make(chan *events.Envelope, platformWriteBufSize),
        done: make(chan struct{}),
    }
    go e.writeLoop()
    return e
}
```

### 2.3 WriteCtx — 背压投递

```go
func (e *pcEntry) WriteCtx(_ context.Context, env *events.Envelope) error {
    // Backpressure: delta/raw 在高水位时丢弃
    if isDroppable(env.Event.Type) && len(e.ch) >= platformDropThreshold {
        metrics.GatewayPlatformDroppedTotal.WithLabelValues(string(env.Event.Type)).Inc()
        return nil
    }

    select {
    case e.ch <- env:
        return nil
    default:
        if isDroppable(env.Event.Type) {
            metrics.GatewayPlatformDroppedTotal.WithLabelValues(string(env.Event.Type)).Inc()
            return nil
        }
        // Guaranteed events: blocking send with timeout
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        select {
        case e.ch <- env:
            return nil
        case <-ctx.Done():
            return fmt.Errorf("platform conn write timeout: buffer full")
        }
    }
}
```

### 2.4 writeLoop — 核心状态机 (Async + Coalescing)

```go
func (e *pcEntry) writeLoop() {
    defer close(e.done)
    defer func() {
        if r := recover(); r != nil {
            slog.Error("pcEntry writeLoop panic", "panic", r, "stack", string(debug.Stack()))
        }
    }()

    var db strings.Builder   // delta accumulate buffer
    var sessionID string
    var timer *time.Timer
    var timerCh <-chan time.Time

    flush := func() {
        if db.Len() == 0 {
            return
        }
        merged := &events.Envelope{
            Version:   events.Version,
            ID:        aep.NewID(),
            SessionID: sessionID,
            Event: events.Event{
                Type: events.MessageDelta,
                Data: events.MessageDeltaData{
                    Content: db.String(),
                },
            },
        }
        db.Reset()
        if timer != nil {
            timer.Stop()
            timerCh = nil
        }
        e.writeOne(merged)
    }

    for {
        select {
        case env, ok := <-e.ch:
            if !ok {
                flush() // drain pending deltas
                return
            }

            if isDroppable(env.Event.Type) {
                // Delta/Raw: accumulate for coalescing
                content := extractDeltaContent(env)
                if db.Len() == 0 {
                    sessionID = env.SessionID
                }
                db.WriteString(content)

                if utf8.RuneCountInString(db.String()) >= coalesceSizeThreshold {
                    flush()
                } else if timer == nil {
                    timer = time.NewTimer(coalesceInterval)
                    timerCh = timer.C
                } else {
                    timer.Reset(coalesceInterval)
                }
            } else {
                // Non-delta: flush pending first, then forward
                flush()
                e.writeOne(env)
            }

        case <-timerCh:
            flush()
        }
    }
}

func (e *pcEntry) writeOne(env *events.Envelope) {
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    if err := e.pc.WriteCtx(ctx, env); err != nil {
        slog.Warn("platform async write failed",
            "event_type", env.Event.Type,
            "session_id", env.SessionID,
            "err", err)
    }
}

// extractDeltaContent extracts text content from a delta/raw envelope.
func extractDeltaContent(env *events.Envelope) string {
    switch env.Event.Type {
    case events.MessageDelta:
        if d, ok := env.Event.Data.(events.MessageDeltaData); ok {
            return d.Content
        }
        if m, ok := env.Event.Data.(map[string]any); ok {
            if c, _ := m["content"].(string); c != "" {
                return c
            }
        }
    case events.Raw:
        if d, ok := env.Event.Data.(events.RawData); ok {
            if m, ok := d.Raw.(map[string]any); ok {
                if t, _ := m["text"].(string); t != "" {
                    return t
                }
            }
        }
    }
    if s, ok := env.Event.Data.(string); ok {
        return s
    }
    return ""
}
```

### 2.5 Close — Drain + Wait

```go
func (e *pcEntry) Close() error {
    close(e.ch)   // signal writeLoop to drain and exit
    <-e.done      // wait for drain completion
    return e.pc.Close()
}
```

### 2.6 JoinPlatformSession 变更

```go
// hub.go: JoinPlatformSession
func (h *Hub) JoinPlatformSession(sessionID string, pc messaging.PlatformConn) {
    h.mu.Lock()
    defer h.mu.Unlock()

    if h.sessions[sessionID] == nil {
        h.sessions[sessionID] = make(map[SessionWriter]bool)
    }

    // Deduplicate by underlying PlatformConn
    for sw := range h.sessions[sessionID] {
        if pce, ok := sw.(*pcEntry); ok && pce.pc == pc {
            return
        }
    }

    h.sessions[sessionID][newPCEntry(pc)] = true
}
```

### 2.7 routeMessage 变更

Platform conn 路径不再使用 `encoded` JSON（hub.go:424-428 的 `aep.EncodeJSON` 结果仅用于 `*Conn`）：

```go
for _, conn := range conns {
    metrics.GatewayMessagesTotal.WithLabelValues("outgoing", string(msg.Env.Event.Type)).Inc()
    if c, ok := conn.(*Conn); ok {
        // WebSocket: synchronous, uses encoded JSON
        if err := c.WriteMessage(websocket.TextMessage, encoded); err != nil {
            h.log.Warn("gateway: write failed", "session_id", msg.Env.SessionID, "err", err)
            _ = conn.Close()
        }
    } else {
        // Platform: async via pcEntry channel (non-blocking)
        if err := conn.WriteCtx(context.Background(), msg.Env); err != nil {
            h.log.Warn("gateway: platform write enqueue failed", "session_id", msg.Env.SessionID, "err", err)
            _ = conn.Close()
        }
    }
}
```

优化：`encoded` 仅在有 `*Conn` 时计算，避免 platform-only session 的冗余 JSON encode。

## 3. Verification — 关键不变量

### 3.1 Seq 分配不受影响 ✓

```
forwardEvents → hub.SendToSession → seqGen.Next() → broadcast channel
                                                       │
                                          routeMessage ← 这里 seq 已分配
                                                       │
                                          pcEntry.WriteCtx → ch → writeLoop
                                                                           │
                                                              coalescing 在此发生
                                                              合并 envelope 的 seq 无关紧要
                                                              (platform adapter 不使用 seq)
```

**结论**: WebSocket 客户端仍然收到每条 delta 的独立 seq；platform adapter 忽略 seq 字段。

### 3.2 消息顺序保证 ✓

```
writeLoop 处理顺序:

  delta1 → delta2 → delta3 → Done
  │         │         │        │
  ├─ accum ─┤─ accum ─┤        │
  │                    │        │
  │    timer fires     │        │
  │    OR              │        │
  │    non-delta       │        │
  ▼                    ▼        ▼
  flush(merged 1+2+3)  →  writeOne(Done)

  PlatformConn.WriteCtx 看到的顺序:
    merged_delta(1,2,3) → Done   ← 保证: delta 在 Done 之前
```

**规则**: Non-delta 事件到达时先 flush pending delta，保证 FIFO + 因果序。

### 3.3 Slack NSW 交互 ✓

```
Before (100 tok/s):
  100 × NSW.Write(token) → 100 × mu.Lock/Unlock → buf
  NSW flushLoop: 150ms ticker → 6-7 × AppendStream API

After (coalesced @30ms):
  ~33 × NSW.Write(merged_3tokens) → 33 × mu.Lock/Unlock → buf
  NSW flushLoop: 150ms ticker → 6-7 × AppendStream API (不变)

改善: mutex 竞争降低 3x
```

### 3.4 Feishu StreamingCard 交互 ✓

```
Before (100 tok/s):
  100 × StreamCtrl.Write(token\n\n) + Flush() → 100 × CardKit API
  ⚠️ 超出 rate limit

After (coalesced @30ms):
  ~33 × StreamCtrl.Write(merged_3tokens\n\n) + Flush() → 33 × CardKit API
  ✅ 安全区

附带修复: 当前每个 delta 追加 \n\n，导致过度段落间距。
合并后 N 次 \n\n 变为 1 次，输出更自然。
```

### 3.5 sendControlToSession 路径 ✓

```
sendControlToSession 直接调用 conn.WriteCtx():
  *Conn:   WriteMessage — 同步, fast
  *pcEntry: WriteCtx → channel → writeLoop 处理

writeLoop 收到 control (PriorityControl):
  isDroppable(control) = false → flush pending → writeOne(control)

Platform adapter 对 control 事件为 no-op (extractResponseText 返回 false)
```

### 3.6 Done 的 dropped 标记 ✓

```go
// bridge.go forwardEvents:
if b.hub.GetAndClearDropped(sessionID) {
    // Done 事件标记 dropped: true
}
```

Hub 层 `sessionDropped` 仅追踪 Hub.broadcast channel 层的 drop。
pcEntry 层的 drop 不追踪 — 64 slot buffer 远大于 30ms 窗口内的 delta 量
(~3-5 tokens)，极端场景下丢失少量 delta 可接受。

### 3.7 Envelope Clone 安全性 ✓

```go
// hub.go SendToSession:
env = events.Clone(env) // Clone before broadcast

// routeMessage:
conn.WriteCtx(ctx, msg.Env) // msg.Env is the clone
```

pcEntry 收到的 envelope 是 clone，writeLoop 可以安全读取 Event.Data 提取 content。
合并后创建新的 synthetic envelope，不影响原始对象。

## 4. Before/After Comparison

```
BEFORE (sync + per-token):
  Hub.Run → routeMessage → pcEntry.WriteCtx → Slack HTTP → [500ms block]
  100 deltas/sec → 100 platform WriteCtx → 100 API ops

AFTER (async + coalesced):
  Hub.Run → routeMessage → pcEntry.WriteCtx → channel send → [~1μs]
                                       └→ writeLoop → coalesce 30ms → merged WriteCtx
  100 deltas/sec → ~33 merged WriteCtx → 33 API ops
```

| 指标 | Before | After |
|------|--------|-------|
| routeMessage 延迟 | 50ms~10s | ~1μs |
| Hub.Run 阻塞风险 | 高 | 无 |
| Slack WriteCtx/sec (100 tok/s) | 100 | ~33 (**3x** ↓) |
| Feishu API calls/sec (100 tok/s) | 100 | ~33 (**3x** ↓) |
| Feishu rate limit 风险 | **超出** | 安全 |
| Slack mutex 竞争 | 100 次/s | ~33 次/s (**3x** ↓) |
| GC 压力 (envelope 对象) | 100 个/s | ~33 个/s |
| 用户感知首 token 延迟 | ~0ms | +30ms (不可感知) |
| Goroutine 开销 | 0 | +1 per platform conn |
| 内存开销 | 0 | ~64 envelopes + delta buf |

## 5. Risk Matrix

| 风险 | 概率 | 缓解措施 |
|------|------|----------|
| writeLoop goroutine 泄漏 | 低 | `Close()` 通过 `close(ch)` + `<-done` 保证退出 |
| 消息丢失 (buffer overflow) | 低 | delta 可丢弃; critical events 阻塞 + 5s timeout |
| 消息乱序 | 极低 | 单 channel FIFO; non-delta 触发 flush |
| 首 token 延迟增加 | 确定 | +30ms coalesce 窗口，远小于 stream start 延迟 (~200ms) |
| Shutdown 时消息丢失 | 低 | `Close()` drain channel + flush pending |
| writeLoop panic | 低 | recover + log + 退出 |
| pcEntry drop 不追踪 | 确定 | Done.dropped 仅反映 Hub 层; pcEntry 64 slot 足够大 |
| 跨 messageID 混合 | 极低 | Platform adapter 不使用 messageID |

## 6. Metrics

```go
// Platform conn backpressure drops
GatewayPlatformDroppedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
    Name: "hotplex_gateway_platform_dropped_total",
    Help: "Events dropped at platform conn buffer level",
}, []string{"event_type"})

// Delta coalescing effectiveness
GatewayDeltaCoalescedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
    Name: "hotplex_gateway_delta_coalesced_total",
    Help: "Number of delta events merged by coalescer",
}, []string{"session_id"})

GatewayDeltaFlushTotal = promauto.NewCounterVec(prometheus.CounterOpts{
    Name: "hotplex_gateway_delta_flush_total",
    Help: "Number of merged delta flushes sent to platform",
}, []string{"session_id"})
```

## 7. Configuration

```yaml
gateway:
  platform_write_buffer: 64        # per-conn channel capacity
  platform_drop_threshold: 56      # begin dropping deltas at this fill level
  delta_coalesce_interval: 30ms    # coalesce window
  delta_coalesce_size: 200         # rune threshold for immediate flush
```

## 8. Implementation Checklist

### Phase 1: Async Writer (BUG-1 fix)
- [ ] 重写 `pcEntry` 结构体: `ch`, `done` 字段
- [ ] 实现 `newPCEntry()` + `writeLoop()` (不含 coalescing)
- [ ] 实现 backpressure `WriteCtx()` 投递逻辑
- [ ] 修改 `Close()`: `close(ch)` + `<-done`
- [ ] 修改 `JoinPlatformSession()`: `newPCEntry()` 包装
- [ ] routeMessage: 跳过 platform conn 的冗余 JSON encode
- [ ] 单元测试: buffer 满 droppable/guaranteed 行为
- [ ] 单元测试: Close() drain
- [ ] 单元测试: writeLoop panic recovery

### Phase 2: Delta Coalescing (PERF-1 optimization)
- [ ] writeLoop 添加 delta accumulate + timer 逻辑
- [ ] 实现 `extractDeltaContent()` 辅助函数
- [ ] 实现 `flush()` 合并 envelope 构造
- [ ] Non-delta 事件 flush-before-forward 保证
- [ ] 添加 Prometheus metrics (coalesced/flush counts)
- [ ] 单元测试: delta 合并正确性
- [ ] 单元测试: timer/size flush 触发
- [ ] 单元测试: non-delta 事件打断合并
- [ ] 单元测试: channel close 时 drain pending

### Integration
- [ ] E2E 测试: 完整 Slack 消息流
- [ ] E2E 测试: 完整 Feishu 消息流
- [ ] 负载测试: 200 tok/s delta 吞吐
- [ ] 监控: rate limit 命中率对比 before/after
