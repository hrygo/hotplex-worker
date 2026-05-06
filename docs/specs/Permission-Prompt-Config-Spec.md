---
type: spec
tags:
  - project/HotPlex
  - worker/claudecode
  - messaging/interaction
  - config
date: 2026-05-06
status: phase-2-done
priority: high
---

# Permission Prompt 可配置化与交互链路修复

> 版本: v2.1
> 日期: 2026-05-06
> 状态: Phase 1 & 2 已完成，Phase 3 测试补充进行中
> 关联: #160 (交互链路修复 PR) / #197 (交互管道加固) / #200 (配置化)

---

## 1. 问题陈述

### 1.1 核心问题

`--permission-prompt-tool stdio` 在 `buildCLIArgs()` 中被**硬编码**（`worker.go:228`），且 `--dangerously-skip-permissions` 为默认值。这导致：

1. **权限请求过多**：即使有 bypass 模式，bypass-immune 操作（修改 `.claude/`、`.git/`、shell 配置等）仍触发 `control_request`，通过交互链路转发给用户，造成频繁打扰
2. **无法关闭**：没有配置项可以去掉 `--permission-prompt-tool stdio`，用户无法选择"静默拒绝"模式
3. **交互 UI 运行时缺陷**：日志分析确认三个运行时 bug（详见 §1.4），**已全部修复**

### 1.2 期望行为

| 模式 | 行为 | 适用场景 |
|------|------|---------|
| **关闭**（默认） | 不传 `--permission-prompt-tool`，`ask` 结果由 Claude Code 静默 auto-deny | 大部分场景，bypass 模式下几乎无感知 |
| **开启** | 传 `--permission-prompt-tool stdio`，`ask` 结果通过 Slack/飞书交互 UI 请求用户决策 | 需要细粒度控制的场景 |

### 1.3 交互链路现状

请求方向（Claude Code → 用户）已打通。响应方向（用户 → Claude Code）存在以下问题：

**Slack**：
- Block Kit 按钮回调通过 Socket Mode 接收，`handleInteractionEvent` → `pi.SendResponse` 链路已打通 (#197)
- 文本 fallback `checkPendingInteraction` 已实现 (#197)，含 DEBUG 日志和候选类型匹配（已修复）

**飞书**：
- 卡片为纯展示（飞书 WS 不转发 `card.action.trigger`）
- 用户需输入"允许/拒绝"文本响应
- `checkPendingInteraction` 文本解析链路已验证完整

### 1.4 日志分析发现的运行时 Bug（2026-05-06）— 已修复

基于 `hotplex.log` 中 session `a3573b29` Turn 3 的完整事件追踪，确认三个 P0 bug。**三者在 commit `018c794` 中已全部修复。**

#### B1：流式 writer 未在交互事件前关闭 — ✅ 已修复

**现象**：用户消息后流式输出进行中收到 `permission_request`，流式 writer 未关闭导致消息悬挂。

**修复**：`adapter.go` WriteCtx 的 `PermissionRequest`/`QuestionRequest`/`ElicitationRequest` case 前增加流式关闭调用。

| 事件类型 | Slack `closeStreamWriter()` | Feishu `clearActiveIndicators()` + `streamCtrl.Close()` |
|----------|:---:|:---:|
| `Done` | line 724 | line 638-653 |
| `Error` | line 724 | line 657-660 |
| `PermissionRequest` | line 736 | line 679-681 |
| `QuestionRequest` | line 742 | line 688-690 |
| `ElicitationRequest` | line 748 | line 697-699 |

#### B2：Block Kit args 预览嵌套反引号导致 `invalid_blocks` — ✅ 已修复

**现象**：`ExitPlanMode` 的 `permission_request` 因 args 含 triple backticks 导致嵌套，Block Kit 解析失败降级为纯文本。

**修复**：发送前 strip 掉 args 中的 triple backticks。

- Slack `interaction.go:183`：`strings.ReplaceAll(preview, "```", "")`
- 飞书 `interaction.go:38`：`strings.ReplaceAll(preview, "```", "")`

#### B3：`checkPendingInteraction` 静默丢弃 — ✅ 已修复

**现象**：`allow <requestID>` 文本回复未被 `checkPendingInteraction` 消费，直接发给 worker 作为普通输入，导致交互超时 auto-deny。

**修复**（`interaction.go:485-616`）：

1. **DEBUG 日志**：`Get(requestID)` 返回 nil 时记录 `request_id`、`action`、`pending_count`（line 506-509）
2. **候选类型匹配**：精确匹配失败后，fallback 到最近 pending interaction 的类型匹配（line 521-528）
   - `allow/deny` → 匹配 `PermissionRequest`
   - `accept/decline` → 匹配 `ElicitationRequest`
   - 原始文本 → 匹配 `QuestionRequest`

> **注**：Spec 原提议的 UUID 前缀匹配方案未采用，实际实现为按 interaction type 的候选匹配，覆盖更广。

---

## 2. 设计方案

### 2.1 配置层改造（Phase 1 — 已完成 #200）

**新增配置项**（`config.yaml` `worker.claude_code` 下）：

```yaml
worker:
  claude_code:
    command: "claude"
    permission_prompt: false        # 是否启用 --permission-prompt-tool stdio（默认关闭）
```

### 2.2 运行时 Bug 修复（Phase 2 — 已完成 018c794）

#### F1：交互事件前关闭流式 writer — 已实现

**Slack** — `internal/messaging/slack/adapter.go` WriteCtx：

```go
case events.PermissionRequest:
    c.closeStreamWriter() // finalize any active stream before interaction
    c.notifyStatus(ctx, "Permission request...")
    err := c.sendPermissionRequest(ctx, env)
    c.clearStatus(ctx)
    return err

case events.QuestionRequest:
    c.closeStreamWriter()
    // ...

case events.ElicitationRequest:
    c.closeStreamWriter()
    // ...
```

**Feishu** — `internal/messaging/feishu/adapter.go` WriteCtx：

```go
case events.PermissionRequest:
    streamCtrl := c.clearActiveIndicators(ctx)
    if streamCtrl != nil && streamCtrl.IsCreated() {
        _ = streamCtrl.Close(ctx)
    }
    // ...

case events.QuestionRequest:
    streamCtrl := c.clearActiveIndicators(ctx)
    if streamCtrl != nil && streamCtrl.IsCreated() {
        _ = streamCtrl.Close(ctx)
    }
    // ...

case events.ElicitationRequest:
    streamCtrl := c.clearActiveIndicators(ctx)
    if streamCtrl != nil && streamCtrl.IsCreated() {
        _ = streamCtrl.Close(ctx)
    }
    // ...
```

#### F2：args 预览反引号清理 — 已实现

**实际实现方式**：inline strip（非独立函数），在 `sendPermissionRequest` 中直接处理：

```go
// Strip triple backticks to prevent nested code blocks in Block Kit.
preview = strings.ReplaceAll(preview, "```", "")
```

Slack `interaction.go:183` 和飞书 `interaction.go:38`。

#### F3：`checkPendingInteraction` 诊断增强 — 已实现

**实际实现**（`interaction.go:502-536`）：

1. `Get(requestID)` 失败时 DEBUG 日志（line 506-509）
2. Fallback 候选匹配：按 `action` 关键字与 pending interaction type 对应（line 521-528）

### 2.3 配置传递路径

```
config.yaml → Viper → WorkerConfig.ClaudeCode.PermissionPrompt → buildCLIArgs()
```

### 2.4 补充方案：PermissionRequest Hooks（Phase 4 — 依赖上游）

在 `.claude/settings.json` 中配置 hooks，自动处理常见权限请求，减少需要转发给用户的数量。

**前置条件**：Claude Code 原生 PermissionRequest hooks 支持（需确认版本可用性）。

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

### Phase 2：运行时 Bug 修复（P0）— 已完成 018c794

**目标**：修复 §1.4 确认的三个运行时 bug

**F1：交互事件前关闭流式 writer**

| 文件 | 操作 |
|------|------|
| `internal/messaging/slack/adapter.go` | WriteCtx: 3 个 case 前加 `closeStreamWriter()` |
| `internal/messaging/feishu/adapter.go` | WriteCtx: 3 个 case 前关闭 `streamCtrl` |

**F2：args 预览反引号清理**

| 文件 | 操作 |
|------|------|
| `internal/messaging/slack/interaction.go` | inline `strings.ReplaceAll(preview, "```", "")` |
| `internal/messaging/feishu/interaction.go` | inline `strings.ReplaceAll(preview, "```", "")` |

**F3：checkPendingInteraction 诊断增强**

| 文件 | 操作 |
|------|------|
| `internal/messaging/slack/interaction.go` | DEBUG 日志 + 候选类型匹配 fallback |
| `internal/messaging/slack/interaction.go` | handleInteractionEvent 日志增强 |

**额外修复**（随 Phase 2 一起提交）：

| 文件 | 操作 |
|------|------|
| `internal/gateway/handler.go` | Gateway handler interaction response 日志增强 |
| `internal/messaging/feishu/adapter.go` | 飞书 permission card 显示 request ID |
| `internal/messaging/feishu/interaction.go` | 飞书 keyword matching 扩展 |

### Phase 3：交互链路端到端验证（P0）— 进行中

**验证清单**：

```
Claude Code stdout → parser → EventControl(can_use_tool) → PermissionRequest AEP
  → Hub broadcast → PlatformConn.WriteCtx → closeStreamWriter() [F1 ✓]
  → sendPermissionRequest → strip backticks [F2 ✓]
  → Slack: Block Kit buttons / Feishu: Card + text instruction
  → 用户响应 → checkPendingInteraction [F3 ✓] / handleInteractionEvent
  → pi.SendResponse → Bridge.Handle → handler.handleInput → worker.Input
  → control.SendPermissionResponse → stdin write
  → Claude Code 继续执行 → Done → closeStreamWriter
```

**需要补充的测试**：

| 测试 | 覆盖 | 优先级 | 状态 |
|------|------|--------|------|
| `TestWriteCtx_PermissionRequest_ClosesStream` | 验证流式 writer 在 permission_request 前被关闭 | P0 | 待补充 |
| `TestSanitizeArgsPreview_NestedBackticks` | 验证含 ``` 的 args 不破坏 block 格式 | P1 | 待补充 |
| `TestSanitizeArgsPreview_Truncation` | 验证长 args 被截断 | P2 | 待补充 |
| `TestCheckPendingInteraction_*`（Slack） | Slack 文本 fallback 匹配各路径 | P0 | **零测试，关键缺口** |
| Slack 按钮回调端到端测试 | Socket Mode callback → SendResponse → stdin | P1 | 仅手动 E2E |
| 飞书文本响应端到端测试 | "允许"/"拒绝" → SendResponse → stdin | P1 | adapter 级部分覆盖 |

### Phase 4：PermissionRequest Hooks 文档与模板（P1）

**目标**：提供常用 hooks 模板，用户可按需启用

**交付物**：
- `docs/permission-hooks-guide.md`：hooks 配置指南
- `configs/permission-hooks-examples.json`：常用自动放行模板
- 更新 Onboard Wizard 中的 hooks 配置引导

---

## 4. 修改文件清单

| 文件 | Phase | 操作 | Commits |
|------|-------|------|---------|
| `internal/config/config.go` | 1 | 新增字段 + 默认值 | `c82d16a` |
| `configs/config.yaml` | 1 | 新增配置项 | `c82d16a` |
| `internal/worker/claudecode/worker.go` | 1 | 条件追加 flag | `c82d16a` |
| `internal/worker/claudecode/worker_test.go` | 1 | 更新测试 | `c82d16a` |
| `internal/messaging/slack/adapter.go` | 2 | 3 处 closeStreamWriter | `018c794` |
| `internal/messaging/feishu/adapter.go` | 2 | 3 处 streamCtrl close + request ID | `018c794` |
| `internal/messaging/slack/interaction.go` | 2 | backtick strip + DEBUG 日志 + 候选匹配 | `018c794` |
| `internal/messaging/feishu/interaction.go` | 2 | backtick strip + keyword 扩展 | `018c794` |
| `internal/gateway/handler.go` | 2 | interaction response 日志增强 | `018c794` |

---

## 5. 风险评估

| 风险 | 影响 | 缓解 |
|------|------|------|
| 默认关闭导致安全操作静默拒绝 | 低：bypass 模式下这些操作本就会被拒绝 | 文档说明 + hooks 补充 |
| 配置热重载不生效 | 低：worker 进程已启动，需新 session 生效 | 文档说明需新 session |
| ~~`closeStreamWriter` 改变消息时序~~ | ~~低~~ | **已验证幂等，无风险** |
| args 预览 strip 反引号丢失格式信息 | 低：权限请求的核心信息是 tool name + description | 保留原始文本但避免嵌套 |

---

## 6. 验收标准

### Phase 1（已完成）

- [x] `permission_prompt: false`（默认）时，Claude Code 启动参数不含 `--permission-prompt-tool`
- [x] `permission_prompt: true` 时，启动参数包含 `--permission-prompt-tool stdio`
- [x] 现有测试全部通过
- [x] `make check` CI 通过

### Phase 2（已完成）

- [x] B1: `PermissionRequest` 到达时，活跃流式 writer 被 finalize（Slack `adapter.go:736`）
- [x] B1: 飞书同样验证流式卡片在交互事件前正确关闭（`adapter.go:679-681`）
- [x] B2: 含嵌套反引号的 args 在 Block Kit 渲染前被 strip（Slack `interaction.go:183`、飞书 `interaction.go:38`）
- [x] B3: `checkPendingInteraction` 匹配失败时产生 DEBUG 日志（`interaction.go:506-509`）
- [x] B3: 精确匹配失败后 fallback 到候选类型匹配（`interaction.go:521-528`）
- [x] `make check` CI 通过

### Phase 3（进行中）

- [ ] `TestWriteCtx_PermissionRequest_ClosesStream`（Slack + Feishu）
- [ ] `TestCheckPendingInteraction_*` 系列（Slack，当前零覆盖）
- [ ] `TestSanitizeArgsPreview_NestedBackticks`（双平台）
- [ ] Slack 端到端交互链路自动化测试
- [ ] 飞书端到端交互链路自动化测试

### Phase 4

- [ ] PermissionRequest hooks 文档完成
- [ ] 常用 hooks 模板可导入
