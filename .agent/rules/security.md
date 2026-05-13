---
paths:
  - "**/security/*.go"
  - "**/config/config.go"
---

# 安全规范

> Mutex / 反模式规范 → 见 AGENTS.md 约定与规范

## JWT 认证

### 必须使用 ES256 签名
```go
if token.Method.Alg() != "ES256" {
    return ErrUnauthorized
}
```

### Claims 完整性
JWT 必须包含：`iss`、`sub`、`aud`、`exp`、`iat`、`jti` + `role`、`scope`、`bot_id`、`session_id`

### Token 生命周期
| 类型 | TTL |
|------|-----|
| Access Token | 5min |
| Gateway Token | 1h |
| Refresh Token | 7d |

### JTI 黑名单（TTL 缓存）
被撤销的 Token jti 必须进入内存黑名单，超时后自动清理。JTI 生成禁止 `math/rand`，必须用 `crypto/rand`。

### 多 Bot 隔离
Token 中的 `bot_id` 必须与请求的 Session 所属 Bot 精确匹配，禁止跨 Bot 操作。

---

## 命令白名单

仅允许 `claude` 和 `opencode`，禁止 shell 执行。`ValidateCommand` 拒绝含路径分隔符的命令名。

---

## 路径安全（SafePathJoin）

5 步流程：Clean → 拒绝绝对路径 → Join → EvalSymlinks → 前缀验证

**规则**：路径操作必须通过 `SafePathJoin`，禁止手动拼接用户路径。

## ValidateWorkDir（SwitchWorkDir 专用）

SwitchWorkDir 必须**同时使用** `config.ExpandAndAbs` + `security.ValidateWorkDir`，缺一不可。

---

## SSRF 防护

验证链路：协议限制 → 主机名黑名单 → IP 段阻止（loopback/private/link-local/IPv6）→ DNS 解析后检查所有返回 IP

**规则**：所有外部 URL 请求必须经过 `ValidateURL`，阻止 DNS 重新绑定攻击。

---

## 环境变量隔离

三层防护：BaseEnvWhitelist（系统变量）→ ProtectedEnvVars（禁止 Worker 覆盖）→ Sensitive 检测（自动脱敏 `AWS_*/ANTHROPIC_*/SLACK_*` 等）

**嵌套 Agent 防护**：`StripNestedAgent()` 剥离 `CLAUDECODE=` 环境变量。

---

## Tool / Model 限制

- Tool 分 4 类：Safe / Risky / Network / System，生产环境仅允许 Safe 类
- Model 白名单见 `security/tool.go` — `AllowedModels`（case-insensitive）

---

## API Key 恒定时间比较

```go
subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
```

---

## SDK 日志脱敏

第三方 SDK URL 中的 `app_secret`、`token`、`access_token` 等参数必须在日志输出前清除为 `[REDACTED]`。
