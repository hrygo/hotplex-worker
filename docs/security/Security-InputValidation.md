---
type: design
tags:
  - project/HotPlex
  - security/input-validation
  - security/command-injection
---

# Security: Input Validation & Command Injection Prevention

> HotPlex v1.0 输入验证与命令注入防护设计，基于行业最佳实践。

---

## 1. 设计原则

### 1.1 行业最佳实践

| 来源 | 核心观点 |
|------|----------|
| OWASP Input Validation | **白名单优先**，明确反对黑名单（"massively flawed"） |
| CWE-78 | 命令注入防护的核心是参数化执行 |
| Go Blog | `os/exec` 参数化执行**天然安全** |
| 12-Factor App | 环境变量隔离，敏感配置外置 |

### 1.2 核心认知

> **Go os/exec 天生安全**：除非显式调用 `sh -c`，否则参数不会经过 shell 解析。
>
> **唯一的危险模式**：将用户输入拼接到 `"sh -c"` 字符串中传递给 `exec.Command`。

---

## 2. 命令注入防护

### 2.1 Go exec 安全模型

**✅ 安全：参数化执行**

```go
// ✅ 安全：参数作为 []string 传递，不经过 shell
cmd := exec.Command("ls", "-la", userInput)  // userInput 作为参数，不是命令
```

**❌ 危险：shell 拼接**

```go
// ❌ 危险：将用户输入拼接到 sh -c 命令
cmd := exec.Command("sh", "-c", "ls -la "+userInput)  // userInput 可能注入命令
```

### 2.2 HotPlex 当前状态评估

| 模式 | HotPlex | 推荐 |
|------|---------|------|
| `exec.Command("claude", args...)` | ✅ 已采用 | 保持 |
| `exec.Command("sh", "-c", ...)` | ❌ 禁止 | 禁止使用 |
| 参数白名单 | ⚠️ 缺失 | 必须添加 |

### 2.3 命令白名单实现

```go
// internal/security/command.go

// AllowedCommands 白名单允许的命令
var AllowedCommands = map[string]bool{
    "claude":  true,
    "opencode": true,
    // 仅允许明确列出的命令
}

// ValidateCommand 验证命令安全性
func ValidateCommand(name string) error {
    if !AllowedCommands[name] {
        return fmt.Errorf("command %q not in whitelist", name)
    }
    return nil
}

// BuildSafeCommand 构建安全的命令执行
func BuildSafeCommand(name string, args []string) (*exec.Cmd, error) {
    if err := ValidateCommand(name); err != nil {
        return nil, err
    }

    cmd := exec.Command(name, args...)
    return cmd, nil
}
```

### 2.4 参数深度防御（可选）

即使 `exec.Command` 本身安全，深度防御仍是好实践：

```go
// internal/security/command.go

// DangerousChars 检测危险字符
var DangerousChars = []string{
    ";", "|", "&", "`", "$", "\\", "\n", "\r",
    "(", ")", "{", "}", "[", "]", "<", ">",
    "!", "#", "~", "*", "?", " ", "\t",
}

func ContainsDangerousChars(input string) bool {
    for _, char := range DangerousChars {
        if strings.Contains(input, char) {
            return true
        }
    }
    return false
}

// SanitizeArg 清理参数（仅在必要时使用）
func SanitizeArg(input string) string {
    // 仅移除危险字符，保留可打印字符
    var result strings.Builder
    for _, r := range input {
        if r >= 32 && r < 127 {
            result.WriteRune(r)
        }
    }
    return result.String()
}
```

---

## 3. 输入验证框架

### 3.1 两层验证模型

```
输入验证
    │
    ├─ 1. 语法验证 (Syntactic)
    │       ├─ 格式校验 (JSON Schema)
    │       ├─ 类型校验
    │       └─ 长度限制
    │
    └─ 2. 语义验证 (Semantic)
            ├─ 白名单校验 (Model/Tool)
            ├─ 业务规则校验
            └─ 安全校验
```

### 3.2 JSON Schema 验证

```go
// internal/security/validator.go

import "github.com/xeipuuv/gojsonschema"

var EnvelopeSchema = gojsonschema.NewStringLoader(`{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "required": ["version", "kind", "timestamp"],
  "properties": {
    "version": {
      "type": "string",
      "pattern": "^aep/v[0-9]+$"
    },
    "kind": {
      "type": "string",
      "enum": ["init", "init_ack", "input", "control", "message.delta", ...]
    },
    "timestamp": {
      "type": "integer",
      "minimum": 0,
      "maximum": 9999999999999
    },
    "data": {
      "type": "object"
    }
  }
}`)

func ValidateEnvelope(data []byte) error {
    document := gojsonschema.NewStringLoader(string(data))
    result, err := gojsonschema.Validate(EnvelopeSchema, document)
    if err != nil {
        return err
    }

    if !result.Valid() {
        return fmt.Errorf("validation errors: %v", result.Errors())
    }

    return nil
}
```

### 3.3 白名单校验

**Model 白名单**：

```go
// internal/security/model_whitelist.go

var AllowedModels = map[string]bool{
    // Claude models
    "claude-3-5-sonnet-20241022": true,
    "claude-3-5-haiku-20241022": true,
    "claude-3-opus-20240229":    true,
    "claude-sonnet-4-6":          true,
    "claude-opus-4-6":            true,
}

func ValidateModel(model string) error {
    if !AllowedModels[strings.ToLower(model)] {
        return fmt.Errorf("model %q not in allowed list", model)
    }
    return nil
}
```

**Tool 白名单**：

```go
var AllowedTools = map[string]bool{
    "Read":   true,
    "Edit":   true,
    "Write":  true,
    "Bash":   true,
    "Grep":   true,
    "Glob":   true,
    "Agent":  true,
    "WebFetch": true,
}

func ValidateTools(tools []string) error {
    for _, tool := range tools {
        if !AllowedTools[tool] {
            return fmt.Errorf("tool %q not in allowed list", tool)
        }
    }
    return nil
}
```

---

## 4. 路径安全

### 4.1 威胁模型

**Path Traversal 攻击**：

```
攻击者输入: ../../../etc/shadow
实际路径:  /var/hotplex/../../../etc/shadow → /etc/shadow
```

### 4.2 安全路径操作

```go
// internal/security/path.go

import (
    "path/filepath"
    "strings"
)

// SafePathJoin 安全地拼接路径
func SafePathJoin(baseDir, userPath string) (string, error) {
    // 1. 清理用户输入
    cleanPath := filepath.Clean(userPath)

    // 2. 拒绝绝对路径
    if filepath.IsAbs(cleanPath) {
        return "", fmt.Errorf("absolute paths not allowed: %s", userPath)
    }

    // 3. 拼接路径
    joined := filepath.Join(baseDir, cleanPath)

    // 4. 解析 symlink（关键！）
    realPath, err := filepath.EvalSymlinks(joined)
    if err != nil {
        return "", fmt.Errorf("path error: %w", err)
    }

    // 5. 验证解析后路径在 baseDir 内
    realBase, _ := filepath.EvalSymlinks(baseDir)
    if !strings.HasPrefix(realPath, realBase+string(filepath.Separator)) {
        return "", fmt.Errorf("path escapes base directory: %s", userPath)
    }

    return realPath, nil
}
```

**关键点**：

> ⚠️ **`filepath.Clean` 不解析 symlink**！必须使用 `filepath.EvalSymlinks` 后再验证前缀。

### 4.3 路径白名单

```go
var AllowedBaseDirs = map[string]bool{
    "/var/hotplex/projects": true,
    "/tmp/hotplex":          true,
}

func ValidateBaseDir(baseDir string) error {
    if !AllowedBaseDirs[baseDir] {
        return fmt.Errorf("base directory %q not in whitelist", baseDir)
    }
    return nil
}
```

---

## 5. 环境变量安全

### 5.1 12-Factor App 原则

| 原则 | HotPlex 实践 |
|------|-------------|
| 配置与代码分离 | ✅ 敏感值在 `.env`，不在代码 |
| 环境之间严格隔离 | ✅ 每个环境有独立 `.env` |
| 环境变量作为唯一配置来源 | ⚠️ 需加强 |

### 5.2 环境变量 Blocklist 隔离

```go
// internal/worker/base/env.go — BuildEnv 7 阶段管线

// Worker blocklist: 默认所有 os.Environ() 变量透传，仅阻止敏感项。
var claudeCodeEnvBlocklist = []string{
    "CLAUDECODE",  // 嵌套 Agent 防护
    "HOTPLEX_",    // 前缀匹配：阻止所有 HOTPLEX_* 网关内部变量
}

// .env 文件中通过 HOTPLEX_WORKER_ 前缀注入 Worker 密钥：
// HOTPLEX_WORKER_GITHUB_TOKEN=xxx → GITHUB_TOKEN=xxx（自动剥离前缀）

// BuildEnv 优先级（低→高）：
//  1. os.Environ() — 经 blocklist 过滤
//  2. HOTPLEX_WORKER_ 前缀剥离注入
//  3. session.Env — 每会话覆盖
//  4. ConfigEnv — 最高优先级配置覆盖
```

```go
// internal/security/env.go — 纵深防御

// StripNestedAgent 移除 CLAUDECODE= 防止嵌套 Agent 调用
func StripNestedAgent(env []string) []string { ... }

// IsProtected 防止 .env 文件覆盖系统关键变量
var cliProtectedVars = map[string]bool{
    "HOME": true, "PATH": true, "USER": true,
    "CLAUDECODE": true, "GATEWAY_ADDR": true,
}
```

---

## 6. 配置传递方案

### 6.1 配置文件方案（推荐）

**原理**：用户可控内容通过配置文件传递，不经过命令行参数。

```json
// /tmp/hotplex/session_<id>/config.json
{
  "session_id": "sess_abc123",
  "system_prompt": "You are a helpful coding assistant...",
  "model": "claude-sonnet-4-6",
  "tools": ["Read", "Edit", "Bash"],
  "work_dir": "/var/hotplex/projects/sess_abc123"
}
```

```go
func (a *ClaudeCodeAdapter) StartSession(cfg *SessionConfig) error {
    // 1. 创建 session 目录
    sessionDir := fmt.Sprintf("/tmp/hotplex/session_%s", cfg.SessionID)
    os.MkdirAll(sessionDir, 0700)

    // 2. 写入配置文件（system_prompt 在此）
    configFile := filepath.Join(sessionDir, "config.json")
    configJSON, _ := json.Marshal(map[string]interface{}{
        "system_prompt": cfg.SystemPrompt,  // ✅ 安全传递
        "model":         cfg.Model,
        "tools":        cfg.Tools,
    })
    os.WriteFile(configFile, configJSON, 0600)

    // 3. CLI 参数仅引用配置文件
    cmd := exec.Command("claude",
        "--config", configFile,
        "--dir",    cfg.WorkDir,
    )

    // 4. 注入安全环境变量
    cmd.Env = security.BuildSafeEnv(map[string]string{
        "HOTPLEX_SESSION_ID": cfg.SessionID,
    })

    return cmd.Start()
}
```

### 6.2 环境变量方案（简单场景）

```go
func (a *ClaudeCodeAdapter) StartSession(cfg *SessionConfig) error {
    cmd := exec.Command("claude", "--dir", cfg.WorkDir)

    // 仅传递非敏感配置
    cmd.Env = security.BuildSafeEnv(map[string]string{
        "CLAUDE_MODEL": cfg.Model,  // ✅ 安全
    })

    return cmd.Start()
}
```

---

## 7. 输出限制

### 7.1 分层限制策略

| 层级 | 限制 | 说明 |
|------|------|------|
| **单行** | 10MB | 防止单行过大 |
| **会话总输出** | 20MB | 防止内存耗尽 |
| **事件大小** | 1MB | Envelope JSON 上限 |

### 7.2 实现

```go
const (
    MaxLineBytes   = 10 * 1024 * 1024   // 10MB
    MaxSessionBytes = 20 * 1024 * 1024  // 20MB
    MaxEnvelopeSize = 1 * 1024 * 1024   // 1MB
)

type OutputLimiter struct {
    mu          sync.Mutex
    totalBytes  int64
}

func (l *OutputLimiter) Check(line []byte) error {
    l.mu.Lock()
    defer l.mu.Unlock()

    if int64(len(line)) > MaxLineBytes {
        return fmt.Errorf("line exceeds %d bytes limit", MaxLineBytes)
    }

    if l.totalBytes+int64(len(line)) > MaxSessionBytes {
        return fmt.Errorf("session output exceeds %d bytes limit", MaxSessionBytes)
    }

    l.totalBytes += int64(len(line))
    return nil
}
```

---

## 8. AI Tool 权限控制

> ⚠️ **HotPlex 驱动 Claude Code 执行 AI 生成命令，需要独立的权限策略。**
>
> 详见 [[AI-Tool-Policy]]。

---

## 9. 安全检查清单

- 禁止 `exec.Command("sh", "-c", ...)`
- 实现命令白名单
- 实现 JSON Schema 验证
- 实现 Model/Tool 白名单
- 实现路径安全（EvalSymlinks + 前缀检查）
- 实现环境变量白名单
- 配置文件传递敏感内容
- 实现输出限制（10MB 单行 + 20MB 会话）
- 安全日志（验证失败记录）

---

## 9. 参考资料

- [OWASP Input Validation Cheat Sheet](https://cheatsheetseries.owasp.org/cheatsheets/Input_Validation_Cheat_Sheet.html)
- [CWE-78: OS Command Injection](https://cwe.mitre.org/data/definitions/78.html)
- [OWASP Command Injection Prevention](https://cheatsheetseries.owasp.org/cheatsheets/OS_Command_Injection_defense_control.html)
- [Go Blog: os/exec](https://pkg.go.dev/os/exec)
- [12-Factor App: Config](https://12factor.net/config)