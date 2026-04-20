---
title: Agent Event Protocol (AEP) v1
type: spec
tags:
  - project/HotPlex
  - protocol/agent
---

# Agent Event Protocol (AEP) v1

## 0. Status

- Status: Active
- Version: `aep/v1`
- Scope: Client ↔ Agent Gateway（通过 WebSocket）
- Direction: **Bidirectional** — 同一 Envelope 覆盖 Client → Server 和 Server → Client
- Non-goal: 不定义 tool schema / agent 内部执行语义

---

## 1. Design Goals

- **Streaming-first**：`message.start` / `message.delta` / `message.end` 三段式流式生命周期
- **Bidirectional**：统一 Envelope 覆盖双向通信
- **User Interaction**：`permission_request` / `question_request` / `elicitation_request` 支持结构化用户交互
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
> **Ping/Pong 无 seq**：心跳消息不分配 seq（seq=0），与业务流程无关。
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
    "version": "aep/v1",
    "worker_type": "claude_code",
    "session_id": "sess_xxx",
    "auth": {
      "token": "<jwt_token>"
    },
    "config": {
      "model": "claude-sonnet-4-6",
      "system_prompt": "You are...",
      "allowed_tools": ["read_file", "write_file"],
      "disallowed_tools": ["rm_rf"],
      "max_turns": 0,
      "work_dir": "/app",
      "metadata": {}
    },
    "client_caps": {
      "supports_delta": true,
      "supports_tool_call": true,
      "supported_kinds": ["message.delta", "tool_call", "tool_result"]
    }
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `version` | 是 | 协议版本，必须为 `aep/v1`（同时存在于 Envelope 层和 data 层） |
| `worker_type` | 是 | Worker 类型标识（如 `claude_code`、`pi-mono`） |
| `session_id` | 否 | 有值 = resume 已有 session；空 = 创建新 session |
| `auth` | 否 | 鉴权载荷（非浏览器或无需 Cookie 环境必传，包含 JWT 等 Token 认证信息） |
| `config` | 否 | Worker 配置 |
| `client_caps` | 否 | Client 能力声明 |

**config 字段**：

| 字段 | 说明 |
|------|------|
| `model` | 模型标识（如 `claude-sonnet-4-6`） |
| `system_prompt` | 系统提示词 |
| `allowed_tools` | 允许的工具列表 |
| `disallowed_tools` | 禁止的工具列表 |
| `max_turns` | 最大对话轮次（0 = 无限） |
| `work_dir` | 工作目录 |
| `metadata` | 自定义元数据 |

**client_caps 字段**：

| 字段 | 说明 |
|------|------|
| `supports_delta` | 是否支持流式 delta |
| `supports_tool_call` | 是否支持工具调用事件 |
| `supported_kinds` | 支持的 event type 列表 |

---

### 3.2 init_ack（S→C — 握手确认）

**成功响应**：

```json
{
  "type": "init_ack",
  "data": {
    "session_id": "sess_xxx",
    "state": "created",
    "server_caps": {
      "protocol_version": "aep/v1",
      "worker_type": "claude_code",
      "supports_resume": true,
      "supports_delta": true,
      "supports_tool_call": true,
      "supports_ping": true,
      "max_frame_size": 32768,
      "modalities": ["text", "code"]
    }
  }
}
```

**错误响应**（state 设为 `deleted`，包含 error + code）：

```json
{
  "type": "init_ack",
  "data": {
    "session_id": "",
    "state": "deleted",
    "error": "init: unsupported version aep/v2",
    "code": "VERSION_MISMATCH",
    "server_caps": {}
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `session_id` | 是 | 分配或恢复的 session ID |
| `state` | 是 | Session 当前状态（错误时为 `deleted`） |
| `server_caps` | 是 | Gateway 能力声明 |
| `error` | 否 | 错误描述（仅错误响应） |
| `code` | 否 | 错误码（仅错误响应） |

**server_caps 字段**：

| 字段 | 说明 |
|------|------|
| `protocol_version` | 协议版本 |
| `worker_type` | Worker 类型 |
| `supports_resume` | 是否支持 session resume |
| `supports_delta` | 是否支持流式 delta |
| `supports_tool_call` | 是否支持工具调用事件 |
| `supports_ping` | 是否支持心跳 |
| `max_frame_size` | 最大帧大小（字节） |
| `max_turns` | 最大对话轮次（可选） |
| `modalities` | 支持的模态（如 `["text", "code"]`） |
| `tools` | 可用工具列表（可选） |

---

### 3.3 input（C→S — 用户输入）

```json
{
  "type": "input",
  "data": {
    "content": "your task",
    "metadata": {}
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `content` | 是 | 用户输入内容 |
| `metadata` | 否 | 自定义元数据（如来源标识、上下文信息等） |

Session 状态为 `running` 时，拒绝 input，返回 `error`（`SESSION_BUSY`）。

> **Client 最佳实践**: 由于并发输入或多 Agent 协同可能引起状态冲突，建议 Client SDK 在收到 `SESSION_BUSY` 时，隐式使用指数退避（Exponential Backoff）进行静默重试，以此降低将瞬间冲突报错抛给最终终端用户的概率。

---

### 3.4 control（双向 — 控制命令）

**Client → Server（客户端控制）**：

```json
{
  "type": "control",
  "data": {
    "action": "terminate|delete|reset|gc",
    "reason": "user_requested"
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
    "delay_ms": 5000,
    "recoverable": false,
    "suggestion": {},
    "details": {}
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
    "delay_ms": 5000
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `action` | 是 | `"reconnect"` |
| `reason` | 是 | 重连原因 |
| `delay_ms` | 否 | 建议延迟重连时间（毫秒），避免 thundering herd |

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
    "details": {}
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `action` | 是 | `"session_invalid"` |
| `reason` | 是 | 失效原因 |
| `recoverable` | 是 | 是否可通过重新创建 session 恢复 |
| `details` | 否 | 详细上下文 |

**客户端行为**：
- `recoverable: true` → 可通过 `init` 创建新 session 并重试任务
- `recoverable: false` → 需要用户重新提交请求

> **参考**: Discord Gateway `op: 9 Invalid Session`，通知客户端 session 失效。

#### 3.4.3 `control.throttle`（S→C — 降级通知）

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
| `action` | 是 | `"throttle"` |
| `reason` | 是 | 降级原因 |
| `suggestion.max_message_rate` | 否 | 建议的最大消息速率（消息/秒） |
| `suggestion.backoff_ms` | 否 | 建议的消息间隔（毫秒） |
| `suggestion.retry_after` | 否 | 建议的重试延迟（毫秒） |

> **注意**: 这是**软限制**，客户端应尽量遵守。如持续超限，Gateway 可能发送 `error(RATE_LIMITED)` 强制拒绝。

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

**服务端行为**：
1. 清空 `SessionInfo.Context`（Gateway 层）
2. 调用 `Worker.ResetContext()`（Worker 自行决定 in-place 或 terminate+start）
3. Session 状态切至 `running`
4. 返回 `state{state: "running", message: "context_reset"}`

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

**服务端行为**：
1. 调用 `Worker.Terminate(ctx)`（Worker 内部自行保存会话状态）
2. 解除 Worker attachment（`DetachWorker`）
3. Session 状态切至 `terminated`
4. 返回 `state{state: "terminated", message: "session_archived"}`

> **注意**: `gc` 后 Session 可通过 `init` + 相同 `session_id` 恢复（resume）。

---

### 3.5 message.start（S→C — 流式消息开始）

标志一条流式消息的开始，提供消息元数据。

```json
{
  "type": "message.start",
  "data": {
    "id": "msg_<uuid>",
    "role": "assistant",
    "content_type": "text",
    "metadata": {}
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `id` | 是 | 消息唯一标识，后续 `message.delta` 和 `message.end` 通过此 ID 关联 |
| `role` | 是 | 消息角色（如 `assistant`） |
| `content_type` | 是 | 内容类型（如 `text`、`code`） |
| `metadata` | 否 | 自定义元数据 |

> **流式生命周期**：`message.start` → `message.delta`* → `message.end` 构成一条完整消息的三段式流式传输。Client 应以此模式替代旧的单字段 delta 推送。

---

### 3.6 message.delta（S→C — 增量输出）

流式消息的增量内容。**唯一的流式输出 event type**。

```json
{
  "type": "message.delta",
  "data": {
    "message_id": "msg_<uuid>",
    "content": " world"
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `message_id` | 是 | 关联的 `message.start` 的 ID |
| `content` | 是 | 增量文本内容 |

对于 raw stdout Worker（如 pi-mono），Worker Adapter 将每行 stdout 转换为 `message.delta { content: "..." }`，`message_id` 由 Gateway 分配。

> **Backpressure**：`message.delta` 可被 backpressure 静默丢弃（bounded channel 满时）。丢弃的 delta 不消耗 seq。如果本轮有过丢弃，`done.dropped` 为 `true`。

---

### 3.7 message.end（S→C — 流式消息结束）

标志一条流式消息的结束。

```json
{
  "type": "message.end",
  "data": {
    "message_id": "msg_<uuid>"
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `message_id` | 是 | 关联的 `message.start` 的 ID |

---

### 3.8 message（S→C — 完整消息）

非流式场景下的完整消息，或流式场景下的兼容回退。

```json
{
  "type": "message",
  "data": {
    "id": "msg_<uuid>",
    "role": "assistant",
    "content": "Hello world",
    "content_type": "text",
    "metadata": {}
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `id` | 是 | 消息唯一标识 |
| `role` | 是 | 消息角色 |
| `content` | 是 | 完整文本内容 |
| `content_type` | 否 | 内容类型 |
| `metadata` | 否 | 自定义元数据 |

> **流式 vs 非流式**：推荐使用 `message.start` / `message.delta` / `message.end` 三段式流式传输。`message` 作为向后兼容的非流式事件保留。Gateway 将 `message` 作为透传事件处理。

---

### 3.9 tool_call（S→C — 工具调用通知）

```json
{
  "type": "tool_call",
  "data": {
    "id": "call_123",
    "name": "read_file",
    "input": {
      "path": "/app/main.py"
    }
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `id` | 是 | 工具调用唯一标识 |
| `name` | 是 | 工具名称 |
| `input` | 是 | 调用参数（键值对） |

**Autonomous 模式**：Worker 自行执行 tool，此事件仅为 **通知** Client（用于 UI 展示）。Client 不需要回传 `tool_result`。

---

### 3.10 tool_result（S→C — 工具执行结果通知）

```json
{
  "type": "tool_result",
  "data": {
    "id": "call_123",
    "output": "file content...",
    "error": ""
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `id` | 是 | 关联的 `tool_call` 的 ID |
| `output` | 否 | 执行结果（成功时） |
| `error` | 否 | 错误描述（失败时） |

同样为 **通知**，Worker 内部完成 tool 执行后将结果通知 Client。

---

### 3.11 state（S→C — 状态变更）

```json
{
  "type": "state",
  "data": {
    "state": "running",
    "message": "context_reset"
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `state` | 是 | Session 当前状态 |
| `message` | 否 | 附加说明（如 `context_reset`、`session_archived`） |

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

### 3.12 error（双向 — 错误通知）

```json
{
  "type": "error",
  "data": {
    "code": "WORKER_CRASH",
    "message": "exit code 139 (SIGSEGV)"
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `code` | 是 | 结构化错误码（见下方枚举） |
| `message` | 是 | 人类可读错误描述 |

**Error Code 枚举**：

| Code | 分类 | 说明 |
|------|------|------|
| `WORKER_CRASH` | Worker | 进程崩溃（SIGSEGV 等，exit code 139） |
| `WORKER_TIMEOUT` | Worker | 执行超时（超过 `execution_timeout`） |
| `WORKER_OOM` | Worker | 内存溢出（exit code 137） |
| `WORKER_OUTPUT_LIMIT` | Worker | 单行输出超限（默认 10MB） |
| `WORKER_START_FAILED` | Worker | runtime 启动失败（binary 不存在 / 权限不足） |
| `INVALID_MESSAGE` | Protocol | 消息格式无效 |
| `PROTOCOL_VIOLATION` | Protocol | 协议违规（如首帧非 init） |
| `VERSION_MISMATCH` | Protocol | 版本不兼容 |
| `CONFIG_INVALID` | Protocol | 配置校验失败 |
| `SESSION_NOT_FOUND` | Session | Session 不存在 |
| `SESSION_EXPIRED` | Session | Session 已过期（GC 回收） |
| `SESSION_BUSY` | Session | 正在执行，拒绝新 input |
| `SESSION_TERMINATED` | Session | Session 已终止 |
| `SESSION_INVALIDATED` | Session | Session 被服务器失效 |
| `UNAUTHORIZED` | Auth | 认证失败（token 无效/过期） |
| `AUTH_REQUIRED` | Auth | 认证缺失 |
| `INTERNAL_ERROR` | Gateway | 内部错误 |
| `GATEWAY_OVERLOAD` | Gateway | 过载（超过最大 session 数） |
| `RATE_LIMITED` | Gateway | 速率限制（配合 `control.throttle`） |
| `EXECUTION_TIMEOUT` | Process | Worker 僵死超时（进程存在但无输出） |
| `PROCESS_SIGKILL` | Process | 被强制终止（SIGKILL） |
| `RECONNECT_REQUIRED` | Control | 服务器要求重连 |
| `RESUME_RETRY` | Resume | Resume 重试 |

Exit code → Error code 映射参考：

```go
func classifyExitError(err error) string {
    var exitErr *exec.ExitError
    if errors.As(err, &exitErr) {
        switch exitErr.ExitCode() {
        case 137: return "WORKER_OOM"
        case 139: return "WORKER_CRASH"
        }
    }
    return "WORKER_CRASH"
}
```

---

### 3.13 done（S→C — 执行完成）

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
    },
    "dropped": false
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `success` | 是 | 是否成功 |
| `stats` | 否 | 执行统计（Worker 有能力时提供） |
| `dropped` | 否 | 是否发生过 backpressure 丢弃（UI 对账标记） |

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

> **UI 对账**：如果 `dropped` 为 `true`，表示本轮有 `message.delta` 被静默丢弃。Client 应以最终的完整 `message` 载荷为准进行全量渲染覆盖，避免静默丢包导致的渲染缺字。

> **参考**: OpenAI Realtime API 在 `response.done` 中返回 `usage` 统计。Claude Code 的 `result` 事件提供 `usage` + `modelUsage` + `total_cost_usd`。HotPlex 将这些映射到统一的 `stats` 结构。

---

### 3.14 ping / pong（双向 — 心跳保活）

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
- **无 seq**：ping/pong 消息 seq=0，不参与业务序号分配

---

### 3.15 reasoning（S→C — Agent 推理过程）

```json
{
  "type": "reasoning",
  "data": {
    "id": "rsn_<uuid>",
    "content": "Let me think about this...",
    "model": "claude-sonnet-4-6"
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `id` | 是 | 推理块唯一标识 |
| `content` | 是 | 推理内容 |
| `model` | 否 | 使用的模型 |

---

### 3.16 step（S→C — 执行阶段标记）

```json
{
  "type": "step",
  "data": {
    "id": "step_1",
    "step_type": "plan",
    "name": "analyzing_codebase",
    "input": { "target": "src/main.py" },
    "output": { "findings": 3 },
    "parent_id": "step_0",
    "duration": 1200
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `id` | 是 | 步骤唯一标识 |
| `step_type` | 是 | 步骤类型（如 `plan`、`execute`、`verify`） |
| `name` | 否 | 步骤名称 |
| `input` | 否 | 步骤输入 |
| `output` | 否 | 步骤输出 |
| `parent_id` | 否 | 父步骤 ID（支持嵌套） |
| `duration` | 否 | 执行耗时（毫秒） |

---

### 3.17 raw（S→C — 透传事件）

```json
{
  "type": "raw",
  "data": {
    "kind": "claude",
    "raw": {}
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `kind` | 是 | 原始事件类型标识（如 Worker 名称） |
| `raw` | 是 | 原始载荷（任意 JSON） |

> **Backpressure**：`raw` 事件可被 backpressure 静默丢弃，与 `message.delta` 行为一致。

---

### 3.18 permission_request（S→C — 权限请求）

Worker 需要人类确认时发送（如文件写入、命令执行等敏感操作）。

```json
{
  "type": "permission_request",
  "data": {
    "id": "perm_123",
    "tool_name": "write_file",
    "description": "Write to /app/main.py",
    "args": ["/app/main.py", "write"]
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `id` | 是 | 权限请求唯一标识 |
| `tool_name` | 是 | 请求权限的工具名 |
| `description` | 否 | 人类可读的操作描述 |
| `args` | 否 | 工具参数列表 |

> **注意**: Autonomous 模式下 Worker 通常自行执行 tool。但某些场景（如 `permission-mode: default`）需要人类审批。此事件为 **可选扩展**，Minimal Compliance 不要求支持。

### 3.19 permission_response（C→S — 权限响应）

Client 对权限请求的响应。

```json
{
  "type": "permission_response",
  "data": {
    "id": "perm_123",
    "allowed": true,
    "reason": "approved by user"
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `id` | 是 | 关联的 `permission_request` 的 ID |
| `allowed` | 是 | 是否允许 |
| `reason` | 否 | 原因说明 |

> **Autonomous 默认行为**: `permission-mode: auto-accept` 时 Worker 不发送 `permission_request`，直接执行。`permission-mode: default` 时需要此交互。

---

### 3.20 question_request（S→C — 结构化用户提问）

Worker 需要用户做出选择时发送，支持单选和多选。

```json
{
  "type": "question_request",
  "data": {
    "id": "q_<uuid>",
    "tool_name": "AskUserQuestion",
    "questions": [
      {
        "question": "Which approach do you prefer?",
        "header": "Approach Selection",
        "options": [
          {
            "label": "Option A",
            "description": "Refactor the existing module",
            "preview": "Lower risk, more time"
          },
          {
            "label": "Option B",
            "description": "Rewrite from scratch",
            "preview": "Higher risk, faster result"
          }
        ],
        "multi_select": false
      }
    ]
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `id` | 是 | 提问请求唯一标识 |
| `tool_name` | 否 | 触发工具名（如 `AskUserQuestion`） |
| `questions` | 是 | 问题列表（至少 1 个） |

**Question 结构**：

| 字段 | 说明 |
|------|------|
| `question` | 问题文本 |
| `header` | 问题标题/分类 |
| `options` | 可选项列表 |
| `multi_select` | 是否允许多选 |

**QuestionOption 结构**：

| 字段 | 说明 |
|------|------|
| `label` | 选项标签（用户选择的标识） |
| `description` | 选项描述 |
| `preview` | 选项预览文本 |

---

### 3.21 question_response（C→S — 用户回答）

Client 对 `question_request` 的响应。

```json
{
  "type": "question_response",
  "data": {
    "id": "q_<uuid>",
    "answers": {
      "Which approach do you prefer?": "Option A"
    }
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `id` | 是 | 关联的 `question_request` 的 ID |
| `answers` | 是 | 问题文本 → 选中标签的映射（`multi_select` 时值为逗号分隔的多选标签） |

---

### 3.22 elicitation_request（S→C — MCP 服务器用户输入请求）

MCP 服务器需要用户提供输入时发送（通过 Worker 转发到 Gateway）。

```json
{
  "type": "elicitation_request",
  "data": {
    "id": "el_<uuid>",
    "mcp_server_name": "github-mcp",
    "message": "Please enter your GitHub token",
    "mode": "input",
    "url": "https://github.com/settings/tokens",
    "elicitation_id": "mcp_el_123",
    "requested_schema": {
      "type": "object",
      "properties": {
        "token": { "type": "string", "description": "GitHub PAT" }
      }
    }
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `id` | 是 | 请求唯一标识 |
| `mcp_server_name` | 是 | 发起请求的 MCP 服务器名称 |
| `message` | 是 | 请求消息 |
| `mode` | 否 | 输入模式 |
| `url` | 否 | 相关 URL（如配置页面） |
| `elicitation_id` | 否 | MCP 协议层的 elicitation ID |
| `requested_schema` | 否 | 期望的输入 JSON Schema |

---

### 3.23 elicitation_response（C→S — MCP 服务器用户输入响应）

Client 对 `elicitation_request` 的响应。

```json
{
  "type": "elicitation_response",
  "data": {
    "id": "el_<uuid>",
    "action": "accept",
    "content": {
      "token": "ghp_xxx..."
    }
  }
}
```

| 字段 | 必选 | 说明 |
|------|------|------|
| `id` | 是 | 关联的 `elicitation_request` 的 ID |
| `action` | 是 | 用户动作：`"accept"`（接受）、`"decline"`（拒绝）、`"cancel"`（取消） |
| `content` | 否 | 用户输入内容（`action` 为 `accept` 时提供） |

---

## 4. Event Ordering

- 同一 session 内 event **严格有序**（单 goroutine 写入）
- `seq` 字段严格递增，从 1 开始
- **被丢弃的 delta 不产生 seq gap** — seq 仅递增于实际发送的事件
- 断线重连时 Client 发送 `session_id`，Worker 通过自身持久化机制恢复上下文（Gateway 不负责 event replay）

---

## 5. Execution Model

### 流式模式（推荐）

```
state(running)
 → message.start
 → message.delta*
 → [tool_call → tool_result]*
 → message.end
 → done
```

### 非流式模式（兼容）

```
state(running)
 → message.delta*
 → [tool_call → tool_result]*
 → message?
 → done
```

### 全局终止弧

```
[ 任何执行阶段 ]
 → error
 → done(success: false)
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
- 版本协商在 `init` 握手中完成（`init.data.version` + `envelope.version` 双重检查）
- **协商失败**：如果 Client 请求的 version Gateway 不支持，返回 `init_ack(state: "deleted", code: "VERSION_MISMATCH")`，然后关闭连接

```json
// Gateway 响应（version 不匹配）
{
  "version": "aep/v1",
  "event": {
    "type": "init_ack",
    "data": {
      "session_id": "",
      "state": "deleted",
      "error": "init: unsupported version aep/v2",
      "code": "VERSION_MISMATCH",
      "server_caps": {}
    }
  }
}
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

- 使用 bounded channel（容量可配置，默认 256）
- `message.delta` / `raw` 可丢弃（非阻塞 select，保留最新的）
- `message` / `done` / `error` / `control` 不可丢弃（必须送达）
- **Priority 语义**：
  - `priority: "control"` → 不经过 backpressure 队列，直接发送
  - `priority: "data"` → 进入 bounded channel，可能被丢弃
- **Seq 语义**：被丢弃的 delta 不消耗 seq 编号，Client 不会观察到 seq gap
- **UI 对账标记**：如果本轮有过丢弃，`done.dropped` 设为 `true`。Client 需以最终完整载荷为准进行全量渲染覆盖
- **Event 分类优先级**：`control` > `error` > `done` > `message` > `state` > `tool_call` / `tool_result` > `message.delta` / `raw`

---

## 10. Minimal Compliance

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
- `question_response` — 用户回答
- `elicitation_response` — MCP 用户输入响应

**S→C（Server → Client）**：
- `message.start` — 流式消息开始
- `message.end` — 流式消息结束
- `message` — 完整消息
- `tool_call` — 工具调用通知
- `tool_result` — 工具执行结果
- `reasoning` — 推理过程
- `step` — 执行阶段
- `raw` — 透传事件
- `permission_request` — 权限请求
- `question_request` — 结构化用户提问
- `elicitation_request` — MCP 服务器用户输入
- **`control`（服务器主动控制）**：
  - `control.reconnect` — 强制重连
  - `control.session_invalid` — Session 失效通知
  - `control.throttle` — 降级通知

> **参考**: MCP 定义 `required` 和 `optional` 两级 capability。OpenAI Realtime API 通过 `session.update` 动态调整订阅的 event type。Discord Gateway 使用 `intents` 协商事件订阅。AEP v1 使用 `client_caps` 在 init 时交换能力。

---

## 11. Future Work

- Worker stdio command（`context_usage` / `mcp_status` / `worker_command`）
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
| **OpenAI Realtime API** | event_id 引用错误源 | init_ack error 响应模式 |
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
