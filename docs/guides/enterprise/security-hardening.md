---
persona: enterprise
difficulty: advanced
title: Security Hardening 企业安全加固指南
last_updated: 2026-05-10
version: v1.10.2
---

# Security Hardening 企业安全加固指南

HotPlex Gateway 采用 **7 层纵深防御**架构，从网络边界到进程输出全链路阻断攻击面。本指南逐层拆解安全机制，帮助企业安全团队完成审计与合规配置。

---

## 1. 网络安全（Network Security）

### TLS 与绑定地址

Gateway 默认监听 `localhost:8888`，仅接受本地连接。生产部署应通过 **Reverse Proxy**（Nginx/Caddy）暴露 TLS：

```
# Nginx 反向代理示例
location /ws {
    proxy_pass http://127.0.0.1:8888;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
}
```

**关键配置**：

| 配置项 | 默认值 | 生产建议 |
|--------|--------|---------|
| `gateway.listen_addr` | `localhost:8888` | 保持 localhost，由反向代理暴露 |
| `admin.listen_addr` | `localhost:9999` | 禁止公网暴露 |
| TLS 终止 | 不内置 | 由 Nginx/Caddy 处理 |
| CORS | 默认限制 | 按需配置 `allowed_origins` |

> **禁止**将 Gateway 直接绑定到 `0.0.0.0`。所有外部流量必须经过反向代理。

---

## 2. 认证（Authentication）

### 2.1 API Key 认证

请求通过 `X-API-Key` Header 或 `?api_key=` Query Param 携带密钥。`Authenticator` 在内存 `map` 中验证，支持热重载（`ReloadKeys`）。

**零密钥 = 开发模式**：未配置 API Key 时自动降级为 `anonymous` 用户，**生产环境必须配置至少一个 Key**。

### 2.2 JWT ES256 Token

仅接受 **ES256**（ECDSA P-256）签名算法，拒绝其他所有算法：

```go
// 算法白名单，仅 ES256
switch token.Method.Alg() {
case "ES256":
    // 验证签名
default:
    return fmt.Errorf("rejected signing method: %v (only ES256)", alg)
}
```

**JWT Claims 结构**（RFC 7519 + HotPlex 扩展）：

| 字段 | 类型 | 说明 |
|------|------|------|
| `iss` | string | 固定 `hotplex` |
| `sub` | string | 用户 ID |
| `aud` | string | 受众校验 |
| `exp` / `iat` / `nbf` | timestamp | 生命周期 |
| `jti` | UUID | 防重放，支持黑名单撤销 |
| `user_id` | string | 用户标识 |
| `bot_id` | string | Bot 隔离 ID |
| `scopes` | []string | 权限范围 |
| `role` | string | 角色 |

### 2.3 Bot ID 隔离

JWT 中 `bot_id` Claim 经过签名验证后提取。每个 Bot 只能操作属于自己的 Session，**禁止跨 Bot 访问**。即使 API Key 相同，不同 `bot_id` 的请求也被严格隔离。

### 2.4 Token 撤销

JTI 黑名单机制：每个 Token 的 `jti` 可被加入内存黑名单，后台每分钟自动清理过期条目。支持 `RevokeToken(jti, ttl)` 单 Token 撤销。

---

## 3. SSRF 防护（4 层校验）

`ValidateURL()` 依次执行 4 层检查，阻止 Worker 进程访问内网资源：

```
Layer 1: Protocol  → 仅允许 http / https
Layer 2: Bare IP   → 直接 IP 匹配 BlockedCIDRs
Layer 3: DNS       → 解析域名获取 IP 列表
Layer 4: Resolved  → 所有解析 IP 匹配 BlockedCIDRs
```

**BlockedCIDRs 覆盖范围**：

| 类别 | CIDR | 用途 |
|------|------|------|
| Loopback | `127.0.0.0/8`, `::1/128` | 本地回环 |
| Private | `10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16` | RFC 1918 私有网络 |
| IPv6 ULA | `fc00::/7` | IPv6 唯一本地地址 |
| Link-local | `169.254.0.0/16`, `fe80::/10` | 链路本地 |
| Cloud Metadata | `169.254.169.254/32`, `100.100.100.200/32` | AWS/GCP/Azure/阿里云元数据 |
| Multicast | `224.0.0.0/4`, `ff00::/8` | 组播 |
| Reserved | `0.0.0.0/8`, `100.64.0.0/10` | 当前主机 / Carrier-grade NAT |

**高安全场景**：`ValidateURLDoubleResolve()` 在首次校验后延迟 1 秒再次 DNS 解析，检测 **DNS Rebinding** 攻击。

---

## 4. 命令白名单（Command Whitelist）

Worker 进程仅允许启动两个二进制：

```go
allowedCommands = map[string]bool{
    "claude":   true,   // Claude Code CLI
    "opencode": true,   // OpenCode Server
}
```

**注册校验**：`RegisterCommand()` 拒绝含路径分隔符（`/`、`\`）和危险字符（`;|&`$` 等 20+ 字符）的命令名，从源头阻断命令注入。

**Bash 策略引擎**：Worker 执行 Bash 命令时，`CheckBashCommand()` 进行模式匹配：

- **P0（自动拒绝）**：`rm -rf /`、`dd of=/`、`mkfs`、`fork bomb` — 无需确认直接阻断
- **P1（警告+确认）**：SSH 密钥读取、Cloud Metadata 探测、Crontab 修改 — 记录日志并要求人工确认

---

## 5. 环境变量隔离（Environment Isolation）

`BuildEnv()` 按 7 阶段构建 Worker 进程环境，实现三层隔离：

### 5.1 Blocklist 过滤

配置 `worker.env_blocklist` 指定禁止传递的变量名。支持前缀匹配（`AWS_` 结尾带下划线 = 阻断所有 `AWS_*` 变量）。

### 5.2 HOTPLEX_WORKER_ 前缀剥离

仅 `HOTPLEX_WORKER_*` 前缀的变量被剥离前缀后注入 Worker：

```
HOTPLEX_WORKER_GITHUB_TOKEN=xxx  →  GITHUB_TOKEN=xxx（Worker 环境可见）
HOTPLEX_JWT_SECRET=yyy           →  完全不可见（Gateway 内部变量）
```

当剥离后的 Key 与系统变量冲突时，系统版本被**动态阻断**，防止 Gateway 自身密钥泄漏到 Worker。

### 5.3 嵌套 Agent 防护

`StripNestedAgent()` 从 Worker 环境中移除 `CLAUDECODE=` 变量，防止 Worker 进程意外启动子 Agent 导致无限递归。

### 5.4 受保护变量

`cliProtectedVars` 保护核心系统变量（`HOME`、`PATH`、`USER`、`SHELL`、`CLAUDECODE`、`GATEWAY_ADDR`、`GATEWAY_TOKEN`），禁止 `.env` 文件覆盖。

---

## 6. Tool Access Control（工具访问控制）

`AllowedTools` 定义两套工具集，按环境严格区分：

| 工具 | Dev Mode | Production | 分类 |
|------|----------|------------|------|
| Read / Grep / Glob | ✅ | ✅ | Safe |
| Edit / Write | ✅ | ❌ | Safe |
| Bash | ✅ | ❌ | Risky |
| WebFetch | ✅ | ❌ | Network |
| Agent / NotebookEdit / TodoWrite | ✅ | ❌ | System |

**生产环境仅允许 3 个只读工具**（Read、Grep、Glob），通过 `ProductionAllowedTools` 严格限制。所有工具通过 `--allowed-tools` 参数注入 Claude Code CLI，未授权工具完全不可调用。

### 权限审批

Risky / Network / System 类工具在开发模式下可用，但 Bash 命令受策略引擎约束（第 4 层），危险操作触发 P0 自动拒绝或 P1 人工确认。

---

## 7. Output Limits（输出限制）

`OutputLimiter` 在三个维度限制 Worker 输出，防止内存耗尽攻击：

| 限制项 | 值 | 作用 |
|--------|------|------|
| `MaxLineBytes` | **10 MB** | 单行输出上限 |
| `MaxSessionBytes` | **20 MB** | 单 Session 累计输出上限 |
| `MaxEnvelopeBytes` | **1 MB** | 单个 AEP Envelope 上限 |

超出任一限制立即返回错误并终止该 Session 的输出收集。`OutputLimiter` 通过 `sync.Mutex` 保护字节计数器，并发安全。

---

## 安全审计清单

| # | 检查项 | 状态 |
|---|--------|------|
| 1 | Gateway 绑定 localhost，未暴露公网 | ☐ |
| 2 | 至少配置一个 API Key（生产环境） | ☐ |
| 3 | JWT 使用 ES256 签名 | ☐ |
| 4 | `bot_id` 隔离验证生效 | ☐ |
| 5 | SSRF BlockedCIDRs 覆盖私有/元数据地址 | ☐ |
| 6 | Worker 命令白名单仅含 claude/opencode | ☐ |
| 7 | `HOTPLEX_WORKER_` 前缀隔离正确配置 | ☐ |
| 8 | 生产环境使用 `ProductionAllowedTools`（3 工具） | ☐ |
| 9 | Output Limits 未被修改 | ☐ |
| 10 | TLS 由反向代理终止 | ☐ |

---

## 相关源码

| 模块 | 文件 |
|------|------|
| API Key + JWT 认证 | `internal/security/auth.go` |
| JWT ES256 验证 | `internal/security/jwt.go` |
| SSRF 4 层防护 | `internal/security/ssrf.go` |
| 命令白名单 + Bash 策略 | `internal/security/command.go` |
| 环境变量隔离 | `internal/security/env.go` |
| Worker Env 构建 | `internal/worker/base/env.go` |
| 路径安全 | `internal/security/path.go` |
| Tool 访问控制 | `internal/security/tool.go` |
| 输出限制 | `internal/security/limits.go` |
