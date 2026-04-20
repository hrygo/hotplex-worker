---
paths:
  - "**/security/*.go"
---

# 安全规范

> hotplex-worker 必须实施多层安全防护：JWT 认证、SSRF 防护、命令白名单、Env 隔离、Tool 限制
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

完整安全流程（按顺序执行）：

1. `filepath.Clean(userPath)` 规范化路径
2. **拒绝绝对路径**：`userPath[0] == '/'`
3. `filepath.Join(baseDir, cleaned)` 拼接
4. `filepath.EvalSymlinks` 解析 symlink
5. **前缀验证**：最终路径必须以 baseDir 开头

```go
func SafePathJoin(baseDir, userPath string) (string, error) {
    cleaned := filepath.Clean(userPath)
    if filepath.IsAbs(cleaned) {
        return "", errors.New("absolute paths not allowed")
    }
    joined := filepath.Join(baseDir, cleaned)
    resolved, err := filepath.EvalSymlinks(joined)
    if err != nil {
        return "", err
    }
    if !strings.HasPrefix(resolved, baseDir) {
        return "", errors.New("path escapes base directory")
    }
    return resolved, nil
}
```

### 危险字符检测（纵深防御）
```go
// 即使在非 shell exec 模式下也触发告警
var dangerousChars = []rune{
    ';', '|', '&', '`', '$', '(', ')', '\\', '\n', '\r',
    '<', '>', '{', '}', '[', ']', '!', '#', '~', '*', '?', ' ',
}

func ContainsDangerousChars(input string) bool {
    for _, ch := range dangerousChars {
        if strings.ContainsRune(input, ch) {
            return true
        }
    }
    return false
}
```

### BaseDir 白名单
```go
var AllowedBaseDirs = []string{
    "/var/hotplex/projects",
    "/tmp/hotplex",
}
```

---

## SSRF 防护

### 协议限制
仅允许 `http://` 和 `https://`，拒绝 `file://`、`ftp://`、`gopher://`、`data://`。

### 私有 IP 段阻止
```go
var blockedCIDRs = []*net.IPNet{
    // Loopback
    {IP: net.ParseIP("127.0.0.0"), Mask: net.CIDRMask(8, 32)},
    // Private 10.x.x.x
    {IP: net.ParseIP("10.0.0.0"), Mask: net.CIDRMask(8, 32)},
    // Private 172.16.x.x
    {IP: net.ParseIP("172.16.0.0"), Mask: net.CIDRMask(12, 32)},
    // Private 192.168.x.x
    {IP: net.ParseIP("192.168.0.0"), Mask: net.CIDRMask(16, 32)},
    // Link-local (169.254.x.x) — 包含 AWS metadata 169.254.169.254
    {IP: net.ParseIP("169.254.0.0"), Mask: net.CIDRMask(16, 32)},
    // IPv6
    {IP: net.ParseIP("::1"), Mask: net.CIDRMask(128, 128)},
    {IP: net.ParseIP("fc00::"), Mask: net.CIDRMask(7, 128)},
    {IP: net.ParseIP("fe80::"), Mask: net.CIDRMask(10, 128)},
}
```

### DNS 重新绑定攻击防护
- 阻止 `localhost`、`metadata.google.internal` 等特定主机名
- DNS 解析后检查**所有返回 IP**

### 完整验证链路
```go
func ValidateURL(rawURL string) error {
    u, err := url.Parse(rawURL)
    if err != nil || u.Host == "" {
        return &SSRFProtectionError{Reason: "empty host"}
    }
    // 1. 协议检查
    if u.Scheme != "http" && u.Scheme != "https" {
        return &SSRFProtectionError{Reason: "non-http scheme"}
    }
    // 2. 主机名黑名单
    if blockedHostnames[u.Hostname()] {
        return &SSRFProtectionError{Reason: "blocked hostname"}
    }
    // 3. IP 前缀黑名单（先解析，若为 IP 直接检查）
    // 4. DNS 解析后检查所有 IP
    addrs, _ := net.LookupHost(u.Hostname())
    for _, addr := range addrs {
        ip := net.ParseIP(addr)
        if isIPBlocked(ip) {
            return &SSRFProtectionError{Reason: "blocked IP range"}
        }
    }
    return nil
}
```

---

## 环境变量隔离

### BaseEnvWhitelist（系统变量）
```go
var BaseEnvWhitelist = []string{
    "HOME", "USER", "SHELL", "PATH", "TERM", "LANG", "LC_ALL", "PWD",
    "GOPROXY", "GOSUMDB",
}
```

### ProtectedEnvVars（禁止 Worker 覆盖）
```go
var ProtectedEnvVars = map[string]bool{
    "HOME":      true, "PATH": true, "GOPATH": true, "GOROOT": true,
    "CLAUDECODE": true, // 防止嵌套 Agent
    "GATEWAY_ADDR": true, "GATEWAY_TOKEN": true,
}
```

### Sensitive 检测（自动脱敏）
```go
var sensitivePrefixes = []string{
    "AWS_", "AZURE_", "GITHUB_", "GH_",
    "ANTHROPIC_", "OPENAI_", "GOOGLE_",
    "SLACK_", "SENTRY_",
}
var sensitiveExact = map[string]bool{
    "API_KEY": true, "API_SECRET": true, "PRIVATE_KEY": true,
    "SECRET_TOKEN": true, "DATABASE_URL": true,
    "DB_PASSWORD": true, "POSTGRES_PASSWORD": true,
}

func IsSensitive(key string) bool {
    for _, p := range sensitivePrefixes {
        if strings.HasPrefix(key, p) {
            return true
        }
    }
    return sensitiveExact[key]
}
```

### 嵌套 Agent 防护
```go
func StripNestedAgent(env []string) []string {
    result := make([]string, 0, len(env))
    for _, e := range env {
        if strings.HasPrefix(e, "CLAUDECODE=") {
            continue // 剥离 CLAUDECODE= 防止嵌套
        }
        result = append(result, e)
    }
    return result
}
```

---

## Tool 限制

### AllowedTools 白名单
```go
var AllowedTools = map[string]ToolCategory{
    // Safe
    "Read":        Safe,
    "Edit":        Safe,
    "Write":       Safe,
    "Grep":        Safe,
    "Glob":        Safe,
    // Risky
    "Bash":        Risky,
    // Network
    "WebFetch":    Network,
    // System
    "Agent":       System,
    "NotebookEdit": System,
    "TodoWrite":   System,
}

var ProductionAllowedTools = map[string]ToolCategory{
    "Read": Safe, "Edit": Safe, "Write": Safe, "Grep": Safe, "Glob": Safe,
    // 禁止 Bash、WebFetch、Agent 等
}
```

### 工具参数构建
```go
func BuildAllowedToolsArgs(tools []string) []string {
    if len(tools) == 0 {
        return nil
    }
    args := make([]string, 0, len(tools)*2)
    for _, t := range tools {
        args = append(args, "--allowed-tools", t)
    }
    return args
}
```

---

## Model 限制
```go
var AllowedModels = map[string]bool{
    "claude-sonnet-4-6":  true,
    "claude-opus-4-6":    true,
    "claude-3-5-sonnet":  true,
    // case-insensitive
}
```

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
