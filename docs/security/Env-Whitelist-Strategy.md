---

---

# Environment Variable Blocklist Strategy

> HotPlex v1.5 环境变量黑名单 + `HOTPLEX_WORKER_` 前缀剥离策略。

---

## 1. 风险分析

### 1.1 危险模式

> **`os.Environ()` 返回所有环境变量**，可能包含网关内部敏感信息。

**泄漏的敏感变量**：

| 变量模式 | 风险 | 严重度 |
|----------|------|--------|
| `HOTPLEX_JWT_SECRET` | JWT 签名密钥 | P0 |
| `HOTPLEX_ADMIN_TOKEN_*` | 管理员 Token | P0 |
| `HOTPLEX_GATEWAY_TOKEN` | 网关认证 Token | P0 |
| `HOTPLEX_SLACK_*` | Slack App 凭证 | P1 |
| `HOTPLEX_FEISHU_*` | 飞书 App 凭证 | P1 |
| `CLAUDECODE` | 嵌套 Agent 注入 | P1 |

### 1.2 安全原则

> **默认通过 + 黑名单拒绝**：所有 `os.Environ()` 变量默认传递给 Worker，仅黑名单中的变量被阻止。

```go
// 旧方案（已废弃）：白名单 — 仅传递列出的变量
cmd.Env = []string{"HOME=/home/test", "PATH=/usr/bin"}

// 新方案：黑名单 — 传递所有变量，仅阻止列出的变量
cmd.Env = base.BuildEnv(session, blocklist, workerTypeLabel)
```

**为什么从白名单切换到黑名单**：
- 白名单方案需要维护允许变量列表，新增工具/SDK 时容易遗漏
- 黑名单方案更安全 — 只需关注"不该传递什么"，而非"应该传递什么"
- `HOTPLEX_WORKER_` 前缀剥离机制让运维精确控制 Worker 环境变量

---

## 2. 黑名单方案

### 2.1 Worker 黑名单

每个 Worker 类型通过 `Capabilities.EnvBlocklist()` 接口定义自己的黑名单：

```go
// internal/worker/worker.go

type Capabilities interface {
    // EnvBlocklist returns the set of environment variable names this worker
    // must NOT receive (empty = all allowed).
    EnvBlocklist() []string
    // ...
}
```

**Claude Code Worker 黑名单**（`internal/worker/claudecode/worker.go`）：

```go
// All os.Environ() vars are passed through by default, except those listed here.
// Gateway-internal secrets use the HOTPLEX_ prefix to prevent leakage.
var claudeCodeEnvBlocklist = []string{
    // Nested agent detection — must never propagate to worker subprocess.
    "CLAUDECODE",
    // Gateway-internal secrets (prefix match blocks all HOTPLEX_* vars;
    // HOTPLEX_SESSION_ID and HOTPLEX_WORKER_TYPE are added separately in BuildEnv).
    "HOTPLEX_",
}
```

**OpenCode Server Worker 黑名单**（`internal/worker/opencodeserver/worker.go`）：

```go
// Env blocklist for OpenCode Server worker.
// All os.Environ() vars are passed through by default, except those listed here.
var openCodeSrvEnvBlocklist = []string{
    // Nested agent detection.
    "CLAUDECODE",
    // Gateway-internal secrets (prefix match).
    "HOTPLEX_",
    // Claude Code specific vars — not relevant for OCS worker.
    "CLAUDE_",
    "ANTHROPIC_",
}
```

### 2.2 前缀匹配

黑名单条目以 `_` 结尾时执行**前缀匹配**：

| 条目 | 匹配规则 | 示例 |
|------|---------|------|
| `"CLAUDECODE"` | 精确匹配 | 仅阻止 `CLAUDECODE` |
| `"HOTPLEX_"` | 前缀匹配 | 阻止 `HOTPLEX_JWT_SECRET`、`HOTPLEX_ADMIN_TOKEN_1` 等所有 `HOTPLEX_*` 变量 |
| `"CLAUDE_"` | 前缀匹配 | 阻止 `CLAUDE_API_KEY`、`CLAUDE_MODEL` 等 |

### 2.3 配置驱动的扩展黑名单

除了硬编码黑名单，还支持通过 `worker.env_blocklist` 配置动态扩展：

```go
// internal/worker/worker.go — SessionInfo 字段

// ConfigBlocklist holds additional env var names from worker.env_blocklist config.
// These are merged with the hardcoded per-worker blocklist in BuildEnv.
ConfigBlocklist []string
```

配置方式：
```yaml
# configs/config.yaml
worker:
  env_blocklist:
    - "AWS_"
    - "KUBECONFIG"
```

---

## 3. BuildEnv — 7 阶段优先级管线

### 3.0 单一入口点

`base.BuildEnv()` 是 Worker 环境变量构建的**唯一入口**：

```go
// internal/worker/base/env.go

// BuildEnv constructs the environment variables for a CLI worker process.
//
// Priority (low → high):
//  1. os.Environ() — filtered through blocklist
//  2. HOTPLEX_WORKER_ prefix-stripped injections from .env
//  3. session.Env — per-session overrides
//  4. ConfigEnv — highest priority config overrides
func BuildEnv(session worker.SessionInfo, blocklist []string, workerTypeLabel string) []string
```

### 3.1 阶段详解

```
Phase 1: 扫描 HOTPLEX_WORKER_* 变量
    ↓  构建 stripMap: HOTPLEX_WORKER_GITHUB_TOKEN → GITHUB_TOKEN
Phase 2: 过滤 os.Environ()
    ↓  排除精确匹配 + 前缀匹配黑名单
    ↓  排除 HOTPLEX_WORKER_* 原始变量
    ↓  动态阻止系统级同名变量（如 GITHUB_TOKEN 被 stripMap 覆盖时阻止）
Phase 3: 注入 HOTPLEX Session 变量
    ↓  HOTPLEX_SESSION_ID + HOTPLEX_WORKER_TYPE
Phase 4: 注入剥离后的 HOTPLEX_WORKER_* 变量
    ↓  覆盖系统级同名变量
Phase 5: 注入 session.Env（每会话覆盖）
Phase 6: StripNestedAgent（纵深防御）
    ↓  移除 CLAUDECODE= 环境变量
Phase 7: 注入 ConfigEnv（最高优先级配置覆盖）
```

### 3.2 HOTPLEX_WORKER_ 前缀剥离

这是安全传递 Secrets 给 Worker 的核心机制：

```go
// internal/worker/base/env.go

const workerSecretPrefix = "HOTPLEX_WORKER_"

// Phase 1: Scan HOTPLEX_WORKER_* vars for prefix stripping.
// HOTPLEX_WORKER_GITHUB_TOKEN → GITHUB_TOKEN.
// Only HOTPLEX_WORKER_ prefix is stripped; other HOTPLEX_* vars are untouched.
stripMap := make(map[string]string)
for _, e := range environ {
    key, val, ok := strings.Cut(e, "=")
    if !ok || !strings.HasPrefix(key, workerSecretPrefix) {
        continue
    }
    strippedKey := strings.TrimPrefix(key, workerSecretPrefix)
    if strippedKey == "" {
        continue
    }
    stripMap[strippedKey] = val
}
```

**.env 文件示例**：
```bash
# .env

# 网关内部变量（不会泄漏给 Worker）
HOTPLEX_JWT_SECRET=xxx
HOTPLEX_ADMIN_TOKEN_1=xxx

# Worker 专用变量（自动剥离前缀后注入 Worker 环境）
HOTPLEX_WORKER_GITHUB_TOKEN=ghp_xxx    # → Worker 收到 GITHUB_TOKEN=ghp_xxx
HOTPLEX_WORKER_ANTHROPIC_API_KEY=sk-xxx # → Worker 收到 ANTHROPIC_API_KEY=sk-xxx
```

### 3.3 动态阻止

当 `HOTPLEX_WORKER_*` 剥离后的变量名与系统变量同名时，系统级版本被动态阻止：

```go
// Phase 2: Filter os.Environ() through blocklist + dynamic blocks.

// Dynamic block: system-level version of a stripped var is blocked
// (e.g., system GITHUB_TOKEN blocked when HOTPLEX_WORKER_GITHUB_TOKEN exists).
if _, blocked := stripMap[key]; blocked {
    continue
}
```

**示例**：

```
系统环境: GITHUB_TOKEN=system-level-token
.env 文件: HOTPLEX_WORKER_GITHUB_TOKEN=worker-specific-token

结果: Worker 收到 GITHUB_TOKEN=worker-specific-token
      系统级 GITHUB_TOKEN 被自动阻止，防止泄漏
```

### 3.4 完整实现

```go
// internal/worker/base/env.go

func BuildEnv(session worker.SessionInfo, blocklist []string, workerTypeLabel string) []string {
    environ := os.Environ()
    env := make([]string, 0, len(environ))

    // Build blocklist set, tracking prefix entries.
    blockSet := make(map[string]bool)
    prefixKeys := make([]string, 0)
    for _, k := range blocklist {
        if strings.HasSuffix(k, "_") {
            prefixKeys = append(prefixKeys, k)
        } else {
            blockSet[k] = true
        }
    }

    // Merge config-driven blocklist (worker.env_blocklist).
    for _, k := range session.ConfigBlocklist {
        if strings.HasSuffix(k, "_") {
            prefixKeys = append(prefixKeys, k)
        } else {
            blockSet[k] = true
        }
    }

    // Phase 1: Scan HOTPLEX_WORKER_* vars for prefix stripping.
    stripMap := make(map[string]string)
    for _, e := range environ {
        key, val, ok := strings.Cut(e, "=")
        if !ok || !strings.HasPrefix(key, workerSecretPrefix) {
            continue
        }
        strippedKey := strings.TrimPrefix(key, workerSecretPrefix)
        if strippedKey != "" {
            stripMap[strippedKey] = val
        }
    }

    // Phase 2: Filter os.Environ() through blocklist + dynamic blocks.
    for _, e := range environ {
        key, _, ok := strings.Cut(e, "=")
        if !ok {
            continue
        }
        if blockSet[key] || hasAnyPrefix(key, prefixKeys) {
            continue
        }
        // HOTPLEX_WORKER_* vars handled by stripping, not passed through.
        if strings.HasPrefix(key, workerSecretPrefix) {
            continue
        }
        // Dynamic block: system-level version of stripped var.
        if _, blocked := stripMap[key]; blocked {
            continue
        }
        env = append(env, e)
    }

    // Phase 3: Add HOTPLEX session vars.
    env = append(env,
        "HOTPLEX_SESSION_ID="+session.SessionID,
        "HOTPLEX_WORKER_TYPE="+workerTypeLabel,
    )

    // Phase 4: Inject stripped HOTPLEX_WORKER_* vars.
    for k, v := range stripMap {
        env = setOrAppend(env, k+"="+v)
    }

    // Phase 5: Add session-specific env vars.
    for k, v := range session.Env {
        if k != "" {
            env = setOrAppend(env, k+"="+v)
        }
    }

    // Phase 6: Strip nested agent config (CLAUDECODE=).
    env = security.StripNestedAgent(env)

    // Phase 7: Apply config-driven env vars (highest priority).
    for _, e := range session.ConfigEnv {
        if e != "" && strings.Contains(e, "=") {
            env = setOrAppend(env, e)
        }
    }

    return env
}
```

### 3.5 调用示例

```go
// Claude Code Worker 启动（internal/worker/claudecode/worker.go）
stdin, _, _, err := w.Proc.Start(bgCtx, binary, fullArgs,
    base.BuildEnv(session, claudeCodeEnvBlocklist, "claude-code"),
    session.ProjectDir,
)

// OpenCode Server 单例进程启动（internal/worker/opencodeserver/singleton.go）
return base.BuildEnv(worker.SessionInfo{}, openCodeSrvEnvBlocklist, "opencode-server")
```

---

## 4. 纵深防御

### 4.1 StripNestedAgent

即使 `CLAUDECODE` 已在黑名单中，`StripNestedAgent` 仍作为 Phase 6 纵深防御：

```go
// internal/security/env.go

// StripNestedAgent removes CLAUDECODE= from the environment to prevent
// nested agent invocation.
func StripNestedAgent(env []string) []string {
    prefix := "CLAUDECODE="
    filtered := make([]string, 0, len(env))
    for _, e := range env {
        if strings.HasPrefix(e, prefix) {
            continue
        }
        filtered = append(filtered, e)
    }
    return filtered
}
```

**双重保护**：
1. Phase 2 黑名单过滤阻止 `CLAUDECODE` 通过
2. Phase 6 `StripNestedAgent` 再次清理 — 防止 session.Env 或 ConfigEnv 注入

### 4.2 IsProtected（CLI .env 校验）

CLI 层面防止 `.env` 文件覆盖关键系统变量：

```go
// internal/security/env.go

// cliProtectedVars are system variables that .env files must not override.
// Separate from worker blocklists since BuildEnv must pass HOME/PATH/USER
// through to worker processes.
var cliProtectedVars = map[string]bool{
    "HOME":          true,
    "PATH":          true,
    "USER":          true,
    "SHELL":         true,
    "CLAUDECODE":    true,
    "GATEWAY_ADDR":  true,
    "GATEWAY_TOKEN": true,
}

// IsProtected reports whether an environment variable key should not be
// overwritten from .env files.
func IsProtected(key string) bool {
    return cliProtectedVars[strings.ToUpper(key)]
}
```

**注意**：`IsProtected` 仅用于 CLI `.env` 校验，不影响 `BuildEnv`。Worker 进程需要 `HOME`、`PATH` 等变量才能正常运行。

### 4.3 防护层次总结

| 层次 | 机制 | 位置 | 作用 |
|------|------|------|------|
| L1 | 黑名单过滤 | `BuildEnv` Phase 2 | 阻止 Worker 不该看到的变量 |
| L2 | 前缀剥离 | `BuildEnv` Phase 1+4 | 安全注入 Worker 专用 Secrets |
| L3 | 动态阻止 | `BuildEnv` Phase 2 | 防止系统级变量覆盖剥离变量 |
| L4 | StripNestedAgent | `BuildEnv` Phase 6 | 纵深防御嵌套 Agent |
| L5 | IsProtected | CLI `.env` 校验 | 防止运维误覆盖关键系统变量 |

---

## 5. 配置

```yaml
# configs/config.yaml
worker:
  # 扩展黑名单（合并到 Worker 硬编码黑名单）
  env_blocklist:
    - "AWS_"           # 前缀匹配：阻止所有 AWS_* 变量
    - "KUBECONFIG"     # 精确匹配：阻止 K8s 配置

  # 最高优先级环境变量（覆盖所有其他来源）
  environment:
    - "MY_TOOL_CONFIG=/etc/mytool/config.yaml"
```

### .env 文件约定

```bash
# .env

# === 网关内部变量（HOTPLEX_ 前缀，不会泄漏给 Worker）===
HOTPLEX_JWT_SECRET=xxx
HOTPLEX_ADMIN_TOKEN_1=xxx
HOTPLEX_GATEWAY_TOKEN=xxx
HOTPLEX_SLACK_APP_TOKEN=xxx
HOTPLEX_FEISHU_APP_ID=xxx

# === Worker 专用变量（HOTPLEX_WORKER_ 前缀，自动剥离后注入）===
HOTPLEX_WORKER_GITHUB_TOKEN=ghp_xxx
HOTPLEX_WORKER_ANTHROPIC_API_KEY=sk-xxx
HOTPLEX_WORKER_AWS_REGION=us-east-1
```

### 优先级总结

```
ConfigEnv（最高）> session.Env > HOTPLEX_WORKER_ 剥离 > os.Environ（过滤后）（最低）
```
