---

---

# AI Tool Policy

> HotPlex v1.0 AI Agent 工具调用安全策略。HotPlex 驱动 Claude Code 运行，Claude Code 自身包含 Bash tool，可生成任意 shell 命令。HotPlex 需要对 AI 生成的命令执行进行安全控制，而非仅保护 `hotplexd → Worker` 这一层。

---

## 1. 威胁模型

### 1.1 两层执行栈

```
┌─────────────────────────────────────────────────────────┐
│  Layer 1: hotplexd → Worker (HotPlex 控制)               │
│           exec.Command("claude", ...)                    │
│           ✅ Blocklist 环境变量过滤、HOTPLEX_WORKER_ 前缀剥离 │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│  Layer 2: Claude Code → AI-generated Bash (HotPlex 无法 │ │
│           直接控制，但可通过 AllowedTools 间接约束)       │
│           Bash tool → shell commands                     │
│           ⚠️ AI 可生成任意命令                           │
└─────────────────────────────────────────────────────────┘
```

### 1.2 攻击场景

| 场景 | 描述 | 风险 |
|------|------|------|
| **恶意命令执行** | AI 被诱导生成 `rm -rf /` | 🔴 P0 |
| **数据泄露** | AI 执行 `cat ~/.ssh/*` 泄露密钥 | 🔴 P0 |
| **内网探测** | AI 执行 `curl http://169.254.169.254/` 探测元数据 | 🔴 P0 |
| **横向移动** | AI 执行 `ssh`/`scp` 连接内网主机 | 🟡 P1 |
| **持久化** | AI 执行 cron 任务或写入 SSH authorized_keys | 🟡 P1 |

---

## 2. AllowedTools 控制

### 2.1 Claude Code AllowedTools

Claude Code 原生支持 `--allowed-tools` 参数，限制可用的工具集：

```bash
claude --allowed-tools Read,Edit,Bash,Grep,Glob
```

**工具分类**：

| 类别 | 工具 | 风险 |
|------|------|------|
| **安全工具** | Read, Edit, Write, Grep, Glob | ✅ 低风险 |
| **执行工具** | Bash | ⚠️ **高风险**，需配合其他策略 |
| **网络工具** | WebFetch | ⚠️ SSRF 风险 |
| **系统工具** | Agent, NotebookEdit | ⚠️ 嵌套执行 |

### 2.2 强制 AllowedTools

> ⚠️ **生产环境必须指定 AllowedTools**，否则 Claude Code 可使用全部工具（含 WebFetch、MCP 等）。

```go
// internal/security/ai_tool_policy.go

var DefaultAllowedTools = []string{
    "Read", "Edit", "Write", "Grep", "Glob",
}

var ProductionAllowedTools = []string{
    "Read", "Grep", "Glob",  // 生产禁用 Edit/Write（只读模式）
}

func BuildAllowedToolsArgs(tools []string) []string {
    args := []string{}
    for _, tool := range tools {
        args = append(args, "--allowed-tools", tool)
    }
    return args
}
```

---

## 3. Bash Tool 命令拦截

### 3.1 危险命令黑名单

```go
// internal/security/bash_policy.go

var DangerousCommandPatterns = []struct {
    Pattern   *regexp.Regexp
    Severity  string
    Reason    string
}{
    // 毁灭性操作
    {regexp.MustCompile(`(?i)^(\s*)?rm\s+-rf\s+/`), "P0", "root递归删除"},
    {regexp.MustCompile(`(?i)^(\s*)?dd\s+.*of=/`), "P0", "直接写入块设备"},
    {regexp.MustCompile(`(?i)^(\s*)?mkfs`), "P0", "格式化文件系统"},
    {regexp.MustCompile(`(?i)^(\s*)?fdisk`), "P0", "磁盘分区操作"},
    {regexp.MustCompile(`(?i)^(\s*)?:(\s*)?\(\s*\)(\s*)?:(\s*)?\|(\s*)?&`), "P0", "fork bomb"},

    // 凭据泄露
    {regexp.MustCompile(`(?i)(ssh|scp|rsync).*-i\s+`), "P1", "指定密钥文件"},
    {regexp.MustCompile(`(?i)cat\s+.*\.ssh/`), "P1", "读取SSH密钥"},
    {regexp.MustCompile(`(?i)gh\s+auth\s+token`), "P1", "GitHub Token泄露"},

    // 内网探测
    {regexp.MustCompile(`(?i)curl.*169\.254\.169\.254`), "P1", "云元数据探测"},
    {regexp.MustCompile(`(?i)wget.*169\.254\.169\.254`), "P1", "云元数据探测"},
    {regexp.MustCompile(`(?i)curl.*metadata\.google`), "P1", "GCP元数据探测"},

    // 持久化
    {regexp.MustCompile(`(?i)(crontab|cron).*-e`), "P1", "修改定时任务"},
    {regexp.MustCompile(`(?i)echo.*>>.*authorized_keys`), "P1", "SSH持久化"},
}

func CheckBashCommand(cmd string) *BashPolicyViolation {
    for _, entry := range DangerousCommandPatterns {
        if entry.Pattern.MatchString(cmd) {
            return &BashPolicyViolation{
                Severity: entry.Severity,
                Reason:   entry.Reason,
                Command:  cmd,
            }
        }
    }
    return nil
}
```

### 3.2 实现位置

> ⚠️ **Bash 拦截应在 Claude Code 进程内部处理**，而非在 HotPlex 侧。
>
> Claude Code 的 Bash tool 支持 `--dangerously-skip-permissions` 标志用于外部权限审批。HotPlex 可通过 AEP 协议 `permission_request` 事件实现外部审批。

```go
// internal/adapter/claude_code.go

// StartSession 注入 AllowedTools + Bash 权限策略
func (a *ClaudeCodeAdapter) StartSession(cfg *SessionConfig) error {
    // 1. 限制工具集
    args := []string{}

    // 2. 注入环境变量（影响 Claude Code 行为）
    cmd := exec.Command("claude", args...)
    cmd.Env = builder.Build()
    cmd.Dir = cfg.WorkDir

    // 3. 监听 permission_request 事件（通过 AEP 协议）
    // AI 尝试执行 Bash 命令时 → hotplexd 收到 permission_request
    // → 根据黑名单策略决定 Allow/Deny
    return nil
}
```

---

## 4. WebFetch SSRF 防护

> ⚠️ **WebFetch 可被滥用于 SSRF 攻击**。详见 [[SSRF-Protection]]。

### 4.1 阻断规则

| 目标 | 策略 | 理由 |
|------|------|------|
| 内网 IP（10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16） | 🚫 阻断 | 禁止内网探测 |
| 云元数据端点 | 🚫 阻断 | AWS/GCP/Azure 元数据 |
| Localhost（127.0.0.0/8） | 🚫 阻断 | 本地服务探测 |
| 非 HTTP/HTTPS | 🚫 阻断 | 协议限制 |
| 外部公开 URL | ✅ 允许 | 正常 AI 用途 |

### 4.2 AllowedTools 配置

```go
// 生产环境：禁用 WebFetch
var ProductionAllowedTools = []string{
    "Read", "Grep", "Glob",
    // WebFetch 已禁用
}

// 开发环境：允许但受 SSRF 策略保护
var DevAllowedTools = []string{
    "Read", "Edit", "Write", "Grep", "Glob", "WebFetch",
}
```

---

## 5. 权限审批协议

### 5.1 AEP permission_request 事件

AI 请求执行特权操作时，HotPlex 通过 AEP 协议与 AI 进行交互式审批：

```json
// Server → Client: AI 请求执行特权命令
{
  "version": "aep/v1",
  "kind": "permission_request",
  "data": {
    "request_id": "req_abc123",
    "type": "execute_command",
    "command": "curl http://169.254.169.254/latest/meta-data/",
    "reason": "AI tool invocation: WebFetch",
    "options": ["ALLOW", "DENY", "DENY_AND_REPORT"]
  }
}

// Client → Server: 用户审批
{
  "version": "aep/v1",
  "kind": "permission_response",
  "data": {
    "request_id": "req_abc123",
    "action": "DENY"
  }
}
```

### 5.2 自动策略（无需用户交互）

对于高风险命令，自动拒绝，无需用户审批：

```go
var AutoDenyPatterns = []*regexp.Regexp{
    regexp.MustCompile(`(?i)rm\s+-rf\s+/`),
    regexp.MustCompile(`(?i)curl.*169\.254\.169\.254`),
    regexp.MustCompile(`(?i)dd\s+.*of=/`),
}

func ShouldAutoDeny(cmd string) bool {
    for _, pattern := range AutoDenyPatterns {
        if pattern.MatchString(cmd) {
            return true
        }
    }
    return false
}
```

---

## 6. 会话级策略配置

```yaml
# configs/worker.yaml
worker:
  ai_tool_policy:
    # 允许的工具列表（按环境）
    allowed_tools:
      dev:  [Read, Edit, Write, Grep, Glob, Bash, WebFetch]
      prod: [Read, Grep, Glob]

    # Bash 命令黑名单
    bash_policy:
      auto_deny:
        - pattern: "rm\\s+-rf\\s+/"
          reason: "dangerous recursive delete"
        - pattern: "curl.*169\\.254\\.169\\.254"
          reason: "SSRF: cloud metadata"
      auto_deny_severity: P0

    # WebFetch SSRF 策略
    webfetch_policy:
      allowed_schemes: [http, https]
      blocked_ip_ranges:
        - 10.0.0.0/8
        - 172.16.0.0/12
        - 192.168.0.0/16
        - 127.0.0.0/8
        - 169.254.0.0/16
```

---

## 7. 安全检查清单

- 生产环境强制 AllowedTools（不使用默认全部工具）
- Bash 命令黑名单覆盖 P0/P1 危险命令
- WebFetch 启用 SSRF 防护（IP 白名单）
- 危险命令自动拒绝（无需用户审批）
- SSRF 防护阻断云元数据端点
- Session 配置与 AllowedTools 绑定（防止配置逃逸）
- 权限审批日志（审计）
- 策略配置分层（dev/prod 不同策略）

---

## 8. 参考资料

- [Claude Code: Allowed Tools](https://docs.anthropic.com/en/docs/claude-code/allowed-tools)
- [OWASP SSRF](https://owasp.org/www-project-web-security-testing-guide/latest/4-Web_Application_Security_Testing/07-Input_Validation_Testing/10-Testing_for_Server-Side_Request_Forgery)
- [CWE-918: Server-Side Request Forgery](https://cwe.mitre.org/data/definitions/918.html)
