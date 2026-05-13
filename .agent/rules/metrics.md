---
paths:
  - "**/metrics/*.go"
  - "**/tracing/*.go"
---

# 可观测性规范

> 日志格式 → `log/slog` JSON handler（见 AGENTS.md）

## Prometheus 指标命名

```
<app_prefix>_<group>_<metric>_<unit_suffix>
前缀固定 hotplex_
```

### 核心指标

| 指标名 | 类型 | 说明 |
|--------|------|------|
| `hotplex_requests_total` | Counter | 请求总数 |
| `hotplex_request_duration_seconds` | Histogram | 请求延迟 |
| `hotplex_request_errors_total` | Counter | 错误总数，标签：error_code |
| `hotplex_sessions_active` | Gauge | 当前活跃 Session |
| `hotplex_sessions_created_total` | Counter | 累计创建数 |
| `hotplex_worker_crashes_total` | Counter | Worker 崩溃数，标签：worker_type, reason |
| `hotplex_worker_memory_bytes` | Gauge | Worker 内存占用，标签：worker_type |
| `hotplex_worker_duration_seconds` | Histogram | Worker 执行时长 |

### 辅助指标
```go
hotplex_broadcast_queue_capacity  // 容量上限
hotplex_broadcast_queue_depth    // 当前深度
hotplex_messages_dropped_total   // 丢弃消息数
hotplex_pool_total / hotplex_pool_used
hotplex_user_pool_used           // per-user，标签：user_id
```

---

## OTel Span 创建

每个 AEP 事件对应一个 Span：

```go
func handleEvent(ctx context.Context, env *aep.Envelope) {
    ctx, span := otel.Tracer("hotplex-gateway").Start(ctx, "aep."+env.Event.Type)
    defer span.End()
    span.SetAttributes(
        attribute.String("session_id", env.SessionID),
        attribute.Int64("seq", env.Seq),
    )
    // trace context 注入事件 metadata
    if spanCtx := trace.SpanContextFromContext(ctx); spanCtx.IsValid() {
        env.Metadata["trace_id"] = spanCtx.TraceID().String()
    }
    handle(ctx, env)
}
```

Span 命名：`aep.{init,input,message.delta,done,error}`

### 尾部采样策略
- ERROR trace：100% 保留
- latency > 5s：优先保留
- 正常 trace：1% 采样
