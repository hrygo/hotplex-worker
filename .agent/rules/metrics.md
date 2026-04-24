---
paths:
  - "**/metrics/*.go"
  - "**/tracing/*.go"
---

# 可观测性规范

> Prometheus 指标、OTel Trace、日志格式
> 参考：`docs/specs/Acceptance-Criteria.md` §OBS-001 ~ §OBS-010

## 日志格式（OTel Log Data Model 兼容）

```go
// 所有日志必须为 JSON 格式
log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
    Level: slog.LevelInfo,
}))

slog.Info("session created",
    "service.name", "hotplex-gateway",
    "session_id", sessionID,
    "trace_id", traceID,  // 若有 trace context
)
```

### 必填字段
| 字段 | 说明 |
|------|------|
| `timestamp` | ISO 8601 / Unix ms |
| `level` | DEBUG / INFO / WARN / ERROR / FATAL |
| `message` | 人类可读消息 |
| `service.name` | 固定 `hotplex-gateway` |
| `trace_id` | 若有 trace context |

### 日志级别规范
- **ERROR**：全量记录，不采样
- **正常日志**：按配置 `sample_rate` 采样（例：10%）

---

## Prometheus 指标

### 命名规范
```
<app_prefix>_<group>_<metric>_<unit_suffix>
前缀固定 hotplex_
```

### 核心指标（API 层 — RED 方法）
| 指标名 | 类型 | 说明 |
|--------|------|------|
| `hotplex_requests_total` | Counter | 请求总数 |
| `hotplex_request_duration_seconds` | Histogram | 请求延迟 |
| `hotplex_request_errors_total` | Counter | 错误总数，标签：error_code |

### 核心指标（基础设施层 — USE 方法）
| 指标名 | 类型 | 说明 |
|--------|------|------|
| `hotplex_sessions_active` | Gauge | 当前活跃 Session |
| `hotplex_sessions_created_total` | Counter | 累计创建数 |
| `hotplex_worker_crashes_total` | Counter | Worker 崩溃数，标签：worker_type, reason |
| `hotplex_worker_memory_bytes` | Gauge | Worker 内存占用，标签：worker_type |
| `hotplex_errors_total` | Counter | 所有错误，标签：component, error_code |
| `hotplex_worker_duration_seconds` | Histogram | Worker 执行时长 |

### 辅助指标
```go
// Backpressure
hotplex_broadcast_queue_capacity  // 容量上限
hotplex_broadcast_queue_depth    // 当前深度
hotplex_messages_dropped_total   // 丢弃消息数

// Pool
hotplex_pool_total       // 全局 pool 总容量
hotplex_pool_used        // 已用槽位
hotplex_user_pool_used   // per-user pool，标签：user_id
```

---

## OTel Span 创建与上下文注入

### 每个 AEP 事件对应一个 Span
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
        env.Metadata["span_id"] = spanCtx.SpanID().String()
    }

    handle(ctx, env) // 下游传播
}
```

### Span 命名格式
```
aep.init        → init 握手
aep.input       → 用户输入
aep.message.delta → 流式输出片段
aep.done        → Turn 结束
aep.error       → 错误事件
```

### 尾部采样策略（Tail-based Sampling）
- **ERROR trace**：100% 保留
- **latency > 5s**：优先保留
- **正常 trace**：1% 采样

---

## SLO 定义

| SLO | 指标 | 目标 |
|-----|------|------|
| Session 创建成功率 | `hotplex_sessions_created_total` | ≥ 99.5% |
| P99 延迟 | `hotplex_request_duration_seconds` P99 | < 5s |
| Worker 可用性 | `1 - hotplex_worker_crashes_total/...` | ≥ 99% |
| WAF 准确率 | 拒绝率/总请求 | > 99.9% |

---

## 告警原则
- **症状告警**（非根因告警）
- `HighSessionCreationFailureRate`：持续 5min 失败率 > 1%
- `HighWorkerCrashRate`：崩溃率 > 1%
- `HighLatency`：P99 > 5s 持续 5min
