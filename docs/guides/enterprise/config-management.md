---
title: Config Management Guide
description: Configuration layers, inheritance, hot reload, history rollback, and multi-environment strategies for HotPlex.
persona: enterprise
difficulty: advanced
---

# Config Management Guide

> 面向企业运维团队的 HotPlex 配置管理指南。涵盖 5 层优先级、配置继承、热重载、版本回滚和多环境策略。

---

## 1. 5 层配置优先级

配置加载遵循 **后者覆盖前者** 原则，共 5 层：

| 优先级 | 来源 | 示例 | 说明 |
|--------|------|------|------|
| 1（最低） | Code Defaults | `Default()` 函数 | 所有字段均有零配置默认值 |
| 2 | 父配置继承 | `inherits: base.yaml` | 递归加载，支持多级 |
| 3 | 配置文件 | `config.yaml` | YAML/JSON/TOML |
| 4 | 环境变量 | `HOTPLEX_POOL_MAX_SIZE=200` | `HOTPLEX_` 前缀 |
| 5（最高） | CLI Flags | `--gateway-addr :9090` | 命令行参数 |

**关键特性**：二进制无需任何配置文件即可运行（Convention over Configuration）。

---

## 2. 配置继承

`inherits` 字段支持配置文件链式继承，实现基础配置共享：

```yaml
# configs/config-base.yaml — 团队共享基线
gateway:
  addr: "0.0.0.0:8888"
pool:
  max_size: 100
  max_idle_per_user: 5

# configs/config-prod.yaml — 生产环境覆盖
inherits: config-base.yaml
pool:
  max_size: 500        # 覆盖基线
security:
  tls_enabled: true    # 生产特有
```

### 环路检测

继承链自动检测循环引用，避免无限递归：

```
config-a.yaml → config-b.yaml → config-a.yaml
# 返回: config: inheritance cycle detected: [a.yaml, b.yaml] → a.yaml
```

### 解析顺序

子文件值覆盖父文件：先加载父配置 → 再用子文件 Viper 实例覆盖 → 最终得到合并结果。

---

## 3. 热重载机制

### 动态字段（立即生效）

以下字段修改后自动生效，无需重启：

| 字段 | 说明 |
|------|------|
| `log.level` | 日志级别 |
| `pool.max_size` | 全局 Session 上限 |
| `pool.max_idle_per_user` | 每用户 Session 上限 |
| `worker.max_lifetime` / `idle_timeout` / `execution_timeout` | Worker 超时 |
| `worker.auto_retry` | 自动重试策略 |
| `security.api_keys` | API Key 列表 |
| `security.allowed_origins` | CORS 来源 |
| `admin.tokens` / `requests_per_sec` / `burst` | Admin API 控制 |

### 静态字段（需重启）

修改后仅记录日志，下次重启生效：

| 字段 | 原因 |
|------|------|
| `gateway.addr` | 端口绑定 |
| `db.path` | 数据库连接 |
| `tls_enabled` / `tls_cert_file` / `tls_key_file` | TLS 配置 |
| `security.jwt_secret` | 签名密钥 |
| `log.format` | 日志格式 |

### 实现原理

- **fsnotify** 监听配置文件所在目录
- **500ms debounce** 防抖，合并连续写入
- `ConfigStore` 原子交换 + 观察者通知
- 回调并发限制（semaphore=4），防止 goroutine 爆发

---

## 4. 配置历史与回滚

### 版本快照

Watcher 维护最多 **64 个版本**的完整配置快照：

- 每次 reload 生成新快照
- 回滚从内存快照恢复，不重读磁盘
- 支持多步回滚（`version=1` 回退一步）

### 回滚操作

```bash
# 回滚到上一个版本
curl -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  http://localhost:9999/admin/config/rollback?version=1
```

### 审计日志

每次配置变更记录完整 diff（字段名、旧值、新值、是否热生效），上限 256 条。敏感字段自动脱敏。

---

## 5. 环境变量展开

配置值支持 `${VAR:-default}` 模板语法：

```yaml
worker:
  environment:
    - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY:-}    # 无默认值 → 未设置时排除
    - CUSTOM_VAR=${CUSTOM_VAR:-fallback_value}     # 有默认值 → 未设置时使用默认
```

**Worker 环境条目规则**：
- 引用已设置的变量 → 展开后注入
- 引用未设置变量 + 有 `:-default` → 使用默认值注入
- 引用未设置变量 + 无默认 → 该条目自动排除（不注入空值）

---

## 6. .env 文件加载顺序

环境变量来源优先级（从低到高）：

```
1. .env              # 仓库级共享配置（git tracked）
2. .env.local        # 本地覆盖（gitignored）
3. Shell 环境变量     # 系统/CI 环境注入
```

建议实践：
- `.env` 存放非敏感默认值（API endpoint、feature flag）
- `.env.local` 存放个人开发配置（密钥、本地路径）
- 生产环境使用系统密钥管理（Vault / K8s Secrets）

---

## 7. 多环境策略

### 目录结构

```
configs/
├── config.yaml              # 默认配置（开发环境）
├── config-staging.yaml      # Staging 覆盖
├── config-prod.yaml         # 生产覆盖
└── env.example              # 环境变量模板
```

### 通过 inherits 共享基线

```yaml
# config-staging.yaml
inherits: config.yaml
pool:
  max_size: 50

# config-prod.yaml
inherits: config.yaml
security:
  tls_enabled: true
pool:
  max_size: 500
  max_memory_per_user: 10737418240  # 10GB
```

### 部署时指定配置

```bash
# systemd
ExecStart=/usr/local/bin/hotplex gateway start --config /etc/hotplex/config-prod.yaml

# Docker
docker run -e HOTPLEX_POOL_MAX_SIZE=500 hotplex/gateway:latest

# 环境变量覆盖
export HOTPLEX_GATEWAY_ADDR=0.0.0.0:8888
export HOTPLEX_SECURITY_TLS_ENABLED=true
```

---

## 8. 配置变更 SOP

1. 在非生产环境验证配置变更
2. 通过热重载应用动态字段变更
3. 检查审计日志确认变更已生效
4. 静态字段变更安排维护窗口重启
5. 变更后验证健康检查和关键业务流程
6. 出现问题时立即执行 `rollback?version=1`
