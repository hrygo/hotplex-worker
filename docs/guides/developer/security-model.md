---
title: 开发者安全指南
weight: 16
description: HotPlex Gateway 安全机制配置：JWT、SSRF 防御、命令白名单与进程隔离
---

# 开发者安全指南

> 理解和配置 HotPlex Gateway 的安全机制，确保 Agent 安全运行

## 概述

HotPlex Gateway 采用纵深防御（Defense in Depth）策略，通过七层安全机制保护系统：

```
网络层 → JWT/API Key 认证 → SSRF 防护 → 命令白名单 → 环境隔离 → Tool 控制 → 输出限制
```

开发者需要理解每层安全机制以正确配置和使用。本文档聚焦权限请求、Tool 访问控制和安全配置最佳实践。

## 权限请求（Permission Request）

### 何时触发

当 AI Agent 需要执行敏感操作时，Worker 通过 AEP `permission_request` 事件请求用户审批：

```json
{
  "type": "permission_request",
  "data": {
    "id": "perm_123",
    "tool_name": "write_file",
    "description": "Write to /app/main.py",
    "args": ["/app/main.py", "write"]
  }
}
```

常见触发场景：
- **文件写入**：`write_file`、`edit_file`
- **命令执行**：`bash`
- **网络请求**：`web_fetch`
- **系统操作**：创建/删除文件、修改权限

### 权限模式

| 模式 | 行为 | 适用场景 |
|------|------|---------|
| `default` | 敏感操作需用户审批 | 生产环境、日常开发 |
| `plan` | 仅在规划阶段请求权限 | 复杂项目、需要人工审查 |
| `bypassPermissions` | 所有操作自动批准 | 受信任的自动化场景、CI/CD |

### 交互超时

默认 5 分钟超时，超时后自动拒绝（auto-deny）。通过 `InteractionManager` 管理。

超时后 Worker 收到 `allowed: false`，可选择替代方案或终止操作。

### 审批最佳实践

1. **检查 tool_name**：确认请求的操作类型
2. **阅读 description**：理解操作的具体内容和影响范围
3. **审查 args**：检查参数是否合理（特别是文件路径和命令内容）
4. **谨慎批准 bash**：命令执行是最敏感的操作

## Tool 访问控制

### Tool 分类

| 类别 | Tool 名称 | 风险级别 | 生产环境 |
|------|----------|---------|---------|
| Safe | `read_file`、`write_file`、`edit_file`、`grep`、`glob` | 低 | 允许 |
| Risky | `bash` | 高 | 需审批 |
| Network | `web_fetch` | 中 | 需审批 |
| System | `agent`、`notebook_edit`、`todo_write` | 低 | 按需 |

### 配置 allowed_tools

通过 `init` 的 `config.allowed_tools` 或 CLI `--allowed-tools` 限制 Session 可用工具：

```json
{
  "config": {
    "allowed_tools": ["read_file", "write_file", "edit_file", "grep", "glob", "bash"],
    "disallowed_tools": ["web_fetch"]
  }
}
```

### /perm 命令

使用 `/perm` 查看或修改当前 Session 的权限设置：

```
/perm              # 查看当前权限状态
/perm mode bypass  # 切换到 bypass 模式（慎用）
/perm mode default # 切换回默认模式
```

**警告**：`bypass` 模式下 AI Agent 可执行任何操作而不请求审批，仅在完全信任的场景下使用。

## Bash 命令策略

`CheckBashCommand()` 检测 Bash 工具中的危险命令：

| 严重级 | 行为 | 示例 |
|--------|------|------|
| P0（自动拒绝） | 直接阻止 | `rm -rf /`、`dd of=/`、`mkfs`、fork bomb |
| P1（记录并标记） | 记录日志并标记为高风险，实际确认由 Permission Request 系统处理 | SSH key 访问、AWS 元数据探测、crontab 修改 |

### 安全审查 Bash 执行

当 AI 请求执行 Bash 命令时：

1. **检查命令内容**：`permission_request` 的 `args` 包含完整命令
2. **识别管道和重定向**：`|`、`>`、`>>` 可能改变命令行为
3. **注意变量展开**：`$VAR`、`${VAR}` 可能包含意外值
4. **确认工作目录**：相对路径依赖当前目录

## 环境变量安全

### Gateway 注入的环境变量

Cron 执行和 Session 启动时，Gateway 注入以下环境变量：

| 变量 | 说明 |
|------|------|
| `GATEWAY_BOT_ID` | 当前 Bot 的 ID |
| `GATEWAY_USER_ID` | 当前用户的 ID |
| `GATEWAY_SESSION_ID` | 当前 Session 的 ID |

### 敏感变量保护

以下变量**不会**传递给 Worker 子进程：

- `AWS_*`、`ANTHROPIC_*`、`SLACK_*` 等前缀匹配的凭证变量
- `API_KEY`、`DATABASE_URL` 等精确匹配的敏感变量
- `CLAUDECODE`（防止嵌套 Agent）
- `GATEWAY_TOKEN`、`GATEWAY_ADDR`（Gateway 内部变量）

## 认证机制

### API Key

`internal/security/auth.go` 提供双层 Key 提取：

1. **HTTP Header**（`X-API-Key`，可自定义）
2. **Query Parameter**（`api_key`，浏览器 WebSocket 客户端专用）

开发模式下未配置 API Key 时，所有请求以 `anonymous` 身份通过。生产环境必须配置 API Key。

### JWT（ES256）

`internal/security/jwt.go` 强制使用 ES256（ECDSA P-256）签名算法：

- 拒绝所有非 ES256 的签名方法
- JTI 黑名单防止 Token 重放攻击
- `bot_id` claim 实现多 Bot 隔离

### JTI 黑名单

被撤销的 Token 通过内存黑名单（`jtiBlacklist`）追踪：
- `sync.Map` 存储 `jti → expiry`
- 后台 goroutine 每 60s 清理过期条目

## 安全配置最佳实践

### 1. 最小权限原则

```
# 只允许必要的 Tool
allowed_tools: ["read_file", "grep", "glob"]

# 而不是全部开放
allowed_tools: ["*"]
```

### 2. 分环境配置

```yaml
# config-dev.yaml
security:
  api_keys: []              # Dev 模式：无需认证
  tool_access: "all"        # 开放所有 Tool

# config-prod.yaml
security:
  api_keys: ["sk-xxx"]      # 强制认证
  tool_access: "safe_only"  # 仅 Safe 类 Tool
```

### 3. 定期轮换密钥

- JWT 签名密钥定期轮换
- API Key 定期更新
- 使用 JTI 黑名单机制撤销旧 Token

### 4. 审计日志

Gateway 的所有安全事件通过 `slog` JSON handler 记录：认证成功/失败、权限请求/响应、SSRF 拦截、Bash 策略触发、Session 状态转换。

### 5. 网络隔离

```yaml
server:
  listen: "127.0.0.1:8888"  # 仅本地访问
```

生产部署时通过反向代理（Nginx/Caddy）暴露，Gateway 本身不直接对外监听。

## SSRF 防护

`internal/security/ssrf.go` 实现四层 SSRF 检查：

| 层级 | 检查内容 |
|------|---------|
| 1. 协议限制 | 仅允许 `http` 和 `https` |
| 2. 裸 IP 检查 | IP 字面值直接匹配 BlockedCIDRs |
| 3. DNS 解析 | 解析主机名获取所有 IP |
| 4. CIDR 匹配 | 所有解析 IP 检查是否在阻断列表 |

高敏感场景使用 `ValidateURLDoubleResolve` 防御 DNS rebinding 攻击。

## 命令白名单

仅允许执行两个二进制：`claude` 和 `opencode`。

- 命令名不得包含路径分隔符或危险字符
- 禁止 Shell 执行（不经过 `/bin/sh`）
- 通过 `RegisterCommand()` 可动态添加新命令
