---
type: spec
tags:
  - project/HotPlex
  - worker/claude-code
  - architecture/integration
date: 2026-04-01
status: implemented
progress: 100
completion_date: 2026-04-01
---

# Claude Code Worker 集成规格

> 本文档详细定义 Claude Code Worker Adapter 与 Claude Code CLI 的集成规格。
> 高阶设计见 [[Worker-Gateway-Design]] §8.1。

---

## 1. 概述

| 维度 | 设计 |
|------|------|
| **Transport** | stdio（stdin/stdout pipe） |
| **Protocol** | stream-json（NDJSON） |
| **进程模型** | 持久进程，多轮复用（Hot-Multiplexing） |
| **源码路径** | `~/claude-code/src` |

**集成命令**：

```bash
claude --print \
  --verbose \                     # stream-json 模式必需
  --output-format stream-json \
  --input-format stream-json \
  --session-id <uuid>
```

> **注意**：Claude Code 要求 `--output-format stream-json` 模式下必须同时使用 `--verbose` 参数。

---

## 2. CLI 参数

### 2.1 核心参数（v1.0 必须）

| 参数 | 说明 | Impl |
|------|------|------|
| `--print` / `-p` | 非交互模式，输出结果后退出 | ✅ `worker.go:149` |
| `--output-format stream-json` | stdout 输出 NDJSON 事件流 | ✅ `worker.go:151` |
| `--input-format stream-json` | stdin 接受 NDJSON 输入 | ✅ `worker.go:152` |
| `--session-id <uuid>` | 指定 session ID（UUID 格式） | ✅ `worker.go:159` |
| `--resume [value]` | 恢复会话（可带 session ID） | ✅ `worker.go:163` |
| `--continue` / `-c` | 继续当前目录的最新会话 | ✅ `worker.go:157` |
| `--permission-mode <mode>` | 权限模式：`default`/`plan`/`auto-accept` | ✅ `worker.go:175` |
| `--dangerously-skip-permissions` | 跳过所有权限检查 | ✅ `worker.go:178` |
| `--allowed-tools <list>` | 允许的工具列表（逗号或空格分隔） | ✅ `worker.go:187` |
| `--disallowed-tools <list>` | 禁止的工具列表 | ✅ `worker.go:181` |
| `--model <model>` | 模型覆盖 | ✅ `worker.go:184` |
| `--system-prompt <prompt>` | **替换**默认系统提示 | ✅ `worker.go:191` |
| `--append-system-prompt <prompt>` | **追加**到现有系统提示末尾 | ✅ `worker.go:194` |
| `--max-turns <n>` | 非交互模式的最大 agentic 轮次 | ✅ `worker.go:203` |

> `--system-prompt` 与 `--append-system-prompt` 的区别：前者**替换**默认系统提示（赋值），后者**追加**到系统提示末尾（拼接）。

### 2.2 扩展参数（v1.1）

| 参数 | 说明 | 优先级 | Impl |
|------|------|--------|------|
| `--fork-session` | 恢复时创建新 session ID（而非复用） | P1 | ✅ `worker.go:165` |
| `--resume-session-at <message id>` | 恢复时仅包含到指定 assistant message 的历史 | P1 | ✅ `worker.go:168` |
| `--rewind-files <user-message-id>` | 将文件恢复到指定用户消息时的状态并退出 | P2 | ✅ `worker.go:171` |
| `--mcp-config <configs...>` | 从 JSON 文件加载 MCP 服务器配置 | P1 | ✅ `worker.go:197` |
| `--strict-mcp-config` | 仅使用 `--mcp-config` 指定的 MCP 服务器 | P1 | ✅ `worker.go:199` |
| `--bare` | 最小化模式：跳过 hooks、LSP、插件同步 | P2 | ✅ `worker.go:206` |
| `--add-dir <dirs...>` | 允许工具访问的额外目录 | P2 | ✅ `worker.go:209` |
| `--max-budget-usd <amount>` | API 调用最大花费（USD） | P3 | ✅ `worker.go:212` |
| `--json-schema <schema>` | 结构化输出的 JSON Schema 验证 | P3 | ✅ `worker.go:215` |
| `--include-hook-events` | 在输出流中包含所有 hook 生命周期事件 | P3 | ✅ `worker.go:218` |
| `--include-partial-messages` | 包含到达的部分消息块 | P3 | ✅ `worker.go:221` |

---

## 3. 环境变量

> 详见 `src/utils/managedEnvConstants.ts`

### 3.1 供应商托管变量

| 变量 | 说明 | Impl |
|------|------|------|
| `ANTHROPIC_API_KEY` / `ANTHROPIC_AUTH_TOKEN` | API 密钥（必需） | ✅ 白名单 |
| `ANTHROPIC_BASE_URL` | API 端点（私有部署时使用） | ✅ 白名单 |
| `ANTHROPIC_BEDROCK_BASE_URL` | Bedrock 端点 | ✅ 白名单 |
| `ANTHROPIC_VERTEX_BASE_URL` | Vertex 端点 | ✅ 白名单 |
| `ANTHROPIC_FOUNDRY_BASE_URL` | Foundry 端点 | ✅ 白名单 |
| `ANTHROPIC_MODEL` | 默认模型 | ✅ 白名单 |

> 当前实现同时支持 `CLAUDE_*` 和 `ANTHROPIC_*` 前缀，两套变量均已在白名单中。

### 3.2 安全变量（可在托管设置中使用）

| 变量 | 说明 | Impl |
|------|------|------|
| `ANTHROPIC_CUSTOM_HEADERS` | 自定义请求头 | ✅ 白名单 |
| `BASH_MAX_TIMEOUT_MS` | Bash 工具最大超时（ms） | ✅ 白名单 |
| `BASH_MAX_OUTPUT_LENGTH` | Bash 输出最大长度 | ✅ 白名单 |
| `MAX_MCP_OUTPUT_TOKENS` | MCP 输出最大 token 数 | ✅ 白名单 |
| `MAX_THINKING_TOKENS` | Extended Thinking 最大 token 数 | ✅ 白名单 |
| `MCP_TIMEOUT` / `MCP_TOOL_TIMEOUT` | MCP 超时配置 | ✅ 白名单 |
| `OTEL_*` | OpenTelemetry 配置（前缀匹配） | ✅ 白名单，`base/env.go` 前缀匹配支持 |

### 3.3 安全集成要求

| 要求 | 说明 | Impl |
|------|------|------|
| **移除 `CLAUDECODE=`** | 防止嵌套调用 | ✅ `security.StripNestedAgent()` |
| **注入 `ANTHROPIC_API_KEY`** | 必需认证 | ⚠️ 见上文注释 |
| **可选注入 `ANTHROPIC_BASE_URL`** | 支持私有部署 |
| **可选注入 MCP 配置** | 通过 `MCP_*` 或 `--mcp-config` |

---

## 4. 输入格式（stdin → Claude Code）

### 4.1 基本格式

每行一个 JSON 对象（必须使用 `\n` 换行）：

```json
{
  "type": "user",
  "message": {
    "role": "user",
    "content": [
      { "type": "text", "text": "user prompt here" }
    ]
  }
}
```

### 4.2 消息优先级（可选）

```json
{
  "type": "user",
  "message": { "role": "user", "content": [...] },
  "priority": "now"   // 立即处理
  "priority": "next" // 排在当前 turn 之后
  "priority": "later" // 排在队列末尾
}
```

### 4.3 任务指令注入

```xml
<context>
<![CDATA[
task instructions here
]]>
</context>

<user_query>
<![CDATA[
user prompt here
]]>
</user_query>
```

---

## 5. 输出格式（stdout → Worker Adapter）

### 5.1 NDJSON 安全序列化

**必须转义 U+2028（行分隔符）和 U+2029（段分隔符）**，否则解析器会在这些字符处截断。

```typescript
// 详见 src/cli/ndjsonSafeStringify.ts
const JS_LINE_TERMINATORS = /\u2028|\u2029/g

export function ndjsonSafeStringify(value: unknown): string {
  return escapeJsLineTerminators(JSON.stringify(value))
}
```

**Worker Adapter 实现**：

```go
import "regexp"

var lineTerminators = regexp.MustCompile(`[\u2028\u2029]`)

func ndjsonSafeMarshal(v any) (string, error) {
    data, err := json.Marshal(v)
    if err != nil {
        return "", err
    }
    // 转义 JS 行终止符
    safe := lineTerminators.ReplaceAllFunc(data, func(b []byte) []byte {
        switch {
        case bytes.Equal(b, []byte{0xE2, 0x80, 0xA8}):
            return []byte("\\u2028")
        case bytes.Equal(b, []byte{0xE2, 0x80, 0xA9}):
            return []byte("\\u2029")
        }
        return b
    })
    return string(safe) + "\n", nil
}
```

### 5.2 SDK 消息类型

| `type` | `subtype` | 说明 | AEP 映射 |
|--------|-----------|------|----------|
| `assistant` | — | 完整助手消息 | `message` |
| `user` | — | 用户消息 | — |
| `result` | `success` | 执行成功 | `done { success: true }` |
| `result` | `error` | 执行错误 | `error` + `done { success: false }` |
| `stream_event` | — | 流式事件 | `message.delta` |
| `tool_progress` | — | 工具执行进度 | `tool_result` |
| `control_request` | — | 控制请求 | 见 §6 |
| `control_response` | — | 控制响应 | — |
| `system` | `init` | 初始化 | — |
| `system` | `status` | 状态更新 | `state` |
| `system` | `compact_boundary` | 上下文压缩边界 | — |
| `system` | `post_turn_summary` | turn 后摘要 | — |
| `session_state_changed` | — | 会话状态变更 | `state` |
| `files_persisted` | — | 文件持久化事件 | — |
| `hook_started` | — | Hook 启动 | — |
| `hook_progress` | — | Hook 进度 | — |
| `hook_response` | — | Hook 响应 | — |
| `task_notification` | — | 任务通知 | — |
| `task_started` | — | 任务开始 | — |
| `task_progress` | — | 任务进度 | — |
| `rate_limit` | — | 速率限制事件 | — |
| `tool_use_summary` | — | 工具使用摘要 | — |
| `elicitation_complete` | — | 信息收集完成 | — |
| `prompt_suggestion` | — | 提示建议 | — |
| `local_command_output` | — | 本地命令输出 | — |

### 5.3 关键消息示例

**thinking（思考过程）**：

```json
{
  "type": "stream_event",
  "event": {
    "type": "thinking",
    "message": {
      "content": [{ "type": "text", "text": "Let me analyze..." }]
    }
  }
}
```

**assistant（文本 + 工具调用）**：

```json
{
  "type": "assistant",
  "message": {
    "role": "assistant",
    "content": [
      { "type": "text", "text": "Hello world" },
      { "type": "tool_use", "id": "call_123", "name": "read_file", "input": { "path": "/app/main.go" } }
    ]
  }
}
```

**tool_progress（工具结果）**：

```json
{
  "type": "tool_progress",
  "tool_use_id": "call_123",
  "content": [{ "type": "tool_result", "tool_use_id": "call_123", "content": "file content..." }]
}
```

**result（turn 结束）**：

```json
{
  "type": "result",
  "subtype": "success",
  "duration_ms": 5200,
  "duration_api_ms": 4800,
  "is_error": false,
  "num_turns": 1,
  "result": "final summary text",
  "total_cost_usd": 0.05,
  "usage": {
    "input_tokens": 1000,
    "output_tokens": 500,
    "cache_read_input_tokens": 800,
    "cache_creation_input_tokens": 200
  },
  "modelUsage": {
    "claude-sonnet-4-6": {
      "input_tokens": 1000,
      "output_tokens": 500,
      "cost_usd": 0.05,
      "context_window": 200000,
      "max_output_tokens": 16384
    }
  },
  "permission_denials": [],
  "uuid": "msg_xxx",
  "session_id": "session_xxx"
}
```

**error（错误）**：

```json
{
  "type": "result",
  "subtype": "error",
  "is_error": true,
  "result": "error message"
}
```

---

## 6. 控制协议（stdin ↔ stdout）

当 `--output-format stream-json` 时，Claude Code 支持双向控制协议。

### 6.1 控制请求（Claude Code → Worker）

```json
{ "type": "control_request", "request_id": "req_xxx", "response": { "subtype": "<type>" } }
```

| `subtype` | 说明 | HotPlex 处理 |
|-----------|------|-------------|
| `can_use_tool` | 权限请求 | 转发 Client 或自动决策 |
| `set_permission_mode` | 设置权限模式 | 更新运行时配置 |
| `set_model` | 切换模型 | 更新运行时配置 |
| `set_max_thinking_tokens` | 设置思考 token 上限 | 更新运行时配置 |
| `mcp_status` | MCP 服务器状态 | 调试/监控 |
| `mcp_set_servers` | 配置 MCP 服务器 | 更新 MCP 配置 |
| `mcp_message` | MCP 消息 | 转发 MCP 请求 |
| `interrupt` | 中断当前执行 | 触发 `control.terminate` |
| `cancel_async_message` | 取消异步消息 | 取消进行中请求 |
| `rewind_files` | 文件状态回滚 | 执行文件回滚 |
| `reload_plugins` | 重新加载插件 | 重载插件配置 |

### 6.2 控制响应（Worker → Claude Code）

```json
{
  "type": "control_response",
  "response": {
    "subtype": "success",
    "request_id": "req_xxx",
    "response": { ... }
  }
}
```

或错误：

```json
{
  "type": "control_response",
  "response": {
    "subtype": "error",
    "request_id": "req_xxx",
    "error": "error message"
  }
}
```

### 6.3 权限请求处理

`parseControlRequest` 返回 `EventControl` 事件，由 `worker.go` 根据 `Subtype` 分发：

| `Subtype` | 处理 |
|-----------|------|
| `can_use_tool` | 构造 `PermissionRequest` envelope，转发 Client |
| `interrupt` | 触发 `EventInterrupt`，优雅终止 |
| `set_*` / `mcp_*` 等 | `ControlHandler` 自动响应 `success` |

```go
// worker.go readOutput 中的 EventControl 分发
case EventControl:
    cr, ok := evt.Payload.(*ControlRequestPayload)
    if !ok {
        continue
    }
    switch cr.Subtype {
    case string(ControlCanUseTool):
        // 构造 PermissionRequest → gateway
        env := events.NewEnvelope(aep.NewID(), w.sessionID, w.nextSeq(),
            events.PermissionRequest,
            events.PermissionRequestData{
                ID:       cr.RequestID,
                ToolName: cr.ToolName,
                Args:     []string{jsonMarshal(cr.Input)},
            })
        w.trySend(env)
    default:
        // set_*, mcp_* 等：ControlHandler 自动 success
        _, _ = w.control.HandlePayload(cr)
    }
```

---

## 7. 事件映射（Claude Code → AEP）

| Claude Code Event | AEP Event Kind | 说明 | Impl |
|-------------------|---------------|------|------|
| `stream_event` + `thinking` | `reasoning` | 思考过程 | ✅ `mapper.go:119-141` — thinking 类型映射为 `events.Reasoning` |
| `stream_event` + 其他 | `message.delta { type: "text"\|"tool_use" }` | 流式增量 | ✅ |
| `assistant` + text | `message.delta { type: "text" }` | 文本增量 | ✅ `mapper.go:39-42` |
| `assistant` + tool_use | `tool_call` | 工具调用 | ✅ `mapper.go:44,99-112` |
| `tool_progress` | `tool_result` | 工具结果 | ✅ `mapper.go:48-53,114-127` |
| `result` subtype=success | `done { success: true, stats: {...} }` | 执行完成 | ✅ `mapper.go:145-155` |
| `result` subtype=error | `error` + `done { success: false }` | 执行错误 | ✅ `mapper.go:155-206` — 同时发送 error 和 done 两个 envelope |
| `control_request` + `can_use_tool` | `permission_request` | 权限请求 | ✅ `parser.go:288-313`, `worker.go:349-377` |
| `control_request` + `interrupt` | 内部中断 | 对应 terminate | ✅ `parser.go:296-300` |
| `control_request` + `set_permission_mode` | — | 自动响应 success | ✅ `control.go:50-54` |
| `control_request` + `set_model` | — | 自动响应 success | ✅ `control.go:50-54` |
| `control_request` + `set_max_thinking_tokens` | — | 自动响应 success | ✅ `control.go:50-54` |
| `control_request` + `mcp_*` | — | 自动响应 success | ✅ `control.go:55-56` |
| `control_request` + `cancel_async_message` | — | — | ❌ 忽略 |
| `control_request` + `rewind_files` | — | — | ❌ 忽略 |
| `control_request` + `reload_plugins` | — | — | ❌ 忽略 |
| `system` subtype=`status` | `state` | 状态变更 | ✅ `parser.go:315-323`, `mapper.go:223-239` |
| `session_state_changed` | `state` | 会话状态 | ✅ `parser.go:329-336`, `mapper.go:241-252` |
| `files_persisted` | — | 内部事件 | ✅ 正确忽略 |
| `rate_limit` | — | 内部事件 | ✅ 正确忽略 |
| `compact_boundary` | — | 上下文压缩 | ✅ 正确忽略 |
| `hook_*` | — | Hook 生命周期 | ✅ 正确忽略 |
| `task_*` | — | 任务通知 | ✅ 正确忽略 |
| `tool_use_summary` | — | 工具使用摘要 | ✅ 正确忽略 |
| `prompt_suggestion` | — | 提示建议 | ✅ 正确忽略 |
| `local_command_output` | — | 本地命令输出 | ✅ 正确忽略 |

---

## 8. Session 管理

### 8.1 Session ID 格式

Claude Code 支持两种格式：

| 格式 | 说明 | HotPlex 处理 |
|------|------|-------------|
| `session_*` | v1 兼容格式 | 直接使用 |
| `cse_*` | v2 基础设施格式 | ✅ `worker.go:449-461` — `ToCompatSessionID` / `ToInfraSessionID` |

```go
// 转换函数（internal/worker/claudecode/worker.go）
ToCompatSessionID(id string) string  // cse_* → session_*
ToInfraSessionID(id string) string   // session_* → cse_*
```

### 8.2 Session 持久化

| 项目 | 路径 |
|------|------|
| 存储位置 | `~/.claude/projects/<workspace-key>/<session-id>.jsonl` |
| workspace-key | 路径替换（`/` → `-`，`/.` → `--`） |
| Gateway 追踪 | `~/.hotplex/sessions/<id>.lock` |

### 8.3 Resume 流程

```
1. 检查 Marker 文件（~/.hotplex/sessions/<id>.lock）
2. 检查 session 文件（~/.claude/projects/.../<id>.jsonl）
3. 两者存在 → --resume 模式
4. 否则 → 新建 session（--session-id）
```

---

## 9. 优雅终止（Graceful Shutdown）

### 9.1 Claude Code 内部流程

```
Gateway SIGTERM
    ↓
Claude Code gracefulShutdown()
    ↓
┌────────────────────────────────────────┐
│ 1. failsafeTimer = 5s（超时强制退出）   │
│ 2. cleanupTerminalModes()              │
│ 3. runCleanupFunctions()（2s 超时）    │
│ 4. SessionEnd hooks                     │
│ 5. 分析数据刷新（500ms 超时）           │
│ 6. forceExit()                         │
└────────────────────────────────────────┘
    ↓ (超时)
SIGKILL
```

### 9.2 Worker Adapter 终止流程

实际实现在 `base.BaseWorker`，委托 `proc.Terminate`：

```go
// base/worker.go — BaseWorker.Terminate
func (w *BaseWorker) Terminate(ctx context.Context) error {
    w.Mu.Lock()
    proc := w.Proc
    w.Mu.Unlock()

    if proc == nil {
        return nil
    }

    // proc.Terminate: SIGTERM → 5s grace → SIGKILL（详见 proc/manager.go）
    if err := proc.Terminate(ctx, syscall.SIGTERM, gracefulShutdownTimeout); err != nil {
        return fmt.Errorf("base: terminate: %w", err)
    }

    w.Mu.Lock()
    w.Proc = nil
    w.Mu.Unlock()

    return nil
}
```

Claude Code Worker 的 `Terminate` 直接调用 `BaseWorker.Terminate`（`worker.go:269-277`）。

---

## 10. MCP 服务器配置

### 10.1 CLI 参数

```bash
claude --print --session-id <id> \
  --mcp-config /path/to/mcp-config.json \
  --strict-mcp-config
```

### 10.2 MCP 配置格式

```json
{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/project"]
    },
    "github": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-github"],
      "env": {
        "GITHUB_PERSONAL_ACCESS_TOKEN": "xxx"
      }
    }
  }
}
```

### 10.3 环境变量配置

| 变量 | 说明 |
|------|------|
| `MCP_TIMEOUT` | MCP 总体超时 |
| `MCP_TOOL_TIMEOUT` | MCP 工具调用超时 |

---

## 11. 实现优先级

### P0（必须实现，v1.0 MVP）

| 项目 | 说明 |
|------|------|
| NDJSON 安全序列化 | 转义 U+2028/U+2029 |
| `stream_event` 处理 | 流式事件解析 |
| `tool_progress` 映射 | 工具结果 |
| `control_request` + `can_use_tool` | 权限请求 → `permission_request` |
| 环境变量白名单 | 移除 `CLAUDECODE=` |
| 分层终止 | SIGTERM → 5s → SIGKILL |

### P1（重要，v1.0 完整支持）

| 项目 | 说明 |
|------|------|
| `--mcp-config` | MCP 服务器配置 |
| `--fork-session` | 新建 session ID |
| `control_response` | 发送控制响应 |
| `session_state_changed` | 会话状态变更 |
| Session ID 兼容 | `session_*` / `cse_*` |

### P2（增强，v1.1）

| 项目 | 说明 |
|------|------|
| `--resume-session-at` | 恢复到指定消息 |
| `--rewind-files` | 文件回滚 |
| `--bare` 模式 | 最小化配置 |
| StructuredIO | 消息预队列 |

---

## 12. 源码关键路径

| 功能 | 源码路径 |
|------|---------|
| CLI 参数解析 | `src/main.tsx` |
| Session ID 兼容 | `src/bridge/sessionIdCompat.ts` |
| 会话创建 | `src/bridge/createSession.ts` |
| 结构化 I/O | `src/cli/structuredIO.ts` |
| NDJSON 序列化 | `src/cli/ndjsonSafeStringify.ts` |
| 优雅关闭 | `src/utils/gracefulShutdown.ts` |
| 环境变量 | `src/utils/managedEnvConstants.ts` |
| SDK 消息 schemas | `src/entrypoints/sdk/coreSchemas.ts` |
| 控制协议 schemas | `src/entrypoints/sdk/controlSchemas.ts` |
| SSE 传输 | `src/cli/transports/SSETransport.ts` |
| Hybrid 传输 | `src/cli/transports/HybridTransport.ts` |
| Tool 权限 | `src/Tool.ts` |

---

## 13. Worker Adapter 核心代码

### 13.1 事件解析

```go
// WorkerEvent is the parsed event returned by Parser.
type WorkerEvent struct {
    Type       EventType            // EventStream, EventAssistant, etc.
    Payload    any                  // Concrete type via type assertion; *ControlRequestPayload for control events
    RawMessage *SDKMessage          // Original SDK message for advanced handling
}

// Parser parses SDK messages into WorkerEvents.
type Parser struct {
    log *slog.Logger
}

func (p *Parser) ParseLine(line string) ([]*WorkerEvent, error) {
    var msg SDKMessage
    if err := json.Unmarshal([]byte(line), &msg); err != nil {
        return nil, err
    }

    switch msg.Type {
    case "stream_event":
        return p.parseStreamEvent(&msg)
    case "assistant":
        return p.parseAssistant(&msg)
    case "tool_progress":
        return p.parseToolProgress(&msg)
    case "result":
        return p.parseResult(&msg)
    case "control_request":
        return p.parseControlRequest(&msg)
    case "system":
        return p.parseSystem(&msg)
    case "session_state_changed":
        return p.parseSessionState(&msg)
    default:
        return nil, nil // 忽略未知类型
    }
}

// parseControlRequest: all subtypes return EventControl with Payload=*ControlRequestPayload;
// worker.go dispatches by Subtype: can_use_tool → gateway, set_*/mcp_* → auto-success.
func (p *Parser) parseControlRequest(msg *SDKMessage) ([]*WorkerEvent, error) {
    var req ControlRequestPayload
    if err := json.Unmarshal(msg.Response, &req); err != nil {
        return nil, err
    }
    req.RequestID = msg.RequestID // canonical source is outer SDKMessage

    switch req.Subtype {
    case "interrupt":
        return []*WorkerEvent{{Type: EventInterrupt, RawMessage: msg}}, nil
    default:
        // can_use_tool, set_permission_mode, set_model, mcp_status, etc.
        return []*WorkerEvent{{Type: EventControl, Payload: &req, RawMessage: msg}}, nil
    }
}
```

### 13.2 会话启动

```go
// Worker implements the Claude Code worker adapter.
type Worker struct {
    *base.BaseWorker
    sessionID string
    parser   *Parser
    mapper   *Mapper
    control  *ControlHandler
}

func (w *Worker) buildCLIArgs(session worker.SessionInfo, resume bool) []string {
    args := []string{
        "--print",
        "--verbose",
        "--output-format", "stream-json",
        "--input-format", "stream-json",
    }

    if session.ContinueSession {
        args = append(args, "--continue")
    } else {
        args = append(args, "--session-id", session.SessionID)
    }

    if resume {
        args = append(args, "--resume")
        if session.ForkSession {
            args = append(args, "--fork-session")
        }
        if session.ResumeSessionAt != "" {
            args = append(args, "--resume-session-at", session.ResumeSessionAt)
        }
    }

    if session.PermissionMode != "" {
        args = append(args, "--permission-mode", session.PermissionMode)
    }

    if len(session.AllowedTools) > 0 {
        args = append(args, "--allowed-tools", strings.Join(session.AllowedTools, ","))
    }

    return args
}
```

---

## 14. 实现状态跟踪

> 更新于 2026-04-02，全部 P0/P1/P2 项目已完成 ✅，无待办项。

### 14.1 汇总

| 类别 | ✅ | ⚠️ | ❌ | 总计 |
|------|---|---|---|------|
| **CLI 参数（P0 核心）** | 14 | 0 | 0 | 14 |
| **CLI 参数（P1/P2/P3）** | 17 | 0 | 0 | 17 |
| **环境变量白名单** | 14 | 0 | 0 | 14 |
| **SDK 事件解析** | 9 | 0 | 0 | 9 |
| **控制协议** | 9 | 0 | 0 | 9 |
| **AEP 事件映射** | 10 | 0 | 1 | 11 |
| **安全集成** | 1 | 1 | 0 | 2 |

### 14.2 P0 完成状态

| 优先级 | 项目 | 位置 | 状态 |
|--------|------|------|------|
| ✅ P0 | **NDJSON 安全序列化** | `internal/aep/codec.go` | ✅ 已完成，`escapeJSTerminators` + 测试 |
| ✅ P0 | `--permission-mode` | `worker.go:175` | ✅ 已实现，测试覆盖 |
| ✅ P0 | `--dangerously-skip-permissions` | `worker.go:178` | ✅ 已实现，测试覆盖 |
| ✅ P0 | `--disallowed-tools` | `worker.go:181` | ✅ 已实现，测试覆盖 |
| ✅ P0 | `--append-system-prompt` | `worker.go:194` | ✅ 已实现，测试覆盖 |
| ✅ P0 | `control_request` + `interrupt` | `parser.go:296-300` | ✅ 已映射，`EventInterrupt` + 优雅终止 |

### 14.3 已完成 P1 项目

| 优先级 | 项目 | 位置 | 状态 |
|--------|------|------|------|
| ✅ P1 | `--system-prompt` | `worker.go:191` | ✅ 替换模式，`SystemPromptReplace` 字段 |
| ✅ P1 | `--mcp-config` | `worker.go:197` | ✅ `MCPConfig` + `StrictMCPConfig` 字段 |
| ✅ P1 | `error` + `done { success: false }` | `mapper.go:155-206` | ✅ `mapResult` 返回两个 envelope |
| ✅ P1 | `session_id` 格式兼容 | `worker.go:449-461` | ✅ `ToCompatSessionID` / `ToInfraSessionID` |
| ✅ P1 | `--continue` / `-c` | `worker.go:157` | ✅ `ContinueSession` 字段 |
| ✅ P1 | `--max-turns <n>` | `worker.go:203` | ✅ `MaxTurns` 字段 |
| ✅ P1 | `--fork-session` | `worker.go:165` | ✅ `ForkSession` 字段，resume 时生效 |
| ✅ P1 | `stream_event` reasoning type | `mapper.go:118-141` | ✅ `thinking` → `events.Reasoning` |
| ✅ P2 | `--bare` | `worker.go:206` | ✅ `Bare` 字段 |
| ✅ P2 | `--add-dir` | `worker.go:209` | ✅ `AllowedDirs` 字段 |
| ✅ P3 | `--max-budget-usd` | `worker.go:212` | ✅ `MaxBudgetUSD` 字段 |
| ✅ P3 | `--json-schema` | `worker.go:215` | ✅ `JSONSchema` 字段 |
| ✅ P1 | `--resume-session-at` | `worker.go:168` | ✅ `ResumeSessionAt` 字段，resume 时生效 |
| ✅ P3 | `--include-hook-events` | `worker.go:218` | ✅ `IncludeHookEvents` 字段 |
| ✅ P3 | `--include-partial-messages` | `worker.go:221` | ✅ `IncludePartialMessages` 字段 |

### 14.4 待完成项目

> 2026-04-02：全部 P0/P1/P2 项目已完成，无待办项。

### 14.5 代码质量

| 项目 | 说明 | 位置 |
|------|------|------|
| `StreamType` 常量 | 消除 `thinking`/`text` 等字符串字面量 | `parser.go:25-31` |
| `ControlSubtype` 常量 | 消除 `can_use_tool`/`interrupt` 等字符串字面量 | `parser.go:35-46` |
| 死代码移除 | `mapControl`（unreachable）、`ControlHandler.pendingRequests` | 已删除 |
| 冗余字段移除 | `Worker.userID`、`Mapper.userID` | 已删除 |
| DRY 重构 | `mapSystem`/`mapSessionState` → `statusToSessionState` | `mapper.go:29-38` |
| 控制请求统一路由 | `WorkerEvent` 双字段 → 单一 `Payload *ControlRequestPayload` + Subtype switch | `parser.go`, `worker.go` |
| DRY 响应构造 | `sendAutoSuccess` / `SendPermissionResponse` → `sendResponse` 共享辅助方法 | `control.go` |
| 控制请求自动成功 | `set_permission_mode`/`set_model`/MCP 等 subtype 自动响应 success | `control.go:64-107` |
| 环境变量前缀匹配 | `OTEL_*` 通过 `OTEL_` 前缀白名单透传 | `base/env.go` |

### 14.6 架构亮点

- ✅ 三层协议分层：`Parser` → `Mapper` → `ControlHandler`
- ✅ `atomic.Int64` 原子 seq 生成，无锁竞争
- ✅ `context` 取消传播，goroutine 退出路径完整
- ✅ 分层终止：SIGTERM → 5s → SIGKILL（`base/worker.go`）
- ✅ `StripNestedAgent` 防止嵌套调用
- ✅ `OTEL_*` 前缀白名单，支持 OpenTelemetry 配置透传
- ✅ 控制请求自动成功：`set_*`/`mcp_*` subtype 不再静默丢弃
- ✅ `WorkerEvent` 统一路由：单一 `Payload *ControlRequestPayload` + Subtype switch，无双字段歧义
- ✅ `sendResponse` DRY 辅助方法：消除 `sendAutoSuccess` / `SendPermissionResponse` 重复构造
- ✅ 测试覆盖：`worker_test.go`、`parser_test.go`、`mapper_test.go`、`worker_integration_test.go`
