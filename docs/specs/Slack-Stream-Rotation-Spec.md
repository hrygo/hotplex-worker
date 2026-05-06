# Slack Stream Rotation Spec

> Issue: #209 | Branch: `feat/slack-stream-rotation`

## Problem

Slack 流式消息在 Worker 长时间无输出后触发 `⚠️ *Stream expired, sending complete content:*` fallback，导致消息分裂、用户体验差。

**根因**：Slack 服务端隐式超时关闭流 → `appendWithRetry` 收到 `message_not_in_streaming_state` → `streamExpired = true` → fallback PostMessage。

**现有 TTL 检查为死代码**（`stream.go:168-180`）：条件 `!w.started && !w.streamStartTime.IsZero()` 永远为 false（`streamStartTime` 仅在首次 `Write()` 中赋值，此时 `started` 已变为 true）。

## Slack vs Feishu TTL 对比

| 平台 | 服务端 TTL | Idle timeout | 旋转 TTL | 来源 |
|------|-----------|-------------|---------|------|
| **Feishu** | 10min | 无 | 6min | 官方文档 |
| **Slack** | **~5min wallclock** | **~30s idle** | **4min** | 社区实测（未文档化） |

**Slack TTL 未公开文档化**。实证数据来自 [python-slack-sdk #1859](https://github.com/slackapi/python-slack-sdk/issues/1859)：
- `hrygo/hotplex-legacy#237`：实测 ~5 分钟 wallclock TTL，即使持续 append 也会断
- `AuraHQ-ai/aura#421`：实测 ~30s idle timeout
- 多个 AI agent bot（Sentry/junior、AuraHQ）独立报告相同问题

**注意**：原代码 `StreamTTL = 10 * time.Minute` 基于错误的假设（与飞书对齐），实际远低于此值。

## Design

对齐飞书已验证的 TTL 旋转模式（`feishu/adapter.go:810-831`），但使用 Slack 的实测 TTL：

1. **在 `writeWithStreaming` 中检测 TTL** — 每次新内容到达时检查流是否过期
2. **过期时主动关闭旧 writer** — Close() 会 flush 残留 buffer → StopStream → deregister
3. **创建新 writer** — 首次 Write() 自动 StartStream，新消息续写

### Rotation TTL

```go
StreamTTL         = 5 * time.Minute  // empirical server-side wallclock limit (undocumented)
StreamRotationTTL = 4 * time.Minute  // proactive rotation before server ~5min limit
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

### 已知限制：Idle Timeout

Slack 还有 ~30s idle timeout（无 append 操作时流被关闭）。当前 flushLoop 每 150ms 触发一次，但仅在有 buffer 内容时才调用 appendWithRetry。Worker 长时间无输出时（如 tool call），buffer 为空，不会发送 keepalive。

**后续优化**（不在本 PR 范围）：在 flushLoop 中添加 idle keepalive — 超过 N 秒无 append 时发送空内容或 last-content 重复以保持流活跃。

## Implementation Phases

### Phase 1: 删除死代码

- 删除 `stream.go:168-180`（Path A TTL 检查）
- 删除 `ttlWarningLogged` 字段
- `streamStartTime` 和 `StreamTTL` 保留（旋转功能使用）

### Phase 2: 实现旋转

- 修正 `StreamTTL = 5min`（基于实测数据）
- 新增 `StreamRotationTTL = 4min`
- 新增 `Expired()` 方法
- 修改 `writeWithStreaming` 添加旋转检测
- 复用现有 `closeStreamWriter` / `NewStreamingWriter` 流程

### Phase 3: 测试

- `TestExpired_*` — Expired() 边界条件
- `TestDeadCodeRemoved` — 验证死代码移除
- `TestStreamRotationTTL` — 验证 TTL 值和不变量

## Acceptance Criteria

- [x] Path A 死代码已删除
- [ ] Worker 长时间无输出后恢复，流式消息不中断
- [ ] `⚠️ *Stream expired*` 不再出现
- [ ] 旋转后内容完整无丢失
- [x] `-race` 测试通过
- [ ] 旋转失败时优雅降级（现有 fallback 不受影响）

## References

- [python-slack-sdk #1859: message_not_in_streaming_state after undocumented timeouts](https://github.com/slackapi/python-slack-sdk/issues/1859)
- 飞书旋转实现：`internal/messaging/feishu/adapter.go:810-831`
- Slack 流式 writer：`internal/messaging/slack/stream.go`
