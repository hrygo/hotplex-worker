---
type: design
tags:
  - project/HotPlex
  - observability/logging
  - observability/metrics
  - observability/tracing
---

# Observability Design

> HotPlex v1.0 可观测性设计，基于行业最佳实践。
>
> ⚠️ **修正**（2026-04-21）：本文档推荐 `zerolog`，但当前代码库使用 Go 标准库 `log/slog`。日志库配置已更新为 `slog`。

---

## 1. 设计原则

### 1.1 行业最佳实践

| 来源 | 核心观点 |
|------|----------|
| OpenTelemetry | 统一 traces/logs/metrics 出口，vendor-neutral |
| Prometheus | RED + USE 双方法覆盖 API 层和基础设施层 |
| Grafana Loki | LogQL 查询语言，label-first 架构 |
| Grafana Tempo | 对象存储成本低，Grafana 生态原生集成 |

### 1.2 推荐技术栈

| 层 | 方案 | 理由 |
|----|------|------|
| **日志库** | `log/slog`（标准库） + OTel Log Bridge | 标准库、结构化、vendor-neutral |
| **日志后端** | Grafana Loki | 与 Prometheus/Tempo 统一生态 |
| **追踪后端** | Grafana Tempo | 对象存储成本低 |
| **指标** | Prometheus + Grafana | 业界标准 |
| **OTel 收集层** | OTel Collector + Tail Sampling | 尾部采样是关键 |
| **SLO/告警** | Grafana SLO + AlertManager | OpenSLO 原生支持 |

---

## 2. 结构化日志

### 2.1 日志格式

**OpenTelemetry Log Data Model 兼容格式**：

```json
{
  "timestamp": "2026-03-30T10:00:00.123Z",
  "level": "INFO",
  "message": "Session created",
  "service.name": "hotplex-gateway",
  "service.version": "v1.0",
  "trace_id": "abc123def456",
  "span_id": "789xyz",
  "resource.session_id": "sess_abc123",
  "resource.user_id": "user_001",
  "resource.worker_type": "claude-code",
  "attributes.duration_ms": 5
}
```

### 2.2 slog 配置（当前实现）

> ⚠️ 本节已更新为与代码库一致的 `log/slog` 实现。原 zerolog 方案已废弃。

```go
import (
    "log/slog"
    "os"
)

func NewLogger(serviceName, version string) *slog.Logger {
    return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
        Level: slog.LevelInfo,
    })).With(
        "service.name", serviceName,
        "service.version", version,
    )
}

// 添加 trace context
func WithTrace(ctx context.Context, logger *slog.Logger) *slog.Logger {
    spanCtx := trace.SpanContextFromContext(ctx)
    if spanCtx.HasTraceID() {
        return logger.With(
            "trace_id", spanCtx.TraceID().String(),
            "span_id", spanCtx.SpanID().String(),
        )
    }
    return logger
}
```

### 2.3 日志级别规范

| 级别 | 场景 | 示例 |
|------|------|------|
| **DEBUG** | 诊断信息 | 函数入口、变量值、详细执行流 |
| **INFO** | 业务里程碑 | Session 创建/终止、连接建立 |
| **WARN** | 异常但可恢复 | 重试、超时、资源警告 |
| **ERROR** | 错误 | Worker crash、DB 错误、认证失败 |
| **FATAL** | 系统不可用 | Gateway 崩溃（应触发告警） |

### 2.4 日志采样策略

```go
// 混合采样：ERROR 全量，正常日志按比例
func (l *SampledLogger) Log() *zerolog.Event {
    if l.level == zerolog.ErrorLevel {
        return l.logger.Error()  // ERROR 全量
    }

    if l.level == zerolog.InfoLevel && l.sampleRate < 1.0 {
        if rand.Float64() > l.sampleRate {
            return l.logger.Discard()  // 丢弃
        }
    }

    return l.logger.Info()
}
```

---

## 3. Prometheus Metrics

### 3.1 指标命名规范

**格式**：`<应用前缀>_<逻辑组>_<度量名称>_<单位后缀>`

```go
// 命名规范示例
hotplex_session_duration_seconds      // Histogram：会话持续时间
hotplex_events_sent_total            // Counter：发送事件总数
hotplex_sessions_active             // Gauge：活跃会话数
hotplex_worker_memory_bytes          // Gauge：Worker 内存使用
```

### 3.2 RED + USE 双方法

**RED 方法**（API 层）：

| 类型 | 指标 | 说明 |
|------|------|------|
| **Rate** | `hotplex_requests_total` | 请求率 |
| **Errors** | `hotplex_request_errors_total` | 错误率 |
| **Duration** | `hotplex_request_duration_seconds` | 延迟分布 |

**USE 方法**（基础设施层）：

| 类型 | 指标 | 说明 |
|------|------|------|
| **Utilization** | `hotplex_worker_cpu_seconds_total` | CPU 使用率 |
| **Saturation** | `hotplex_sessions_active` / `hotplex_sessions_max` | 会话饱和度 |
| **Errors** | `hotplex_worker_crashes_total` | 错误数 |

### 3.3 核心指标定义

```go
// internal/telemetry/metrics.go

import "github.com/prometheus/client_golang/prometheus"

var Metrics = struct {
    // Session metrics
    SessionsActive prometheus.Gauge
    SessionsCreated prometheus.Counter
    SessionsTerminated prometheus.Counter

    // Event metrics
    EventsSent *prometheus.CounterVec
    EventsReceived *prometheus.CounterVec
    EventDuration prometheus.Histogram

    // Worker metrics
    WorkerProcessesActive prometheus.Gauge
    WorkerMemoryBytes *prometheus.GaugeVec
    WorkerCPUSeconds prometheus.Counter
    WorkerCrashes *prometheus.CounterVec

    // WebSocket metrics
    WSConnectionsActive prometheus.Gauge
    WSConnectionsTotal prometheus.Counter
    WSMessagesSent prometheus.Counter
    WSMessagesReceived prometheus.Counter

    // Error metrics
    ErrorsTotal *prometheus.CounterVec
}{
    SessionsActive: prometheus.NewGauge(prometheus.GaugeOpts{
        Name: "hotplex_sessions_active",
        Help: "Number of active sessions",
    }),

    EventsSent: prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "hotplex_events_sent_total",
        Help: "Total number of events sent",
    }, []string{"kind", "direction"}),

    EventDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
        Name:    "hotplex_event_duration_seconds",
        Help:    "Event processing duration",
        Buckets: prometheus.DefBuckets,
    }),

    WorkerCrashes: prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "hotplex_worker_crashes_total",
        Help: "Total number of worker crashes",
    }, []string{"worker_type", "reason"}),

    ErrorsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
        Name: "hotplex_errors_total",
        Help: "Total number of errors",
    }, []string{"component", "error_code"}),
}

// Register all metrics
func init() {
    prometheus.MustRegister(
        Metrics.SessionsActive,
        Metrics.EventsSent,
        Metrics.EventDuration,
        Metrics.WorkerCrashes,
        Metrics.ErrorsTotal,
        // ...
    )
}
```

### 3.4 多租户支持

```go
// 通过 label 支持多租户
var SessionMetrics = prometheus.NewGaugeVec(prometheus.GaugeOpts{
    Name: "hotplex_sessions_active",
    Help: "Number of active sessions",
}, []string{"tenant_id", "worker_type"})

// 使用
SessionMetrics.WithLabelValues("tenant_001", "claude-code").Inc()
```

---

## 4. 分布式追踪

### 4.1 OpenTelemetry 集成

```go
// internal/telemetry/tracer.go

import (
    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/trace"
    "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
    "go.opentelemetry.io/otel/sdk/trace/tracetest"
    semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
)

func InitTracer(ctx context.Context, config *TracerConfig) (func(), error) {
    var exporter trace.SpanExporter
    var err error

    switch config.Exporter {
    case "otlp":
        exporter, err = otlptracegrpc.New(ctx,
            otlptracegrpc.WithEndpoint(config.Endpoint),
            otlptracegrpc.WithInsecure(),
        )
    case "tempo":
        // Grafana Tempo via OTLP
        exporter, err = otlptracegrpc.New(ctx,
            otlptracegrpc.WithEndpoint(config.TempoEndpoint),
        )
    case "stdout", "":
        exporter = tracetest.NewStdoutExporter()
    default:
        exporter = tracetest.NewNoopExporter()
    }

    tp := trace.NewTracerProvider(
        trace.WithBatcher(exporter),
        trace.WithResource(resource.NewWithAttributes(
            semconv.SchemaURL,
            semconv.ServiceName("hotplex-gateway"),
            semconv.ServiceVersion("v1.0"),
        )),
        trace.WithSampler(trace.ParentBased(
            trace.TraceIDRatioBased(config.SampleRate),
        )),
    )

    otel.SetTracerProvider(tp)

    return func() {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        tp.Shutdown(ctx)
    }, nil
}
```

### 4.2 AEP 事件追踪

```go
// internal/telemetry/aep_tracer.go

type AEPTracer struct {
    tracer trace.Tracer
}

func (t *AEPTracer) StartSpan(ctx context.Context, event *AEPEvent) (context.Context, trace.Span) {
    spanName := fmt.Sprintf("aep.%s", event.Kind)

    ctx, span := t.tracer.Start(ctx, spanName,
        trace.WithAttributes(
            attribute.String("session_id", event.SessionID),
            attribute.String("event_id", event.ID),
            attribute.String("kind", event.Kind),
            attribute.Int64("seq", event.Seq),
        ),
    )

    // 注入 trace context 到事件
    if event.Metadata == nil {
        event.Metadata = make(map[string]string)
    }
    spanCtx := span.SpanContext()
    event.Metadata["trace_id"] = spanCtx.TraceID().String()
    event.Metadata["span_id"] = spanCtx.SpanID().String()

    return ctx, span
}

// 从事件中提取 trace context
func ExtractTraceContext(event *AEPEvent) context.Context {
    if event.Metadata == nil {
        return context.Background()
    }

    traceIDStr := event.Metadata["trace_id"]
    spanIDStr := event.Metadata["span_id"]

    traceID, _ := trace.TraceIDFromHex(traceIDStr)
    spanID, _ := trace.SpanIDFromHex(spanIDStr)

    spanCtx := trace.NewSpanContext(trace.SpanContextConfig{
        TraceID:    traceID,
        SpanID:     spanID,
        TraceFlags: trace.FlagsSampled,
        Remote:     true,
    })

    return trace.ContextWithSpanContext(context.Background(), spanCtx)
}
```

### 4.3 采样策略

**Head Sampling（开发）**：
- 所有 trace 以固定概率采样
- 简单，但无法捕获尾部异常

**Tail Sampling（生产，必须）**：

```yaml
# otel-collector-config.yaml
processors:
  tail_sampling:
    decision_wait: 10s
    policies:
      # 错误 trace 100% 保留
      - name: errors-policy
        type: status_code
        status_code: {status_codes: [ERROR]}

      # 慢 trace 优先保留
      - name: slow-traces-policy
        type: latency
        latency: {threshold_ms: 5000}

      # 正常 trace 1% 采样
      - name: probabilistic-policy
        type: probabilistic
        probabilistic: {sampling_percentage: 1}
```

---

## 5. 告警与 SLO

### 5.1 告警原则

> ⚠️ **告警症状而非根因**

| ❌ 错误告警 | ✅ 正确告警 |
|-------------|-------------|
| `PostgresDown` | `HighSessionFailureRate` |
| `KafkaBrokerDown` | `MessageProcessingLatencyHigh` |
| `WorkerProcessCrash` | `SessionAvailabilityLow` |

### 5.2 SLO 定义

```yaml
# slo.yaml (OpenSLO format)
apiVersion: openslo/v1
kind: SLO
metadata:
  name: session-availability
spec:
  service: hotplex-gateway
  sli:
    thresholdMetric:
      source: prometheus
      query: |
        sum(rate(hotplex_sessions_created_total[5m])) /
        sum(rate(hotplex_sessions_terminated_total[5m])) > 0.995
  objectives:
    - displayName: 99.5% Availability
      target: 0.995
  timeWindow:
    - duration: 30d
```

### 5.3 关键 SLO

| SLO | Target | 测量方式 |
|-----|--------|----------|
| Session 创建成功率 | 99.5% | `created / (created + errors)` |
| 响应延迟 P99 | < 5s | `histogram_quantile(0.99)` |
| Worker 可用性 | 99% | `active / (active + crashed)` |
| WAF 准确率 | > 99.9% | `(blocked_attacks) / (blocked_attacks + false_positives)` |

### 5.4 Prometheus 告警规则

```yaml
# prometheus/alerts.yml
groups:
  - name: hotplex
    rules:
      # Critical: Session 创建失败率高
      - alert: HighSessionCreationFailureRate
        expr: |
          rate(hotplex_sessions_create_errors_total[5m]) /
          rate(hotplex_sessions_created_total[5m]) > 0.01
        for: 5m
        labels:
          severity: critical
        annotations:
          summary: "Session creation failure rate > 1%"

      # Warning: 延迟高
      - alert: HighLatency
        expr: |
          histogram_quantile(0.99,
            rate(hotplex_event_duration_seconds_bucket[5m])) > 5
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "P99 latency > 5s"

      # Warning: Worker 崩溃率高
      - alert: HighWorkerCrashRate
        expr: |
          rate(hotplex_worker_crashes_total[5m]) > 0.1
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "Worker crash rate elevated"

      # Critical: Worker 池耗尽
      - alert: WorkerPoolExhausted
        expr: |
          hotplex_worker_processes_active /
          hotplex_worker_processes_max >= 0.95
        for: 10m
        labels:
          severity: critical
        annotations:
          summary: "Worker pool at 95% capacity"
```

---

## 6. Grafana Dashboard

### 6.1 核心面板

```json
{
  "panels": [
    {
      "title": "Active Sessions",
      "type": "stat",
      "targets": [{ "expr": "hotplex_sessions_active" }]
    },
    {
      "title": "Events Throughput",
      "type": "timeseries",
      "targets": [
        { "expr": "rate(hotplex_events_sent_total[1m])", "legendFormat": "{{kind}}" }
      ]
    },
    {
      "title": "Event Processing Latency (P50/P95/P99)",
      "type": "timeseries",
      "targets": [
        { "expr": "histogram_quantile(0.50, rate(hotplex_event_duration_seconds_bucket[5m]))", "legendFormat": "P50" },
        { "expr": "histogram_quantile(0.95, rate(hotplex_event_duration_seconds_bucket[5m]))", "legendFormat": "P95" },
        { "expr": "histogram_quantile(0.99, rate(hotplex_event_duration_seconds_bucket[5m]))", "legendFormat": "P99" }
      ]
    },
    {
      "title": "Worker Resource Usage",
      "type": "timeseries",
      "targets": [
        { "expr": "hotplex_worker_memory_bytes / 1024 / 1024", "legendFormat": "{{worker_type}} MB" }
      ]
    },
    {
      "title": "Error Rate by Component",
      "type": "timeseries",
      "targets": [
        { "expr": "rate(hotplex_errors_total[5m])", "legendFormat": "{{component}} - {{error_code}}" }
      ]
    },
    {
      "title": "Trace Overview",
      "type": "traces-panel",
      "targets": []
    }
  ]
}
```

---

## 7. 配置

```yaml
# configs/observability.yaml
observability:
  logging:
    level: info
    format: json
    sample_rate: 1.0  # 1.0 = 全量, 0.1 = 10%

  metrics:
    enabled: true
    port: 9090
    path: "/metrics"

  tracing:
    enabled: true
    exporter: "otlp"  # otlp|tempo|stdout|none
    endpoint: "${OTEL_EXPORTER_OTLP_ENDPOINT}"
    sample_rate: 0.1  # Head sampling rate
    tail_sampling: true

  health:
    enabled: true
    port: 9999
    path: "/health"
```

---

## 8. 实施路线图

| 阶段 | 任务 | 产出 |
|------|------|------|
| **Phase 1** | 结构化日志 (zerolog) | JSON 格式日志输出 |
| **Phase 2** | Prometheus Metrics | `/metrics` 端点 + 核心指标 |
| **Phase 3** | OpenTelemetry Tracing | Trace context 传播 |
| **Phase 4** | Tail Sampling | OTel Collector 配置 |
| **Phase 5** | Grafana Dashboard | SLO Dashboard |
| **Phase 6** | AlertManager | 告警规则 |

---

## 9. 参考资料

- [OpenTelemetry SDK](https://opentelemetry.io/docs/)
- [Prometheus Metrics](https://prometheus.io/docs/concepts/metric_types/)
- [Prometheus Naming Conventions](https://prometheus.io/docs/practices/naming/)
- [Grafana Loki](https://grafana.com/docs/loki/latest/)
- [Grafana Tempo](https://grafana.com/docs/tempo/latest/)
- [OpenSLO Specification](https://github.com/openslo/openslo)
- [RED Method](https://grafana.com/blog/2022/11/29/how-we-used-the-red-method-and-grafana-to-identify-a-customer-impacting-bug/)
- [USE Method](http://www.brendangregg.com/usemethod.html)