# Worker GAP Analysis — OCS vs CC Worker

> **日期**: 2026-05-15 · **版本**: v1.13.2 · **范围**: `internal/worker/claudecode/` + `internal/worker/opencodeserver/`

## 概述

基于对 `hotplex/`、`~/opencode/`、`~/claude-code-src/` 三个源码库的深入对比分析，识别 HotPlex 的 OpenCode Server (OCS) Worker 与 Claude Code (CC) Worker 在能力覆盖上的差距。

### 分析方法

| 阶段 | 内容 | 数据量 |
|------|------|--------|
| 第一轮 | 全面对比三个代码库的架构与功能 | 17 个 parallel explore agents |
| 第二轮 | 精确定位两个 Worker 的逐项差距 | 接口实现、SessionInfo 字段、事件输出、控制协议 |
| 第三轮 | 源码验证每个 GAP 的底层工具支持情况 | 核实 `~/claude-code-src/main.tsx`、`~/opencode/packages/opencode/src/` |

### 关键源码参考

| 文件 | 用途 |
|------|------|
| `internal/worker/claudecode/worker.go` | CC Worker：CLI 参数构建 (`buildCLIArgs`)、输出读取 (`readOutput`)、事件映射 |
| `internal/worker/claudecode/control.go` | CC Worker：stdin NDJSON 控制协议、`SendControlRequest` |
| `internal/worker/opencodeserver/worker.go` | OCS Worker：HTTP API 调用、EventBus 订阅、`forwardBusEvents` |
| `internal/worker/opencodeserver/commands.go` | OCS Worker：`ServerCommander`（Compact/Clear/Rewind + ControlRequest） |
| `internal/worker/opencodeserver/singleton.go` | OCS Worker：SingletonProcessManager（懒启动、引用计数、空闲回收） |
| `internal/worker/worker.go` | 核心接口定义：`SessionInfo`（24 字段）、`Worker`、可选接口 |
| `~/claude-code-src/src/main.tsx` | Claude Code CLI 标志定义（`main.tsx:976-1006`，21 个标志全部验证有效） |
| `~/claude-code-src/src/entrypoints/sdk/controlSchemas.ts` | Claude Code 的 control_request subtype 全集 |
| `~/claude-code-src/src/utils/sessionRestore.ts` | Claude Code 的 Continue/Fork/Resume 实现 |
| `~/opencode/packages/opencode/src/session/session.ts` | OpenCode Session 创建 API |
| `~/opencode/packages/opencode/src/session/prompt.ts` | OpenCode PromptInput schema |
| `~/opencode/packages/opencode/src/session/llm.ts` | OpenCode 系统提示组装逻辑 |
| `~/opencode/packages/opencode/src/session/processor.ts` | OpenCode LLM 事件处理 → Bus/SSE |

---

## 一、SessionInfo 字段覆盖差距

`SessionInfo` 共定义 **24 个字段**。对比两个 Worker 的使用情况：

| 字段 | CC Worker | OCS Worker | 工具支持 | 状态 |
|------|:---------:|:----------:|:--------:|:----:|
| `SessionID` | ✅ | ✅ | — | OK |
| `UserID` | ✅ | ✅ | — | OK |
| `ProjectDir` | ✅ | ✅ | — | OK |
| `SystemPrompt` | ✅ `--append-system-prompt-file` | ✅ `conn.systemPrompt` | ✅ | OK |
| `SystemPromptReplace` | ✅ `--system-prompt-file` | ❌ 未使用 | ❌ OpenCode 不可完全替换（`llm.ts:103`） | 🔴 工具受限 |
| `AllowedTools` | ✅ `--allowed-tools` | ❌ 未使用 | ❌ OpenCode 仅代理级权限 | 🔴 工具受限 |
| `AllowedModels` | ✅ `--model` | ❌ 未使用 | ⚠️ OpenCode 可设当前模型 | 🟡 可实现 |
| `AllowedDirs` | ✅ `--add-dir` | ❌ 未使用 | ❌ OpenCode 无此概念 | 🔴 工具受限 |
| `DisallowedTools` | ✅ `--disallowed-tools` | ❌ 未使用 | ❌ OpenCode 仅代理级权限 | 🔴 工具受限 |
| `MCPConfig` | ✅ `--mcp-config`（临时文件） | ❌ 未使用 | ❌ OpenCode 仅全局 MCP 配置 | 🔴 工具受限 |
| `StrictMCPConfig` | ✅ `--strict-mcp-config` | ❌ 未使用 | ❌ 同上 | 🔴 工具受限 |
| `PermissionMode` | ✅ `--permission-mode` | ✅ `applyPermissions()` | ✅ | OK |
| `SkipPermissions` | ✅ `--dangerously-skip-permissions` | ❌ 未使用 | ❌ OpenCode 无此概念 | 🔴 工具受限 |
| `MaxTurns` | ✅ `--max-turns` | ❌ 未使用 | ⚠️ OpenCode 代理级 `steps` | 🟡 绕行 |
| `Bare` | ✅ `--bare` | ❌ 未使用 | ❌ OpenCode 无此模式 | 🔴 工具受限 |
| `MaxBudgetUSD` | ✅ `--max-budget-usd` | ❌ 未使用 | ❌ OpenCode 仅追踪无上限 | 🔴 工具受限 |
| `JSONSchema` | ✅ `--json-schema` | ❌ 未使用 | ✅ `OutputFormatJsonSchema`（`message-v2.ts:64`） | 🟢 可实现 |
| `IncludeHookEvents` | ✅ `--include-hook-events` | ❌ 未使用 | ❌ OpenCode 不暴露钩子事件 | 🔴 工具受限 |
| `IncludePartialMessages` | ✅ `--include-partial-messages` | ❌ 未使用 | ❌ 无此 toggle | 🔴 工具受限 |
| `ConfigEnv` | ❌ → 🟢 可实现 | ❌ | ✅ CC: `settings.env`（`managedEnv.ts:187`） | 🟢 CC 可实现 |
| `ConfigBlocklist` | ❌ | ❌ | ❌ 双方工具均不支持 | 🔴 工具受限 |
| `ContinueSession` | ❌ → 🟢 可实现 | ❌ | ✅ CC: `--continue`（`main.tsx:988`） | 🟢 CC 可实现 |
| `ForkSession` | ❌ → 🟢 可实现 | ❌ | ✅ CC: `--fork-session`（`main.tsx:988`） | 🟢 CC 可实现 |
| `ResumeSessionAt` | ❌ → 🟢 可实现 | ❌ | ✅ CC: `--resume-session-at`（`main.tsx:991`） | 🟢 CC 可实现 |
| `WorkerSessionID` | ❌（CC 无此概念） | ✅ | — | OK |

**统计**：CC Worker 已使用 16/24，可实现 +4；OCS Worker 已使用 6/24，可实现 +3，绕行 +2。

---

## 二、可选接口实现差距

| 接口 | CC | OCS | 说明 |
|------|:--:|:---:|------|
| `Worker` | ✅ | ✅ | 核心生命周期 |
| `ControlRequester` | ✅ | ✅ | `SendControlRequest` |
| `SessionFileChecker` | ✅ | ❌ | `HasSessionFiles()` — Resume 前检查文件是否存在 |
| `InPlaceReseter` | ❌ | ✅ | 就地重置（HTTP API vs 杀进程） |
| `WorkerCommander` | ❌ | ✅ | `Compact/Clear/Rewind` 结构化命令 |
| `WorkerSessionIDHandler` | ❌ | ✅ | 内部 session ID 管理 |
| `InputRecoverer` | ❌ | ✅ | 崩溃恢复时重新投递输入 |

---

## 三、事件输出差距

| AEP 事件 | CC | OCS | 工具验证 |
|----------|:--:|:---:|---------|
| `reasoning` | ✅ | ❌ → 🟢 | 需 `OPENCODE_EXPERIMENTAL_EVENT_SYSTEM=true` |
| `message.delta` | ✅ | ✅ | — |
| `permission_request` | ✅ | ✅ | — |
| `question_request` | ✅ | ✅ | — |
| `elicitation_request` | ✅ | ❌ 🔴 | OpenCode 无 MCP elicitation |
| `tool_call` | ✅ | ✅ | — |
| `tool_result` | ✅ | ✅ | — |
| `state` | ✅ | ✅ | — |
| `done` | ✅ | ✅ | — |
| `error` | ✅ | ✅ | — |
| `context_usage` | ✅ | ✅ | — |
| `mcp_status` | ✅ | ✅ | — |

---

## 四、控制协议差距

| 操作 | CC | OCS | 验证 |
|------|:--:|:---:|------|
| `context_usage` | ✅ stdin control_request | ✅ HTTP API | ✅ |
| `mcp_status` | ✅ stdin control_request | ✅ HTTP API | ✅ |
| `set_model` | ✅ stdin control_request | ✅ 存储 pendingModel | ✅ |
| `set_permission_mode` | ✅ 自动成功 | ✅ PATCH /session | ✅ |
| `set_max_thinking_tokens` | ✅ 自动成功 | ❌ unsupported | OCS 可通过 variant |
| `mcp_set_servers` | ✅ 自动成功 | ❌ unsupported | 🔴 |
| `mcp_message` | ✅ 自动成功 | ❌ unsupported | 🔴 |
| `compact` | ❌（仅文本 "/compact"） | ✅ HTTP API | CC: `supportsNonInteractive:true` |
| `clear` | ❌（仅交互模式） | ✅ HTTP API | CC: `supportsNonInteractive:false` |
| `rewind` | ⚠️ `rewind_files` | ✅ HTTP API | CC 有结构化协议 |

---

## 五、进程模型根本差异

| 维度 | CC Worker | OCS Worker |
|------|-----------|------------|
| 进程模型 | 每 session 一个 `claude` 进程 | 所有 session 共享 `opencode serve` |
| 传输 | stdin/stdout NDJSON | HTTP POST + SSE EventBus |
| 崩溃影响 | 单 session | 全部 session |
| Terminate | SIGTERM → 5s → SIGKILL | 释放引用 + 关闭 SSE |
| Reset | 杀进程 → 删文件 → 重建 | 就地 `POST /session/{id}/reset` |
| Modalities | text + code + image | text + code（缺 image） |

---

## 六、实施计划

### Batch 1: CC Worker P0 修复（立即可实现）

| ID | 改点 | 位置 | 说明 |
|----|------|------|------|
| B1-1 | `ContinueSession` → `--continue` | `claudecode/worker.go:buildCLIArgs` | 恢复当前目录最近会话 |
| B1-2 | `ForkSession` → `--fork-session` | 同上 | 必须配合 `--resume` |
| B1-3 | `ResumeSessionAt` → `--resume-session-at` | 同上 | 必须配合 `--resume`，恢复到指定消息 |
| B1-4 | `ConfigEnv` → `--settings` | 同上 | 通过 `--settings '{"env":{...}}'` 注入 |

### Batch 2: OCS Worker P0 修复（立即可实现）

| ID | 改点 | 位置 | 说明 |
|----|------|------|------|
| B2-1 | 传递 `AllowedModels[0]` → `model` | `opencodeserver/worker.go:createSession` | session 创建时设置模型 |
| B2-2 | 传递 `JSONSchema` → `format` | `opencodeserver/worker.go:conn.Send` | PromptInput.format |
| B2-3 | 传递 variant（思考令牌） | 同上 | PromptInput.variant |
| B2-4 | 添加 `reasoning` AEP 事件 | `opencodeserver/worker.go:forwardBusEvents` | 处理 `session.next.reasoning.*` |
| B2-5 | 设置 `OPENCODE_EXPERIMENTAL_EVENT_SYSTEM=true` | singleton 环境变量 | 激活 V2 事件系统 |

### Batch 3: P1 绕行方案

| ID | 改点 | 方案 |
|----|------|------|
| B3-1 | OCS `MaxTurns` | 选择有 `steps` 限制的 agent |
| B3-2 | OCS `AllowedTools` | PATCH `/session/:id` 注入 permission rules |
| B3-3 | CC `Compact` 结构化 | 发送 "/compact" 作为用户消息（`supportsNonInteractive:true`） |
| B3-4 | CC `Rewind` 结构化 | 使用 `rewind_files` control_request subtype |

### 🔴 无法实现（工具限制）

- OCS: `MCPConfig`/`MaxBudgetUSD`/`Bare`/`IncludeHookEvents`/`Elicitation`/`SystemPromptReplace`/`AllowedTools`/`AllowedDirs`/`DisallowedTools`
- CC: `InPlaceReset`、`Clear` 结构化
- 双方: `ConfigBlocklist`

---

## 相关 Issues

- [#431](https://github.com/hrygo/hotplex/issues/431) — **Batch 1**: CC Worker P0（ContinueSession/ForkSession/ResumeSessionAt/ConfigEnv）
- [#432](https://github.com/hrygo/hotplex/issues/432) — **Batch 2**: OCS Worker P0（model/JSONSchema/variant/reasoning 事件）
- [#433](https://github.com/hrygo/hotplex/issues/433) — **Batch 3**: P1 绕行方案（MaxTurns/AllowedTools/Compact/Rewind）
