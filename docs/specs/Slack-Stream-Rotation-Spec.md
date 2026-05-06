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
| **Slack** | **~5min wallclock** | **~30s idle** | **4min / 20s idle** | 社区实测（未文档化） |

**Slack TTL 未公开文档化**。实证数据来自 [python-slack-sdk #1859](https://github.com/slackapi/python-slack-sdk/issues/1859)：
- `hrygo/hotplex-legacy#237`：实测 ~5 分钟 wallclock TTL，即使持续 append 也会断
- `AuraHQ-ai/aura#421`：实测 ~30s idle timeout
- 多个 AI agent bot（Sentry/junior、AuraHQ）独立报告相同问题

## HotPlex CC 场景分析

| 场景 | CC 行为 | Slack 流状态 | 风险 |
|------|---------|-------------|------|
| **活跃文本输出** | message.delta 每 50-200ms | appendStream 每 150-270ms | 安全 |
| **快速 tool call** (Read/Glob/Edit) | 10ms-2s 暂停 | 1-2s 无 append | 安全 |
| **慢 tool call** (Bash/make check) | 数秒到数分钟 | 无 append | **idle 30s 超时** |
| **Agent dispatch** | 子 agent 期间无输出 | 无 append | **idle 30s 超时** |
| **长文本生成** | 持续输出 >4min | 持续 append | **wallclock 5min 超时** |

## Design

对齐飞书旋转模式，但使用 Slack 的实测 TTL，覆盖 **wallclock 和 idle 两种超时**。

### Rotation TTL 常量

```go
StreamTTL         = 5 * time.Minute   // empirical server-side wallclock limit
StreamRotationTTL = 4 * time.Minute   // proactive rotation before ~5min wallclock limit
StreamIdleTTL     = 20 * time.Second  // proactive rotation before ~30s idle limit
```

### Expired() / Idle() 方法

```go
func (w *NativeStreamingWriter) Expired() bool {
    // wallclock: time.Since(streamStartTime) > 4min
}

func (w *NativeStreamingWriter) Idle() bool {
    // idle: time.Since(lastActivityTime) > 20s
    // lastActivityTime updated on StartStream + each successful AppendStream
}
```

### writeWithStreaming 三层防护

**层 1 — 主动旋转**（Write 前）：
```go
if c.streamWriter != nil && (c.streamWriter.Expired() || c.streamWriter.Idle()) {
    oldWriter.SetSkipFallback()  // 避免旋转 Close() 发送重复内容
    go func() { _ = oldWriter.Close() }()
    // 创建新 writer...
}
```

**层 2 — 错误恢复**（Write 失败后）：
```go
if err := c.streamWriter.Write(text); err != nil {
    // 服务端可能已杀死流，用新 writer 重试
    oldWriter.SetSkipFallback()
    go func() { _ = oldWriter.Close() }()
    newWriter := c.adapter.NewStreamingWriter(...)
    if newWriter.Write(text) == nil {
        return nil  // 恢复成功
    }
}
```

**层 3 — Fallback**（恢复也失败时）：
```go
// 上层 WriteCtx 回退到 writeWithPostMessage
```

### 为什么旋转时 Close() 不发送重复内容

通过 `SetSkipFallback()` 标志：
- 旋转 Close() 时 `skipFallback = true` → 跳过 fallback PostMessage
- 已显示的内容不会重复发送
- 新流继续从当前内容开始

### lastActivityTime 追踪

- `Write()` 首次调用（StartStream 成功）时设置
- `appendWithRetry()` 每次 AppendStream 成功时更新
- 受 `mu` 互斥锁保护（appendWithRetry 在 flushLoop goroutine 运行）

## Implementation Phases

### Phase 1: 删除死代码

- [x] 删除 `stream.go:168-180`（Path A TTL 检查）
- [x] 删除 `ttlWarningLogged` 字段

### Phase 2: 实现 Wallclock 旋转

- [x] 修正 `StreamTTL = 5min`、`StreamRotationTTL = 4min`
- [x] 新增 `Expired()` 方法
- [x] 修改 `writeWithStreaming` 添加 wallclock 旋转检测

### Phase 3: 实现 Idle 旋转

- [x] 新增 `StreamIdleTTL = 20s`、`lastActivityTime` 字段
- [x] 新增 `Idle()` 方法
- [x] 更新 `appendWithRetry` 记录 `lastActivityTime`
- [x] 新增 `SetSkipFallback()` 方法
- [x] 修改 `Close()` 支持 `skipFallback`
- [x] 修改 `writeWithStreaming` 添加 idle 旋转 + 错误恢复

### Phase 4: 测试

- [x] `TestExpired_*` — Expired() 边界条件
- [x] `TestIdle_*` — Idle() 边界条件
- [x] `TestStreamIdleTTL` — 验证 TTL 值和不变量
- [x] `TestSetSkipFallback` — 验证 skipFallback 机制

## Acceptance Criteria

- [x] Path A 死代码已删除
- [x] Wallclock 旋转（4min / 5min）
- [x] Idle 旋转（20s / 30s）
- [x] Write 失败时流恢复（非 PostMessage fallback）
- [x] 旋转时不发送重复内容（skipFallback）
- [x] `-race` 测试通过
- [ ] 生产验证（下一版本发布后观察）

## References

- [python-slack-sdk #1859: message_not_in_streaming_state after undocumented timeouts](https://github.com/slackapi/python-slack-sdk/issues/1859)
- 飞书旋转实现：`internal/messaging/feishu/adapter.go:810-831`
- Slack 流式 writer：`internal/messaging/slack/stream.go`
