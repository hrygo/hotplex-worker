---
title: Compliance and Audit Guide
weight: 29
description: Security auditing, config change tracking, credential management, and token lifecycle for compliance requirements.
persona: enterprise
difficulty: advanced
---

# Compliance and Audit Guide

> 面向企业合规团队的 HotPlex 审计与安全指南。涵盖安全审计、配置变更追踪、凭证管理和 Token 生命周期。

---

## 1. 安全审计

### 诊断检查

使用内置 security 诊断命令进行安全基线检查：

```bash
hotplex check --security
```

检查范围包括：JWT 配置完整性、TLS 状态、Admin API 暴露面、环境变量泄漏风险、Worker 命令白名单合规性。

### 输入验证层级

所有外部输入经过双层验证：

| 层级 | 检查项 | 限制 |
|------|--------|------|
| 句法层 | JSON Schema、类型、长度 | MaxEnvelopeBytes = 1MB |
| 语义层 | 命令白名单、业务规则 | 仅 `claude` / `opencode` |

---

## 2. 配置变更审计

### 审计日志

Config Watcher 自动记录所有配置变更，包含完整的 diff 信息：

```
ConfigChange{
  Timestamp: 2026-05-10T08:30:00Z,
  Field:     "pool.max_size",
  OldValue:  "100",
  NewValue:  "200",
  Hot:       true,              // 是否立即生效
}
```

- 审计日志上限 **256 条**，超出后 FIFO 裁剪
- 敏感字段（`security.api_keys`、`security.jwt_secret`）自动脱敏为 `[REDACTED]`
- 通过 `Watcher.AuditLog()` API 获取完整审计记录

### 配置历史与回滚

Watcher 维护 **64 个版本**的完整配置快照：

```bash
# 回滚到上一个版本
curl -X POST -H "Authorization: Bearer $ADMIN_TOKEN" \
  http://localhost:9999/admin/config/rollback?version=1
```

**回滚特性**：
- `version=1` 回退一步，`version=2` 回退两步
- 从内存快照恢复，不依赖磁盘文件
- 回滚后通过 ConfigStore 原子传播给所有观察者

### Hot vs Static 字段区分

| 类别 | 字段 | 生效方式 |
|------|------|----------|
| Hot（立即生效） | log.level, pool.*, worker.timeout, admin.tokens | fsnotify + 500ms debounce |
| Static（需重启） | gateway.addr, db.path, tls.*, jwt_secret | 仅记录，下次重启生效 |

---

## 3. Session Event Store

所有 Session 事件持久化到 SQLite Event Store，提供完整的操作审计链：

- Session 创建、状态转换、输入输出记录
- Worker 生命周期事件（启动、终止、崩溃恢复）
- 事件保留期与 Session retention_period 一致（默认 7 天）

```bash
# 查询特定 session 的事件历史
curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  "http://localhost:9999/admin/sessions/{id}/events"
```

---

## 4. 凭证管理

### 原则：凭证不进配置文件

所有敏感凭证通过环境变量注入，Config struct 中敏感字段标记 `mapstructure:"-"`，永不从 YAML 读取：

| 凭证 | 环境变量 | 注入方式 |
|------|----------|----------|
| JWT Secret | `HOTPLEX_JWT_SECRET` | SecretsProvider |
| Admin Token | `HOTPLEX_ADMIN_TOKEN_1` / `_2` | 编号聚合 |
| API Key | `HOTPLEX_SECURITY_API_KEY_1` | 编号聚合 |
| Slack Token | `HOTPLEX_MESSAGING_SLACK_BOT_TOKEN` | 环境覆盖 |
| Feishu Secret | `HOTPLEX_MESSAGING_FEISHU_APP_SECRET` | 环境覆盖 |

### SecretsProvider 链

支持多来源凭证链式查找，按顺序尝试直到找到值：

```go
// 默认链（仅环境变量）
ChainedSecretsProvider{
    EnvSecretsProvider{}, // 从 HOTPLEX_* 环境变量读取
}

// 可扩展：实现 SecretsProvider 接口接入 Vault 等外部密钥管理
// type VaultProvider struct{ ... }
// func (p *VaultProvider) Get(key string) string { ... }
```

### Worker 环境隔离

Worker 进程的 Environment 列表支持 `${VAR:-default}` 模板展开：
- 引用未设置且无默认值的变量 → 该条目自动排除（不注入空值）
- `Sensitive` 检测自动脱敏 `AWS_*`、`ANTHROPIC_*`、`SLACK_*` 等前缀变量

---

## 5. JWT Token 生命周期

### Token 类型与 TTL

| Token 类型 | TTL | 用途 |
|------------|-----|------|
| Access Token | 5 分钟 | 客户端短期访问凭证 |
| Gateway Token | 1 小时 | WebSocket 长连接认证 |
| Refresh Token | 7 天 | Token 续期 |

### 安全机制

- **ES256 签名**：强制 ECDSA P-256，拒绝其他算法
- **JTI 防重放**：`crypto/rand` 生成唯一 ID + 内存黑名单 TTL 缓存
- **Bot 隔离**：`bot_id` claim 精确匹配，跨 Bot 操作被拒绝
- **密钥长度**：JWT Secret 必须 >= 32 字节（base64 编码）

### Admin Token 双 Token 轮转模式

使用 `_1` / `_2` 后缀实现零停机轮转：

```bash
# .env 文件
HOTPLEX_ADMIN_TOKEN_1=tk_current_xxx    # 当前活跃
HOTPLEX_ADMIN_TOKEN_2=tk_previous_yyy   # 前一代（过渡期）

# 轮转步骤：
# 1. 生成新 Token，设置到 _2
# 2. 所有客户端切换到 _2 的值
# 3. 将 _1 更新为新 Token
# 4. 清理旧 _2 值
```

两个 Token 同时有效，确保轮转期间无请求失败。

---

## 6. 审计报告生成

定期导出审计数据用于合规审查：

```bash
# 导出配置变更审计
curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  http://localhost:9999/admin/config/audit > audit-config-$(date +%Y%m%d).json

# 导出 Session 事件
curl -H "Authorization: Bearer $ADMIN_TOKEN" \
  http://localhost:9999/admin/sessions?format=audit > audit-sessions-$(date +%Y%m%d).json

# 数据库级审计
sqlite3 /var/lib/hotplex/hotplex.db <<EOF
SELECT 'sessions' as tbl, COUNT(*) as cnt FROM sessions
UNION ALL
SELECT 'events', COUNT(*) FROM events;
EOF
```

---

## 7. 合规检查清单

- [ ] JWT Secret >= 32 字节，仅通过环境变量注入
- [ ] Admin API 启用 IP 白名单 + Rate Limit
- [ ] 非本地地址启用 TLS (`tls_enabled: true`)
- [ ] 敏感凭证不在 config.yaml 中明文存储
- [ ] Worker 命令白名单仅含 `claude` / `opencode`
- [ ] 配置变更审计日志定期归档
- [ ] Session 事件保留期满足合规要求（默认 7 天，可调）
- [ ] Admin Token 定期轮转（建议月度）
