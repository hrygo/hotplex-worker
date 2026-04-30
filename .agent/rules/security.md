---
paths:
  - "**/security/*.go"
  - "**/config/config.go"
---

# 安全规范

> hotplex 必须实施多层安全防护：JWT 认证、SSRF 防护、命令白名单、Env 隔离、Tool 限制
> 参考：`docs/specs/Acceptance-Criteria.md` §SEC-001 ~ §SEC-045

---

## JWT 认证

### 必须使用 ES256 签名
```go
// 拒绝非 ES256 算法
if token.Method.Alg() != "ES256" {
    return ErrUnauthorized
}
```

### Claims 完整性
JWT 必须包含 RFC 7519 标准字段 + HotPlex 扩展：
- `iss`（issuer）、`sub`（subject）、`aud`（audience）、`exp`、`iat`、`jti`
- `role`、`scope`、`bot_id`、`session_id`

### Token 生命周期
| 类型 | TTL |
|------|-----|
| Access Token | 5min |
| Gateway Token | 1h |
| Refresh Token | 7d |

### JTI 生成（禁止 math/rand）
```go
func GenerateJTI() (string, error) {
    b := make([]byte, 16)
    _, err := crypto_rand.Read(b)
    if err != nil {
        return "", err // 禁止回退到 math/rand
    }
    return uuid.NewUUID().String(), nil
}
```

### JTI 黑名单（TTL 缓存）
被撤销的 Token jti 必须进入内存黑名单，超时后自动清理。

### 多 Bot 隔离
Token 中的 `bot_id` 必须与请求的 Session 所属 Bot 精确匹配，禁止跨 Bot 操作。

---

## 命令白名单

仅允许两个二进制：`claude`、`opencode`，禁止 shell 执行。

```go
var AllowedCommands = map[string]bool{
    "claude":   true,
    "opencode": true,
}

// ValidateCommand 拒绝含路径分隔符的命令名
func ValidateCommand(name string) error {
    if strings.Contains(name, "/") || strings.Contains(name, "\\") {
        return errors.New("command name must not contain path separators")
    }
    if !AllowedCommands[name] {
        return errors.New("not in whitelist")
    }
    return nil
}
```

### 双层验证：句法 + 语义
- **句法层**：JSON Schema、类型、长度（MaxEnvelopeBytes = 1MB）
- **语义层**：白名单、业务规则

---

## 路径安全（SafePathJoin）

5 步流程（按序执行）：Clean → 拒绝绝对路径 → Join → EvalSymlinks → 前缀验证

详见 `security/path.go` — `SafePathJoin()`、`ContainsDangerousChars()`、`AllowedBaseDirs`

**规则**：路径操作必须通过 `SafePathJoin`，禁止手动拼接用户路径。

## ValidateWorkDir（SwitchWorkDir 专用）

SwitchWorkDir 必须**同时使用**以下两个安全函数：

```go
// 1. ExpandAndAbs：展开环境变量 + 转绝对路径
workDir, err := cfg.ExpandAndAbs(req.Path)
if err != nil {
    return fmt.Errorf("expand work dir: %w", err)
}

// 2. ValidateWorkDir：验证路径安全性（返回 error，非 bool）
if err := security.ValidateWorkDir(workDir); err != nil {
    return fmt.Errorf("unsafe work dir: %w", err)
}
```

**禁止**：只用其中一个。`ExpandAndAbs` 处理 `$VAR` 和相对路径，`ValidateWorkDir` 做安全边界检查，两者缺一不可。

---

## SSRF 防护

验证链路：协议限制（仅 http/https）→ 主机名黑名单 → IP 段阻止（loopback/private/link-local/IPv6）→ DNS 解析后检查所有返回 IP

详见 `security/ssrf.go` — `ValidateURL()`、`blockedCIDRs`、`blockedHostnames`

**规则**：所有外部 URL 请求必须经过 `ValidateURL`，阻止 DNS 重新绑定攻击。

---

## 环境变量隔离

三层防护（详见 `security/env.go`）：
- **BaseEnvWhitelist**：系统变量白名单（HOME/USER/PATH 等）
- **ProtectedEnvVars**：禁止 Worker 覆盖的变量（HOME/PATH/CLAUDECODE/GATEWAY_* 等）
- **Sensitive 检测**：`IsSensitive()` 自动脱敏（前缀匹配 `AWS_*/ANTHROPIC_*/SLACK_*` 等 + 精确匹配 `API_KEY/DATABASE_URL` 等）

**嵌套 Agent 防护**：`StripNestedAgent()` 剥离 `CLAUDECODE=` 环境变量，防止嵌套。

---

## Tool 限制

工具分 4 类：Safe（Read/Edit/Write/Grep/Glob）、Risky（Bash）、Network（WebFetch）、System（Agent/NotebookEdit/TodoWrite）。生产环境仅允许 Safe 类。详见 `security/tool.go` — `AllowedTools`、`ProductionAllowedTools`

---

## Model 限制

允许的模型列表见 `security/tool.go` — `AllowedModels`（case-insensitive）。新增模型需同步更新白名单。

---

## API Key 恒定时间比较
```go
import "crypto/subtle"

func CompareKeys(a, b string) bool {
    return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
```

---

## SDK 日志脱敏

第三方 SDK URL 中的敏感参数必须在日志输出前清除：

```go
// feishu/sdk_logger.go
func redactSensitiveURLParams(urlStr string) string {
    u, err := url.Parse(urlStr)
    if err != nil {
        return "[REDACTED]"
    }
    // 移除 app_secret, token 等敏感查询参数
    sensitiveParams := []string{"app_secret", "token", "access_token", "refresh_token"}
    q := u.Query()
    for _, p := range sensitiveParams {
        if q.Get(p) != "" {
            q.Set(p, "[REDACTED]")
        }
    }
    u.RawQuery = q.Encode()
    return u.String()
}
```
