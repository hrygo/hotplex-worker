<<<<<<< HEAD
# Slack Stream Rotation Spec

> Issue: #209 | Branch: `feat/slack-stream-rotation`

## Problem

Slack 流式消息在 Worker 长时间无输出后触发 `⚠️ *Stream expired, sending complete content:*` fallback，导致消息分裂、用户体验差。

**根因**：Slack 服务端隐式超时关闭流 → `appendWithRetry` 收到 `message_not_in_streaming_state` → `streamExpired = true` → fallback PostMessage。

**现有 TTL 检查为死代码**（`stream.go:168-180`）：条件 `!w.started && !w.streamStartTime.IsZero()` 永远为 false（`streamStartTime` 仅在首次 `Write()` 中赋值，此时 `started` 已变为 true）。

## Slack 流式 API 限制现状

### 官方文档

完整阅读了 [chat.startStream](https://docs.slack.dev/reference/methods/chat.startStream)、[chat.appendStream](https://docs.slack.dev/reference/methods/chat.appendStream)、[chat.stopStream](https://docs.slack.dev/reference/methods/chat.stopStream) 三个方法的官方参考文档。

**已文档化的限制**：
- Rate limit: Tier 2 (20+/min)
- `markdown_text` 单次上限: 12,000 字符
- blocks 上限: 50 via `blocks` + 50 via `chunks` = 100

**完全未文档化**：
- 流最大存活时间 / wallclock TTL
- 空闲超时
- 最大 append 次数
- `message_not_in_streaming_state` 错误的触发条件
- 流恢复流程

### slack-go 客户端

`StartStreamContext` / `AppendStreamContext` / `StopStreamContext` 为无状态 HTTP wrapper，零超时、零 TTL、零 keepalive 逻辑。

### 设计决策

由于 Slack 官方未文档化流式超时，采用与飞书对齐的策略：
- `StreamTTL = 10min`（参照飞书服务端 10min 限制）
- `StreamRotationTTL = 6min`（与飞书旋转时长一致）

## Design

对齐飞书已验证的 TTL 旋转模式（`feishu/adapter.go:810-831`）：

1. **在 `writeWithStreaming` 中检测 TTL** — 每次新内容到达时检查流是否过期
2. **过期时主动关闭旧 writer** — Close() 会 flush 残留 buffer → StopStream → deregister
3. **创建新 writer** — 首次 Write() 自动 StartStream，新消息续写

### Rotation TTL

```go
StreamTTL         = 10 * time.Minute // server-side streaming limit (undocumented, aligned with Feishu)
StreamRotationTTL = 6 * time.Minute  // proactive rotation before server limit (aligned with Feishu)
```

### Expired() 方法

```go
func (w *NativeStreamingWriter) Expired() bool {
    w.mu.Lock()
    defer w.mu.Unlock()
    if w.streamStartTime.IsZero() || !w.started || w.closed {
        return false
    }
    return time.Since(w.streamStartTime) > StreamRotationTTL
}
```

### writeWithStreaming 旋转逻辑

```go
// TTL rotation: proactively replace expired streams before
// Slack's server-side streaming limit kicks in.
if c.streamWriter != nil && c.streamWriter.Expired() {
    oldWriter := c.streamWriter
    c.streamWriter = nil
    go func() { _ = oldWriter.Close() }()
    c.adapter.Log.Info("slack: stream rotated", ...)
}

// Create new streaming writer if needed (same as before)
if c.streamWriter == nil { ... }
```

### 为什么 Close() 不会触发 fallback

旋转时旧 writer 状态：
- `streamExpired = false`（未收到 API 错误）
- `integrityOK = true`（所有 buffer 已 flush）
- → `Close()` 跳过 `⚠️ *Stream expired*` fallback

## Implementation Phases

### Phase 1: 删除死代码

- [x] 删除 `stream.go:168-180`（Path A TTL 检查）
- [x] 删除 `ttlWarningLogged` 字段

### Phase 2: 实现旋转

- [x] 新增 `Expired()` 方法
- [x] 修改 `writeWithStreaming` 添加旋转检测
- [x] TTL 与飞书对齐：`StreamTTL=10min`, `StreamRotationTTL=6min`

### Phase 3: 测试

- [x] `TestExpired_*` — Expired() 边界条件
- [x] `TestDeadCodeRemoved` — 验证死代码移除
- [x] `TestStreamRotationTTL` — 验证 TTL 值和不变量

## Acceptance Criteria

- [x] Path A 死代码已删除
- [ ] Worker 长时间无输出后恢复，流式消息不中断
- [ ] `⚠️ *Stream expired*` 不再出现
- [x] `-race` 测试通过
- [ ] 旋转失败时优雅降级（现有 fallback 不受影响）

## References

- [chat.startStream method](https://docs.slack.dev/reference/methods/chat.startStream)
- [chat.appendStream method](https://docs.slack.dev/reference/methods/chat.appendStream)
- [chat.stopStream method](https://docs.slack.dev/reference/methods/chat.stopStream)
- 飞书旋转实现：`internal/messaging/feishu/adapter.go:810-831`
- Slack 流式 writer：`internal/messaging/slack/stream.go`
