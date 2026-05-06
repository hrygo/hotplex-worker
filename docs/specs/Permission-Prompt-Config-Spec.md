---
type: spec
tags:
  - project/HotPlex
  - worker/claudecode
  - messaging/interaction
  - config
date: 2026-05-06
status: proposed
priority: high
---

# Permission Prompt 可配置化与交互链路修复

> 版本: v2.0
> 日期: 2026-05-06
> 状态: Proposed
> 关联: #160 (交互链路修复 PR) / #197 (交互管道加固) / #200 (配置化)

---

## 1. 问题陈述

### 1.1 核心问题

`--permission-prompt-tool stdio` 在 `buildCLIArgs()` 中被**硬编码**（`worker.go:228`），且 `--dangerously-skip-permissions` 为默认值。这导致：

1. **权限请求过多**：即使有 bypass 模式，bypass-immune 操作（修改 `.claude/`、`.git/`、shell 配置等）仍触发 `control_request`，通过交互链路转发给用户，造成频繁打扰
2. **无法关闭**：没有配置项可以去掉 `--permission-prompt-tool stdio`，用户无法选择"静默拒绝"模式
3. **交互 UI 运行时缺陷**：日志分析确认三个运行时 bug（详见 §1.4）

### 1.2 期望行为

| 模式 | 行为 | 适用场景 |
|------|------|---------|
| **关闭**（默认） | 不传 `--permission-prompt-tool`，`ask` 结果由 Claude Code 静默 auto-deny | 大部分场景，bypass 模式下几乎无感知 |
| **开启** | 传 `--permission-prompt-tool stdio`，`ask` 结果通过 Slack/飞书交互 UI 请求用户决策 | 需要细粒度控制的场景 |

### 1.3 交互链路现状

请求方向（Claude Code → 用户）已打通。响应方向（用户 → Claude Code）存在以下问题：

**Slack**：
- Block Kit 按钮回调通过 Socket Mode 接收，`handleInteractionEvent` → `pi.SendResponse` 链路已打通 (#197)
- 文本 fallback `checkPendingInteraction` 已实现 (#197)，但存在静默丢弃问题（见 §1.4-B3）

**飞书**：
- 卡片为纯展示（飞书 WS 不转发 `card.action.trigger`）
- 用户需输入"允许/拒绝"文本响应
- `checkPendingInteraction` 文本解析链路已验证完整

### 1.4 日志分析发现的运行时 Bug（2026-05-06）

基于 `hotplex.log` 中 session `a3573b29` Turn 3 的完整事件追踪，确认三个 P0 bug：

#### B1：流式 writer 未在交互事件前关闭 — 导致内容"断裂"

**现象**：用户消息 "综合全局，形成本次优化的 spec，输出到 docs" 后，流式输出在 09:54:02 开始（`message.delta`），至 09:55:27 收到 `permission_request` 时流式 writer 仍活跃。权限请求作为新消息发送，但原流式消息悬挂未关闭。

**日志证据**：
```
09:54:02 message.delta → 流式 writer 开始
09:55:09 message.delta → 继续写入
09:55:27 permission_request(ExitPlanMode) → 未关闭流式 writer！
（Turn 3 永远没有收到 done 事件）
```

**根因**：`adapter.go` `WriteCtx` 的 `PermissionRequest`/`QuestionRequest`/`ElicitationRequest` case（lines 735-749）未调用 `closeStreamWriter()`。对比 `Done`/`Error` case（line 724）会显式关闭。

**影响范围**：Slack 和飞书双平台均存在。Slack 流式消息悬挂直到 10min TTL 到期以 `"stream closed with issues"` 关闭；飞书卡片悬挂直到 6min TTL rotation 触发替换。

| 事件类型 | Slack `closeStreamWriter()` | Feishu `streamCtrl.Close()` |
|----------|:---:|:---:|
| `Done` | line 724 | line 653 |
| `Error` | line 724 | line 660 |
| `PermissionRequest` | **缺失** (line 735) | **缺失** (line 678) |
| `QuestionRequest` | **缺失** (line 740) | **缺失** (line 683) |
| `ElicitationRequest` | **缺失** (line 745) | **缺失** (line 688) |

#### B2：Block Kit args 预览嵌套反引号导致 `invalid_blocks` — 降级为纯文本

**现象**：`ExitPlanMode` 的 `permission_request` 发送 Block Kit 失败，降级为纯文本 fallback。用户看到无按钮的纯文本权限请求。

**日志证据**：
```
09:55:29 WARN "slack: sent permission request as plain text fallback" request_id=3a8cc63a-c639-4760-b7c5-8b01a36b96ba
```

**根因**：`sendPermissionRequest` (`interaction.go:180`) 用 `` ```{args}``` `` 包裹 args 预览。`ExitPlanMode` 的 args 包含 Claude 生成的 plan 文本（含 triple backticks），形成嵌套的 ```` ```...```...``` ````，Slack Block Kit mrkdwn 解析器拒绝。

```
实际构建: ```ExitPlanMode plan with ```code block``` inside```
                ↑ 外层开始       ↑ 内层开始  ↑ 内层结束  ↑ 外层不匹配
```

**现有防御层分析**：

| 层 | 作用 | 缺失 |
|----|------|------|
| `SanitizeBlocks` (validator.go:226) | 截断文本到 3000 字符 | 不处理 markdown 内容 |
| `sanitizeSectionBlock` (validator.go:226) | 长度检查 | 不转义反引号 |
| `FormatMrkdwn` (format.go:73) | 保护已有代码块 | 未在权限请求路径调用；正则无法处理嵌套 |
| `isInvalidBlocksError` (validator.go:381) | 检测 Slack 拒绝 | 仅作为 fallback 触发器，不预防拒绝 |

**飞书同样存在**：`feishu/interaction.go:37` 使用 `` ```\n{args}\n``` `` 包裹，飞书 CardKit 渲染器容忍度更高但结构问题一致。

#### B3：文本 fallback "allow \<requestID\>" 静默丢弃 — 交互超时 auto-deny

**现象**：用户在纯文本权限请求后输入 `allow 3a8cc63a-c639-4760-b7c5-8b01a36b96ba`，文本未被 `checkPendingInteraction` 消费，直接发给 worker 作为普通输入。5 分钟后交互超时 auto-deny。

**日志证据**：
```
09:55:29 注册交互 request_id=3a8cc63a-c639-4760-b7c5-8b01a36b96ba
09:56:03 用户发送 "allow 3a8cc63a..." (text_len=45) → StartPlatformSession → 直接发给 worker
10:00:29 interaction: timeout, auto-deny
```

**根因分析** — `checkPendingInteraction` (`interaction.go:481-599`) 的静默丢弃路径：

1. 用户输入 "allow 3a8cc63a..."，`DetectCommand` 返回 `CmdNone`（"allow" 不匹配任何命令）
2. `checkPendingInteraction` 被调用（line 427）
3. `a.Interactions.Get(requestID)` 返回 nil → `matched` 为 nil
4. 进入 fallback 路径（line 505-518）
5. **Line 511**: `if action != "" { return false }` — 因为 `action = "allow"` 非空，**立即返回 false**
6. 文本被当作普通消息发送给 worker

**`Get(requestID)` 返回 nil 的可能原因（按概率排序）**：

| # | 假设 | 可能性 | 依据 |
|---|------|--------|------|
| 1 | 交互已被 `watchTimeout` 超时移除 | 低 | 注册 09:55:29 + 响应 09:56:03 = 34s，未超 5min |
| 2 | 交互注册在错误的 adapter 实例上 | 排除 | `c.adapter` 与 `a` 是同一实例 |
| 3 | `requestID` 不匹配（文本中 UUID 被截断或包含额外字符） | 中 | `text_len=45` 比 "allow " + UUID(36) = 42 多 3 字符 |
| 4 | 交互从未注册（`registerInteraction` 未被调用） | 低 | WARN/INFO 日志确认 PostMessage 成功后注册 |

**关键设计缺陷**：当 `Get(requestID)` 返回 nil 且 `action` 非空时，函数**静默返回 false**，无任何日志。这使得诊断极其困难。

---

## 2. 设计方案

### 2.1 配置层改造（Phase 1 — 已实现 #200）

**新增配置项**（`config.yaml` `worker.claude_code` 下）：

```yaml
worker:
  claude_code:
    command: "claude"
    permission_prompt: false        # 是否启用 --permission-prompt-tool stdio（默认关闭）
```

### 2.2 运行时 Bug 修复（Phase 2 — 新增）

#### F1：交互事件前关闭流式 writer

**修复位置**：

**Slack** — `internal/messaging/slack/adapter.go` WriteCtx：

```go
case events.PermissionRequest:
    c.closeStreamWriter()                    // ← 新增：关闭活跃流
    c.notifyStatus(ctx, "Permission request...")
    err := c.sendPermissionRequest(ctx, env)
    c.clearStatus(ctx)
    return err

case events.QuestionRequest:
    c.closeStreamWriter()                    // ← 新增
    c.notifyStatus(ctx, "Awaiting response...")
    qErr := c.sendQuestionRequest(ctx, env)
    c.clearStatus(ctx)
    return qErr

case events.ElicitationRequest:
    c.closeStreamWriter()                    // ← 新增
    c.notifyStatus(ctx, "Gathering input...")
    eErr := c.sendElicitationRequest(ctx, env)
    c.clearStatus(ctx)
    return eErr
```

**Feishu** — `internal/messaging/feishu/adapter.go` WriteCtx 同样需要在 PermissionRequest/QuestionRequest/ElicitationRequest case 前关闭 `streamCtrl`。

**效果**：
- 流式消息在权限请求前完整 finalize（Slack: `StopStreamContext` / Feishu: card close）
- 用户看到清晰的 "流式输出 → 权限请求" 过渡，而非悬挂的流 + 独立的权限消息

#### F2：args 预览反引号转义

**修复位置**：`internal/messaging/slack/interaction.go` `sendPermissionRequest` (line 174-181) 和 `internal/messaging/feishu/interaction.go` (line 32-38)

**方案 A（推荐）：截断含代码块的 args，避免嵌套**

```go
// Replace triple backticks in args preview to prevent nested code blocks.
func sanitizeArgsPreview(args string, maxLen int) string {
    if len(args) > maxLen {
        args = args[:maxLen] + "..."
    }
    // Strip triple backticks that would nest inside the outer code fence.
    args = strings.ReplaceAll(args, "```", "```")
    // Or simpler: just strip them
    args = strings.ReplaceAll(args, "```", "")
    return args
}
```

**方案 B：改用缩进代码块显示 args**

不在 SectionBlock 中用 fenced code block 包裹 args，改用 `>` 引用块或 plain text 展示，彻底避免嵌套风险。

**方案 C：在 `SanitizeBlocks` 层面防御**

在 `sanitizeSectionBlock` 中增加 mrkdwn 内容清洗，检测并转义嵌套的反引号。这能保护所有 SectionBlock，不仅限于权限请求。

**推荐方案 A**：最小改动，仅在 args 预览构建处修复，不影响其他 block 内容。

#### F3：`checkPendingInteraction` 静默丢弃修复

**修复位置**：`internal/messaging/slack/interaction.go` `checkPendingInteraction` (line 481-599)

**修复 1：关键路径增加 DEBUG 日志**

```go
// Line ~499: Get 返回 nil 时记录
if requestID != "" {
    if pi, ok := a.Interactions.Get(requestID); ok {
        matched = pi
    } else {
        a.adapter.Log.Debug("slack: interaction text has action+requestID but no matching pending interaction",
            "action", action, "request_id", requestID, "pending_count", a.Interactions.Len())
    }
}

// Line ~511: fallback 被 action 关键字阻断时记录
if action != "" {
    a.adapter.Log.Debug("slack: interaction text has action keyword but requestID not found, dropping",
        "action", action, "request_id", requestID)
    return false
}
```

**修复 2：text_len 不匹配问题排查**

`text_len=45` vs 预期 42 的 3 字节差异。可能来源：
- Slack 消息中的 invisible Unicode（zero-width joiner、variation selector）
- 用户输入了额外字符（如 "allow \<id\> ok"）
- `ResolveMentions` 未完全清理的 mention 残留

建议在 `checkPendingInteraction` 入口处记录 `normalized` 文本的实际内容和 `words` 切片，便于生产环境诊断。

**修复 3（可选）：宽松匹配策略**

当 `Get(exactRequestID)` 失败但 `action` 是有效的交互操作词时，尝试前缀匹配或遍历所有 pending interactions 查找包含该 ID 前缀的条目：

```go
if matched == nil && requestID != "" && (action == "allow" || action == "deny" || action == "accept" || action == "decline") {
    // Try prefix match (UUID may be truncated in some clients)
    candidates := a.Interactions.GetAll()
    for _, c := range candidates {
        if strings.HasPrefix(c.ID, requestID) {
            matched = c
            break
        }
    }
}
```

### 2.3 配置传递路径

**方案 A（推荐）：config.yaml 全局配置**

```
config.yaml → Viper → WorkerConfig.ClaudeCode.PermissionPrompt → buildCLIArgs()
```

- 全局生效，不需要 per-session 配置
- 简单直接，符合"约定大于配置"原则
- 重启生效（已有热重载机制可扩展）

**方案 B：per-session 动态配置**

通过 `SessionInfo` 或 slash 命令动态切换：
- 更灵活但复杂度高
- 留作 Phase 2

### 2.4 补充方案：PermissionRequest Hooks

在 `.claude/settings.json` 中配置 hooks，自动处理常见权限请求，减少需要转发给用户的数量：

```json
{
  "hooks": {
    "PermissionRequest": [
      {
        "matcher": "Bash(git *)",
        "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"allow\"}'" }]
      },
      {
        "matcher": "Bash(npm *)",
        "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"allow\"}'" }]
      },
      {
        "matcher": "Read",
        "hooks": [{ "type": "command", "command": "echo '{\"decision\": \"allow\"}'" }]
      }
    ]
  }
}
```

**机制**：Claude Code 在发送 `control_request` 前，先运行 PermissionRequest hooks。Hook 返回 `allow/deny` 时，不触发外部交互链路。仅当所有 hooks 均未决策时，才发送 `control_request` 给 HotPlex。

**适用场景**：
- 读文件、git 操作等低风险命令自动放行
- Bash 写操作、网络请求等高风险命令仍需用户确认
- 与 `--permission-prompt-tool stdio` 配合使用，减少无意义的打扰

**配置位置**：项目级 `.claude/settings.json`（Git 管理）或用户级 `~/.claude/settings.json`

---

## 3. 实施计划

### Phase 1：配置可配置化（P0）— 已完成 #200

**目标**：`--permission-prompt-tool stdio` 变为可选配置，默认关闭

**文件变更**：

| 文件 | 变更 |
|------|------|
| `internal/config/config.go` | `ClaudeCodeConfig` 新增 `PermissionPrompt bool` 字段 + 默认值 `false` |
| `configs/config.yaml` | `worker.claude_code` 下新增 `permission_prompt: false` |
| `internal/worker/claudecode/worker.go` | `buildCLIArgs()` 条件追加 `--permission-prompt-tool stdio` |
| `internal/worker/claudecode/worker_test.go` | 更新测试：验证有/无 flag 的参数列表 |

### Phase 2：运行时 Bug 修复（P0）

**目标**：修复 §1.4 确认的三个运行时 bug

**F1：交互事件前关闭流式 writer**

| 文件 | 操作 | 行数 |
|------|------|------|
| `internal/messaging/slack/adapter.go` | WriteCtx: 3 个 case 前加 `closeStreamWriter()` | +3 |
| `internal/messaging/feishu/adapter.go` | WriteCtx: 3 个 case 前关闭 `streamCtrl` | +9 |

**F2：args 预览反引号转义**

| 文件 | 操作 | 行数 |
|------|------|------|
| `internal/messaging/slack/interaction.go` | 新增 `sanitizeArgsPreview` + 替换 line 180 | +10 |
| `internal/messaging/feishu/interaction.go` | 替换 line 37 的 args 包裹逻辑 | +5 |
| `internal/messaging/slack/interaction_test.go` | 新增嵌套反引号测试 | +20 |
| `internal/messaging/feishu/interaction_test.go` | 新增嵌套反引号测试 | +15 |

**F3：checkPendingInteraction 静默丢弃修复**

| 文件 | 操作 | 行数 |
|------|------|------|
| `internal/messaging/slack/interaction.go` | 关键 return false 路径增加 DEBUG 日志 | +10 |
| `internal/messaging/slack/interaction_test.go` | 新增 text fallback 匹配失败场景测试 | +30 |

### Phase 3：交互链路端到端验证（P0）

**验证清单**：

```
Claude Code stdout → parser → EventControl(can_use_tool) → PermissionRequest AEP
  → Hub broadcast → PlatformConn.WriteCtx → closeStreamWriter() [F1]
  → sendPermissionRequest → sanitizeArgsPreview() [F2]
  → Slack: Block Kit buttons / Feishu: Card + text instruction
  → 用户响应 → checkPendingInteraction / handleInteractionEvent [F3 日志]
  → pi.SendResponse → Bridge.Handle → handler.handleInput → worker.Input
  → control.SendPermissionResponse → stdin write
  → Claude Code 继续执行 → Done → closeStreamWriter
```

**需要补充的测试**：

| 测试 | 覆盖 |
|------|------|
| `TestWriteCtx_PermissionRequest_ClosesStream` | 验证流式 writer 在 permission_request 前被关闭 |
| `TestSanitizeArgsPreview_NestedBackticks` | 验证含 ``` 的 args 不破坏 block 格式 |
| `TestSanitizeArgsPreview_Truncation` | 验证长 args 被截断 |
| `TestCheckPendingInteraction_DebugLogging` | 验证匹配失败时产生 DEBUG 日志 |
| `TestCheckPendingInteraction_PrefixMatch` | 验证前缀匹配 fallback（如采用方案 3） |
| Slack 按钮回调端到端测试 | Socket Mode callback → SendResponse → stdin |
| 飞书文本响应端到端测试 | "允许"/"拒绝" → SendResponse → stdin |

### Phase 4：PermissionRequest Hooks 文档与模板（P1）

**目标**：提供常用 hooks 模板，用户可按需启用

**交付物**：
- `docs/permission-hooks-guide.md`：hooks 配置指南
- `configs/permission-hooks-examples.json`：常用自动放行模板
- 更新 Onboard Wizard 中的 hooks 配置引导

---

## 4. 修改文件清单

| 文件 | Phase | 操作 | 行数 |
|------|-------|------|------|
| `internal/config/config.go` | 1 | 修改：新增字段 + 默认值 | +3 |
| `configs/config.yaml` | 1 | 修改：新增配置项 | +1 |
| `internal/worker/claudecode/worker.go` | 1 | 修改：条件追加 flag | +4 |
| `internal/worker/claudecode/worker_test.go` | 1 | 修改：更新测试 | +20 |
| `internal/messaging/slack/adapter.go` | 2 | 修改：3 处 closeStreamWriter | +3 |
| `internal/messaging/feishu/adapter.go` | 2 | 修改：3 处 streamCtrl close | +9 |
| `internal/messaging/slack/interaction.go` | 2 | 修改：sanitizeArgsPreview + 日志 | +20 |
| `internal/messaging/feishu/interaction.go` | 2 | 修改：sanitizeArgsPreview | +5 |
| `internal/messaging/slack/interaction_test.go` | 2 | 新增：嵌套反引号 + 匹配失败测试 | +50 |
| `internal/messaging/feishu/interaction_test.go` | 2 | 新增：嵌套反引号测试 | +15 |

---

## 5. 风险评估

| 风险 | 影响 | 缓解 |
|------|------|------|
| 默认关闭导致安全操作静默拒绝 | 低：bypass 模式下这些操作本就会被拒绝 | 文档说明 + hooks 补充 |
| 配置热重载不生效 | 低：worker 进程已启动，需新 session 生效 | 文档说明需新 session |
| `closeStreamWriter` 改变消息时序 | 低：流式消息 finalize 是幂等操作 | 测试覆盖 finalize 后再发送交互 UI |
| args 预览 strip 反引号丢失格式信息 | 低：权限请求的核心信息是 tool name + description | 保留原始文本但避免嵌套 |
| 前缀匹配导致误匹配 | 低：UUID 碰撞概率极低 | 仅在精确匹配失败后作为 fallback |

---

## 6. 验收标准

### Phase 1（已完成）

- [x] `permission_prompt: false`（默认）时，Claude Code 启动参数不含 `--permission-prompt-tool`
- [x] `permission_prompt: true` 时，启动参数包含 `--permission-prompt-tool stdio`
- [x] 现有测试全部通过
- [x] `make check` CI 通过

### Phase 2

- [ ] B1: `PermissionRequest` 到达时，活跃流式 writer 被 finalize（Slack 日志无 "stream closed with issues"）
- [ ] B1: 飞书同样验证流式卡片在交互事件前正确关闭
- [ ] B2: `ExitPlanMode`（含 plan markdown）的 `permission_request` 以 Block Kit 按钮正常展示，不触发 `invalid_blocks`
- [ ] B2: 飞书卡片含嵌套反引号的 args 正常展示
- [ ] B3: `checkPendingInteraction` 匹配失败时产生 DEBUG 日志，包含 action、requestID、pending count
- [ ] `make check` CI 通过

### Phase 3

- [ ] Slack：权限请求卡片出现，按钮点击后 Claude Code 继续执行
- [ ] 飞书：权限请求卡片出现，文本"允许/拒绝"响应后 Claude Code 继续执行
- [ ] 交互超时（5min）后自动拒绝，Claude Code 收到 deny 响应
- [ ] `make check` CI 通过

### Phase 4

- [ ] PermissionRequest hooks 文档完成
- [ ] 常用 hooks 模板可导入
