---
title: Observability Guide
weight: 26
description: Structured logging, Prometheus metrics, OpenTelemetry tracing, health endpoints, and alerting best practices for HotPlex Gateway.
persona: enterprise
difficulty: advanced
---

# Observability Guide

> 面向企业运维团队的 HotPlex Gateway 可观测性指南。涵盖结构化日志、Prometheus 指标体系、OpenTelemetry 分布式追踪、健康检查端点及告警最佳实践。

---

## 1. 结构化日志

HotPlex 使用 `log/slog` JSON Handler 输出结构化日志，兼容 OTel Log Data Model。

### 必填字段

| 字段 | 说明 |
|------|------|
| `timestamp` | ISO 8601 / Unix ms（slog 自动生成） |
| `level` | DEBUG / INFO / WARN / ERROR |
| `message` | 人类可读事件描述 |
| `service.name` | 固定 `hotplex-gateway` |
| `session_id` | 会话标识 |
| `user_id` | 用户标识 |
| `bot_id` | Bot 实例标识 |
| `trace_id` | 分布式追踪上下文（若存在） |

### 示例

```json
{
  "time": "2026-05-10T22:00:00.000Z",
  "level": "INFO",
  "msg": "session created",
  "service.name": "hotplex-gateway",
  "session_id": "01234567-89ab-cdef",
  "user_id": "U_ABC123",
  "bot_id": "B_XYZ789",
  "trace_id": "abc123def456"
}
```

### 日志级别规范

- **ERROR**：全量记录，不采样，触发告警评估
- **WARN**：降级或非致命异常，需关注但无需立即介入
- **INFO**：正常业务事件（session 创建/销毁、worker 启动等）
- **DEBUG**：开发调试信息，生产环境默认关闭

---

## 2. Prometheus 指标体系

所有指标以 `hotplex_` 为前缀，通过 `GET /admin/metrics` 以 Prometheus 格式暴露。

### 2.1 Session 指标

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `hotplex_sessions_active` | Gauge | `state` (created/running/idle) | 当前活跃会话数 |
| `hotplex_sessions_total` | Counter | `worker_type` | 累计创建会话数 |
| `hotplex_sessions_terminated_total` | Counter | `reason` (idle_timeout/max_lifetime/client_kill/admin_kill/zombie/crash) | 会话终止原因 |
| `hotplex_sessions_deleted_total` | Counter | — | GC 保留清理数 |

### 2.2 Worker 指标

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `hotplex_workers_running` | Gauge | `worker_type` | 当前运行 Worker 进程数 |
| `hotplex_worker_starts_total` | Counter | `worker_type`, `result` (success/failed) | Worker 启动尝试 |
| `hotplex_worker_exec_duration_seconds` | Histogram | `worker_type` | 执行耗时（桶：1/5/15/30/60/120/300/600/1800s） |
| `hotplex_worker_crashes_total` | Counter | `worker_type`, `exit_code` | Worker 崩溃计数 |
| `hotplex_worker_memory_bytes` | Gauge | `worker_type` | 预估内存（RLIMIT_AS 上限） |

### 2.3 Gateway 指标

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `hotplex_gateway_connections_open` | Gauge | — | 当前 WebSocket 连接数 |
| `hotplex_gateway_messages_total` | Counter | `direction`, `event_type` | WS 消息收发 |
| `hotplex_gateway_events_total` | Counter | `event_type`, `direction` | AEP 事件透传 |
| `hotplex_gateway_deltas_dropped_total` | Counter | — | 背压丢弃的 delta 事件 |
| `hotplex_gateway_platform_dropped_total` | Counter | `event_type` | 平台连接缓冲区溢出丢弃 |
| `hotplex_gateway_events_no_subscribers_dropped_total` | Counter | `event_type` | 无订阅者丢弃 |
| `hotplex_gateway_delta_coalesced_total` | Counter | — | Delta 合并数 |
| `hotplex_gateway_delta_flush_total` | Counter | — | 合并 delta 刷新数 |
| `hotplex_gateway_errors_total` | Counter | `error_code` | 错误分类计数 |

### 2.4 Pool 指标

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `hotplex_pool_acquire_total` | Counter | `result` (success/pool_exhausted/user_quota_exceeded) | 配额获取结果 |
| `hotplex_pool_release_errors_total` | Counter | — | 双重释放错误（代码 Bug 指标） |
| `hotplex_pool_utilization_ratio` | Gauge | — | Pool 利用率（0-1） |

### 2.5 Cron 指标

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `hotplex_cron_fires_total` | Counter | `job_name` | 任务触发次数 |
| `hotplex_cron_errors_total` | Counter | `job_name`, `error_type` | 执行错误分类 |
| `hotplex_cron_duration_seconds` | Histogram | `job_name` | 执行耗时（桶同 Worker） |

### 2.6 Streaming 指标

| 指标 | 类型 | 标签 | 说明 |
|------|------|------|------|
| `hotplex_streaming_card_rotations_total` | Counter | — | TTL 触发的卡片轮转 |
| `hotplex_streaming_card_rotation_failures_total` | Counter | `phase` (close_old/ensure_card) | 轮转失败 |
| `hotplex_streaming_card_flush_fallbacks_total` | Counter | — | CardKit 降级到 IM Patch |

---

## 3. OpenTelemetry 分布式追踪

### 启用方式

通过环境变量控制，无需修改代码：

```bash
# 启用追踪（设置 endpoint 即激活）
# 注意：当前使用 stdouttrace 导出器，endpoint 仅作为启用开关
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4317

# 显式禁用（默认也是禁用）
OTEL_SDK_DISABLED=true

# 服务名（可选，默认 hotplex-gateway）
OTEL_SERVICE_NAME=hotplex-gateway
```

> 注意：当前使用 `stdouttrace` 导出器（JSON 行输出到 stdout），`OTEL_EXPORTER_OTLP_ENDPOINT` 仅用于启用/禁用开关，实际 trace 数据写入 stdout。适合容器环境由日志采集器收集。生产环境需替换为 `otlptrace` 导出器直连 OTel Collector。

### Span 命名规范

每个 AEP 事件类型对应一个 Span，命名格式为 `aep.<event_type>`：

| Span 名称 | 触发时机 |
|-----------|---------|
| `aep.init` | WS 握手初始化 |
| `aep.input` | 用户输入 |
| `aep.message.delta` | 流式输出片段 |
| `aep.done` | Turn 完成 |
| `aep.error` | 错误事件 |

### 上下文传播

Span 的 `trace_id` 和 `span_id` 自动注入 AEP Envelope 的 `metadata` 字段，实现跨服务链路追踪。

### 采样策略（建议）

- **ERROR trace**：100% 保留
- **latency > 5s**：优先保留
- **正常 trace**：1% 采样

---

## 4. 健康检查端点

所有端点位于 Admin API（默认 `localhost:9999`）。

| 端点 | 认证 | 用途 | 适用场景 |
|------|------|------|----------|
| `GET /admin/health` | 无需认证 | Gateway 整体状态（含 DB、Workers） | 负载均衡探针 |
| `GET /admin/health/workers` | `health:read` | 按 Worker 类型主动探活 | 运维排障 |
| `GET /admin/health/ready` | 无需认证 | 就绪检查 | K8s readinessProbe / Docker HEALTHCHECK |

### 使用示例

```bash
# 负载均衡健康检查
curl http://localhost:9999/admin/health

# Worker 状态探查（需 Token）
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:9999/admin/health/workers

# K8s readinessProbe / Docker HEALTHCHECK
curl -sf http://localhost:9999/admin/health/ready
```

### Docker HEALTHCHECK 配置

Dockerfile 已内置：

```
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD curl -f http://localhost:9999/admin/health/ready || exit 1
```

---

## 5. Grafana Dashboard 建议

### 核心面板

| 面板 | PromQL | 类型 | 用途 |
|------|--------|------|------|
| 活跃 Session | `sum(hotplex_sessions_active)` | Stat | 容量规划 |
| Session 按状态分布 | `hotplex_sessions_active` | Pie | 状态倾斜 |
| WS 连接数 | `hotplex_gateway_connections_open` | Time series | 连接趋势 |
| Worker 崩溃率 | `rate(hotplex_worker_crashes_total[5m])` | Time series | 稳定性 |
| Worker 执行 P99 | `histogram_quantile(0.99, rate(hotplex_worker_exec_duration_seconds_bucket[5m]))` | Time series | 性能 |
| Pool 利用率 | `hotplex_pool_utilization_ratio * 100` | Gauge | 容量告警 |
| Delta 背压丢弃 | `rate(hotplex_gateway_deltas_dropped_total[5m])` | Time series | 流量压力 |
| Cron 错误率 | `rate(hotplex_cron_errors_total[5m])` | Time series | 定时任务健康 |
| 错误分类 | `hotplex_gateway_errors_total` | Stacked bar | 错误归因 |

### 布局建议

1. **顶栏**：活跃 Session / WS 连接 / Pool 利用率 / Uptime（4 个 Stat 面板）
2. **中间行**：Session 生命周期 + Worker 执行耗时趋势
3. **底部行**：错误分类 + 背压指标 + Cron 健康

---

## 6. 告警最佳实践

### 原则

- **症状告警**（Symptom-based）：告警用户可见的故障，而非根因指标
- **持续阈值**：连续 5 分钟超过阈值才触发，避免瞬时毛刺
- **分级响应**：P0（立即介入）→ P1（当日处理）→ P2（纳入迭代）

### 推荐告警规则

| 告警名 | PromQL | 阈值 | 级别 | 说明 |
|--------|--------|------|------|------|
| HighWorkerCrashRate | `rate(hotplex_worker_crashes_total[5m]) / rate(hotplex_worker_starts_total[5m])` | > 1% | P0 | Worker 崩溃率 |
| HighSessionFailureRate | `rate(hotplex_sessions_terminated_total{reason=~"crash\|zombie"}[5m])` | > 0 | P0 | 异常终止 |
| PoolExhaustion | `hotplex_pool_utilization_ratio` | > 0.9 | P1 | Pool 即将耗尽 |
| HighDeltaDropRate | `rate(hotplex_gateway_deltas_dropped_total[5m])` | > 10/s | P1 | 严重背压 |
| HighWorkerLatencyP99 | `histogram_quantile(0.99, rate(hotplex_worker_exec_duration_seconds_bucket[5m]))` | > 300s | P1 | Worker 执行卡顿 |
| CronJobFailures | `rate(hotplex_cron_errors_total[10m])` | > 0 | P2 | 定时任务异常 |
| PoolDoubleRelease | `increase(hotplex_pool_release_errors_total[1h])` | > 0 | P2 | 代码 Bug 信号 |

### SLO 参考

| SLO | 指标 | 目标 |
|-----|------|------|
| Session 创建成功率 | `hotplex_sessions_total` | >= 99.5% |
| Worker 可用性 | `1 - crashes/(starts+crashes)` | >= 99% |
| Worker 执行 P99 | `hotplex_worker_exec_duration_seconds` | < 300s |
