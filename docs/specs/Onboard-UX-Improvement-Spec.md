# HotPlex Onboard UX 改进设计文档

> **日期**: 2026-05-06
> **状态**: Draft
> **前置文档**: [CLI-Self-Service-Spec.md](CLI-Self-Service-Spec.md)
> **设计原则**: 约定大于配置 · 渐进式引导 · 职责单一

---

## 1. 概述

### 1.1 问题

当前 `hotplex onboard` 完成了基础设施配置（secrets、config.yaml、.env、service），但存在三个结构性缺陷：

| 缺陷 | 表现 | 影响 |
|------|------|------|
| **流程不闭环** | 写完配置即结束，无 Next Steps、无端到端验证 | 用户不知道下一步做什么，启动后才发现问题 |
| **UX 粗糙** | 纯文本问答、无进度指示、全量或零增量、输入无校验 | 配置体验差，出错率高，重复配置破坏已有设置 |
| **引导不足** | Agent Config 模板已含内容但缺少个性化引导、平台凭据无引导、STT 只告警不修复 | 用户不知道可定制，高级功能形同虚设 |

### 1.2 目标

1. **流程闭环**：onboard 结束时用户有明确的下一步行动路径，且 gateway 可用
2. **UX 升级**：进度可见、输入实时校验、增量重跑、智能跳过已完成步骤
3. **职责分离**：CLI wizard 负责基础设施，AI Skill 负责个性化，两者通过桥接提示衔接

### 1.3 不做什么

- 不引入 TUI 框架（保持 `fmt.Fprintf(os.Stderr, ...)` 风格，仅增加结构化输出）
- 不在 CLI wizard 中做 Agent 个性化交互（交给 AI Skill）
- 不改变现有配置体系（config.yaml + .env + 环境变量三级）

---

## 2. 当前架构分析

### 2.1 入口路径

```
用户场景             入口命令                 目标用户
──────────────────  ───────────────────────  ──────────────
源码开发             make quickstart          开发者
快速开发             scripts/quickstart.sh    开发者
生产部署             hotplex onboard          运维
自动化部署           hotplex onboard --non-interactive  CI/CD
二进制安装           install.sh → hotplex onboard  终端用户
```

### 2.2 Wizard 流程（10 步）

```
displayBanner()
  ↓
stepEnvPreCheck()          ← Go/OS/磁盘
  ↓
detectExistingConfig()     ← 检测已有配置 → keep/reconfigure
  ↓
stepRequiredConfig()       ← JWT/Admin/WorkerType
  ↓
stepWorkerDep()            ← 检查二进制在 PATH
  ↓
stepMessaging()            ← Slack/飞书凭据 + 策略
  ↓
stepConfigGen()            ← 生成 config.yaml
  ↓
stepWriteConfig()          ← 生成 .env
  ↓
stepAgentConfig()          ← 写默认模板（已有内容，缺个性化引导）
  ↓
stepSTTCheck()             ← STT 依赖检测（只告警）
  ↓
stepVerify()               ← 跑 checker 验证
  ↓
stepServiceInstall()       ← 安装系统服务
  ↓
(结束，无 Next Steps)
```

### 2.3 关键问题定位

| 文件 | 行号 | 问题 |
|------|------|------|
| `wizard.go:288` | 版本号 | `"v0.36.2"` 硬编码在 `displayBanner()`，实际版本 v1.5.4 |
| `wizard.go:381-397` | Worker 检查 | `exec.LookPath` 只确认二进制存在，不验证功能 |
| `wizard.go:713-749` | Agent 模板 | `O_EXCL` 写入模板（已有内容），但缺个性化引导和 3 级 fallback 说明 |
| `agentconfig_templates.go:17` | 模板列表 | SOUL/AGENTS/SKILLS/USER/MEMORY 已有实质内容，但 USER.md 字段为空待填充 |
| `wizard.go:585-612` | Verify | 只跑 environment/config/stt 三类 checker（缺少 security/dependencies/runtime） |
| `onboard.go:65-99` | Run() 后渲染 | 已有 `CommandBox` 输出下一步，但无 Agent 个性化桥接提示 |
| `templates.go:169-191` | YAML 生成 | 已有 `extractPlatformBlock`/`writeKeptPlatform` 保留平台块，但其他块为全量覆盖 |

---

## 3. 改进方案

按优先级分三个 Phase，每个 Phase 内的改进项可并行实施。

### Phase 1: 流程闭环（P0）

> 目标：onboard 结束时 gateway 可用，用户知道下一步做什么。

#### 3.1 Next Steps 输出

**改动文件**: `cmd/hotplex/onboard.go`（非 `wizard.go`）

`onboard.go:93-98` 已有 `CommandBox` 输出（"keep" → `hotplex doctor`，"reconfigure" → `hotplex gateway start`）。在此基础上扩展：

```go
// 现有 onboard.go:93-98 的 switch 替换为：
switch result.Action {
case "keep":
    fmt.Fprint(os.Stderr, output.CommandBox("hotplex doctor"))
default:
    fmt.Fprint(os.Stderr, output.CommandBox(
        "hotplex gateway start",
        "hotplex doctor",
        "open http://localhost:8888",
    ))
}

// 新增：Agent 个性化桥接提示
if len(result.AgentConfigNew) > 0 {
    fmt.Fprint(os.Stderr, output.NoteBox("Agent Personalization",
        "Default templates created at ~/.hotplex/agent-configs/\n"+
        "To customize your AI coding partner, start a conversation\n"+
        "and request \"hotplex-setup\" for interactive guided setup.\n\n"+
        "This will help you configure:\n"+
        "  • Agent personality and communication style (SOUL.md)\n"+
        "  • Your technical profile and preferences (USER.md)\n"+
        "  • Workflow preferences and autonomy (AGENTS.md)\n"+
        "  • Per-platform or per-bot customizations (3-level fallback)"))
}
```

**注意**：`output.CommandBox`、`output.NoteBox`、`output.SectionHeader` 已在 `internal/cli/output/theme.go` 中定义，直接复用。

#### 3.2 Worker 深度验证

**改动文件**: `internal/cli/onboard/wizard.go` → `stepWorkerDep()`

从"检查 PATH"升级为"检查功能"：

```go
func stepWorkerDep(workerType string) StepResult {
    // 现有 PATH 检查保留
    // ...

    // 新增：功能验证
    switch workerType {
    case "claude_code":
        // claude --version → 验证 CLI 可执行
        // claude --print "hello" --output-format json → 验证 SDK 可用（可选，超时 5s）
    case "opencode_server":
        // opencode --version → 验证版本 >= 最低要求
    }
}
```

**验证等级**：

| 等级 | 检查内容 | 超时 | 失败处理 |
|------|---------|------|---------|
| Level 1 | 二进制在 PATH | 即时 | warn + 安装指引 |
| Level 2 | `--version` 可执行 | 3s | warn + 版本要求说明 |
| Level 3 | 功能性测试（可选） | 5s | skip（不影响 onboard） |

#### 3.3 端到端验证增强

**改动文件**: `internal/cli/onboard/wizard.go` → `stepVerify()`

扩展验证范围：

```go
func stepVerify(configPath string) StepResult {
    // 现有：environment + config + stt
    // 新增：
    allCheckers = append(allCheckers, cli.DefaultRegistry.ByCategory("security")...)
    allCheckers = append(allCheckers, cli.DefaultRegistry.ByCategory("dependencies")...)
    allCheckers = append(allCheckers, cli.DefaultRegistry.ByCategory("runtime")...)

    // 新增：配置加载冒烟测试
    // 尝试 config.Load() + RequireSecrets()，确认无解析错误

    // 新增：端口预检
    // 检查 8888/9999 端口是否可用（不启动服务）
}
```

---

### Phase 2: UX 改进（P1）

> 目标：配置过程可感知、可恢复、可校验。

#### 3.4 进度指示

**改动文件**: `internal/cli/onboard/wizard.go`

在 `Run()` 入口和每步之间增加进度指示：

```
  HotPlex Worker Gateway — Setup Wizard  v1.5.4
  ─────────────────────────────────────────────

  [1/8] Environment Pre-check ............. ✓ pass
  [2/8] Required Configuration ............ ✓ pass
  [3/8] Worker Dependency ................. ✓ pass
  [4/8] Messaging Platform ................ ✓ pass
  [5/8] Generate Configuration ............ ✓ pass
  [6/8] Write Environment File ............ ✓ pass
  [7/8] Agent Config Templates ............ ✓ pass
  [8/8] Verify ............................ ✓ pass

  ✓ 8 passed  ✗ 0 failed  ⚠ 1 warning
```

实现方式：每步开始时打印 `[N/total] Step Name`，完成时追加状态符号。

#### 3.5 动态版本号

**改动文件**: `internal/cli/onboard/wizard.go` → `displayBanner()` + `WizardOptions`

`onboard.go:66` 已有 `versionString()` 返回正确版本（通过 ldflags 注入）。将版本传入 wizard 而非在 banner 内硬编码：

```go
// 1. WizardOptions 新增 Version 字段
type WizardOptions struct {
    // ... 现有字段 ...
    Version string // 从 onboard.go 传入
}

// 2. displayBanner 接收版本参数
func displayBanner(version string) {
    if version == "" {
        version = "dev"
    }
    fmt.Fprintf(os.Stderr, "\n  %s\n", output.Bold("HotPlex Worker Gateway — Setup Wizard"))
    fmt.Fprintf(os.Stderr, "  %s\n", output.Dim("AI Coding Agent Gateway  "+version))
    // ...
}

// 3. onboard.go 调用处
result, err := onboard.Run(ctx, onboard.WizardOptions{
    // ...
    Version: versionString(),
})
```

> 不使用 `runtime/debug.ReadBuildInfo()` — 在 `go install` 构建下返回 `(devel)`，不可靠。

#### 3.6 输入实时校验

**改动文件**: `internal/cli/onboard/wizard.go` → 各 `prompt*` 函数

在凭据输入时增加格式校验：

```go
func promptWithValidation(reader *bufio.Reader, question string, validate func(string) error) string {
    for {
        val := prompt(reader, question)
        if val == "" {
            return "" // 空值走默认逻辑（如自动生成）
        }
        if err := validate(val); err != nil {
            fmt.Fprintf(os.Stderr, "  ✗ %s\n", err.Error())
            continue
        }
        return val
    }
}

// 校验规则示例
var slackBotTokenValidator = func(val string) error {
    if !strings.HasPrefix(val, "xoxb-") {
        return fmt.Errorf("Slack Bot Token must start with xoxb-")
    }
    if len(val) < 20 {
        return fmt.Errorf("token appears too short")
    }
    return nil
}
```

**校验清单**：

| 字段 | 规则 | 提示 |
|------|------|------|
| Slack Bot Token | `xoxb-` 前缀 + 长度 ≥ 20 | "Slack Bot Token must start with xoxb-" |
| Slack App Token | `xapp-` 前缀 + 长度 ≥ 20 | "Slack App Token must start with xapp-" |
| Feishu App ID | `cli_` 前缀 | "Feishu App ID must start with cli_" |
| Feishu App Secret | 长度 ≥ 16 | "App Secret appears too short" |
| JWT Secret | base64 解码后 ≥ 32 bytes | "JWT secret too short (need >= 32 bytes)" |
| Admin Token | 长度 ≥ 16 | "Admin token too short" |
| DM/Group Policy | 枚举值 `open\|allowlist\|deny` | "Policy must be open, allowlist, or deny" |
| Allow From | 非空时逗号分隔，每项非空 | "User ID cannot be empty" |

#### 3.7 增量重跑（含 Select Steps）

**改动文件**: `internal/cli/onboard/wizard.go` → `Run()`

重新设计已有配置的处理逻辑，首版即实现三档选择：

```
当前行为：
  检测到已有配置 → "Keep existing? [Y/n]" → keep（跳过所有）或 reconfigure（全量覆盖）

改进行为：
  检测到已有配置 → 展示当前状态 → 三档选择
```

**三档交互**：

```
  Existing Configuration Detected
    Config:  ~/.hotplex/config.yaml ✓
    Slack:   enabled (configured)
    Feishu:  disabled
    Service: installed (user level)

  What would you like to do?
    1) Keep all — skip to verify
    2) Reconfigure everything — full reset
    3) Select steps — choose what to change

  Select [1]: 3

  ── Select Steps to Reconfigure ─────────────────
  ? Secrets (JWT/Admin Token) [y/N]: n
  ? Worker Type [y/N]: n
  ? Slack Configuration [y/N]: n
  ? Feishu Configuration [y/N]: y
  ? System Service [y/N]: n

  ── Feishu Configuration ────────────────────────
  Feishu App ID: cli_xxxx
  ...
```

**实现策略**：将 `Run()` 拆解为 step 注册 + 选择执行：

```go
// 步骤注册表
type wizardStep struct {
    Name     string
    Fn       func(ctx *wizardContext) StepResult
    Selected bool // 默认 true，增量模式下按需 false
}

func Run(ctx context.Context, opts WizardOptions) (*WizardResult, error) {
    steps := []wizardStep{
        {Name: "required_config", Fn: runRequiredConfig},
        {Name: "worker_dep", Fn: runWorkerDep},
        {Name: "messaging", Fn: runMessaging},
        {Name: "config_gen", Fn: runConfigGen},
        {Name: "write_config", Fn: runWriteConfig},
        {Name: "agent_config", Fn: runAgentConfig},
        {Name: "stt_check", Fn: runSTTCheck},
        {Name: "verify", Fn: runVerify},
        {Name: "service_install", Fn: runServiceInstall},
    }

    // 增量模式：标记选中步骤
    if mode == modeSelectSteps {
        choices := promptStepSelection(steps, existing)
        for i := range steps {
            steps[i].Selected = choices[i]
        }
    }

    // 共享上下文：步骤间通过 struct 传数据，不走闭包
    wctx := &wizardContext{
        opts: opts,
        existing: existing,
        // 从已有配置预填充，未选中步骤保持原值
        jwtSecret:  readExistingEnv(envPath, "HOTPLEX_JWT_SECRET"),
        adminToken: readExistingEnv(envPath, "HOTPLEX_ADMIN_TOKEN_1"),
        workerType: readExistingConfigField(configPath, "worker.type"),
        // ...
    }

    for _, step := range steps {
        if !step.Selected {
            continue
        }
        result.add(step.Fn(wctx))
    }
}
```

**数据依赖解决**：未选中步骤从已有配置文件/env 预填充 `wizardContext`，而非依赖前序步骤输出。新增工具函数：

```go
func readExistingEnv(envPath, key string) string      // 从 .env 读已有值
func readExistingConfigField(path, field string) string // 从 config.yaml 读已有值
```

#### 3.8 配置生成合并模式

**改动文件**: `internal/cli/onboard/templates.go` → `BuildConfigYAML()`

当前行为是全量覆盖，但已有部分合并机制：`extractPlatformBlock` + `writeKeptPlatform`（`templates.go:213-271`）可保留已有平台配置块。在此基础上扩展：

1. **保留用户注释**：读取原 config.yaml，保留非 `# ──` 分隔符和 `# Generated by` 行的注释
2. **保留未知字段**：用户手动添加的字段不应被丢弃
3. **精确块替换**：将 `extractPlatformBlock` 模式扩展到 worker 等块

实现策略：**扩展现有 `extractPlatformBlock` 为通用 `extractBlock`**。

```go
// 扩展现有 extractPlatformBlock 为通用版本
func extractBlock(configPath, blockKey string) string {
    // 复用 templates.go:227-271 的行扫描逻辑
    // 将 "  " + platform + ":" 泛化为匹配任意顶级/二级 key
}

func MergeConfigYAML(existing []byte, opts ConfigTemplateOptions) []byte {
    // 1. 解析 existing 为行数组
    // 2. 按顶级 key 分块（gateway:, admin:, db:, ...）
    // 3. 对需要更新的块用新模板替换，未涉及的保留原样
    // 4. 保留用户注释
}
```

#### 3.9 WebChat Onboard 感知

**改动文件**: `webchat/app/components/chat/ChatContainer.assistant-ui.tsx`

**当前行为分析**：gateway 不可达时，`useSessions.refreshSessions()` 抛异常 → `error` 被设为 `"Failed to load sessions"` → Header 红点 ERROR 徽章，但主区域仍显示 "Empower Your Coding" + "Start Your First Project" 按钮（点击也会失败）。

**改进策略**：在现有的 `error` 状态判断中增加网络错误识别，替换 empty state 内容：

```typescript
// ChatContainer 中的 empty state 渲染逻辑（约 line 158-179）
// 当前：(!activeSessionId && isLoading) → spinner，否则 → "Empower Your Coding"
// 改为三态：

{(!activeSessionId && isLoading) ? (
  // 现有 loading spinner — 不变
  <LoadingSpinner />
) : !activeSessionId && error && isNetworkError(error) ? (
  // 新增：Gateway 不可达 → setup 引导
  <SetupGuide />
) : !activeSessionId ? (
  // 现有 empty state — 增强 welcome 提示
  <WelcomeState />
)}
```

**`isNetworkError` 判断**：检查 `useSessions` 的 error 是否为网络级错误（fetch failed / connection refused），而非业务错误（如 401/403）。

```typescript
function isNetworkError(error: string): boolean {
  const lower = error.toLowerCase();
  return lower.includes('failed to fetch') ||
         lower.includes('networkerror') ||
         lower.includes('err_connection_refused') ||
         lower.includes('load sessions');
}
```

**`SetupGuide` 组件** (Gateway 不可达)：

```tsx
function SetupGuide() {
  return (
    <div className="absolute inset-0 flex flex-col items-center justify-center bg-[var(--bg-base)] p-8 text-center">
      <div className="w-20 h-20 rounded-3xl glass-dark flex items-center justify-center mb-8">
        <SetupIcon size={48} />
      </div>
      <h2 className="text-xl font-display font-bold text-[var(--text-primary)] mb-3">
        HotPlex Setup Required
      </h2>
      <p className="text-sm text-[var(--text-muted)] max-w-md mb-8 leading-relaxed">
        The gateway is not running. Follow these steps to get started:
      </p>
      <div className="text-left space-y-3 text-sm text-[var(--text-secondary)] max-w-md">
        <SetupStep n={1} cmd="curl -fsSL https://raw.githubusercontent.com/hrygo/hotplex/main/scripts/install.sh | bash" />
        <SetupStep n={2} cmd="hotplex onboard" />
        <SetupStep n={3} cmd="hotplex gateway start" />
      </div>
      <p className="text-xs text-[var(--text-muted)] mt-8">
        📖 Full guide: <a href="https://github.com/hrygo/hotplex/blob/main/INSTALL.md" ...>INSTALL.md</a>
      </p>
    </div>
  );
}
```

**`WelcomeState` 组件** (Gateway 可达，无 session，替换现有 "Empower Your Coding")：

```tsx
function WelcomeState() {
  return (
    <div className="..."> {/* 现有样式 */}
      <BrandIcon ... />
      <h2>Welcome to HotPlex</h2>
      <p>Your AI coding partner is ready.</p>
      <div className="quick-start-tips">
        <Tip>Type a message to start coding</Tip>
        <Tip>Use /help for available commands</Tip>
        <Tip>Use /skills to discover capabilities</Tip>
      </div>
      <button onClick={handleCreateNew}>Start Your First Project</button>
      <p className="tip">
        💡 Personalize your agent with hotplex-setup after your first chat.
      </p>
    </div>
  );
}
```

---

### Phase 3: 引导增强（P2）

> 目标：降低平台配置门槛，让高级功能可用。

#### 3.10 平台配置引导

**改动文件**: `internal/cli/onboard/wizard.go` → `collectPlatformConfig()` + 新增嵌入模板

平台引导文本使用 `go:embed` 模板，与代码分离：

**新增文件**：`internal/cli/onboard/guides/`

```
internal/cli/onboard/guides/
  slack.md     ← Slack App 创建 + Socket Mode + 权限配置步骤
  feishu.md    ← 飞书 App 创建 + 事件订阅 + 权限配置步骤
```

```go
//go:embed guides/*.md
var guideFS embed.FS

func showPlatformGuide(platform string) {
    data, err := guideFS.ReadFile("guides/" + platform + ".md")
    if err != nil {
        return
    }
    fmt.Fprint(os.Stderr, string(data))
}
```

**`guides/slack.md` 示例**：

```markdown
  ── Slack Setup Guide ─────────────────────────

  1. Create App: https://api.slack.com/apps → "Create New App"
  2. Enable Socket Mode: Features → Socket Mode → Enable
  3. Add Bot Scopes (OAuth & Permissions):
     chat:write, channels:history, groups:history, im:history
  4. Install to Workspace → Copy Bot Token (xoxb-...)
  5. Copy App-Level Token (Basic Information → xapp-...)

  Docs: https://api.slack.com/apis/connections/socket
```

**`guides/feishu.md` 示例**：

```markdown
  ── 飞书配置引导 ──────────────────────────────

  1. 创建应用: https://open.feishu.cn/app → "创建企业自建应用"
  2. 添加权限 (权限管理):
     im:message, im:message:send_as_bot, im:resource
  3. 启用事件订阅 (事件订阅):
     im.message.receive_v1
  4. 获取凭据: 凭据与基础信息 → App ID + App Secret

  Docs: https://open.feishu.cn/document/home
```

**调用方式**：在 `collectPlatformConfig` 中，用户选择配置后、输入凭据前展示：

```go
func collectPlatformConfig(reader *bufio.Reader, platform string, credPrompts map[string]string) messagingPlatformConfig {
    if !promptYesNo(reader, fmt.Sprintf("Configure %s?", platform)) {
        return messagingPlatformConfig{credentials: map[string]string{}}
    }

    // 展示嵌入引导模板
    showPlatformGuide(platform)

    cfg := messagingPlatformConfig{enabled: true, credentials: map[string]string{}}
    for envKey, promptText := range credPrompts {
        val := promptWithValidation(reader, "  "+promptText, validators[envKey])
        // ...
    }
    // ... 后续策略配置 ...
}
```

#### 3.11 配置预览

**改动文件**: `internal/cli/onboard/wizard.go`

在 `stepConfigGen()` 之前展示配置摘要，供用户确认：

```
  ── Configuration Preview ──────────────────────

  Gateway:   localhost:8888
  Admin API: localhost:9999
  Database:  ~/.hotplex/data/hotplex.db
  Worker:    claude_code
  Slack:     enabled, dm=allowlist, group=allowlist, @mention required
  Feishu:    disabled
  Service:   user level (launchd)

  Config:  ~/.hotplex/config.yaml
  Secrets: ~/.hotplex/.env

  ? Write configuration files? [Y/n]:
```

#### 3.12 Agent Config 模板增强

**改动文件**: `internal/cli/onboard/templates/*.md`

当前模板已有实质内容（SOUL.md 30 行含身份/原则/红线，USER.md 28 行含技术背景/偏好字段），非空壳。改进方向为增量增强：

**1. 添加 3 级 fallback 说明**（所有模板末尾追加）：

```markdown
## 配置层级

此文件支持 3 级 fallback，高优先级完整替换低优先级：
- 全局级：~/.hotplex/agent-configs/SOUL.md（本文件）
- 平台级：~/.hotplex/agent-configs/slack/SOUL.md
- Bot 级：~/.hotplex/agent-configs/slack/U12345/SOUL.md

使用 `hotplex-setup` skill 进行交互式个性化配置。
```

**2. USER.md 空字段添加引导注释**：

当前 USER.md 字段已有 `<!-- -->` 注释，但内容为空。改进为带示例值的占位提示：

```markdown
- **主要语言**：Go
- **框架**：Gin, gRPC
- **基础设施**：Docker, Kubernetes
```

保留 `<!-- -->` 注释说明可修改，首次写入时 AI 引导填入实际值。

**3. `displayAgentConfigPanel` 增强**（`onboard.go:122-157`）：

当前已展示文件名 + 描述 + 热重载提示。追加个性化引导：

```go
// 在 onboard.go displayAgentConfigPanel 末尾追加
fmt.Fprintf(os.Stderr, "  %s\n", output.Dim(
    "使用 hotplex-setup skill 交互式定制 Agent 人格和偏好。"))
```

#### 3.13 STT 安装引导

**改动文件**: `internal/cli/onboard/wizard.go` → `stepSTTCheck()`

当前只告警。改进为交互式安装引导。

**注意**：当前签名 `stepSTTCheck(configPath string) StepResult` 不接收 `reader` 和 `opts`，需改为：

```go
func stepSTTCheck(configPath string, reader *bufio.Reader, nonInteractive bool) StepResult {
    // ... 现有检测逻辑 ...

    // 如果检测到缺失，且交互模式：
    if len(details) > 0 && !nonInteractive {
        if promptYesNo(reader, "Install STT dependencies?") {
            // python3 -m pip install funasr-onnx modelscope
            // python3 ~/.hotplex/scripts/stt_server.py --download-model
        }
    }
}
```

---

### Phase 4: 职责分离（跨 Phase）

#### 3.14 Onboard → hotplex-setup 深度集成

本改进项是 Phase 4 的核心。`hotplex-setup` skill 不再是简单的"下一步建议"，而是作为 onboard 的 **AI 个性化引擎**，与 CLI wizard 形成完整闭环。

**职责划分**：

```
hotplex onboard (CLI)                    hotplex-setup (AI Skill)
─────────────────────                    ──────────────────────────
基础设施层（确定性、幂等）                个性化层（交互式、AI 驱动）
  ✓ 环境检测                              ✓ Agent 人格定制
  ✓ Secrets 生成                          ✓ 用户画像填充
  ✓ config.yaml 生成                      ✓ 工作流偏好配置
  ✓ .env 写入                             ✓ 平台/Bot 级配置引导
  ✓ 默认模板写入                          ✓ 3 级 fallback 策略指导
  ✓ 服务安装                              ✓ 配置内容审阅与调优
  ✗ 不做 AI 交互                          ✗ 不碰基础设施配置
```

##### 3.14.1 Onboard 侧桥接（`cmd/hotplex/onboard.go`）

`onboard.go:77-79` 已有 `displayAgentConfigPanel` 展示新建文件。在此基础上：

1. **面板增强**：追加 hotplex-setup 引导（见 3.12）
2. **Result 扩展**：`WizardResult` 新增 `SetupSkillHint bool` 标记
3. **条件触发**：仅当 `AgentConfigNew` 非空（有新文件）或 `USER.md` 未被个性化时展示

```go
// onboard.go post-Run 渲染扩展
if result.AgentConfigNew != nil || !isUserConfigPersonalized(config.HotplexHome()) {
    fmt.Fprint(os.Stderr, output.NoteBox("Agent Personalization", buildSkillHint()))
}
```

##### 3.14.2 hotplex-setup Skill 重构

**当前状态**：Skill 有 9 步，Step 7（Worker 与 Agent）仅提及环境变量，完全没有个性化引导能力。

**重构为**：基础设施 Steps 1-6 保持不变，Step 7（Worker 配置）拆分后新增 **Agent 个性化** 作为独立的核心流程。

**新增 Step 8：Agent 个性化配置**

```markdown
### 第 8 步：Agent 个性化配置

**触发条件**：
- 基础设施配置已完成（onboard 或手动配置过）
- `~/.hotplex/agent-configs/` 目录存在

**检测流程**：
1. 读取 `~/.hotplex/agent-configs/` 目录下的所有文件
2. 检查 USER.md 是否为默认模板（含 "<!-- -->" 空注释或空字段）
3. 如果全部已个性化 → 跳过，展示当前配置摘要
4. 如果有未个性化文件 → 启动交互式引导

**交互式引导**：

Phase A — 用户画像 (USER.md)：
  "你主要使用什么编程语言和框架？"
  "你的角色是什么？（如：后端工程师、全栈开发者）"
  "你偏好简洁回复还是详细解释？"
  "代码审查时希望 Agent 关注哪些方面？"
  → 收集后写入 USER.md 对应字段

Phase B — Agent 人格微调 (SOUL.md)：
  "当前 Agent 人格已配置为 [展示关键特征]。需要调整吗？"
  "沟通语言偏好？（默认：用户语言 + 英文术语）"
  "输出密度偏好？（默认：结论先行，省略开场白）"
  → 仅修改用户明确要求的字段，未提及的保持默认

Phase C — 3 级 Fallback 策略引导：
  "当前配置层级："
  "  全局：~/.hotplex/agent-configs/SOUL.md（当前生效）"
  "  平台：~/.hotplex/agent-configs/slack/SOUL.md（未配置）"
  "  Bot：~/.hotplex/agent-configs/slack/U12345/SOUL.md（未配置）"
  "是否需要平台级或 Bot 级定制？"
  → 如需要，引导创建对应目录和文件

Phase D — 确认与写入：
  展示所有变更的 diff
  "确认写入？[Y/n]"
  → 写入后自动生效（热重载）
```

**关键规则**：
- **幂等**：重复运行只更新用户明确回答的字段
- **最小变更**：不重写整个文件，用 diff 展示 + 精确编辑
- **尊重现有配置**：已个性化的内容不覆盖，除非用户明确要求
- **AI 判断**：当用户回答模糊时，Agent 应推理合理默认值并展示给用户确认

##### 3.14.3 完整用户旅程闭环

```
用户安装 HotPlex
  │
  ├─→ hotplex onboard (CLI)
  │     1. 环境检测 ✓
  │     2. Secrets 生成 ✓
  │     3. 平台配置 ✓
  │     4. config.yaml 生成 ✓
  │     5. Agent 模板写入 ✓
  │     6. 服务安装 ✓
  │     7. 验证通过 ✓
  │     8. 输出 Next Steps
  │        ├─ hotplex gateway start
  │        ├─ hotplex doctor
  │        └─ "使用 hotplex-setup 个性化 Agent"
  │
  ├─→ hotplex gateway start
  │
  ├─→ 连接消息平台 或 打开 WebChat
  │
  └─→ 对话中发送 "hotplex-setup" 或自然语言触发
        │
        AI Skill 交互式引导
          Phase A: 用户画像 → USER.md
          Phase B: Agent 人格 → SOUL.md
          Phase C: 3 级 Fallback 策略
          Phase D: 确认写入
        │
        个性化完成，Agent 行为自动适配
```

---

## 4. 实施优先级与依赖

```
Phase 1 (P0) ──────────────────────────────────────────
  3.1 Next Steps         ─── 无依赖，独立可做
  3.2 Worker 深度验证    ─── 无依赖，独立可做
  3.3 E2E 验证增强       ─── 无依赖，独立可做

Phase 2 (P1) ──────────────────────────────────────────
  3.4 进度指示           ─── 无依赖（总步数为编译时常量）
  3.5 动态版本号         ─── 无依赖
  3.6 输入校验           ─── 无依赖
  3.7 增量重跑           ─── 依赖 3.8（合并模式）+ 3.5（WizardOptions 扩展）
  3.8 配置合并           ─── 无依赖
  3.9 WebChat 感知       ─── 无依赖（前端独立，复用现有 error 状态）

Phase 3 (P2) ──────────────────────────────────────────
  3.10 平台引导          ─── 依赖 3.6（校验框架）+ 新增 embed 模板文件
  3.11 配置预览          ─── 依赖 3.7（增量重跑的选择结果）
  3.12 Agent 模板增强    ─── 无依赖
  3.13 STT 安装引导      ─── 无依赖

Phase 4 (核心) ──────────────────────────────────────────
  3.14 Onboard↔Skill 集成 ─── 依赖 3.1（Next Steps）+ 3.12（模板增强）
          ├─ 3.14.1 onboard.go 桥接提示
          └─ 3.14.2 hotplex-setup Skill 重构（核心交付物）
```

**建议实施顺序**：

```
Batch 1: 3.5 → 3.4 → 3.1 → 3.14.1 (onboard.go 桥接提示)
Batch 2: 3.6 → 3.2 → 3.3
Batch 3: 3.8 → 3.7 → 3.11
Batch 4: 3.10 → 3.12 → 3.13
Batch 5: 3.9 (前端独立)
Batch 6: 3.14.2 (hotplex-setup Skill 重构 — 核心交付物)
```

---

## 5. 涉及文件清单

| 改进项 | 主要改动文件 | 改动类型 |
|--------|-------------|---------|
| 3.1 Next Steps | `cmd/hotplex/onboard.go` | 扩展 post-Run 渲染 |
| 3.2 Worker 深度验证 | `internal/cli/onboard/wizard.go` | 修改 `stepWorkerDep` |
| 3.3 E2E 验证 | `internal/cli/onboard/wizard.go` | 修改 `stepVerify` |
| 3.4 进度指示 | `internal/cli/onboard/wizard.go` | 复用 `output.StepLine` |
| 3.5 动态版本 | `wizard.go` + `onboard.go` | `WizardOptions.Version` + 修改 `displayBanner` |
| 3.6 输入校验 | `internal/cli/onboard/wizard.go` | 新增 `promptWithValidation` |
| 3.7 增量重跑 | `internal/cli/onboard/wizard.go` | 重构 `Run` 为 step 注册表 + 选择执行 |
| 3.8 配置合并 | `internal/cli/onboard/templates.go` | 扩展 `extractPlatformBlock` |
| 3.9 WebChat 感知 | `webchat/app/components/chat/ChatContainer.assistant-ui.tsx` | 新增状态检测 |
| 3.10 平台引导 | `wizard.go` + `internal/cli/onboard/guides/*.md`（新增） | embed 模板 + 修改 `collectPlatformConfig` |
| 3.11 配置预览 | `internal/cli/onboard/wizard.go` | 新增 `displayPreview` |
| 3.12 Agent 模板 | `internal/cli/onboard/templates/*.md` + `onboard.go` | 增量增强 + 面板引导 |
| 3.13 STT 引导 | `internal/cli/onboard/wizard.go` | 修改 `stepSTTCheck`（签名变更） |
| 3.14 Onboard↔Skill | `onboard.go` + `.agent/skills/hotplex-setup/SKILL.md` | 桥接提示 + Skill 重构 |

---

## 6. 验证方案

### 6.1 单元测试

每个改进项对应的函数需新增 table-driven 测试：

| 测试文件 | 覆盖范围 |
|---------|---------|
| `wizard_test.go` | `displayNextSteps`、增量选择逻辑、输入校验函数 |
| `templates_test.go` | `MergeConfigYAML` 合并正确性、注释保留 |
| `validate_test.go`（新建） | 各字段校验规则 |

### 6.2 集成验证

```bash
# Phase 1 验证
hotplex onboard --force              # 全新配置，检查 Next Steps 输出
hotplex onboard                       # 增量重跑，检查识别已有配置
hotplex doctor                        # 确认 verify 覆盖所有 checker category

# Phase 2 验证
hotplex onboard --force              # 检查进度指示、版本号、输入校验
# 输入错误格式的 token，确认实时校验

# Phase 3 验证
hotplex onboard --force              # 检查平台引导文本
cat ~/.hotplex/agent-configs/SOUL.md  # 确认模板内容有意义

# WebChat 验证
# 1. Gateway 未启动 → 检查 setup 引导 UI
# 2. Gateway 启动 → 检查 welcome UI
```

### 6.3 质量门禁

```bash
make check  # fmt + lint + test + build
```

---

## 7. 风险与约束

| 风险 | 缓解措施 |
|------|---------|
| `MergeConfigYAML` 解析 YAML 注释边界 case 复杂 | 扩展现有 `extractPlatformBlock` 行扫描模式，不引入 YAML AST |
| Worker 功能验证（`claude --print`）可能涉及 API 调用 | 仅做本地验证（`--version`），功能性测试标记为可选 Level 3 |
| WebChat `isNetworkError` 误判 | 仅匹配已知网络错误模式（fetch failed / connection refused），其他错误走现有 ERROR 流程 |
| 增量重跑 `Run()` 拆解为 step 注册表 | `wizardContext` 从已有配置预填充，未选中步骤跳过执行，步骤间无运行时依赖 |
| `stepSTTCheck` 签名变更需同步更新调用方 | wizard.go `Run()` 中唯一调用点，改动范围可控 |
| hotplex-setup Skill 个性化引导过度修改文件 | 最小变更原则 + diff 确认 + 仅修改用户回答的字段 |

---

## 附录 A: 现有 Checker 注册清单

用于 `stepVerify` 扩展参考：

| Category | Checker ID | 文件 |
|----------|-----------|------|
| environment | `environment.go_version` | `checkers/environment.go` |
| environment | `environment.os_arch` | `checkers/environment.go` |
| environment | `environment.build_tools` | `checkers/environment.go` |
| config | `config.exists` | `checkers/config.go` |
| config | `config.syntax` | `checkers/config.go` |
| config | `config.required` | `checkers/config.go` |
| config | `config.values` | `checkers/config.go` |
| config | `config.env_vars` | `checkers/config.go` |
| dependencies | `dependencies.worker_binary` | `checkers/dependencies.go` |
| dependencies | `dependencies.sqlite_path` | `checkers/dependencies.go` |
| security | `security.jwt_strength` | `checkers/security.go` |
| security | `security.admin_token` | `checkers/security.go` |
| security | `security.file_permissions` | `checkers/security.go` |
| security | `security.env_in_git` | `checkers/security.go` |
| runtime | `runtime.disk_space` | `checkers/runtime.go` |
| runtime | `runtime.port_available` | `checkers/runtime.go` |
| runtime | `runtime.orphan_pids` | `checkers/runtime.go` |
| runtime | `runtime.data_dir_writable` | `checkers/runtime.go` |
| messaging | `messaging.slack_creds` | `checkers/messaging.go` |
| messaging | `messaging.feishu_creds` | `checkers/messaging.go` |
| stt | `stt.runtime` | `checkers/stt.go` |
| agent_config | `agent.suffix_deprecated` | `checkers/agentconfig.go` |
| agent_config | `agent.directory_structure` | `checkers/agentconfig.go` |
| agent_config | `agent.global_files` | `checkers/agentconfig.go` |
