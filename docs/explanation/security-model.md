# 安全模型

> HotPlex Gateway 的安全设计哲学与多层防护体系解析

## 设计哲学：白名单优先 + 纵深防御

HotPlex Gateway 的安全模型遵循两条核心原则：

1. **白名单优先**（Whitelist-first）：默认拒绝一切，仅显式允许已知安全的操作
2. **纵深防御**（Defense-in-Depth）：独立的安全层叠加，单层被突破不影响整体安全

这意味着即使某一层防护被绕过，其他层仍然提供保护。例如：即使 Worker 进程被攻破，环境变量隔离和命令白名单仍然限制其行动范围。

## 5 层安全体系

```
┌─────────────────────────────────────┐
│  Layer 5: AI Execution Safety       │  Agent 行为约束、Tool 限制
├─────────────────────────────────────┤
│  Layer 4: Network Security          │  SSRF 防护、绑定 localhost
├─────────────────────────────────────┤
│  Layer 3: Input Validation          │  Envelope 校验、XML Sanitizer
├─────────────────────────────────────┤
│  Layer 2: Authentication            │  API Key + JWT ES256
├─────────────────────────────────────┤
│  Layer 1: Protocol Security         │  AEP 版本协商、命令白名单
└─────────────────────────────────────┘
```

### Layer 1：Protocol Security（协议安全）

- AEP `version` 字段强制为 `aep/v1`，版本不匹配立即拒绝
- 首帧必须为 `init`（30s 超时），否则 `PROTOCOL_VIOLATION`
- Envelope 大小限制 1MB（`MaxEnvelopeBytes`）
- NDJSON 编码，每行独立解析，防止格式混淆攻击

### Layer 2：Authentication（认证）

- **API Key**：Gateway 级别的访问控制
- **JWT**：Session 级别的身份验证，携带 `user_id`、`bot_id`、`scopes`

### Layer 3：Input Validation（输入验证）

- JSON Schema 验证所有 Envelope 必填字段
- `DisallowUnknownFields` 防止注入未知字段
- **XML Sanitizer**：对保留标签进行 HTML 转义，预防 XML 注入
- Bash 命令策略引擎（P0 自动拒绝、P1 警告）

### Layer 4：Network Security（网络安全）

- SSRF 4 层防护（详见下文）
- 默认仅绑定 `localhost`（安全基线）
- 路径安全验证（`SafePathJoin`、`ValidateWorkDir`）

### Layer 5：AI Execution Safety（AI 执行安全）

- Tool 分类与限制（Safe / Risky / Network / System）
- 环境变量隔离，防止凭证泄露到 Worker 子进程
- 嵌套 Agent 防护（`StripNestedAgent`）
- Permission 交互协议，敏感操作需人类审批

## 为什么只用 ES256 JWT

HotPlex 强制使用 **ES256**（ECDSA P-256）签名算法：

### 非对称优势

- **公钥分发**：验证 Token 只需公钥，私钥永远不出现在 Gateway 配置之外的任何地方
- **Bot 隔离**：每个 Bot 使用独立的密钥对，`bot_id` 嵌入 JWT claims，实现天然的多租户隔离
- **密钥泄露影响范围**：单个私钥泄露只影响对应的 Bot，不影响整个系统

### 对称算法的问题

HS256 等对称算法的密钥既用于签名又用于验证。如果密钥泄露，攻击者可以伪造任何 Bot 的 Token，破坏多租户隔离。

### ES256 在代码中的强制执行

```go
// jwt.go — 拒绝所有非 ES256 算法
switch token.Method.Alg() {
case "ES256":
    return publicKey, nil
default:
    return nil, fmt.Errorf("rejected signing method: %v (only ES256 is allowed)", alg)
}
```

## 为什么使用命令白名单

HotPlex 仅允许执行两个二进制：`claude` 和 `opencode`。

### 设计理由

- **消除 shell 注入**：直接 `exec.Command`，不通过 shell（`sh -c`），参数不经过 shell 解释
- **禁止路径分隔符**：命令名不允许包含 `/` 或 `\`，防止指定任意路径
- **危险字符检测**：命令名包含 `;|&`$` 等字符直接拒绝

```go
// command.go — 白名单 + 安全检查
var allowedCommands = map[string]bool{
    "claude":   true,
    "opencode": true,
}

func ValidateCommand(name string) error {
    if strings.Contains(name, "/") || strings.Contains(name, "\\") {
        return fmt.Errorf("must not contain path separators")
    }
    if !allowedCommands[name] {
        return fmt.Errorf("not in whitelist")
    }
    return nil
}
```

### 为什么不用 sh -c

使用 `sh -c` 会引入 shell 元字符解释（`$()`、`` ` ``、`&&`、`||` 等），攻击者可通过精心构造的输入注入任意命令。直接 `exec.Command` 将参数作为 argv 传递给子进程，完全绕过 shell。

## 为什么需要环境变量隔离

Worker 子进程继承 Gateway 的环境变量，如果不加过滤，所有敏感信息（API Key、数据库密码、云凭证）都会泄露给 Worker。

### 三层防护

1. **BaseEnvWhitelist**：只传递系统必要变量（`HOME`、`PATH`、`USER`、`SHELL`）
2. **ProtectedEnvVars**：禁止 Worker 覆盖的关键变量（`CLAUDECODE`、`GATEWAY_ADDR`、`GATEWAY_TOKEN`）
3. **Sensitive 检测**：自动脱敏前缀匹配 `AWS_*`、`ANTHROPIC_*`、`SLACK_*` 等敏感变量

### 嵌套 Agent 防护

`StripNestedAgent()` 从环境变量中移除 `CLAUDECODE=`，防止 Worker 进程内的 AI Agent 递归启动新的 Agent 实例。

## SSRF 4 层防护

Server-Side Request Forgery（SSRF）允许攻击者通过 Worker 发起请求访问内部网络。HotPlex 使用 4 层检查防御：

```
Layer 1: 协议限制 → 仅允许 http / https
Layer 2: 裸 IP 检查 → 直接拒绝私有 IP 地址的 URL
Layer 3: DNS 解析  → 解析域名获取 IP
Layer 4: IP 段检查 → 所有解析结果与 BlockedCIDRs 比对
```

### 被阻止的 IP 范围

| CIDR | 用途 |
|------|------|
| `127.0.0.0/8`、`::1/128` | Loopback |
| `10.0.0.0/8`、`172.16.0.0/12`、`192.168.0.0/16` | RFC 1918 私有网络 |
| `169.254.169.254/32` | AWS/GCP/Azure 元数据服务 |
| `100.100.100.200/32` | 阿里云元数据服务 |
| `224.0.0.0/4`、`ff00::/8` | 多播 |
| `0.0.0.0/8` | 当前主机 |
| `fc00::/7` | IPv6 唯一本地地址 |

### DNS 重新绑定防护

高敏感场景可使用 `ValidateURLDoubleResolve`：第一次解析后等待 1 秒，第二次解析验证 IP 未变化，防止攻击者通过短 TTL DNS 记录实施 DNS rebinding 攻击。

## Permission 审批协议

### 为什么有 5 分钟超时

当 AI Agent 请求执行敏感操作（如写入文件、执行命令），Worker 发送 `permission_request` 等待用户审批。5 分钟超时的设计理由：

1. **防止无限等待**：如果用户已离线，Worker 进程不应永久挂起，占用系统资源
2. **安全默认值**：超时后自动拒绝（auto-deny），而非自动允许
3. **资源回收**：5 分钟足以让用户在移动端收到通知并审批

### 为什么选择 auto-deny

`auto-deny`（而非 `auto-allow`）遵循最小权限原则：未经明确批准的操作一律拒绝。这确保了即使用户未及时响应，系统也不会执行未授权的操作。

## Dev 模式 vs 生产模式

| 维度 | Dev 模式 | 生产模式 |
|------|---------|---------|
| API Key | 未配置时允许所有请求（`anonymous`） | 必须配置，所有请求需认证 |
| JWT | 可选 | 强制 |
| SSRF | 标准检查 | 标准 + Double Resolve |
| Tool 限制 | 所有 Tool | 仅 Safe 类 |
| Bash 策略 | P1 改为警告 | P1 严格阻止 |
| 网络绑定 | `localhost` | 按需配置 |

**核心权衡**：Dev 模式优先开发效率（减少认证摩擦），生产模式优先安全（严格的访问控制）。两者共享同一套安全检查代码，只是策略参数不同。
