---
type: design
tags:
  - project/HotPlex
  - environment/whitelist
  - security/env-injection
---

# Environment Variable Whitelist Strategy

> HotPlex v1.0 环境变量白名单策略。

---

## 1. 风险分析

### 1.1 危险模式

> ⚠️ **`os.Environ()` 返回所有环境变量**，可能包含敏感信息。

**泄漏的敏感变量**：

| 变量模式 | 风险 | 严重度 |
|----------|------|--------|
| `AWS_.*_KEY` | 云凭证 | 🔴 P0 |
| `GITHUB_` | 代码仓库凭证 | 🔴 P0 |
| `.*_API_KEY` | API 凭证 | 🔴 P0 |
| `KUBECONFIG` | K8s 集群访问 | 🔴 P0 |
| `JWT_SECRET` | JWT 签名密钥 | 🔴 P0 |

### 1.2 安全原则

> ✅ **Go os/exec 不继承父进程环境变量**（除非显式设置 `cmd.Env`）。

```go
// 默认：子进程不继承父进程环境
cmd := exec.Command("ls")
// cmd.Env == nil

// 显式设置：仅传递允许的变量
cmd.Env = []string{"HOME=/home/test", "PATH=/usr/bin"}
```

---

## 2. 白名单方案

### 2.1 基础白名单

```go
// internal/security/env.go

// BaseEnvWhitelist 基础系统变量
var BaseEnvWhitelist = []string{
    "HOME", "USER", "SHELL", "PATH", "TERM",
    "LANG", "LC_ALL", "PWD",
}

// GoEnvWhitelist Go 运行时变量
var GoEnvWhitelist = []string{
    "GOPROXY", "GOSUMDB", "GONOSUMDB", "GOPRIVATE",
}
```

### 2.2 Worker 特定白名单

```go
// WorkerEnvWhitelist 按 Worker 类型配置
var WorkerEnvWhitelist = map[string][]string{
    "claude-code": append(BaseEnvWhitelist,
        "CLAUDE_API_KEY", "CLAUDE_MODEL", "CLAUDE_BASE_URL",
    ),
    "opencode-server": {},  // Server 模式无需环境变量
}
```

### 2.3 HotPlex 控制变量

```go
// HotPlexRequired 必需的 HotPlex 变量
var HotPlexRequired = []string{
    "HOTPLEX_SESSION_ID",
    "HOTPLEX_WORKER_TYPE",
}

// HotPlexOptional 可选的 HotPlex 变量
var HotPlexOptional = []string{
    "HOTPLEX_WORK_DIR",
    "HOTPLEX_TRACE_ENABLED",
    "HOTPLEX_LOG_LEVEL",
}
```

---

## 3. SafeEnvBuilder

### 3.0 安全原则（关键）

> ⚠️ **secrets 和 hotplexVars 中的 key 不得覆盖系统变量，否则可导致执行环境被劫持。**

### 3.1 ProtectedEnvVars（禁止作为 Secret Key）

```go
// internal/security/env.go

// ProtectedEnvVars 禁止作为 secret/HotPlex key 的系统变量
// ⚠️ 防止 AddSecret/AddHotPlexVar 覆盖系统变量导致执行环境被劫持
var ProtectedEnvVars = []string{
    // 系统基础（覆盖会导致执行行为改变）
    "HOME", "USER", "SHELL", "PATH", "TERM",
    "LANG", "LC_ALL", "PWD", "GID", "UID", "SHLVL",
    // Go 运行时（覆盖会影响 Go 程序行为）
    "GOROOT", "GOPATH", "GOPROXY", "GOSUMDB",
    // 命令解析器（覆盖会导致 PATH 被篡改）
    "BASH", "BASH_VERSION", "ZSH_VERSION",
}

func IsProtectedEnvVar(key string) bool {
    for _, protected := range ProtectedEnvVars {
        if key == protected {
            return true
        }
    }
    return false
}
```

### 3.2 实现

```go
// internal/security/env_builder.go

type SafeEnvBuilder struct {
    whitelist   []string
    hotplexVars map[string]string
    secrets     map[string]string
}

func NewSafeEnvBuilder() *SafeEnvBuilder {
    return &SafeEnvBuilder{
        whitelist:   BaseEnvWhitelist,
        hotplexVars: make(map[string]string),
        secrets:     make(map[string]string),
    }
}

func (b *SafeEnvBuilder) AddWorkerType(workerType string) *SafeEnvBuilder {
    if whitelist, ok := WorkerEnvWhitelist[workerType]; ok {
        b.whitelist = append(b.whitelist, whitelist...)
    }
    return b
}

func (b *SafeEnvBuilder) AddHotPlexVar(key, value string) error {
    // ⚠️ 必须检查：禁止覆盖系统变量
    if IsProtectedEnvVar(key) {
        return fmt.Errorf("cannot use %q as HotPlex var: protected system variable", key)
    }
    b.hotplexVars[key] = value
    return nil
}

func (b *SafeEnvBuilder) AddSecret(key, value string) error {
    // ⚠️ 必须检查：禁止以系统变量名作为 secret key
    // 攻击场景：AddSecret("PATH", "/malicious") → 覆盖系统 PATH
    if IsProtectedEnvVar(key) {
        return fmt.Errorf("cannot use %q as secret key: protected system variable", key)
    }
    b.secrets[key] = value
    return nil
}

func (b *SafeEnvBuilder) Build() []string {
    env := []string{}

    // 1. 添加白名单变量（系统变量基础值）
    for _, key := range b.whitelist {
        if val := os.Getenv(key); val != "" {
            env = append(env, key+"="+val)
        }
    }

    // 2. 添加 HotPlex 变量（已校验，不覆盖系统变量）
    for key, val := range b.hotplexVars {
        env = append(env, key+"="+val)
    }

    // 3. 添加 Secret 变量（已校验，不覆盖系统变量）
    for key, val := range b.secrets {
        env = append(env, key+"="+val)
    }

    return env
}
```

**攻击防护验证**：

```go
func TestSafeEnvBuilder_BlocksSystemOverride(t *testing.T) {
    builder := NewSafeEnvBuilder()

    // ❌ 这些都会被拒绝
    err := builder.AddSecret("PATH", "/malicious")
    assert.Error(t, err)
    assert.Contains(t, err.Error(), "protected system variable")

    err = builder.AddHotPlexVar("HOME", "/tmp/pwned")
    assert.Error(t, err)

    // ✅ 这些会被接受
    err = builder.AddSecret("CLAUDE_API_KEY", "sk-xxx")
    assert.NoError(t, err)

    err = builder.AddHotPlexVar("HOTPLEX_SESSION_ID", "sess_123")
    assert.NoError(t, err)
}
```

### 3.3 使用示例

```go
func (a *ClaudeCodeAdapter) StartSession(cfg *SessionConfig) error {
    builder := NewSafeEnvBuilder().
        AddWorkerType("claude-code").
        AddHotPlexVar("HOTPLEX_SESSION_ID", cfg.SessionID).    // ✅ 返回 error
        AddHotPlexVar("HOTPLEX_WORKER_TYPE", "claude-code").
        AddSecret("CLAUDE_API_KEY", a.secretsProvider.Get("CLAUDE_API_KEY"))  // ✅ 返回 error

    // 检查所有操作是否成功
    if err := builder.LastError(); err != nil {
        return err
    }

    cmd := exec.Command("claude", args...)
    cmd.Env = builder.Build()
    cmd.Dir = cfg.WorkDir

    return cmd.Start()
}
```

```go
func (a *ClaudeCodeAdapter) StartSession(cfg *SessionConfig) error {
    // 构建安全环境
    env := NewSafeEnvBuilder().
        AddWorkerType("claude-code").
        AddHotPlexVar("HOTPLEX_SESSION_ID", cfg.SessionID).
        AddHotPlexVar("HOTPLEX_WORKER_TYPE", "claude-code").
        AddSecret("CLAUDE_API_KEY", a.secretsProvider.Get("CLAUDE_API_KEY")).
        Build()

    cmd := exec.Command("claude", args...)
    cmd.Env = env  // ✅ 仅包含白名单变量
    cmd.Dir = cfg.WorkDir

    return cmd.Start()
}
```

---

## 4. 敏感变量检测

### 4.1 敏感模式

```go
// SensitivePatterns 敏感环境变量模式
// ⚠️ 使用 [A-Z0-9_]* 替代 .*，避免 ReDoS（正则表达式拒绝服务）
// Go RE2 不支持回溯，但长字符串匹配 .* 仍有性能问题
var SensitivePatterns = []string{
    `^AWS_[A-Z0-9_]*KEY$`, `^AWS_[A-Z0-9_]*SECRET`,
    `^GITHUB_`,
    `^.*_[A-Z0-9_]*API_KEY$`, `^.*_[A-Z0-9_]*TOKEN$`,
    `^DATABASE_URL$`, `^.*_[A-Z0-9_]*PASSWORD$`,
    `^JWT_`, `^.*_[A-Z0-9_]*SECRET`,
    `^KUBECONFIG$`,
}

func IsSensitive(key string) bool {
    for _, pattern := range SensitivePatterns {
        matched, _ := regexp.MatchString(pattern, key)
        if matched {
            return true
        }
    }
    return false
}
```

### 4.2 警告日志

```go
func CheckForSensitiveVars() {
    leaked := []string{}

    for _, env := range os.Environ() {
        key := strings.SplitN(env, "=", 2)[0]
        if IsSensitive(key) {
            leaked = append(leaked, key)
        }
    }

    if len(leaked) > 0 {
        log.Warn("sensitive environment variables detected",
            "count", len(leaked),
            "vars", leaked,
            "note", "these variables should be managed by secrets provider",
        )
    }
}
```

---

## 5. 配置

```yaml
# configs/worker.yaml
worker:
  env:
    # 基础白名单
    base_whitelist:
      - HOME
      - USER
      - PATH
      - TERM
      - LANG

    # Worker 特定白名单
    worker_whitelist:
      claude-code:
        - CLAUDE_API_KEY
        - CLAUDE_MODEL
        - OPENAI_API_KEY
      opencode-server: []

    # HotPlex 必需变量
    hotplex_required:
      - HOTPLEX_SESSION_ID
      - HOTPLEX_WORKER_TYPE
```