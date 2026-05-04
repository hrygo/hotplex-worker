---
type: spec
tags:
  - project/HotPlex
  - gateway/handler
  - gateway/bridge
  - messaging/interaction
  - worker/claudecode
  - worker/opencodeserver
  - bug/critical
date: 2026-05-04
status: proposed
priority: critical
estimated_hours: 12
---

# 交互授权链全链路修复规格书

> 版本: v3.0
> 日期: 2026-05-04
> 状态: Proposed
> 影响: Slack / 飞书 / WebChat 三个平台的用户交互授权完全不可用

---

## 1. 概述

### 1.1 问题

HotPlex Gateway 的用户交互授权链（PermissionRequest / QuestionRequest / ElicitationRequest）**双方向全部断裂**，不仅响应方向（用户 → Worker）有 3 个致命 bug，请求方向（Worker → 用户）的源头也被堵死。

**用户从未在 Slack / 飞书上看到过任何授权请求**，因为 Worker 启动参数缺少关键的 stdio 控制协议标志，且权限绕过模式被硬编码。

### 1.2 核心设计原则

**授权系统与权限模式完全解耦。** 交互链路是独立的安全兜底通道，无论 Worker 使用何种权限模式（`bypassPermissions` / `default` / `plan`），授权系统必须始终可用：

- `--dangerously-skip-permissions` 是可配置的缺省选项，不是要移除的功能
- 即使在 bypass 模式下，Claude Code 对 bypass-immune 操作（修改 `.claude/`、`.git/`、shell 配置等）仍会发出 `control_request`
- 授权系统的管道（请求路由、响应回传、超时拒绝）独立于权限模式工作

### 1.3 交互类型

| 类型 | AEP Kind | 触发场景 |
|------|----------|---------|
| PermissionRequest / Response | `permission_request` / `permission_response` | 工具执行授权（Bash、FileEdit 等） |
| QuestionRequest / Response | `question_request` / `question_response` | AskUserQuestion 多选题 |
| ElicitationRequest / Response | `elicitation_request` / `elicitation_response` | MCP Server 用户输入请求 |

### 1.4 完整数据流（当前状态：全链路断裂）

```
                         ┌─── BP#0: 请求源头被堵死 ──────────────────────────┐
                         │                                                   │
Claude Code              │  缺少 --permission-prompt-tool stdio             │
  → 永远不会输出          │  → effectivePermissionPromptToolName=undefined   │
  → control_request +    │  → getCanUseToolFn() 返回 hasPermissionsToUseTool │
    can_use_tool         │  → ask 结果被内部吞没，不输出到 stdout            │
                         │                                                   │
  叠加:                   │  --dangerously-skip-permissions (硬编码)          │
  → bypass-immune 操作   │  → bypass-immune 步骤 1g 在 step 2a 之前返回 ask │
    修改 .claude/ 等     │  → 但无 stdio 通道 → ask 被静默拒绝               │
                         │                                                   │
  叠加:                   │  正常操作                                         │
  → 正常工具调用          │  → step 2a bypass 返回 allow → 直接批准          │
                         │  → 不触发 control_request                         │
                         └───────────────────────────────────────────────────┘
OCS                      ┌─── BP#0-OCS: 同样源头被堵死 ──────────────────────┐
  → set_permission_mode  │  "bypassPermissions" (硬编码)                      │
  → 永远不会触发          │  → 所有工具权限在本地自动批准                       │
  → permission.asked     └───────────────────────────────────────────────────┘

EVEN IF 请求发出（修复 BP#0 后）:

Worker ──interaction_event──→ forwardEvents() ──→ Hub ──→ pcEntry ──→ PlatformConn
                                                                       │
                                                            ┌──────────┼──────────┐
                                                            ▼          ▼          ▼
                                                        Slack UI    飞书 Card   WebChat
                                                        (按钮 ✅)   (文本 ✅)   (仅Perm)
                                                            │          │          │
                         ┌─── BP#1-3: 响应方向断裂 ──────────┤          │          │
                         ▼                                   ▼          ▼          ▼
                    用户操作 ──→ pi.SendResponse(metadata)
                                   │
                         ❌ BP#1: OwnerID="" → Bridge.Handle() 拒绝
                         ❌ BP#2: _ = 吞没错误，无日志
                         ❌ BP#3: handleInput() → w.Input(ctx, content, nil) → metadata 丢失
                                   │
                         5 分钟超时 → 自动拒绝 → Worker 收到超时拒绝
```

---

## 2. 断裂点详细分析

### 2.0 BP#0: 请求源头被堵死 — 缺少 stdio 控制协议 + 权限绕过硬编码（根本原因）

**这是整个功能不可用的根本原因。** 两个独立问题叠加导致交互请求事件**永远不会产生**。

#### 根因 A: 缺少 `--permission-prompt-tool stdio`（Claude Code Worker）

**文件**: `internal/worker/claudecode/worker.go` — `buildCLIArgs()`

当前启动参数：
```go
args := []string{
    "--print",
    "--verbose",
    "--output-format", "stream-json",
    "--input-format", "stream-json",
    "--dangerously-skip-permissions",
}
// 缺少: "--permission-prompt-tool", "stdio"
```

**Claude Code SDK 的权限提示机制**（源码参考 `claude-code-src/src/cli/print.ts:802-805`）：

```typescript
// 当使用 SDK URL 时，强制使用 stdio 权限提示
const effectivePermissionPromptToolName = options.sdkUrl
  ? 'stdio'
  : options.permissionPromptToolName
```

**`permissionPromptToolName` 三种取值**：

| 值 | 行为 | 对 HotPlex 的影响 |
|----|------|-------------------|
| `undefined`（当前） | `getCanUseToolFn()` 返回 `hasPermissionsToUseTool()`，仅内置规则检查，**不输出 control_request** | ❌ 交互请求永远不会产生 |
| `'stdio'`（需要） | `getCanUseToolFn()` 返回 `structuredIO.createCanUseTool()`，通过 stdout 输出 `control_request`，等待 stdin 的 `control_response` | ✅ 交互请求正确流经 HotPlex |
| MCP tool name | 调用指定 MCP tool 作为权限 oracle | 不适用 |

**`getCanUseToolFn()` 路由逻辑**（`print.ts:4267-4334`）：

```typescript
export function getCanUseToolFn(permissionPromptToolName, structuredIO, ...) {
  if (permissionPromptToolName === 'stdio') {
    return structuredIO.createCanUseTool(onPermissionPrompt)  // ← 输出 control_request
  }
  // undefined 分支 — 不输出 control_request
  return async (tool, input, ...) =>
    forceDecision ?? (await hasPermissionsToUseTool(tool, input, ...))
}
```

**结论**: 缺少 `--permission-prompt-tool stdio` 是**请求方向断裂的第一根因**。即使移除 `--dangerously-skip-permissions`，没有此标志，`control_request` 永远不会输出到 stdout。

#### 根因 B: 硬编码 `--dangerously-skip-permissions`（加剧因素）

**文件**: `internal/worker/claudecode/worker.go:218-225`

```go
func (w *Worker) buildCLIArgs(session worker.SessionInfo, resume bool) []string {
    args := []string{
        "--print",
        "--verbose",
        "--output-format", "stream-json",
        "--input-format", "stream-json",
        "--dangerously-skip-permissions",  // ← 硬编码！
    }
    // ...
    if session.PermissionMode != "" {
        args = append(args, "--permission-mode", session.PermissionMode)
    }
    if session.SkipPermissions {
        args = append(args, "--dangerously-skip-permissions")  // ← 重复添加
    }
```

**Claude Code 权限检查流程**（`permissions.ts:hasPermissionsToUseToolInner()`，10 步决策链）：

```
Step 1a: Deny rule?          → deny（bypass-immune）
Step 1b: Ask rule (whole)?   → ask（bypass-immune）
Step 1c: Tool checkPerms?    → passthrough/ask/deny
Step 1d: Tool denied?        → deny（bypass-immune）
Step 1e: requiresUserInteraction? → ask（bypass-immune）
Step 1f: Content-specific ask?    → ask（bypass-immune）
Step 1g: safetyCheck?        → ask（bypass-immune）← .claude/ .git/ .bashrc 等
─── bypass 分界线 ───
Step 2a: bypassPermissions?  → allow（bypass 生效）
Step 2b: Always-allowed?     → allow
Step 3:  Convert passthrough → ask
```

**影响**: `--dangerously-skip-permissions` 使 step 2a 在所有正常操作上直接返回 `allow`。只有 bypass-immune 操作（步骤 1a-1g）能穿透到 `ask`。但由于缺少根因 A 的 `--permission-prompt-tool stdio`，即使穿透到 `ask` 也不会输出 `control_request`。

**两根因叠加效果**：
- 正常操作 → step 2a bypass → `allow` → 无请求
- bypass-immune 操作 → step 1g `ask` → 无 stdio 通道 → 被静默拒绝

#### OCS Worker — 通配符规则注入阻断权限事件

**机制**: OCS 没有 `bypassPermissions` 模式，其权限系统是**纯规则引擎**（`allow/deny/ask`）。无匹配规则时默认为 `ask`，触发 `permission.asked` SSE 事件。HotPlex 通过注入通配符 allow-all 规则实现等效 bypass。

**调用链**: `Start()` → `initSessionConn()` → `applyPermissions()` → `setPermissionMode()` → `PATCH /session/{id}`

**文件 1**: `internal/worker/opencodeserver/worker.go:490-502` — 无条件调用 bypass

```go
func (w *Worker) applyPermissions(ctx context.Context, _ worker.SessionInfo) error {
    // ...
    _, err := cmd.SendControlRequest(ctx, "set_permission_mode", map[string]any{
        "mode": "bypassPermissions",  // ← 硬编码！
    })
    return err
}
```

**文件 2**: `internal/worker/opencodeserver/commands.go:155-168` — 翻译为通配符规则

```go
func (c *ServerCommander) setPermissionMode(ctx context.Context, body map[string]any) (map[string]any, error) {
    mode, _ := body["mode"].(string)
    var rules []map[string]any
    switch mode {
    case "bypassPermissions":
        rules = []map[string]any{{"permission": "*", "action": "allow", "pattern": "*"}}
    default:
        rules = []map[string]any{}
    }
    // PATCH /session/{id} → OCS 服务端合并规则到 session.ruleset
    if err := c.doPatch(ctx, "/session/"+c.sessionID, map[string]any{"permission": rules}); err != nil {
        return nil, fmt.Errorf("opencode set permission: %w", err)
    }
    return map[string]any{"success": true, "mode": mode}, nil
}
```

**OCS 服务端处理**（`opencode/server/routes/instance/session.ts:259-317`）：

```typescript
// PATCH /:sessionID — 接收 permission 规则数组
if (updates.permission !== undefined) {
    yield* session.setPermission({
        sessionID,
        // 合并: 当前规则 + 新规则，按 findLast 匹配
        permission: Permission.merge(current.permission ?? [], updates.permission),
    })
}
```

**OCS 规则评估**（`opencode/permission/evaluate.ts:9-15`）：

```typescript
export function evaluate(permission, pattern, ...rulesets): Rule {
    const rules = rulesets.flat()
    const match = rules.findLast(
        (rule) => Wildcard.match(permission, rule.permission) && Wildcard.match(pattern, rule.pattern),
    )
    return match ?? { action: "ask", permission, pattern: "*" }  // 无匹配 → ask
}
```

**影响**: 通配符规则 `{permission:"*", action:"allow", pattern:"*"}` 匹配所有工具调用的所有 pattern。`Permission.ask()` 中 `evaluate()` 对每个 pattern 都返回 `allow`，`needsAsk` 始终为 `false`，函数在 `if (!needsAsk) return` 处提前返回，**永远不会发布** `permission.asked` SSE 事件。

**与 Claude Code 的关键区别**：OCS 没有 bypass-immune 概念。规则引擎对安全关键操作（修改配置文件等）与普通操作一视同仁 — 只要规则匹配 `allow` 就自动批准。

#### 对比参考实现

| 维度 | Claude Code 原生 | HotPlex (当前) | HotPlex (修复后) |
|------|-----------------|----------------|------------------|
| 控制协议 | 终端交互 / SDK `--permission-prompt-tool stdio` | **未启用** | `--permission-prompt-tool stdio` |
| 权限模式 | 用户选择 | **硬编码 bypass** | 可配置，默认 bypass |
| 交互触发 | 每次需要确认的工具调用 | **永不触发** | bypass-immune 始终触发，其他按模式 |
| 用户参与 | 终端对话框 / SDK host | **无** | Slack/飞书/WebChat UI |

---

### 2.1 BP#1: OwnerID 为空导致 Bridge 立即拒绝（致命）

**根因**: `registerInteraction()` 闭包创建的响应 envelope 缺少 OwnerID。

| 平台 | 文件 | 行号 | 问题 |
|------|------|------|------|
| Slack | `internal/messaging/slack/interaction.go` | 424 | `OwnerID: ""` |
| 飞书 | `internal/messaging/feishu/interaction.go` | 150 | OwnerID 字段缺失 |

**守卫代码** (`internal/messaging/bridge.go:75`):
```go
func (b *Bridge) Handle(ctx context.Context, env *events.Envelope, pc PlatformConn) error {
    if env.OwnerID == "" {
        return fmt.Errorf("messaging bridge: OwnerID not set for platform message")
    }
    // ... 永远到不了这里
}
```

**为何闭包无法获取 OwnerID**: `registerInteraction()` 的参数是 `(requestID, sessionID, kind, conn)`，不包含 userID/ownerID。闭包在创建时没有捕获用户的 OwnerID。

### 2.2 BP#2: 错误被静默吞没（致命）

| 平台 | 文件 | 行号 |
|------|------|------|
| Slack | `internal/messaging/slack/interaction.go` | 438 |
| 飞书 | `internal/messaging/feishu/interaction.go` | 163 |

```go
_ = a.Bridge().Handle(respCtx, env, conn)  // 错误被丢弃
```

**影响**: 即使 BP#1 被修复，后续可能出现的错误（session 不存在、worker 已终止等）也不会被记录或反馈给用户。用户看到"已收到响应"确认，但响应从未送达。

同理，`InteractionManager.watchTimeout()` 中的 `SendResponse` 调用也会触发同样的静默失败。

### 2.3 BP#3: handleInput() 不传递 metadata 给 Worker（致命）

**文件**: `internal/gateway/handler.go:183`

```go
func (h *Handler) handleInput(ctx context.Context, env *events.Envelope) error {
    data, ok := env.Event.Data.(map[string]any)
    content, _ := data["content"].(string)
    // ... metadata 从未提取 ...

    w := h.sm.GetWorker(env.SessionID)
    if w != nil {
        w.Input(ctx, content, nil)  // ← metadata 始终为 nil
    }
}
```

**Worker 端** (`internal/worker/claudecode/worker.go:290-333`):
```go
func (w *Worker) Input(ctx context.Context, content string, metadata map[string]any) error {
    if metadata != nil {
        // 检查 permission_response / question_response / elicitation_response
        // ← 永远不执行，因为 metadata 是 nil
    }
    // 走正常用户消息路径 → 发送空字符串给 Worker → 无效操作
}
```

**注意**: OCS Worker (`internal/worker/opencodeserver/worker.go:236-253`) 同样依赖 metadata 来路由 `permission_response` 和 `question_response` 到 HTTP 端点。此 bug 同时影响两种 Worker 类型。

### 2.4 BP#4: WebChat 事件类型不完整（次要）

**文件**: `webchat/lib/ai-sdk-transport/client/constants.ts`

WebChat 客户端仅定义:
```typescript
PermissionRequest: 'permission_request',
PermissionResponse: 'permission_response',
```

缺失: `QuestionRequest`、`QuestionResponse`、`ElicitationRequest`、`ElicitationResponse`

**文件**: `webchat/lib/ai-sdk-transport/client/browser-client.ts`

仅有 `sendPermissionResponse()` 方法，无 `sendQuestionResponse()` / `sendElicitationResponse()`。

---

## 3. 修复方案

### 3.0 Fix #0: 启用 stdio 控制协议 + 权限模式可配置化（根本修复）

**设计原则**: 授权系统与权限模式解耦。交互链路始终可用，权限模式仅决定哪些操作触发请求。

#### 3.0.1 Claude Code Worker — 启用 stdio 控制协议

**文件**: `internal/worker/claudecode/worker.go` — `buildCLIArgs()`

```go
func (w *Worker) buildCLIArgs(session worker.SessionInfo, resume bool) []string {
    args := []string{
        "--print",
        "--verbose",
        "--output-format", "stream-json",
        "--input-format", "stream-json",
        "--permission-prompt-tool", "stdio", // ← 新增: 启用 control_request/control_response 协议
    }

    // 权限模式: 默认 bypass（保持现有行为），可配置覆盖
    if session.SkipPermissions {
        args = append(args, "--dangerously-skip-permissions")
    } else if session.PermissionMode != "" {
        args = append(args, "--permission-mode", session.PermissionMode)
    } else {
        args = append(args, "--dangerously-skip-permissions") // 默认 bypass
    }
    // ...
}
```

**关键变化**:

1. **新增 `--permission-prompt-tool stdio`**: 这是使 Claude Code 通过 stdout 输出 `control_request` 的必要条件。没有此标志，无论权限模式如何设置，`control_request` 永远不会产生。

2. **保留 `--dangerously-skip-permissions` 作为默认**: 保持现有行为不变。在 bypass 模式下：
   - 正常操作: step 2a 自动批准，无 `control_request`
   - bypass-immune 操作（修改 `.claude/`、`.git/`、shell 配置等）: step 1g 返回 `ask`，通过 stdio 输出 `control_request`

3. **`--permission-prompt-tool stdio` 与 `--dangerously-skip-permissions` 的协同**:
   - `hasPermissionsToUseToolInner()` 的 bypass-immune 检查（步骤 1a-1g）在 bypass（步骤 2a）之前执行
   - 当 bypass-immune 操作返回 `ask` 时，`structuredIO.createCanUseTool()` 会输出 `control_request`
   - `--dangerously-skip-permissions` 只影响正常操作的自动批准，不影响 bypass-immune 操作的请求

**Claude Code SDK 验证** (`claude-code-src/src/cli/structuredIO.ts:533-658`):

```typescript
// createCanUseTool() 内部:
// 1. 先调用 hasPermissionsToUseTool() 进行规则检查
const mainPermissionResult = await hasPermissionsToUseTool(tool, input, ...)

// 2. allow 或 deny → 直接返回，不输出 control_request
if (mainPermissionResult.behavior === 'allow' || mainPermissionResult.behavior === 'deny') {
  return mainPermissionResult
}

// 3. ask → 输出 control_request，等待 control_response
const result = await this.sendRequest<PermissionToolOutput>({
  type: 'control_request',
  subtype: 'can_use_tool',
  tool_name: tool.name,
  input,
  ...
})
```

#### 3.0.2 OCS Worker — 规则注入可配置化

**机制**: OCS 没有 `--permission-prompt-tool` 等效标志，也不需要。OCS 默认行为已经是 `ask`（规则引擎无匹配时发布 `permission.asked`）。修复只需控制是否注入通配符 allow-all 规则。

**文件 1**: `internal/worker/opencodeserver/worker.go` — `applyPermissions()`

```go
func (w *Worker) applyPermissions(ctx context.Context, session worker.SessionInfo) error {
    w.Mu.Lock()
    cmd := w.cmd
    w.Mu.Unlock()
    if cmd == nil {
        return fmt.Errorf("commander not initialized")
    }

    // 默认 bypass（保持现有行为），可配置覆盖
    mode := "bypassPermissions"
    if session.SkipPermissions {
        mode = "bypassPermissions"
    } else if session.PermissionMode != "" {
        mode = session.PermissionMode
    }

    _, err := cmd.SendControlRequest(ctx, "set_permission_mode", map[string]any{
        "mode": mode,
    })
    return err
}
```

**文件 2**: `internal/worker/opencodeserver/commands.go` — `setPermissionMode()` 扩展模式支持

```go
func (c *ServerCommander) setPermissionMode(ctx context.Context, body map[string]any) (map[string]any, error) {
    mode, _ := body["mode"].(string)
    var rules []map[string]any
    switch mode {
    case "bypassPermissions":
        // 通配符 allow-all: 所有工具调用自动批准
        rules = []map[string]any{{"permission": "*", "action": "allow", "pattern": "*"}}
    case "default", "":
        // 不注入规则: OCS 默认行为（无匹配规则 → ask → 发布 permission.asked）
        rules = []map[string]any{}
    case "plan":
        // 只读允许 + 写入需审批
        rules = []map[string]any{
            {"permission": "read", "action": "allow", "pattern": "*"},
        }
    default:
        rules = []map[string]any{}
    }
    if err := c.doPatch(ctx, "/session/"+c.sessionID, map[string]any{"permission": rules}); err != nil {
        return nil, fmt.Errorf("opencode set permission: %w", err)
    }
    return map[string]any{"success": true, "mode": mode}, nil
}
```

**OCS 与 Claude Code 修复对比**：

| 维度 | Claude Code Worker | OCS Worker |
|------|--------------------|------------|
| 根因 | 缺少 `--permission-prompt-tool stdio` + 硬编码 bypass | 通配符规则注入阻塞所有权限事件 |
| 修复方式 | 添加 CLI 标志 + 可配置模式 | 控制规则注入内容 |
| bypass 机制 | CLI 标志影响 SDK 内部决策链 | PATCH 注入 allow-all 规则到 session |
| bypass-immune | 有（步骤 1a-1g 穿透 bypass） | **无**（规则引擎统一处理） |
| 默认行为 | SDK 无提示机制 → 需 stdio | 服务端默认 ask → 只需不注入规则 |

#### 3.0.3 环境变量覆盖

增加 `HOTPLEX_PERMISSION_MODE` 环境变量，允许全局覆盖权限模式（用于 CI/自动化场景）:

| 值 | 行为 |
|----|------|
| `default` | 需要用户授权（通过 Slack/飞书/WebChat） |
| `bypassPermissions` | 自动批准所有权限（当前默认行为） |
| `plan` | 只读 + 显式审批 |
| 空 / 未设置 | 使用 `bypassPermissions`（保持现有行为） |

**优先级**: `session.SkipPermissions=true` > `session.PermissionMode` > `HOTPLEX_PERMISSION_MODE` > `bypassPermissions`（默认）

#### 3.0.4 修复后的交互触发矩阵

| 权限模式 | 正常工具调用 | bypass-immune 操作 | AskUserQuestion | MCP Elicitation |
|---------|-------------|-------------------|-----------------|-----------------|
| `bypassPermissions`（默认） | 自动批准 | ✅ 触发 control_request | ✅ 触发 control_request | ✅ 触发 control_request |
| `default` | ✅ 触发 control_request | ✅ 触发 control_request | ✅ 触发 control_request | ✅ 触发 control_request |
| `plan` | 只读自动批准 | ✅ 触发 control_request | ✅ 触发 control_request | ✅ 触发 control_request |

**注意**: `--permission-prompt-tool stdio` 是所有场景的前提条件。没有此标志，上表中所有 ✅ 项都不会触发。

### 3.1 Fix #1: 闭包捕获 OwnerID

**策略**: `registerInteraction()` 增加 `ownerID` 参数，闭包内设置到 envelope。

**OwnerID 来源**: 在 `sendPermissionRequest()` / `sendQuestionRequest()` / `sendElicitationRequest()` 方法中，原始请求 envelope `env.OwnerID` 包含了发起 session 的用户 ID。

#### Slack 适配器

`internal/messaging/slack/interaction.go` — `registerInteraction()`:

```go
func (a *Adapter) registerInteraction(requestID, sessionID, ownerID string, kind events.Kind, _ string, conn *SlackConn) {
    a.Interactions.Register(&messaging.PendingInteraction{
        // ...
        SendResponse: func(metadata map[string]any) {
            // ...
            env := &events.Envelope{
                // ...
                OwnerID: ownerID,  // ← 修复: 使用捕获的 ownerID
            }
            // ...
        },
    })
}
```

所有调用点传 `env.OwnerID` 作为 `ownerID` 参数。

#### 飞书适配器

`internal/messaging/feishu/interaction.go` — `registerInteraction()`:

```go
func (a *Adapter) registerInteraction(requestID, sessionID, ownerID string, kind events.Kind, conn *FeishuConn) {
    // 同上
}
```

### 3.2 Fix #2: 错误日志化

**策略**: 替换 `_ =` 为带日志的错误处理。

```go
SendResponse: func(metadata map[string]any) {
    // ...
    if a.Bridge() != nil {
        if err := a.Bridge().Handle(respCtx, env, conn); err != nil {
            a.Log.Error("interaction: failed to send response",
                "request_id", requestID,
                "session_id", sessionID,
                "err", err)
        }
    } else {
        a.Log.Error("interaction: bridge not available",
            "request_id", requestID,
            "session_id", sessionID)
    }
},
```

### 3.3 Fix #3: handleInput() 提取并传递 metadata

**文件**: `internal/gateway/handler.go` — `handleInput()`

```go
func (h *Handler) handleInput(ctx context.Context, env *events.Envelope) error {
    data, ok := env.Event.Data.(map[string]any)
    if !ok {
        return h.sendErrorf(ctx, env, events.ErrCodeInvalidMessage, "malformed input data")
    }

    content, _ := data["content"].(string)

    // 修复: 提取 metadata
    var metadata map[string]any
    if md, ok := data["metadata"].(map[string]any); ok && len(md) > 0 {
        metadata = md
    }

    // 交互响应 (metadata 非空, content 为空): 跳过命令检测和状态转换
    if metadata != nil {
        w := h.sm.GetWorker(env.SessionID)
        if w != nil {
            if err := w.Input(ctx, content, metadata); err != nil {
                h.log.Warn("gateway: worker interaction response", "err", err, "session_id", env.SessionID)
            }
        } else {
            h.log.Warn("gateway: interaction response but no worker", "session_id", env.SessionID)
        }
        return nil
    }

    // --- 以下为正常用户输入路径 (metadata == nil)，行为不变 ---
    // ... command detection, state check, state transition ...
    w := h.sm.GetWorker(env.SessionID)
    if w != nil {
        w.Input(ctx, content, nil)
    }
    // ...
}
```

**设计考量**:

1. **交互响应不应触发命令检测**: `content` 为空且 `metadata` 非空时，跳过 help/control/worker 命令解析。
2. **交互响应不应触发状态转换**: Worker 等待交互响应时 session 处于 RUNNING 状态，无需 IDLE → RUNNING 转换。
3. **交互响应不应记录到 ConversationStore**: 空内容的交互响应无需持久化为用户消息。
4. **交互响应不应 CaptureInbound**: 控制响应不是用户输入，不应进入 replay 缓冲区。

### 3.4 Fix #4: WebChat 补全事件类型（次要）

**文件**: `webchat/lib/ai-sdk-transport/client/constants.ts`:

```typescript
export const EventKind = {
    // ... existing ...
    QuestionRequest: 'question_request',
    QuestionResponse: 'question_response',
    ElicitationRequest: 'elicitation_request',
    ElicitationResponse: 'elicitation_response',
} as const
```

**文件**: `webchat/lib/ai-sdk-transport/client/browser-client.ts`:

增加事件分发和响应方法 `sendQuestionResponse()`, `sendElicitationResponse()`。

**文件**: `webchat/lib/ai-sdk-transport/client/envelope.ts`:

增加 envelope builder: `createQuestionResponseEnvelope`, `createElicitationResponseEnvelope`。

---

## 4. 修改文件清单

| # | 文件 | 修改类型 | 优先级 | 说明 |
|---|------|---------|--------|------|
| 0a | `internal/worker/claudecode/worker.go` | 新增 `--permission-prompt-tool stdio`；`--dangerously-skip-permissions` 改为可配置 | **P0** | 请求源头（双根因） |
| 0b | `internal/worker/opencodeserver/worker.go` | `applyPermissions` 接受 session 配置，控制通配符规则注入 | **P0** | 请求源头 |
| 0c | `internal/worker/opencodeserver/commands.go` | `setPermissionMode()` 扩展模式支持（default/plan/bypass） | **P0** | 请求源头 |
| 1 | `internal/messaging/slack/interaction.go` | `registerInteraction` 增加 ownerID 参数 | **P0** | 响应路由 |
| 2 | `internal/messaging/feishu/interaction.go` | 同上 + 错误日志化 | **P0** | 响应路由 |
| 3 | `internal/gateway/handler.go` | `handleInput()` 提取 metadata 并传递给 Worker | **P0** | 响应路由 |
| 4 | `webchat/lib/ai-sdk-transport/client/constants.ts` | 补全事件类型常量 | P1 | WebChat |
| 5 | `webchat/lib/ai-sdk-transport/client/browser-client.ts` | 补全事件分发和响应方法 | P1 | WebChat |
| 6 | `webchat/lib/ai-sdk-transport/client/envelope.ts` | 补全 response envelope builder | P1 | WebChat |

---

## 5. 依赖与约束

### 5.1 向后兼容

- **Fix #0 (新增 `--permission-prompt-tool stdio`)** 是行为变更但影响最小：
  - 默认仍使用 `--dangerously-skip-permissions`，正常操作行为不变
  - 新增的是 bypass-immune 操作的交互能力（修改 `.claude/`、`.git/` 等），之前这些操作被静默拒绝
  - 通过 `HOTPLEX_PERMISSION_MODE=bypassPermissions` 显式设置与默认行为一致
- **Fix #0 (权限模式可配置)** 保持默认行为不变：
  - `--dangerously-skip-permissions` 仍是默认值
  - 用户可通过 `HOTPLEX_PERMISSION_MODE=default` 切换到全量授权模式
- `registerInteraction()` 签名变更是内部 API
- `handleInput()` 的 metadata 传递是纯增量 — metadata 为 nil 时行为与当前完全一致
- WebChat 事件类型新增不影响现有 Permission 流程

### 5.2 跨平台一致性

三个平台的交互响应必须产生结构一致的 metadata:

| 交互类型 | metadata key | value 结构 |
|---------|-------------|-----------|
| Permission | `permission_response` | `{request_id, allowed, reason}` |
| Question | `question_response` | `{id, answers: {"_": value}}` |
| Elicitation | `elicitation_response` | `{id, action, content?}` |

### 5.3 超时行为不变

`InteractionManager.watchTimeout()` 的 5 分钟自动拒绝行为保持不变。Fix #1 和 #2 修复后，超时响应也能正确到达 Worker。

### 5.4 Claude Code 权限模式参考

| 模式 | 行为 | 适用场景 |
|------|------|---------|
| `default` | 每个工具调用需用户确认 | 全量授权（`HOTPLEX_PERMISSION_MODE=default`） |
| `bypassPermissions` | 自动批准所有（bypass-immune 除外） | **HotPlex 默认** + bypass-immune 兜底 |
| `plan` | 只读 + 显式审批 | 只读分析场景 |
| `acceptEdits` | 自动批准文件编辑，其他需确认 | 可信项目 |
| `dontAsk` | 自动拒绝所有需要确认的操作 | 后台运行 |

### 5.5 bypass-immune 操作清单（仅 Claude Code）

**注意**: 此清单仅适用于 Claude Code Worker。OCS 使用规则引擎，没有 bypass-immune 概念 — 所有操作的授权行为完全由注入的规则集决定。

以下 Claude Code 操作无论权限模式如何，都会触发 `control_request`（前提: 已启用 `--permission-prompt-tool stdio`）:

| 类别 | 受保护路径/场景 | 触发步骤 |
|------|---------------|---------|
| 危险文件 | `.gitconfig`, `.bashrc`, `.zshrc`, `.profile`, `.mcp.json`, `.claude.json` | Step 1g (safetyCheck) |
| 危险目录 | `.git/`, `.claude/`, `.vscode/`, `.idea/` | Step 1g (safetyCheck) |
| Claude 配置 | `.claude/settings.json`, `.claude/commands/`, `.claude/agents/`, `.claude/skills/` | Step 1g (safetyCheck) |
| 沙箱网络访问 | 任何出站网络连接请求 | structuredIO.createSandboxAskCallback |
| 跨机器桥消息 | SendMessageTool bridge messages | Step 1g (safetyCheck) |
| 用户配置的 deny 规则 | 用户 deny 规则匹配的工具 | Step 1a (deny) |
| 用户配置的 ask 规则 | 用户 ask 规则匹配的操作 | Step 1f (ask rule) |

---

## 6. 测试要求

### 6.1 单元测试

| 测试 | 覆盖点 |
|------|--------|
| `TestBuildCLIArgs_StdioPermissionPrompt` | 默认包含 `--permission-prompt-tool stdio` |
| `TestBuildCLIArgs_DefaultBypass` | 默认包含 `--dangerously-skip-permissions` |
| `TestBuildCLIArgs_SkipPermissions` | `session.SkipPermissions=true` 时包含 `--dangerously-skip-permissions` |
| `TestBuildCLIArgs_CustomPermissionMode` | `session.PermissionMode=default` 时使用 `--permission-mode default`，不包含 `--dangerously-skip-permissions` |
| `TestBuildCLIArgs_NoDuplicateSkipPermissions` | `--dangerously-skip-permissions` 不重复添加 |
| `TestApplyPermissions_DefaultBypass` | OCS 默认注入通配符 allow-all 规则 |
| `TestApplyPermissions_CustomMode` | OCS 使用 session 指定的权限模式（不注入/注入部分规则） |
| `TestSetPermissionMode_RulesetTranslation` | `commands.go` 正确翻译 bypassPermissions/default/plan 为规则集 |
| `TestRegisterInteraction_OwnerID` | 闭包创建的 envelope OwnerID 不为空 |
| `TestRegisterInteraction_ErrorLogged` | Bridge.Handle 返回错误时产生日志 |
| `TestHandleInput_MetadataPassThrough` | metadata 正确提取并传递给 Worker.Input() |
| `TestHandleInput_MetadataSkipsCommandDetection` | metadata 非空时跳过命令检测 |
| `TestHandleInput_MetadataSkipsStateTransition` | metadata 非空时跳过 IDLE→RUNNING |
| `TestHandleInput_MetadataSkipsConversationStore` | metadata 非空时不写入 ConversationStore |
| `TestHandleInput_NilMetadata_UnchangedBehavior` | metadata 为 nil 时行为不变（回归测试） |

### 6.2 集成测试

| 测试 | 覆盖点 |
|------|--------|
| E2E Permission Flow (Claude Code, bypass-immune) | Worker 修改 `.claude/` 文件 → 输出 `control_request` → 平台展示 → 用户响应 → Worker 收到 `control_response` |
| E2E Permission Flow (Claude Code, default mode) | Worker 调用任意工具 → 输出 `control_request` → 平台展示 → 用户响应 → Worker 收到 `control_response` |
| E2E Permission Flow (OCS) | SSE `permission.asked` → 平台展示 → 用户响应 → HTTP POST /reply |
| E2E Question Flow | Worker 发出 `question_request` → 用户响应 → Worker 收到 `control_response` |
| E2E Timeout Flow | 交互超时 5 分钟 → 自动拒绝 → Worker 收到超时拒绝 |

### 6.3 验证清单

- [ ] Claude Code Worker 包含 `--permission-prompt-tool stdio`
- [ ] Claude Code Worker 默认包含 `--dangerously-skip-permissions`（行为不变）
- [ ] Claude Code Worker `session.PermissionMode=default` 时不包含 `--dangerously-skip-permissions`
- [ ] OCS Worker 默认 `set_permission_mode bypassPermissions`
- [ ] Slack 上看到 bypass-immune 授权请求卡片（修改 `.claude/` 文件时）
- [ ] Slack 点击 Allow/Deny 按钮 → Worker 收到 permission_response
- [ ] Slack 点击选项按钮 → Worker 收到 question_response
- [ ] 飞书看到 bypass-immune 授权卡片，回复"允许"/"拒绝" → Worker 收到 permission_response
- [ ] 飞书回复选项文本 → Worker 收到 question_response
- [ ] 交互超时 5 分钟 → Worker 收到自动拒绝
- [ ] `HOTPLEX_PERMISSION_MODE=default` → 所有工具调用触发授权请求
- [ ] WebChat Permission/Question/Elicitation UI 正常工作
- [ ] `make quality` 通过 (fmt + lint + test)
- [ ] 无破坏性: 正常用户消息输入（无 metadata）行为不变

---

## 7. 参考实现对比

### 7.1 Claude Code（源码参考）

| 维度 | 实现 |
|------|------|
| 权限协议 | `control_request` / `control_response` stdin/stdout |
| 触发机制 | `can_use_tool` 子类型（权限）、`AskUserQuestion`（问题）、`elicitation`（MCP） |
| 响应路径 | Bridge 竞速 4 条: 本地 UI、CCR、Channel relay、Hooks |
| 关联机制 | `BridgePermissionCallbacks.sendResponse(requestId, response)` |
| 模式选择 | 用户启动时选择: default / plan / acceptEdits / bypass |
| stdio 启用 | `--permission-prompt-tool stdio` 或 `--sdk-url`（自动启用 stdio） |
| bypass-immune | Steps 1a-1g 在 step 2a 之前执行，safetyCheck 类型的 `ask` 不受 bypass 影响 |

### 7.2 OpenCode Server（源码参考）

| 维度 | 实现 |
|------|------|
| 权限协议 | SSE bus 事件 + HTTP POST 响应 |
| 触发机制 | `permission.asked` / `question.asked` bus 事件 |
| 响应路径 | `POST /permission/:id/reply`（`once`/`always`/`reject`） |
| 阻塞机制 | Effect Deferred: tool 执行挂起，用户响应后解除 |
| 规则引擎 | `evaluate()` 使用 `findLast` 匹配规则，无匹配时默认 `ask` |
| 规则注入 | `PATCH /session/{id}` 接受 `permission` 字段，合并到 session ruleset |
| bypass 机制 | **无原生 bypass 模式**，HotPlex 通过通配符 `{permission:"*", action:"allow", pattern:"*"}` 实现 |
| bypass-immune | **不存在**，规则引擎对所有操作一视同仁 |
| 事件发布条件 | 仅当 `evaluate()` 返回 `ask`（且非 `deny`/`allow`）时发布 `permission.asked` |
| always 回复 | 用户回复 `always` 时，将对应 pattern 的 allow 规则加入 runtime ruleset，自动审批同 session 后续匹配请求 |

### 7.3 HotPlex 设计意图

HotPlex 使用统一的 AEP metadata 协议（`Input` 事件 + `metadata` 字段），与 Claude Code 的 `control_request/control_response` 和 OpenCode 的 HTTP POST 路径不同，但设计上是合理的 — 允许 Gateway handler 对所有交互类型保持协议无关。只需修复上述 4+1 个断裂点即可使整条链路贯通。

### 7.4 关键源码引用

| 源码位置 | 关键逻辑 |
|---------|---------|
| `claude-code-src/src/cli/print.ts:802-805` | `effectivePermissionPromptToolName` 决定是否启用 stdio 协议 |
| `claude-code-src/src/cli/print.ts:4267-4334` | `getCanUseToolFn()` 路由: `stdio` → `createCanUseTool()`, `undefined` → 内部检查 |
| `claude-code-src/src/cli/structuredIO.ts:533-658` | `createCanUseTool()`: 规则检查 → `ask` 则输出 `control_request` |
| `claude-code-src/src/utils/permissions/permissions.ts:1158+` | 10 步决策链: bypass-immune (1a-1g) → bypass (2a) → always-allow (2b) |
| `claude-code-src/src/utils/permissions/filesystem.ts:57-79` | 受保护路径定义: `.git/`, `.claude/`, `.bashrc` 等 |
| `opencode/packages/opencode/src/permission/index.ts:180-215` | `Permission.ask()`: 规则评估 → ask 则发布 `permission.asked` |
| `opencode/packages/opencode/src/permission/evaluate.ts:9-15` | `evaluate()`: findLast 匹配，无匹配默认 `ask` |
| `opencode/packages/opencode/src/server/routes/instance/session.ts:259-317` | `PATCH /:sessionID`: 接收并合并 permission 规则集 |
| `opencode/packages/opencode/src/server/routes/instance/permission.ts:1-73` | `POST /:requestID/reply`: 权限响应端点 |
