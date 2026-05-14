---
title: "安全策略参考"
weight: 9
description: "JWT、SSRF、命令白名单、Tool 控制、API Key 等安全配置完整参考"
---

# 安全策略参考

> 所有安全相关配置项、策略和阈值的完整参考手册。

## 概述

HotPlex Gateway 的安全策略分布在多个配置层：环境变量、`config.yaml`、SQLite 持久化配置。本文档按安全域组织所有配置项。

## JWT 配置

### 环境变量

| 变量 | 必填 | 说明 |
|------|------|------|
| `HOTPLEX_JWT_SECRET` | 是 | JWT 签名密钥。使用 `openssl rand -base64 32` 生成 |

### 签名算法

ES256（ECDSA P-256）是唯一允许的签名算法。源码实现位于 `internal/security/jwt.go`：

```go
// 拒绝所有非 ES256 的签名方法
switch token.Method.Alg() {
case "ES256":
    // 唯一允许的算法
default:
    return nil, fmt.Errorf("rejected signing method: %v", token.Header["alg"])
}
```

### Claims 结构

| 字段 | JSON Key | 类型 | 说明 |
|------|----------|------|------|
| Issuer | `iss` | string | 固定值 `hotplex` |
| Subject | `sub` | string | 用户 ID |
| Audience | `aud` | string | 受众（可配置校验） |
| ExpiresAt | `exp` | timestamp | 过期时间 |
| IssuedAt | `iat` | timestamp | 签发时间 |
| NotBefore | `nbf` | timestamp | 生效时间 |
| ID | `jti` | string | 唯一 ID（UUID v4），用于撤销检测 |
| UserID | `user_id` | string | 用户标识 |
| Scopes | `scopes` | []string | 权限范围 |
| Role | `role` | string | 角色 |
| BotID | `bot_id` | string | Bot 标识 |
| SessionID | `session_id` | string | Session 标识 |

### 密钥派生（HKDF）

当配置为 `[]byte`（原始密钥）时，通过 HKDF (RFC 5869) 从字节派生 ECDSA P-256 密钥。info 参数 `"hotplex-ecdsa-p256"` 将派生密钥绑定到特定上下文，防止跨协议密钥复用：

```go
func deriveECDSAP256Key(secret []byte) *ecdsa.PrivateKey {
    // HKDF-SHA256 extract-then-expand
    scalarBytes, _ := hkdf.Key(sha256.New, secret, nil, "hotplex-ecdsa-p256", 32)
    s := new(big.Int).SetBytes(scalarBytes)
    N := elliptic.P256().Params().N
    s.Mod(s, new(big.Int).Sub(N, big.NewInt(1)))
    s.Add(s, big.NewInt(1))         // scalar ∈ [1, N-1]
    x, y := elliptic.P256().ScalarBaseMult(s.Bytes())
    return &ecdsa.PrivateKey{...}
}
```

> **升级注意**：v1.11.3 从 `copy(secret)` 直接截断改为 HKDF。同一个 `HOTPLEX_JWT_SECRET` 会派生出不同的 ECDSA 密钥对，所有旧 token 在升级后立即失效。Go Client SDK 已同步更新。

### JTI 黑名单

| 参数 | 值 | 说明 |
|------|------|------|
| 存储 | `sync.Map` | 并发安全 |
| 清理间隔 | 60s | 后台 goroutine |
| TTL | Token TTL × 2 | 默认为 Token 过期时间的 2 倍 |

### Token 生命周期

| 类型 | 推荐 TTL |
|------|---------|
| Access Token | 5 分钟 |
| Gateway Token | 1 小时 |
| Refresh Token | 7 天 |

## API Key 配置

### 环境变量

```bash
# 支持多 Key 轮换（后缀 _1..N）
HOTPLEX_SECURITY_API_KEY_1=key-1
HOTPLEX_SECURITY_API_KEY_2=key-2

# 自定义 Header 名称（默认 X-API-Key）
# 通过 config.yaml: security.api_key_header
```

### 认证流程

```
1. 检查 HTTP Header（默认 X-API-Key）
2. 若 Header 为空，检查 query parameter（api_key）
3. Key 匹配 validKey map → 通过
4. 未配置任何 Key → 开发模式（anonymous 通过）
```

### Admin Token

```bash
# Admin API Token（独立于 API Key）
HOTPLEX_ADMIN_TOKEN_1=admin-token-1
# HOTPLEX_ADMIN_TOKEN_2=admin-token-2
```

## SSRF 防护配置

SSRF 防护在 `internal/security/ssrf.go` 中实现，属于编译时内置策略，不需要运行时配置。

### 阻断的 CIDR 列表

| CIDR | 描述 |
|------|------|
| `127.0.0.0/8` | IPv4 Loopback |
| `::1/128` | IPv6 Loopback |
| `10.0.0.0/8` | RFC 1918 Class A |
| `172.16.0.0/12` | RFC 1918 Class B |
| `192.168.0.0/16` | RFC 1918 Class C |
| `fc00::/7` | IPv6 唯一本地 |
| `169.254.0.0/16` | IPv4 Link-local |
| `fe80::/10` | IPv6 Link-local |
| `169.254.169.254/32` | AWS/GCP/Azure IMDS |
| `100.100.100.200/32` | 阿里云元数据 |
| `192.0.0.0/24` | RFC 8520 DHCP |
| `224.0.0.0/4` | IPv4 Multicast |
| `ff00::/8` | IPv6 Multicast |
| `0.0.0.0/8` | 当前主机 |
| `100.64.0.0/10` | Carrier-grade NAT |

### 检查函数

| 函数 | 用途 |
|------|------|
| `ValidateURL(targetURL)` | 标准 SSRF 检查（协议 → 裸 IP → DNS → CIDR） |
| `ValidateURLDoubleResolve(targetURL)` | 增加防 DNS 重新绑定（延迟 1s 后重解析） |
| `ValidateURLAndLog(url, logger)` | 阻断时自动记录 warn 日志 |

## 命令白名单配置

### 默认白名单

| 命令 | 说明 |
|------|------|
| `claude` | Claude Code Worker |
| `opencode` | OpenCode Server Worker |

### 扩展白名单

通过 `RegisterCommand()` 动态添加：

```go
err := security.RegisterCommand("custom-agent")
```

### 验证规则

| 规则 | 实现 |
|------|------|
| 无路径分隔符 | 拒绝 `/` 和 `\` |
| 无危险字符 | 拒绝 `;`, `\|`, `&`, `` ` ``, `$`, `\n` 等 20+ 字符 |
| 仅 ASCII 可打印 | `0x20 ≤ char ≤ 0x7E` |
| 非空 | 拒绝空字符串 |

### Bash 命令策略

| 级别 | 模式 | 行为 |
|------|------|------|
| P0 | `rm -rf /`, `dd of=/`, `mkfs`, `fork bomb` | 自动拒绝 |
| P1 | SSH key 访问, AWS 元数据, crontab 修改 | 记录 + 需确认 |

## Tool 访问控制

### 开发环境工具集

```go
var AllowedTools = map[string]bool{
    "Read": true, "Edit": true, "Write": true,
    "Bash": true, "Grep": true, "Glob": true,
    "Agent": true, "WebFetch": true, "NotebookEdit": true,
    "TodoWrite": true,
}
```

### 生产环境工具集

```go
var ProductionAllowedTools = map[string]bool{
    "Read": true, "Grep": true, "Glob": true,
}
```

### 模型白名单

| 模型 | 标识符 |
|------|--------|
| Claude Sonnet 4.6 | `claude-sonnet-4-6` |
| Claude Opus 4.6 | `claude-opus-4-6` |
| Claude 3.5 Sonnet | `claude-3-5-sonnet-20241022` |
| Claude 3.5 Haiku | `claude-3-5-haiku-20241022` |
| Claude 3 Opus | `claude-3-opus-20240229` |
| Claude 3 Sonnet | `claude-3-sonnet-20240229` |

模型名匹配为 **case-insensitive**。

## 环境变量隔离

### CLI 保护变量（不可被 .env 覆盖）

```
HOME, PATH, USER, SHELL, CLAUDECODE, GATEWAY_ADDR, GATEWAY_TOKEN
```

### Worker 环境注入

Worker 进程继承系统环境变量，但以下内容被过滤：
- `IsSensitive()` 检测的敏感变量（前缀 `AWS_*`, `ANTHROPIC_*`, `SLACK_*` 等）
- `CLAUDECODE=` 变量被剥离（防嵌套 Agent）

## 输出限制

| 限制项 | 值 | 环境变量 |
|--------|------|---------|
| 单行输出 | 10 MB | 编译时常量 `MaxLineBytes` |
| 单 Session 总输出 | 20 MB | 编译时常量 `MaxSessionBytes` |
| 单 Envelope | 1 MB | 编译时常量 `MaxEnvelopeBytes` |

## WebChat CSP

```
default-src 'self';
script-src 'self' 'unsafe-inline' 'unsafe-eval';
style-src 'self' 'unsafe-inline';
connect-src 'self' ws://localhost:* wss://*;
img-src 'self' data: blob:;
font-src 'self' data:
```

> **注意**：以上为默认开发配置。`wss://*` 允许连接任意 WSS 端点，在开发环境中方便快速连接本地 Gateway。**生产环境必须收紧**，将 `wss://*` 替换为具体的 Gateway 域名（如 `wss://gateway.example.com`），同时移除 `ws://localhost:*`。`unsafe-inline` 和 `unsafe-eval` 用于支持嵌入式 SPA 的 Next.js 运行时，生产部署时应考虑使用 nonce-based CSP 替代。

## 路径安全

### SafePathJoin 参数

| 步骤 | 函数 |
|------|------|
| 1. 清理 | `path.Clean()` |
| 2. 拒绝绝对路径 | 检查首字符非 `/` |
| 3. 拼接 | `filepath.Join(base, userPath)` |
| 4. 解析符号链接 | `filepath.EvalSymlinks()` |
| 5. 前缀验证 | 结果必须以 `base` 为前缀 |

## 参考

- [安全模型](../guides/developer/security-model.md)：安全架构详解
- [AEP 协议](aep-protocol.md)：协议层规范
