---
title: Integration Patterns Guide
description: Reverse proxy, CI/CD, monitoring, custom Worker, SDK integration, and webhook patterns for HotPlex.
persona: enterprise
difficulty: advanced
---

# Integration Patterns Guide

> 面向企业架构团队的 HotPlex 集成方案指南。涵盖反向代理、CI/CD、监控体系、自定义 Worker、SDK 集成和 Webhook 模式。

---

## 1. 反向代理集成

### Nginx

```nginx
upstream hotplex {
    server 127.0.0.1:8888;
    keepalive 64;
}

server {
    listen 443 ssl http2;
    server_name hotplex.example.com;

    ssl_certificate     /etc/ssl/certs/hotplex.pem;
    ssl_certificate_key /etc/ssl/private/hotplex.key;

    # WebSocket 升级
    location /ws {
        proxy_pass http://hotplex;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }

    # Admin API
    location /admin/ {
        proxy_pass http://127.0.0.1:9999;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        allow 10.0.0.0/8;
        deny all;
    }
}
```

### Caddy

```caddyfile
hotplex.example.com {
    reverse_proxy /ws localhost:8888 {
        header_up Connection {>Connection}
        header_up Upgrade {>Upgrade}
    }
    reverse_proxy /admin/* localhost:9999
}
```

### 注意事项

- WebSocket 长连接需要 `proxy_read_timeout >= 3600s`
- 设置 `X-Forwarded-Proto` 确保 HotPlex 正确识别 TLS
- Admin API 建议限制内网访问

---

## 2. CI/CD 集成

### Admin API 自动化

通过 Admin API 实现 CI/CD 流水线集成：

```bash
# 健康检查（部署后验证）
curl -sf -H "Authorization: Bearer $ADMIN_TOKEN" \
  http://localhost:9999/admin/health | jq '.status'

# Session 管理
curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  http://localhost:9999/admin/sessions | jq

# 强制清理过期 Session
curl -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  http://localhost:9999/admin/sessions/gc

# 配置回滚（CI/CD 安全网）
curl -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  http://localhost:9999/admin/config/rollback?version=1
```

### GitHub Actions 示例

```yaml
name: Deploy HotPlex
on:
  push:
    branches: [main]
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - name: Build
        run: make build
      - name: Deploy
        run: |
          scp ./bin/hotplex prod:/usr/local/bin/hotplex
          ssh prod "sudo systemctl restart hotplex"
      - name: Verify
        run: |
          curl -sf http://prod-server:9999/admin/health
```

---

## 3. 监控集成

### Prometheus + Grafana

HotPlex 内置 Prometheus 指标，直接对接标准监控栈：

```yaml
# prometheus.yml
scrape_configs:
  - job_name: hotplex
    static_configs:
      - targets: ['localhost:9999']
    metrics_path: /admin/metrics
    scrape_interval: 15s
```

**关键指标**：

| 指标 | 类型 | 说明 |
|------|------|------|
| `hotplex_pool_utilization_ratio` | Gauge | Session 池利用率 |
| `hotplex_pool_acquire_total` | Counter | 配额获取（按 result 分维） |
| `hotplex_session_active` | Gauge | 活跃 Session 数 |
| `hotplex_worker_memory_bytes` | Gauge | Worker 内存估算 |

### OpenTelemetry 集成

Gateway 入口处自动创建 OTel Span，链路传播到 Worker 生命周期：

```yaml
# 可选：启用 OTel 导出
OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4317
OTEL_SERVICE_NAME=hotplex-gateway
```

### 告警规则示例

```yaml
# Prometheus AlertManager
groups:
  - name: hotplex
    rules:
      - alert: HotPlexPoolExhausted
        expr: hotplex_pool_utilization_ratio > 0.9
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "HotPlex Session 池利用率超过 90%"
```

---

## 4. 自定义 Worker 集成

HotPlex Worker 采用 **BaseWorker embedding** 模式，支持自定义 Worker 类型：

### 接口要求

```go
// 必须实现的接口方法
type Worker interface {
    Start(ctx context.Context, env []string) error
    Input(ctx context.Context, content string) error
    Terminate() error
    Wait() error
    Health() error
}
```

### 注册模式

```go
// internal/worker/myworker/worker.go
package myworker

import (
    "github.com/hrygo/hotplex/internal/worker/base"
    "github.com/hrygo/hotplex/internal/worker"
)

type adapter struct {
    *base.BaseWorker  // 共享生命周期方法
}

func New(deps base.Deps) worker.Worker {
    return &adapter{BaseWorker: base.New(deps)}
}

// init() 注册到全局工厂
func init() {
    worker.Register("my_worker", New)
}
```

### 自定义配置

```yaml
worker:
  my_worker:
    command: "/usr/local/bin/my-worker"
    max_lifetime: 8h
```

---

## 5. SDK 集成

### Go SDK

```go
import "github.com/hrygo/hotplex/client"

client := client.New("ws://localhost:8888/ws",
    client.WithToken("your-jwt-token"),
)

// 创建 Session
session, err := client.CreateSession(ctx, &client.SessionRequest{
    WorkerType: "claude_code",
    WorkDir:    "/workspace/project",
})

// 发送输入
err = session.Input(ctx, "分析这个代码库的性能瓶颈")

// 接收流式输出
for event := range session.Events() {
    fmt.Println(event.Kind, event.Data)
}
```

### TypeScript SDK

```typescript
import { HotPlexClient } from '@hotplex/sdk';

const client = new HotPlexClient('ws://localhost:8888/ws', {
  token: 'your-jwt-token',
});

const session = await client.createSession({
  workerType: 'claude_code',
  workDir: '/workspace/project',
});

session.on('message.delta', (data) => process.stdout.write(data.text));
session.on('state', (data) => console.log('State:', data.state));
await session.input('分析这个代码库的性能瓶颈');
```

### Python SDK

```python
from hotplex import HotPlexClient

client = HotPlexClient("ws://localhost:8888/ws", token="your-jwt-token")
session = client.create_session(worker_type="claude_code", work_dir="/workspace/project")

for event in session.stream("分析这个代码库的性能瓶颈"):
    if event.kind == "message.delta":
        print(event.data["text"], end="")
```

---

## 6. Webhook 与定时集成

### Cron + AI 定时检查

利用 HotPlex AI-native Cron 调度器实现定时 Webhook 模式：

```bash
# 创建每日代码质量检查定时任务
hotplex cron create \
  --name "daily-code-quality" \
  --schedule "cron:0 9 * * 1-5" \
  -m "检查代码质量并汇总到飞书群" \
  --bot-id "$BOT_ID" \
  --owner-id "$USER_ID"
```

### Cron 配置限制

```yaml
cron:
  enabled: true
  max_concurrent_runs: 3   # 最大并发执行数
  max_jobs: 50              # 最大任务数
  default_timeout_sec: 300  # 单次执行超时
  tick_interval_sec: 60     # 调度器 tick 间隔
```

### 结果投递

Cron 任务执行结果自动投递到配置的平台（飞书卡片 / Slack 消息），无需额外 Webhook 配置。

### 外部触发

```bash
# 通过 Admin API 手动触发（CI/CD 集成）
curl -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  http://localhost:9999/admin/cron/trigger/daily-code-quality
```

---

## 7. 集成架构参考

```
                    ┌─────────────┐
                    │   Nginx     │
                    │  (TLS/WS)   │
                    └──────┬──────┘
                           │
                    ┌──────▼──────┐
                    │  HotPlex    │ ◄── Prometheus ── Grafana
                    │  Gateway    │ ◄── OTel ── Jaeger
                    │  :8888      │
                    └──┬───┬───┬──┘
                       │   │   │
              ┌────────┘   │   └────────┐
              ▼            ▼            ▼
        ┌──────────┐ ┌──────────┐ ┌──────────┐
        │ Claude   │ │ OpenCode │ │ Custom   │
        │ Code     │ │ Server   │ │ Worker   │
        │ Worker   │ │ Worker   │ │          │
        └──────────┘ └──────────┘ └──────────┘
              │            │            │
              ▼            ▼            ▼
        Slack/飞书    Admin API    CI/CD Pipeline
```
