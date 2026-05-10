---
title: Enterprise Deployment Guide
description: Production deployment, security hardening, and operational best practices for HotPlex Gateway.
persona: enterprise
difficulty: advanced
---

# Enterprise Deployment Guide

> 面向企业运维团队的 HotPlex Gateway 生产部署指南。涵盖部署架构选型、安全加固、资源配额、多租户隔离及监控体系。

---

## 1. 部署架构选型

| 场景 | 推荐方案 | 理由 |
|------|----------|------|
| 小团队（<20 人） | 单二进制 + systemd/launchd | 部署简单，运维成本低 |
| 中型团队（20-100 人） | Docker Compose | 内置备份、监控、自动重启 |
| 大型组织 / K8s | Docker + 外部编排 | 水平扩展、统一监控栈 |
| Windows Server | 单二进制 + SCM | 原生 service 管理 |

### 1.1 单二进制

```bash
make build
cp configs/env.example .env   # 至少设置 HOTPLEX_JWT_SECRET、HOTPLEX_ADMIN_TOKEN_1
./hotplex gateway start

# 注册为系统服务（Linux/macOS/Windows）
hotplex service install               # 用户级
hotplex service install --level system  # 系统级（需 sudo）
```

### 1.2 Docker

```bash
docker run -d --name hotplex --init --restart unless-stopped \
  -p 8888:8888 -p 9999:9999 \
  -e HOTPLEX_JWT_SECRET="${JWT_SECRET}" \
  -e HOTPLEX_ADMIN_TOKEN_1="${ADMIN_TOKEN}" \
  -v hotplex-data:/var/lib/hotplex/data \
  -v hotplex-logs:/var/log/hotplex \
  -v ./configs:/etc/hotplex:ro \
  hotplex:latest
```

`--init` **必须**——Worker fork 子进程需要 init 回收僵尸进程。

### 1.3 Docker Compose（推荐）

项目提供开箱即用的 Compose 配置，含 Gateway、备份 sidecar、Prometheus + Grafana：

```bash
docker-compose up -d                                        # 开发
docker-compose -f docker-compose.yml -f docker-compose.prod.yml up -d  # 生产
```

生产 override 增强：Traefik 自动 HTTPS（Let's Encrypt）、资源限制（CPU 4 核 / 8GB）、`GOMEMLIMIT=6500MiB`、`on-failure` 重启策略。备份 sidecar 每小时备份 SQLite，保留 30 天。

### 1.4 反向代理

Gateway 前放置 Nginx/Caddy/Traefik 终止 TLS。**关键**：WebSocket 长连接需 `proxy_read_timeout 3600s`（默认 60s 会导致断连）。

---

## 2. 安全加固

### 2.1 TLS

**决策规则**：Gateway 非 localhost 监听时，**必须**启用 TLS。推荐反向代理终止 TLS，Gateway 保持 localhost。

```yaml
security:
  tls_enabled: true
  tls_cert_file: "/etc/hotplex/tls/server.crt"
  tls_key_file: "/etc/hotplex/tls/server.key"
  allowed_origins: ["https://app.yourdomain.com"]
```

### 2.2 JWT 认证

ES256（ECDSA P-256）签名，**禁止 HS256**。JWT 包含 `sub`（用户）、`bot_id`（Bot 隔离）、`scope`（权限），Gateway 验证 `aud: "hotplex-gateway"`。

```bash
export HOTPLEX_JWT_SECRET="$(openssl rand -base64 32 | tr -d '\n')"  # 不要写入配置文件
```

### 2.3 API Key + Admin Token

编号式环境变量支持无损轮转（先加新 token，再移除旧的）：

```bash
# 客户端认证
HOTPLEX_SECURITY_API_KEY_1=ak-xxxxx   # 请求头：X-API-Key: ak-xxxxx

# Admin API 认证（生成时去掉 /+= 避免解析问题）
HOTPLEX_ADMIN_TOKEN_1="$(openssl rand -base64 32 | tr -d '/+=' | head -c 43)"
```

### 2.4 IP 白名单

```yaml
admin:
  ip_whitelist_enabled: true
  allowed_cidrs: ["127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12"]
```

Docker/K8s 环境建议用 NetworkPolicy 替代，避免 NAT 后 IP 失真。

### 2.5 内建安全机制（开箱即用）

| 能力 | 说明 |
|------|------|
| SSRF 防护 | 默认阻止 Worker 访问内网地址 |
| 环境变量隔离 | `env_blocklist` 阻止敏感变量泄露 |
| 命令白名单 | 仅允许 `claude`/`opencode` 二进制 |
| XML Sanitizer | 强制开启，防注入 |
| 路径安全 | `work_dir` 限制在允许目录树内 |

---

## 3. 网络配置

| 端口 | 服务 | 默认绑定 | 生产建议 |
|------|------|----------|----------|
| 8888 | WebSocket Gateway | `localhost:8888` | 反向代理后保持 localhost |
| 9999 | Admin API | `localhost:9999` | 绑定内网 IP 或保持 localhost |

外部访问时修改绑定：`gateway.addr: ":8888"`（监听所有接口，**必须**配合 TLS）。Admin API 通过 `admin.addr: "10.0.1.5:9999"` 绑定内网。

---

## 4. 资源管理

```yaml
pool:
  max_size: 100               # 全局最大并发 session
  max_idle_per_user: 5        # 每用户最大空闲 session
  max_memory_per_user: 3221225472  # 每用户内存上限（3GB）

worker:
  max_lifetime: 24h           # Worker 最大存活时间
  idle_timeout: 60m           # 空闲超时自动回收
  execution_timeout: 30m      # 单次执行超时
```

**调优参考**：

| 参数 | 小团队 | 中型 | 大型 |
|------|--------|------|------|
| `pool.max_size` | 50 | 100-200 | 500+ |
| `max_memory_per_user` | 1GB | 3GB | 3GB |
| `worker.max_lifetime` | 8h | 24h | 24h |

Docker 需同时配置容器级限制（`deploy.resources.limits: cpus 4, memory 8G`）+ `GOMEMLIMIT=6500MiB`（Go GC 软限制）。

---

## 5. 多租户（Bot 隔离）

### Per-Bot Agent 配置

三级 fallback（全局 -> 平台 -> Bot），**命中即终止**：

```
~/.hotplex/agent-configs/
├── SOUL.md                    # 全局默认
├── slack/SOUL.md              # Slack 平台覆盖
├── slack/U12345678/SOUL.md    # 特定 Bot 覆盖
└── feishu/ou_xxxx/SOUL.md     # 飞书特定 Bot
```

### 路由 + 工作目录隔离

JWT `bot_id` claim 实现 session 路由隔离。Per-bot `work_dir` 实现文件系统隔离：

```yaml
messaging:
  slack:
    work_dir: /var/hotplex/workspace/slack
  feishu:
    work_dir: /var/hotplex/workspace/feishu
```

---

## 6. 配置管理

**热重载**：`pool.max_size`、`admin.tokens`、`worker.auto_retry.*` 运行时生效，无需重启。

**继承**：`config-prod.yaml` 通过 `inherits: config.yaml` 仅覆盖差异项。优先级：CLI flag > env var > config file > code default。

**版本管理**：配置变更自动记录 64 个版本历史，支持快速回滚：

```bash
# 回滚到指定版本（version=1 回退一步）
curl -X POST -H "Authorization: Bearer $TOKEN" localhost:9999/admin/config/rollback?version=1
```

---

## 7. 健康检查与监控

### 健康端点

| 端点 | 用途 | 适用场景 |
|------|------|----------|
| `/admin/health` | Gateway 整体状态 | 负载均衡探针 |
| `/admin/health/workers` | Worker 进程状态 | 运维排障 |
| `/admin/health/ready` | 就绪检查（含 DB） | K8s readinessProbe |

### 监控栈

- **Prometheus 指标**：`/admin/metrics`，Docker Compose 预配置 `--profile monitoring` 启用 Prometheus + Grafana
- **OpenTelemetry**：设置 `OTEL_EXPORTER_OTLP_ENDPOINT` + `OTEL_SERVICE_NAME`

### SLO 目标

| 指标 | 目标 | 告警阈值 |
|------|------|----------|
| Session 创建成功率 | >= 99.5% | < 99% -> P1 |
| Session 创建 P99 延迟 | < 5s | > 8s -> P2 |
| Worker 崩溃率 | < 0.1% | > 0.5% -> P1 |
| Health check 成功率 | >= 99.9% | 连续 3 次失败 -> P1 |

---

## 8. 运维速查

```bash
curl -sf localhost:9999/admin/health/ready                              # 健康检查
curl -H "Authorization: Bearer $TOKEN" localhost:9999/admin/sessions    # Session 状态
hotplex update --check                                                  # 检查新版本
hotplex update -y --restart                                             # 更新并重启
sqlite3 /var/lib/hotplex/data/hotplex.db ".backup '/backups/$(date +%Y%m%d).db'"  # 备份
hotplex service logs -f                                                 # 日志（systemd/launchd）
```
