# AEP v1 协议参考

> Agent Event Protocol v1 完整参考文档——事件类型、Envelope 格式、背压机制、错误码

## 协议概述

AEP v1（Agent Event Protocol）是 HotPlex Gateway 的 WebSocket 通信协议：

- **传输层**：WebSocket，NDJSON（Newline-Delimited JSON）编码
- **通信模式**：全双工（Full-Duplex），Client 和 Server 可同时发送消息
- **设计理念**：Streaming-first、统一 Envelope、结构化用户交互
- **版本标识**：`aep/v1`

## Envelope 格式

所有消息共用统一的 Envelope 结构：

```json
{
  "version": "aep/v1",
  "id": "evt_<uuid>",
  "seq": 42,
  "priority": "data",
  "session_id": "sess_<uuid>",
  "timestamp": 1710000000123,
  "event": {
    "type": "message.delta",
    "data": {}
  }
}
```

| 字段 | 类型 | 必选 | 说明 |
|------|------|------|------|
| `version` | string | 是 | 固定 `aep/v1` |
| `id` | string | 是 | 事件唯一标识，UUID v4（`evt_` 前缀） |
| `seq` | int64 | 是 | 单调递增序列号，从 1 开始，per-session 独立空间 |
| `priority` | string | 否 | `"control"` 或 `"data"`（默认） |
| `session_id` | string | 是 | Session 标识（`sess_` 前缀） |
| `timestamp` | int64 | 是 | Unix 毫秒时间戳 |
| `event.type` | string | 是 | 事件类型 |
| `event.data` | object | 是 | 事件载荷 |

### Priority 语义

- **`control`**：跳过背压队列，优先发送（如 `control.reconnect`、`error`、`done`）
- **`data`**：正常排队，受背压控制（如 `message.delta`、`tool_call`）

### Seq 规则

- 仅实际发送的事件消耗 seq（被丢弃的 delta 不递增 seq）
- `ping`/`pong` 的 seq 为 0（不参与序号分配）
- Client 不应通过 seq gap 检测丢包——gap 不会出现

## Client → Server 事件

### init（连接握手）

WebSocket 连接建立后的**第一帧**必须是 `init`，30 秒超时。

```json
{
  "type": "init",
  "data": {
    "version": "aep/v1",
    "worker_type": "claude_code",
    "session_id": "sess_xxx",
    "auth": { "token": "<jwt>" },
    "config": {
      "model": "claude-sonnet-4-6",
      "allowed_tools": ["read_file", "write_file"],
      "max_turns": 0,
      "work_dir": "/app"
    },
    "client_caps": {
      "supports_delta": true,
      "supports_tool_call": true,
      "supported_kinds": ["message.delta", "tool_call"]
    }
  }
}
```

`session_id` 非空时为 Resume 模式，空时创建新 Session。

### input（用户输入）

```json
{
  "type": "input",
  "data": {
    "content": "请帮我重构 login 函数",
    "metadata": {}
  }
}
```

Session 处于 `RUNNING` 时发送 input 会被拒绝（`SESSION_BUSY`）。

### permission_response（权限响应）

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

### question_response（用户回答）

```json
{
  "type": "question_response",
  "data": {
    "id": "q_<uuid>",
    "answers": { "问题文本": "选项标签" }
  }
}
```

### elicitation_response（MCP 输入响应）

```json
{
  "type": "elicitation_response",
  "data": {
    "id": "el_<uuid>",
    "action": "accept",
    "content": { "token": "ghp_xxx" }
  }
}
```

`action` 取值：`"accept"` | `"decline"` | `"cancel"`。

### worker_command（Worker 命令）

```json
{
  "type": "worker_command",
  "data": {
    "command": "context_usage",
    "args": "",
    "extra": {}
  }
}
```

### ping（心跳）

```json
{ "type": "ping", "data": {} }
```

## Server → Client 事件

### init_ack（握手确认）

```json
{
  "type": "init_ack",
  "data": {
    "session_id": "sess_xxx",
    "state": "created",
    "server_caps": {
      "protocol_version": "aep/v1",
      "supports_resume": true,
      "supports_delta": true,
      "max_frame_size": 32768
    }
  }
}
```

错误时 `state` 为 `"deleted"`，附带 `error` + `code` 字段。

### message.start / message.delta / message.end（流式输出）

三段式流式消息生命周期：

```
message.start → message.delta* → message.end
```

- `message.start`：消息元数据（ID、role、content_type）
- `message.delta`：增量文本（可被背压丢弃，`dropped` 标记）
- `message.end`：消息结束标记

### message（完整消息）

非流式场景的完整消息，向后兼容。流式场景推荐使用三段式。

### tool_call / tool_result（工具调用通知）

```json
{ "type": "tool_call", "data": { "id": "call_123", "name": "read_file", "input": {"path": "/app/main.py"} } }
{ "type": "tool_result", "data": { "id": "call_123", "output": "file content...", "error": "" } }
```

Autonomous 模式下为**通知性质**，Worker 内部执行，Client 无需回传结果。

### state（状态变更）

```json
{ "type": "state", "data": { "state": "running", "message": "context_reset" } }
```

状态集合：`created` | `running` | `idle` | `terminated`

### done（执行完成）

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
      "total_tokens": 1700,
      "model": "claude-sonnet-4-6",
      "context_used_percent": 45.2
    },
    "dropped": false
  }
}
```

`dropped: true` 表示本轮有 `message.delta` 被丢弃，Client 应以最终完整载荷覆盖渲染。

### permission_request / question_request / elicitation_request（用户交互）

Worker 请求人类介入的结构化交互事件。默认 5 分钟超时自动拒绝（auto-deny）。

### reasoning / step / raw（辅助事件）

- `reasoning`：Agent 思维过程（thinking）
- `step`：执行阶段标记（plan/execute/verify）
- `raw`：Worker 原始事件透传

### context_usage（Context 用量报告）

```json
{
  "type": "context_usage",
  "data": {
    "total_tokens": 62000,
    "max_tokens": 200000,
    "percentage": 31,
    "categories": [{"name": "system_prompt", "tokens": 4500}]
  }
}
```

### mcp_status（MCP 服务状态）

```json
{
  "type": "mcp_status",
  "data": {
    "servers": [{"name": "github-mcp", "status": "connected"}]
  }
}
```

### pong（心跳响应）

```json
{ "type": "pong", "data": { "state": "idle" } }
```

## 双向事件

### control（控制命令）

**Client → Server**：

| Action | 说明 |
|--------|------|
| `terminate` | 终止 Worker，Session → `TERMINATED` |
| `delete` | 删除 Session + 清理 runtime |
| `reset` | 清空上下文，Session 保持 `RUNNING` |
| `gc` | 归档会话：终止 Worker，保留历史，可 Resume |
| `cd` | 切换工作目录（创建新 Session 继承原 Session 上下文） |

**Server → Client**（`priority: "control"`）：

| Action | 说明 |
|--------|------|
| `reconnect` | 强制重连（服务器维护、版本升级） |
| `session_invalid` | Session 失效通知 |
| `throttle` | 降级通知（速率限制建议） |

### error（错误通知）

```json
{ "type": "error", "data": { "code": "WORKER_CRASH", "message": "exit code 139" } }
```

`error` 后必须跟随 `done`。

## 错误码参考

### Worker 类

| Code | 说明 |
|------|------|
| `WORKER_START_FAILED` | Worker 启动失败 |
| `WORKER_CRASH` | 进程崩溃（SIGSEGV 等） |
| `WORKER_TIMEOUT` | 执行超时 |
| `WORKER_OOM` | 内存溢出（exit code 137） |
| `PROCESS_SIGKILL` | 被强制终止 |
| `WORKER_OUTPUT_LIMIT` | 单行输出超限（10MB） |

### Session 类

| Code | 说明 |
|------|------|
| `SESSION_NOT_FOUND` | Session 不存在 |
| `SESSION_EXPIRED` | Session 已过期 |
| `SESSION_BUSY` | 正在执行，拒绝新 input |
| `SESSION_TERMINATED` | Session 已终止 |
| `SESSION_INVALIDATED` | Session 被失效 |

### Protocol 类

| Code | 说明 |
|------|------|
| `INVALID_MESSAGE` | 消息格式无效 |
| `PROTOCOL_VIOLATION` | 协议违规 |
| `VERSION_MISMATCH` | 版本不兼容 |
| `CONFIG_INVALID` | 配置校验失败 |

### Auth / Gateway 类

| Code | 说明 |
|------|------|
| `UNAUTHORIZED` | 认证失败 |
| `AUTH_REQUIRED` | 认证缺失 |
| `INTERNAL_ERROR` | 内部错误 |
| `GATEWAY_OVERLOAD` | 过载 |
| `RATE_LIMITED` | 速率限制 |
| `EXECUTION_TIMEOUT` | Worker 僵死超时 |
| `RECONNECT_REQUIRED` | 服务端要求客户端重连 |
| `RESUME_RETRY` | Session resume 失败，建议重试 |
| `NOT_SUPPORTED` | 操作不支持 |
| `TURN_TIMEOUT` | Turn 执行超时 |

## 背压机制

Worker 产出过快时，Gateway 使用 bounded channel（默认容量 256）缓冲消息：

| 事件类型 | 背压行为 |
|---------|---------|
| `message.delta` / `raw` | **可丢弃**（非阻塞 select） |
| `message` / `done` / `error` / `control` | **不可丢弃**（阻塞发送） |
| `priority: "control"` | 跳过背压队列，直接发送 |

丢弃的 delta 不消耗 seq，通过 `done.dropped` 标记通知 Client。

## Init 握手流程

```
Client                          Server
  |                               |
  |--- init(version, caps) ------>|
  |                               |--- 创建/恢复 Session
  |<-- init_ack(session_id) ------|
  |                               |
  |--- input(content) ----------->|--- state(running)
  |                               |--- message.start
  |<-- message.delta * ------------|--- message.delta
  |<-- message.end ---------------|
  |<-- done(success) -------------|--- state(idle)
  |                               |
```

## 全双工通信流

```
Client ←→ Server

Input Flow:     input → [state(running)] → [tool_call → tool_result]* → [message.delta*] → done
Control Flow:   control(action) → state(new_state) / error
Interactive:    permission_request ←→ permission_response
                question_request ←→ question_response
                elicitation_request ←→ elicitation_response
Heartbeat:      ping ←→ pong
```

## 最小合规要求

**必须支持**：`init`、`input`、`control`、`ping`、`init_ack`、`message.delta`、`state`、`error`、`done`、`pong`

**可选扩展**：`message.start/end`、`message`、`tool_call/result`、`reasoning`、`step`、`raw`、`permission_*`、`question_*`、`elicitation_*`、`context_usage`、`mcp_status`、`worker_command`
