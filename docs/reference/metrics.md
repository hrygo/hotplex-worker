---
title: "Metrics 参考"
description: "HotPlex Gateway 暴露的所有 Prometheus 指标完整参考"
---

# Metrics 参考

> 所有 Prometheus 指标的名称、类型、标签和使用场景参考。

## 概述

HotPlex Gateway 通过 `internal/metrics/metrics.go` 注册 Prometheus 指标。所有指标使用 `promauto` 自动注册，前缀固定为 `hotplex_`。

## 采集端点

指标通过 Admin API 暴露：

```
GET http://localhost:9999/admin/metrics
```

Prometheus scrape 配置示例：

```yaml
scrape_configs:
  - job_name: 'hotplex-gateway'
    static_configs:
      - targets: ['localhost:9999']
    metrics_path: '/admin/metrics'
    scrape_interval: 15s
```

## Gateway 指标

### 连接指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `hotplex_gateway_connections_open` | Gauge | 当前打开的 WebSocket 连接数 |
| `hotplex_gateway_messages_total` | Counter | 消息总数，label: `direction`（incoming/outgoing）, `event_type` |
| `hotplex_gateway_events_total` | Counter | 转发事件总数，label: `event_type`, `direction` |

### 背压指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `hotplex_gateway_deltas_dropped_total` | Counter | 因背压丢弃的 `message.delta` 事件数 |
| `hotplex_gateway_events_no_subscribers_dropped_total` | Counter | 无订阅者时丢弃的事件数，label: `event_type` |
| `hotplex_gateway_platform_dropped_total` | Counter | 平台连接缓冲区满时丢弃的事件数，label: `event_type` |

### Delta 聚合指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `hotplex_gateway_delta_coalesced_total` | Counter | 被 coalescer 合并的 delta 事件数 |
| `hotplex_gateway_delta_flush_total` | Counter | 合并后刷新到平台连接的次数 |

### 错误指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `hotplex_gateway_errors_total` | Counter | 错误总数，label: `error_code` |

### 常用查询

```promql
# 每秒消息速率
rate(hotplex_gateway_messages_total[5m])

# Delta 丢弃率
rate(hotplex_gateway_deltas_dropped_total[5m])

# 按事件类型的消息分布
sum by (event_type) (rate(hotplex_gateway_messages_total{direction="outgoing"}[5m]))
```

## Session 指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `hotplex_sessions_active` | Gauge | 当前活跃 Session 数，label: `state`（created/running/idle） |
| `hotplex_sessions_total` | Counter | 累计创建 Session 数，label: `worker_type` |
| `hotplex_sessions_terminated_total` | Counter | 终止 Session 数，label: `reason` |
| `hotplex_sessions_deleted_total` | Counter | GC 物理删除 Session 数 |

### 终止原因标签

| reason | 说明 |
|--------|------|
| `idle_timeout` | 空闲超时 |
| `max_lifetime` | 最大生命周期 |
| `client_kill` | 客户端主动终止 |
| `admin_kill` | Admin API 终止 |
| `zombie` | Zombie 检测（无响应） |
| `crash` | Worker 进程崩溃 |

### 常用查询

```promql
# 当前活跃 Session
hotplex_sessions_active

# Session 创建成功率
rate(hotplex_sessions_total[5m])

# Zombie 终止率
rate(hotplex_sessions_terminated_total{reason="zombie"}[5m])
```

## Worker 指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `hotplex_workers_running` | Gauge | 当前运行的 Worker 进程数，label: `worker_type` |
| `hotplex_worker_starts_total` | Counter | Worker 启动次数，label: `worker_type`, `result`（success/failed） |
| `hotplex_worker_exec_duration_seconds` | Histogram | Worker 执行时长，label: `worker_type` |
| `hotplex_worker_crashes_total` | Counter | Worker 崩溃次数，label: `worker_type`, `exit_code` |
| `hotplex_worker_memory_bytes` | Gauge | Worker 估算内存，label: `worker_type` |

### 执行时长分桶

```
1s, 5s, 15s, 30s, 60s, 120s, 300s, 600s, 1800s
```

### 常用查询

```promql
# Worker 可用性 SLO
1 - (rate(hotplex_worker_crashes_total[5m]) / rate(hotplex_worker_starts_total[5m]))

# P95 执行时长
histogram_quantile(0.95, rate(hotplex_worker_exec_duration_seconds_bucket[5m]))

# Worker 内存总量
sum by (worker_type) (hotplex_worker_memory_bytes)
```

## Pool 配额指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `hotplex_pool_acquire_total` | Counter | 配额获取尝试次数，label: `result` |
| `hotplex_pool_release_errors_total` | Counter | 双重释放错误数（表示 bug） |
| `hotplex_pool_utilization_ratio` | Gauge | Pool 利用率（0-1） |

### 获取结果标签

| result | 说明 |
|--------|------|
| `success` | 成功获取 |
| `pool_exhausted` | 全局 Worker 数已满 |
| `user_quota_exceeded` | 单用户 Session 数已满 |
| `memory_exceeded` | 单用户内存超限 |

### 常用查询

```promql
# Pool 利用率
hotplex_pool_utilization_ratio

# 配额拒绝率
rate(hotplex_pool_acquire_total{result!="success"}[5m])
```

## Cron 指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `hotplex_cron_fires_total` | Counter | 任务触发次数，label: `job_name` |
| `hotplex_cron_errors_total` | Counter | 执行错误数，label: `job_name`, `error_type` |
| `hotplex_cron_duration_seconds` | Histogram | 执行时长，label: `job_name` |

### 执行时长分桶

```
1s, 5s, 15s, 30s, 60s, 120s, 300s, 600s, 1800s
```

### 常用查询

```promql
# Cron 成功率
rate(hotplex_cron_fires_total[5m]) - rate(hotplex_cron_errors_total[5m])

# 按任务名的平均执行时长
rate(hotplex_cron_duration_seconds_sum[5m]) / rate(hotplex_cron_duration_seconds_count[5m])
```

## Streaming Card 指标

| 指标 | 类型 | 说明 |
|------|------|------|
| `hotplex_streaming_card_rotations_total` | Counter | Streaming Card TTL 触发的旋转次数 |
| `hotplex_streaming_card_rotation_failures_total` | Counter | 旋转失败数，label: `phase`（close_old/ensure_card） |
| `hotplex_streaming_card_flush_fallbacks_total` | Counter | CardKit 降级到 IM Patch 的次数 |

## SLO 参考

| SLO | 指标 | 目标 |
|-----|------|------|
| Session 创建成功率 | `sessions_total` | >= 99.5% |
| P99 执行延迟 | `worker_exec_duration_seconds` P99 | < 5s |
| Worker 可用性 | `1 - crashes/starts` | >= 99% |
| Pool 拒绝率 | `pool_acquire_total{result!="success"}` | < 0.1% |

## 参考

- [可观测性配置](../guides/enterprise/observability.md)：日志和 trace 配置
- [资源限制](../guides/enterprise/resource-limits.md)：资源配额详解
