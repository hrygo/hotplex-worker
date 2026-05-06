# Slack Stream Rotation Spec

> Issue: #209 | Branch: `feat/slack-stream-rotation`

## Problem

Slack 流式消息在 Worker 长时间无输出后触发 `⚠️ *Stream expired, sending complete content:*` fallback，导致消息分裂、用户体验差。

**根因**：Slack 服务端隐式超时关闭流 → `appendWithRetry` 收到 `message_not_in_streaming_state` → `streamExpired = true` → fallback PostMessage。

**现有 TTL 检查为死代码**（`stream.go:169-180`）：条件 `!w.started && !w.streamStartTime.IsZero()` 永远为 false（`streamStartTime` 仅在首次 `Write()` 中赋值，此时 `started` 已变为 true）。

## Design

对齐飞书已验证的 TTL 旋转模式（`feishu/adapter.go:810-831`）：

1. **在 `writeWithStreaming` 中检测 TTL** — 每次新内容到达时检查流是否过期
2. **过期时主动关闭旧 writer** — Close() 会 flush 残留 buffer → StopStream → deregister
3. **创建新 writer** — 首次 Write() 自动 StartStream，新消息续写

### Rotation TTL

```go
StreamRotationTTL = 8 * time.Minute  // Slack 服务端 ~10min 限制，2min 安全余量
```

### Expired() 方法

```go
func (w *NativeStreamingWriter) Expired() bool {
    w.mu.Lock()
    defer w.mu.Unlock()
    if w.streamStartTime.IsZero() || !w.started {
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

- 删除 `stream.go:168-180`（Path A TTL 检查）
- 删除 `ttlWarningLogged` 字段
- `streamStartTime` 和 `StreamTTL` 保留（旋转功能使用）

### Phase 2: 实现旋转

- 新增 `StreamRotationTTL` 常量
- 新增 `Expired()` 方法
- 修改 `writeWithStreaming` 添加旋转检测
- 复用现有 `closeStreamWriter` / `NewStreamingWriter` 流程

### Phase 3: 测试

- `TestExpired_NewStream` — 未启动/刚启动时 Expired() 返回 false
- `TestExpired_AfterTTL` — 超过 TTL 后 Expired() 返回 true
- `TestRotation_CloseOldCreatesNew` — 旋转后 Write 继续到新 writer

## Acceptance Criteria

- [ ] Path A 死代码已删除
- [ ] Worker 长时间无输出后恢复，流式消息不中断
- [ ] `⚠️ *Stream expired*` 不再出现
- [ ] 旋转后内容完整无丢失
- [ ] `-race` 测试通过
- [ ] 旋转失败时优雅降级（现有 fallback 不受影响）
