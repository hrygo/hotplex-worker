---
title: Agent Event Protocol (AEP) v1
type: spec
tags:
  - project/HotPlex
  - protocol/agent
---

# Agent Event Protocol (AEP) v1

## 0. Status

- Status: Draft (MVP-ready)
- Version: `aep/v1`
- Scope: Client ↔ Agent Gateway（通过 WebSocket）
- Direction: **Bidirectional** — 同一 Envelope 覆盖 Client → Server 和 Server → Client
- Non-goal: 不定义 tool schema / agent 内部执行语义

---

## 1. Design Goals

- **Streaming-first**：`message.delta` 一等公民
- **Bidirectional**：统一 Envelope 覆盖双向通信
- **统一表达**：chat / coding / tool agent
- **可扩展**：multi-agent / trace / UI
- **弱 schema**：允许 passthrough（`raw` type）

---

## 2. Envelope（统一包裹结构）

所有消息共用同一 Envelope，**不区分方向**：

```json
{
  "version": "aep/v1",
  "id": "evt_<uuid>",
  "seq": 42,
  "priority": "data",
  "session_id": "sess_<uuid>",
  "timestamp": 1710000000123,
  "event": {
    "type": "state",
    "data": {}
  }
}
```

| 字段 | 类型 | 必选 | 说明 |
|------|------|------|------|
| `version` | string | 是 | 协议版本，固定 `aep/v1` |
| `id` | string | 是 | 事件唯一标识，UUID v4。错误响应通过此字段引用触发事件 |
| `seq` | integer | 是 | 递增序列号，同一 session 内严格递增，从 1 开始。**仅分配给实际发送的事件**（被 backpressure 丢弃的 delta 不消耗 seq） |
| `priority` | string | 否 | 优先级：`"control"` 或 `"data"`（默认）。控制消息优先发送，跳过 backpressure 队列 |
| `session_id` | string | 是 | Session 标识 |
| `timestamp` | integer | 是 | Unix **毫秒**级时间戳，支持高频 delta 事件的精确时序排序 |
| `event.type` | string | 是 | 事件类型 |
| `event.data` | object | 是 | 事件载荷 |

**Priority 语义**：
- `priority: "control"` — 控制消息，Gateway 优先发送，不经过 backpressure 队列，可插队
- `priority: "data"` — 数据消息（默认），正常排队，受 backpressure 控制
- 控制消息示例：`control.reconnect`、`control.session_invalid`、`control.throttle`、`error`、`done`
- 数据消息示例：`message.delta`、`tool_call`、`tool_result`

> **Seq 语义**：seq 仅递增于**实际发送给 Client 的事件**。当 `message.delta` 因 backpressure 被丢弃时，该 delta 不消耗 seq 编号。因此 Client 不应通过 seq gap 检测丢包 — seq gap 不存在。关键事件（`message`/`done`/`error`/`control`）的 seq 保证连续。
>
> **参考**: Discord Gateway 使用类似的 seq 机制，但 seq 递增于所有事件（包括丢弃的），需要 Client 处理 gap。HotPlex 选择 "丢弃不递增" 策略，简化 Client 实现。WebSocket RFC 6455 使用协议层 Control Frames（Close/Ping/Pong）实现优先级，HotPlex 在应用层通过 `priority` 字段实现类似语义。

---

## 3. Event Type（完整集合）

### 方向标记

| 标记  | 含义              |
| --- | --------------- |
| C→S | Client → Server |
| S→C | Server → Client |
| 双向  | 双向均可用           |

---

### 3.1 init（C→S — 连接握手）

```json
{
  "type": "init",
  "data": {
    "session_id": "sess_xxx",
    "worker_type": "claude|opencode|pi",
    "config": {
      "model": "claude-sonnet-4-6",
      "system_prompt": "You are...",
      "allowed_tools": ["read_file", "write_file"]
    },
    "auth": {
      "token": "<jwt_token>"
    },
    "client_caps": ["message.delta", "tool_call", "tool_result"]
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `session_id` | 否 | 有值 = resume 已有 session；空 = 创建新 session |
| `worker_type` | 是 | Worker 类型标识 |
| `config` | 否 | Worker 配置（model、prompt、tools 等） |
| `auth` | 否 | 鉴权载荷（非浏览器或无需 Cookie 环境必传，包含 JWT 等 Token 认证信息） |
| `client_caps` | 是 | Client 支持的 event type 列表，用于能力协商 |

---

### 3.2 init_ack（S→C — 握手确认）

```json
{
  "type": "init_ack",
  "data": {
    "session_id": "sess_xxx",
    "state": "idle",
    "server_caps": ["message.delta", "tool_call", "tool_result", "state", "done", "error"]
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `session_id` | 是 | 分配或恢复的 session ID |
| `state` | 是 | Session 当前状态 |
| `server_caps` | 是 | Gateway 支持的 event type 列表 |

---

### 3.3 input（C→S — 用户输入）

```json
{
  "type": "input",
  "data": {
    "content": "your task"
  }
}
```

Session 状态为 `running` 时，拒绝 input，返回 `error`（`SESSION_BUSY`）。

> **Client 最佳实践**: 由于并发输入或多 Agent 协同可能引起状态冲突，建议 Client SDK 在收到 `SESSION_BUSY` 时，隐式使用指数退避（Exponential Backoff）进行静默重试，以此降低将瞬间冲突报错抛给最终终端用户的概率。

---

### 3.4 control（双向 — 控制命令）

**Client → Server（客户端控制）**：

```json
{
  "type": "control",
  "data": {
    "action": "terminate|delete"
  }
}
```

| action | 说明 |
|--------|------|
| `terminate` | 终止 Worker runtime，Session 进入 `terminated` |
| `delete` | 删除 Session 记录 + 清理 runtime |
| `reset` | 清空 Session.Context，Worker 自行决定 in-place 或 terminate+start，Session 进入 `running` |
| `gc` | 归档会话：终止 Worker（保留历史），Session 进入 `terminated`，后续可 resume |

**Server → Client（服务器主动控制，`priority: "control"`）**：

```json
{
  "type": "control",
  "priority": "control",
  "data": {
    "action": "reconnect|session_invalid|throttle",
    "reason": "...",
    // ... action-specific fields
  }
}
```

#### 3.4.1 `control.reconnect`（S→C — 强制重连）

服务器要求客户端断开当前连接并重新建立连接。

```json
{
  "type": "control",
  "priority": "control",
  "data": {
    "action": "reconnect",
    "reason": "server_maintenance|version_upgrade|load_balance",
    "delay_ms": 5000,
    "resume_session": true
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `reason` | 是 | 重连原因：`server_maintenance`（维护）、`version_upgrade`（版本升级）、`load_balance`（负载均衡） |
| `delay_ms` | 否 | 建议延迟重连时间（毫秒），避免 thundering herd |
| `resume_session` | 否 | 是否可通过 `session_id` resume（默认 `true`） |

**客户端行为**：
1. 收到 `reconnect` 后立即停止发送新消息
2. 等待当前 turn 完成（收到 `done`）或超时（5s）
3. 断开 WebSocket
4. 等待 `delay_ms` 后重新连接
5. 通过 `init(session_id)` resume session

> **参考**: Discord Gateway `op: 7 Reconnect`，服务器主动要求客户端重连。

#### 3.4.2 `control.session_invalid`（S→C — Session 失效通知）

通知客户端当前 session 已失效，无法继续使用。

```json
{
  "type": "control",
  "priority": "control",
  "data": {
    "action": "session_invalid",
    "reason": "session_expired|worker_crash|admin_killed|capacity_exceeded",
    "recoverable": false,
    "details": {
      "expired_at": 1710000000000,
      "message": "Session expired after 7 days of inactivity"
    }
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `reason` | 是 | 失效原因：`session_expired`（过期）、`worker_crash`（Worker 崩溃）、`admin_killed`（管理员终止）、`capacity_exceeded`（容量超限） |
| `recoverable` | 是 | 是否可通过重新创建 session 恢复（`false` = 需要 Client 重新发起完整请求） |
| `details` | 否 | 详细上下文（如过期时间、错误消息） |

**客户端行为**：
- `recoverable: true` → 可通过 `init` 创建新 session 并重试任务
- `recoverable: false` → 需要用户重新提交请求（如权限不足、配额超限）

> **参考**: Discord Gateway `op: 9 Invalid Session`，通知客户端 session 失效。

#### 3.4.3 `control.throttle`（S→C — 降级通知）

服务器检测到过载，要求客户端降低请求频率。

```json
{
  "type": "control",
  "priority": "control",
  "data": {
    "action": "throttle",
    "reason": "gateway_overload|rate_limit_exceeded",
    "suggestion": {
      "max_message_rate": 10,
      "backoff_ms": 1000,
      "retry_after": 5000
    }
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `reason` | 是 | 降级原因：`gateway_overload`（Gateway 过载）、`rate_limit_exceeded`（限流） |
| `suggestion.max_message_rate` | 否 | 建议的最大消息速率（消息/秒） |
| `suggestion.backoff_ms` | 否 | 建议的消息间隔（毫秒） |
| `suggestion.retry_after` | 否 | 建议的重试延迟（毫秒） |

**客户端行为**：
1. 降低 `input` 发送频率（按 `suggestion.max_message_rate`）
2. 增加 `input` 之间的延迟（按 `suggestion.backoff_ms`）
3. 如果当前请求被拒绝，等待 `retry_after` 后重试

> **注意**: 这是**软限制**，客户端应尽量遵守。如持续超限，Gateway 可能发送 `error(RATE_LIMIT_EXCEEDED)` 强制拒绝。

#### 3.4.4 `control.reset`（C→S — 清空会话上下文）

客户端请求清空当前会话的上下文，开始全新对话。

```json
{
  "type": "control",
  "data": {
    "action": "reset",
    "reason": "user_requested|new_conversation"
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `reason` | 否 | 重置原因：`user_requested`（用户主动）、`new_conversation`（新对话） |

**服务端行为**：
1. 清空 `SessionInfo.Context`（Gateway 层）
2. 调用 `Worker.ResetContext()`（Worker 自行决定 in-place 或 terminate+start）
3. Session 状态切至 `running`
4. 返回 `state{state: "running", message: "context_reset"}`

**详细流程**：
```
Client → Gateway: control{action: "reset"}
  Gateway: sm.ClearContext(sessionID)  → SessionInfo.Context = {}
  Gateway: w.ResetContext(ctx)         → Worker 清空运行时上下文
  Gateway: sm.Transition(RUNNING)       → 状态切换
  Gateway → Client: state{state: "running", message: "context_reset"}
```

> **注意**: `reset` 与 `terminate` 的区别 — `terminate` 是终止 Worker 并进入 `terminated` 状态；`reset` 是清空上下文并保持在 `running` 状态，开始全新对话。

#### 3.4.5 `control.gc`（C→S — 会话归档）

客户端请求将会话归档，Worker 终止但保留历史，后续可 resume。

```json
{
  "type": "control",
  "data": {
    "action": "gc",
    "reason": "user_idle|explicit_request"
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `reason` | 否 | 归档原因：`user_idle`（用户空闲超时）、`explicit_request`（用户主动） |

**服务端行为**：
1. 调用 `Worker.Terminate()`（Worker 内部自行保存会话状态）
2. 解除 Worker attachment（`sm.DetachWorker`）
3. Session 状态切至 `terminated`
4. 返回 `state{state: "terminated", message: "session_archived"}`

**详细流程**：
```
Client → Gateway: control{action: "gc"}
  Gateway: w.Terminate(ctx)            → Worker 终止，保存状态
  Gateway: sm.DetachWorker(sessionID)  → 解除 Worker attachment
  Gateway: sm.Transition(TERMINATED)   → 状态切换
  Gateway → Client: state{state: "terminated", message: "session_archived"}
```

> **注意**: `gc` 后 Session 可通过 `init` + 相同 `session_id` 恢复（resume）。

---

### 3.5 message.delta（S→C — 增量输出）

**唯一的流式输出 event type**。替代原 `token` type（已废弃）。

```json
{
  "type": "message.delta",
  "data": {
    "delta": {
      "type": "text",
      "text": " world"
    }
  }
}
```

`delta.type` 扩展：

| type | 说明 |
|------|------|
| `text` | 文本增量 |
| `code` | 代码块增量 |
| `image` | 图片 URL / base64 |

对于 raw stdout Worker（如 pi-mono），Worker Adapter 将每行 stdout 转换为 `message.delta { type: "text" }`。

---

### 3.6 message（S→C — 完整消息）

```json
{
  "type": "message",
  "data": {
    "role": "assistant",
    "content": [
      { "type": "text", "text": "Hello world" },
      { "type": "code", "language": "python", "text": "print('hi')" },
      { "type": "tool_use", "id": "call_123", "name": "read_file", "input": {...} }
    ]
  }
}
```

`content[].type` 扩展表：

| type | 说明 | 字段 |
|------|------|------|
| `text` | 文本内容 | `text` |
| `code` | 代码块 | `text`, `language` |
| `image` | 图片 | `url` 或 `data`（base64）, `media_type` |
| `tool_use` | 工具调用记录 | `id`, `name`, `input` |
| `tool_result` | 工具结果记录 | `tool_use_id`, `content` |

一次执行结束时，Worker 可能发送完整消息作为最终输出。

---

### 3.7 tool_call（S→C — 工具调用通知）

```json
{
  "type": "tool_call",
  "data": {
    "id": "call_123",
    "name": "read_file",
    "status": "executing",
    "arguments": {
      "path": "/app/main.py"
    }
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `id` | 是 | 工具调用唯一标识 |
| `name` | 是 | 工具名称 |
| `status` | 否 | 调用状态：`executing` / `completed` / `failed`（默认 `executing`） |
| `arguments` | 是 | 调用参数 |

**Autonomous 模式**：Worker 自行执行 tool，此事件仅为 **通知** Client（用于 UI 展示）。Client 不需要回传 `tool_result`。

> **`status` 字段**：允许 Client 区分 "正在执行" 和 "执行完毕"，无需依赖后续 `tool_result` 推断。

---

### 3.8 tool_result（S→C — 工具执行结果通知）

```json
{
  "type": "tool_result",
  "data": {
    "tool_call_id": "call_123",
    "result": "file content..."
  }
}
```

同样为 **通知**，Worker 内部完成 tool 执行后将结果通知 Client。

---

### 3.9 state（S→C — 状态变更）

```json
{
  "type": "state",
  "data": {
    "state": "running"
  }
}
```

状态集合：

| 状态 | 说明 |
|------|------|
| `created` | 已创建，未启动 runtime |
| `running` | 正在执行 |
| `idle` | 等待输入 |
| `terminated` | 已终止 |

> `deleted` 是控制面状态，通过 Admin API 管理，不走 AEP event channel。
> Worker 内部执行 tool 期间状态仍为 `running`，Client 通过 `tool_call` / `tool_result` 事件推断 Worker 阶段。

状态机：

```
created → running ⟷ idle → terminated
```

---

### 3.10 error（双向 — 错误通知）

```json
{
  "type": "error",
  "data": {
    "message": "something failed",
    "code": "WORKER_CRASH",
    "event_id": "evt_abc123",
    "details": {}
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `message` | 是 | 人类可读错误描述 |
| `code` | 是 | 结构化错误码（见下方枚举） |
| `event_id` | 否 | 引用触发此错误的原始事件 ID（参考 OpenAI Realtime API 的 error.event_id 模式） |
| `details` | 否 | 额外上下文（如 `{"exit_code": 139, "signal": "SIGSEGV"}`） |

**Error Code 枚举**：

| Code | 分类 | 说明 |
|------|------|------|
| `WORKER_CRASH` | Worker | 进程崩溃（SIGSEGV 等，exit code 139） |
| `WORKER_TIMEOUT` | Worker | 执行超时（超过 `execution_timeout`） |
| `WORKER_OOM` | Worker | 内存溢出（exit code 137） |
| `WORKER_OUTPUT_LIMIT` | Worker | 单行输出超限（默认 10MB） |
| `WORKER_START_FAILED` | Worker | runtime 启动失败（binary 不存在 / 权限不足） |
| `SESSION_NOT_FOUND` | Session | Session 不存在 |
| `SESSION_EXPIRED` | Session | Session 已过期（GC 回收） |
| `SESSION_BUSY` | Session | 正在执行，拒绝新 input（建议 Client SDK 使用 Exponential Backoff 进行静默重试） |
| `SESSION_TERMINATED` | Session | Session 已终止 |
| `SESSION_INVALIDATED` | Session | Session 被服务器失效（配合 `control.session_invalid`） |
| `GATEWAY_OVERLOAD` | Gateway | 过载（超过最大 session 数） |
| `RATE_LIMIT_EXCEEDED` | Gateway | 速率限制（配合 `control.throttle`，客户端持续超限后强制拒绝） |
| `PROTOCOL_ERROR` | Protocol | 协议错误（格式错误 / 未知 type） |
| `VERSION_MISMATCH` | Protocol | 版本不兼容 |
| `CONFIG_INVALID` | Protocol | 配置校验失败 |
| `PROCESS_SIGKILL` | Process | 被强制终止（SIGKILL） |
| `PROCESS_SIGTERM` | Process | 被正常终止（SIGTERM） |
| `EXECUTION_TIMEOUT` | Process | Worker 僵死超时（进程存在但无输出） |
| `AUTH_REQUIRED` | Auth | 认证缺失或失败 |
| `RECONNECT_REQUIRED` | Control | 服务器要求重连（配合 `control.reconnect`，客户端未响应时强制断开） |

Exit code → Error code 映射参考：

```go
func classifyExitError(err error) string {
    var exitErr *exec.ExitError
    if errors.As(err, &exitErr) {
        switch exitErr.ExitCode() {
        case 137: return "WORKER_OOM"
        case 139: return "WORKER_CRASH"
        case 143: return "PROCESS_SIGTERM"
        }
    }
    return "WORKER_CRASH"
}
```

---

### 3.11 done（S→C — 执行完成）

```json
{
  "type": "done",
  "data": {
    "success": true,
    "stats": {
      "duration_ms": 5200,
      "tool_calls": 3,
      "input_tokens": 1000,
      "output_tokens": 500,
      "cache_read_tokens": 800,
      "cache_write_tokens": 200,
      "total_tokens": 1700,
      "cost_usd": 0.05,
      "model": "claude-sonnet-4-6",
      "context_used_percent": 45.2
    }
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `success` | 是 | 是否成功 |
| `stats` | 否 | 执行统计（Worker 有能力时提供） |

`stats` 字段：

| 字段 | 说明 | 来源 |
|------|------|------|
| `duration_ms` | 执行耗时（毫秒） | Gateway 计时 |
| `tool_calls` | Tool 调用次数 | Gateway 计数 |
| `input_tokens` | 输入 token 数 | Worker result 事件 |
| `output_tokens` | 输出 token 数 | Worker result 事件 |
| `cache_read_tokens` | 缓存命中 token 数 | Worker result 事件 |
| `cache_write_tokens` | 缓存写入 token 数 | Worker result 事件 |
| `total_tokens` | 总 token 数 | Worker result 事件 |
| `cost_usd` | 费用（USD） | Worker result 事件 |
| `model` | 使用的模型 | Worker result 事件 |
| `context_used_percent` | 上下文窗口使用百分比 | Worker result 事件 |

> **参考**: OpenAI Realtime API 在 `response.done` 中返回 `usage` 统计。Claude Code 的 `result` 事件提供 `usage` + `modelUsage` + `total_cost_usd`。HotPlex 将这些映射到统一的 `stats` 结构。

---

### 3.12 ping / pong（双向 — 心跳保活）

**Client → Server**：

```json
{ "type": "ping", "data": {} }
```

**Server → Client**：

```json
{ "type": "pong", "data": { "state": "idle" } }
```

- 间隔：30s（默认，可配置）
- Pong 附带当前 session state
- 超时策略：3 次无响应 → 视为断线，触发 reconnect 流程

---

### 3.13 reasoning（S→C — 预留）

```json
{
  "type": "reasoning",
  "data": {
    "text": "...",
    "visibility": "private"
  }
}
```

---

### 3.14 step（S→C — 执行阶段）

```json
{
  "type": "step",
  "data": {
    "name": "plan",
    "status": "start"
  }
}
```

---

### 3.15 raw（S→C — 透传）

```json
{
  "type": "raw",
  "data": {
    "source": "claude",
    "payload": {}
  }
}
```

### 3.16 permission_request（S→C — 权限请求）

Worker 需要人类确认时发送（如文件写入、命令执行等敏感操作）。

```json
{
  "type": "permission_request",
  "data": {
    "id": "perm_123",
    "tool": "write_file",
    "description": "Write to /app/main.py",
    "details": {
      "path": "/app/main.py",
      "operation": "write"
    }
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `id` | 是 | 权限请求唯一标识 |
| `tool` | 是 | 请求权限的工具名 |
| `description` | 是 | 人类可读的操作描述 |
| `details` | 否 | 额外上下文 |

> **注意**: Autonomous 模式下 Worker 通常自行执行 tool。但某些场景（如 `permission-mode: default`）需要人类审批。此事件为 **可选扩展**，Minimal Compliance 不要求支持。

### 3.17 permission_response（C→S — 权限响应）

Client 对权限请求的响应。

```json
{
  "type": "permission_response",
  "data": {
    "permission_id": "perm_123",
    "granted": true
  }
}
```

> **Autonomous 默认行为**: `permission-mode: auto-accept` 时 Worker 不发送 `permission_request`，直接执行。`permission-mode: default` 时需要此交互。

---

## 4. Event Ordering

- 同一 session 内 event **严格有序**（单 goroutine 写入）
- `seq` 字段严格递增，从 1 开始
- Client 可通过 `seq` 检测丢包（seq gap = 中间有事件丢失）
- 断线重连时 Client 发送 `session_id`，Worker 通过自身持久化机制恢复上下文（Gateway 不负责 event replay）

---

## 5. Execution Model

```
state(running)
 → message.delta*
 → tool_call?          ← Worker 自行执行 tool（Autonomous 模式）
 → tool_result?        ← 通知 Client
 → message?
 → done
```

---

## 6. Error Handling

- `error` 后必须跟随 `done`
- `done.success = false`（推荐）

---

## 7. Versioning

```json
{
  "version": "aep/v1"
}
```

策略：

- 未知字段忽略（forward compatible）
- 未知 type 忽略（forward compatible）
- 版本协商在 `init` 握手中完成
- **协商失败**：如果 Client 请求的 version Gateway 不支持，返回 `error(VERSION_MISMATCH)` + 支持的最高版本号，然后关闭连接

```json
// Client 发送
{ "version": "aep/v2", "event": { "type": "init", ... } }

// Gateway 响应
{ "version": "aep/v1", "event": { "type": "error", "data": {
  "code": "VERSION_MISMATCH",
  "message": "unsupported version: aep/v2",
  "details": { "max_supported": "aep/v1" }
}}}
// → WS close
```

---

## 8. Extensibility

允许扩展：

```json
{
  "type": "custom.xxx"
}
```

命名：

- 标准：无前缀
- 扩展：`custom.*` / `vendor.*`

---

## 9. Backpressure（MVP）

Worker 产出过快时：

- 使用 bounded channel（容量 1）
- `message.delta` 可丢弃（保留最新的）
- `message` / `done` / `error` / `control` 不可丢弃（必须送达）
- **Priority 语义**：
  - `priority: "control"` → 不经过 backpressure 队列，直接发送
  - `priority: "data"` → 进入 bounded channel，可能被丢弃
- **Seq 语义**：被丢弃的 delta 不消耗 seq 编号，Client 不会观察到 seq gap
- **UI 对账强制约束**：如果本轮（turn）执行中发生过任何 `message.delta` 的丢弃，Gateway **必须**在下发 `done` 之前发送完整的 `message` 事件。前端 UI 需以最后收到的 `message` 载荷为准进行全量渲染与覆盖，以避免静默丢包导致的渲染缺字死结。
- **Event 分类优先级**：`control` > `error` > `done` > `message` > `state` > `tool_call` / `tool_result` > `message.delta`

**控制消息处理**：

```go
// Gateway 发送控制消息时，跳过 backpressure 队列
func (g *Gateway) sendControlEvent(sessionID string, event Event) error {
    sess := g.pool.Get(sessionID)
    if sess == nil {
        return ErrSessionNotFound
    }

    // 控制消息直接写入 WebSocket，不经过 bounded channel
    envelope := Envelope{
        Version:   "aep/v1",
        ID:        generateEventID(),
        Seq:       sess.nextSeq(),  // seq 仍然递增
        Priority:  "control",
        SessionID: sessionID,
        Timestamp: time.Now().UnixMilli(),
        Event:     event,
    }

    sess.connMu.Lock()
    defer sess.connMu.Unlock()
    return json.NewEncoder(sess.wsConn).Encode(envelope)
}
```

> **参考**: gRPC streaming 使用 HTTP/2 flow control 实现自动背压（WINDOW_UPDATE frame）。WebSocket RFC 6455 使用协议层 Control Frames（Close/Ping/Pong）实现优先级。HotPlex 在应用层通过 `priority` 字段实现类似语义。v1.1 考虑引入 client-side throttling signal（类似 A2A 的 ACK + window_size 机制）。

---

## 10. Minimal Compliance（MVP）

**必须支持**：

**C→S（Client → Server）**：
- `init` — 握手
- `input` — 用户输入
- `control`（`terminate`/`delete`/`reset`/`gc`）— 客户端控制命令
- `ping` — 心跳

**S→C（Server → Client）**：
- `init_ack` — 握手确认
- `message.delta` — 增量输出
- `state` — 状态变更
- `error` — 错误通知
- `done` — 执行完成
- `pong` — 心跳响应

**可选扩展（Full Compliance）**：

**C→S（Client → Server）**：
- `permission_response` — 权限响应

**S→C（Server → Client）**：
- `message` — 完整消息
- `tool_call` — 工具调用通知
- `tool_result` — 工具执行结果
- `reasoning` — 推理过程
- `step` — 执行阶段
- `raw` — 透传事件
- `permission_request` — 权限请求
- **`control`（服务器主动控制）**：
  - `control.reconnect` — 强制重连
  - `control.session_invalid` — Session 失效通知
  - `control.throttle` — 降级通知

> **参考**: MCP 定义 `required` 和 `optional` 两级 capability。OpenAI Realtime API 通过 `session.update` 动态调整订阅的 event type。Discord Gateway 使用 `intents` 协商事件订阅。AEP v1 使用 `client_caps` 在 init 时交换能力。

**可选扩展**（Full Compliance）：

C→S：`ping`、`permission_response`
S→C：`message`、`tool_call`、`tool_result`、`pong`、`reasoning`、`step`、`raw`、`permission_request`

> **参考**: MCP 定义 `required` 和 `optional` 两级 capability。OpenAI Realtime API 通过 `session.update` 动态调整订阅的 event type。

---

## 11. Future Work

- multi-agent correlation（A2A 协议集成）
- JSON patch streaming（增量更新，减少带宽）
- tool schema 标准化（MCP tool definition 对齐）
- UI binding schema（前端组件渲染协议）
- client-side throttling signal（ACK + window_size）
- SSE fallback transport（WebSocket 不可达时的降级）
- 运行时动态配置（`config.update` 协议扩展）
- Binary frame 支持（protobuf / flatbuffers，降低序列化开销）

---

## 12. 行业协议参考

| 协议 | 借鉴点 | 应用方式 |
|------|--------|----------|
| **A2A (Agent-to-Agent)** | Agent Card 能力协商 | Worker Capabilities 在 init 时交换 |
| **MCP (Model Context Protocol)** | initialize → initialized 握手 | AEP 的 init / init_ack 设计 |
| **OpenAI Realtime API** | event_id 引用错误源 | error.event_id 引用触发事件 |
| **Discord Gateway** | seq 编号 + 心跳机制 + 服务器主动控制 | seq 严格递增 + `control.reconnect`/`session_invalid`/`throttle` |
| **WebSocket RFC 6455** | Control Frames 优先级 | `priority` 字段实现应用层控制帧，跳过 backpressure |
| **SSE (EventSource)** | 断线重连模式 | session_id resume（Worker 自行持久化） |
| **gRPC Streaming** | HTTP/2 flow control | v1.1 client-side throttling 参考 |

**控制流设计借鉴详解**：

| 协议 | 控制流机制 | AEP 应用 |
|------|-----------|----------|
| **Discord Gateway** | `op: 7 Reconnect`（服务器要求重连）<br>`op: 9 Invalid Session`（Session 失效） | `control.reconnect`<br>`control.session_invalid` |
| **WebSocket** | Control Frames（Close/Ping/Pong）可插队发送 | `priority: "control"` 跳过 backpressure 队列 |
| **OpenAI Realtime** | `session.update`（动态配置） | v1.1 `config.update` 扩展 |
| **gRPC** | RST_STREAM/GOAWAY（强制终止） | `control.session_invalid` + WS close |
