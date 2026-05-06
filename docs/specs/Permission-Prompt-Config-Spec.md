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

> 版本: v1.0
> 日期: 2026-05-06
> 状态: Proposed
> 关联: #160 (交互链路修复 PR) / #197 (交互管道加固)

---

## 1. 问题陈述

### 1.1 核心问题

`--permission-prompt-tool stdio` 在 `buildCLIArgs()` 中被**硬编码**（`worker.go:228`），且 `--dangerously-skip-permissions` 为默认值。这导致：

1. **权限请求过多**：即使有 bypass 模式，bypass-immune 操作（修改 `.claude/`、`.git/`、shell 配置等）仍触发 `control_request`，通过交互链路转发给用户，造成频繁打扰
2. **无法关闭**：没有配置项可以去掉 `--permission-prompt-tool stdio`，用户无法选择"静默拒绝"模式
3. **交互 UI 不生效**：Slack Block Kit 按钮和飞书卡片响应回传链路存在断裂

### 1.2 期望行为

| 模式 | 行为 | 适用场景 |
|------|------|---------|
| **关闭**（默认） | 不传 `--permission-prompt-tool`，`ask` 结果由 Claude Code 静默 auto-deny | 大部分场景，bypass 模式下几乎无感知 |
| **开启** | 传 `--permission-prompt-tool stdio`，`ask` 结果通过 Slack/飞书交互 UI 请求用户决策 | 需要细粒度控制的场景 |

### 1.3 交互链路现状

请求方向（Claude Code → 用户）已打通。响应方向（用户 → Claude Code）存在以下问题：

**Slack**：
- Block Kit 按钮回调通过 Socket Mode 接收
- 但 `hp_interact/allow/<id>` 和 `hp_interact/deny/<id>` 按钮点击后，`handleInteractionEvent` 中的 `pi.SendResponse` 回调可能未正确触发 ControlHandler 写回 stdin

**飞书**：
- 卡片为纯展示（飞书 WS 不转发 `card.action.trigger`）
- 用户需输入"允许/拒绝"文本响应
- `checkPendingInteraction` 文本解析链路需要验证完整性

---

## 2. 设计方案

### 2.1 配置层改造

**新增配置项**（`config.yaml` `worker.claude_code` 下）：

```yaml
worker:
  claude_code:
    command: "claude"
    permission_prompt: false        # 新增：是否启用 --permission-prompt-tool stdio（默认关闭）
```

**代码变更**：

1. `ClaudeCodeConfig` 新增 `PermissionPrompt bool` 字段
2. `buildCLIArgs()` 根据配置决定是否追加 `--permission-prompt-tool stdio`
3. 配置通过 `workerEnv` 或 `SessionInfo` 传递（待定，见 2.3）

### 2.2 交互链路修复

**修复范围**：

| # | 问题 | 修复 | 优先级 |
|---|------|------|--------|
| F1 | Slack 按钮回调 → `SendResponse` → stdin 写回链路验证 | 补充端到端测试 + 日志追踪 | P0 |
| F2 | 飞书文本响应解析 → `checkPendingInteraction` 链路验证 | 补充端到端测试 + 日志追踪 | P0 |
| F3 | 交互超时自动拒绝后的 worker 状态同步 | 验证 `CancelAll` + worker done 流程 | P1 |

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

### Phase 1：配置可配置化（P0）

**目标**：`--permission-prompt-tool stdio` 变为可选配置，默认关闭

**文件变更**：

| 文件 | 变更 |
|------|------|
| `internal/config/config.go` | `ClaudeCodeConfig` 新增 `PermissionPrompt bool` 字段 + 默认值 `false` |
| `configs/config.yaml` | `worker.claude_code` 下新增 `permission_prompt: false` |
| `internal/worker/claudecode/worker.go` | `buildCLIArgs()` 条件追加 `--permission-prompt-tool stdio` |
| `internal/worker/claudecode/worker_test.go` | 更新测试：验证有/无 flag 的参数列表 |

**具体实现**：

```go
// config.go
type ClaudeCodeConfig struct {
    Command           string `mapstructure:"command"`
    PermissionPrompt  bool   `mapstructure:"permission_prompt"`
}

// worker.go buildCLIArgs()
func (w *Worker) buildCLIArgs(session worker.SessionInfo, resume bool) []string {
    args := []string{
        "--print",
        "--verbose",
        "--output-format", "stream-json",
        "--input-format", "stream-json",
    }

    // Conditionally enable permission prompt tool
    if w.cfg.ClaudeCode.PermissionPrompt {
        args = append(args, "--permission-prompt-tool", "stdio")
    }

    // ... rest unchanged
}
```

**向后兼容**：
- 默认 `false`，现有部署行为改变：不再出现权限询问（回归到之前"不问"的状态）
- 需要显式开启 `permission_prompt: true` 才启用交互链路

### Phase 2：交互链路端到端修复（P0）

**目标**：当 `permission_prompt: true` 时，Slack 和飞书的交互 UI 完整可用

**验证清单**：

```
Claude Code stdout → parser → EventControl(can_use_tool) → PermissionRequest AEP
  → Hub broadcast → PlatformConn.WriteCtx → sendPermissionRequest
  → Slack: Block Kit buttons / Feishu: Card + text instruction
  → 用户响应 → checkPendingInteraction / handleInteractionEvent
  → pi.SendResponse → Bridge.Handle → handler.handleInput → worker.Input
  → control.SendPermissionResponse → stdin write
  → Claude Code 继续执行
```

**需要补充的测试**：

| 测试 | 覆盖 |
|------|------|
| `TestBuildCLIArgs_PermissionPromptEnabled` | 验证 flag 存在 |
| `TestBuildCLIArgs_PermissionPromptDisabled` | 验证 flag 不存在 |
| Slack 按钮回调端到端测试 | Socket Mode callback → SendResponse → stdin |
| 飞书文本响应端到端测试 | "允许"/"拒绝" → SendResponse → stdin |
| 交互超时自动拒绝 | InteractionManager timeout → auto-deny → stdin |

### Phase 3：PermissionRequest Hooks 文档与模板（P1）

**目标**：提供常用 hooks 模板，用户可按需启用

**交付物**：
- `docs/permission-hooks-guide.md`：hooks 配置指南
- `configs/permission-hooks-examples.json`：常用自动放行模板
- 更新 Onboard Wizard 中的 hooks 配置引导

---

## 4. 修改文件清单

### Phase 1

| 文件 | 操作 | 行数估计 |
|------|------|---------|
| `internal/config/config.go` | 修改：新增字段 + 默认值 | +3 |
| `configs/config.yaml` | 修改：新增配置项 | +1 |
| `internal/worker/claudecode/worker.go` | 修改：条件追加 flag | +4 |
| `internal/worker/claudecode/worker_test.go` | 修改：更新测试 | +20 |

### Phase 2

| 文件 | 操作 | 行数估计 |
|------|------|---------|
| `internal/messaging/slack/interaction.go` | 修改：增强日志 + 修复 | TBD |
| `internal/messaging/feishu/interaction.go` | 修改：增强日志 + 修复 | TBD |
| `internal/messaging/interaction_test.go` | 新增：端到端测试 | TBD |

---

## 5. 风险评估

| 风险 | 影响 | 缓解 |
|------|------|------|
| 默认关闭导致安全操作静默拒绝 | 低：bypass 模式下这些操作本就会被拒绝 | 文档说明 + hooks 补充 |
| 配置热重载不生效 | 低：worker 进程已启动，需新 session 生效 | 文档说明需新 session |
| 交互链路修复引入新 bug | 中 | 充分的端到端测试覆盖 |

---

## 6. 验收标准

### Phase 1

- [ ] `permission_prompt: false`（默认）时，Claude Code 启动参数不含 `--permission-prompt-tool`
- [ ] `permission_prompt: true` 时，启动参数包含 `--permission-prompt-tool stdio`
- [ ] 现有测试全部通过
- [ ] `make check` CI 通过

### Phase 2

- [ ] Slack：权限请求卡片出现，按钮点击后 Claude Code 继续执行
- [ ] 飞书：权限请求卡片出现，文本"允许/拒绝"响应后 Claude Code 继续执行
- [ ] 交互超时（5min）后自动拒绝，Claude Code 收到 deny 响应
- [ ] `make check` CI 通过

### Phase 3

- [ ] PermissionRequest hooks 文档完成
- [ ] 常用 hooks 模板可导入
