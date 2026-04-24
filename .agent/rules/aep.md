---
paths:
  - "**/aep/*.go"
  - "**/gateway/*.go"
  - "**/pkg/events/*.go"
---

# AEP v1 协议规范

> hotplex 对外暴露的统一 WebSocket 全双工通信协议
> 参考文档：`docs/specs/Acceptance-Criteria.md` §AEP-001 ~ §AEP-030

## Envelope 结构

| 字段 | 类型 | 要求 |
|------|------|------|
| `id` | string | non-empty，消息唯一标识 |
| `version` | string | 必须为 `aep/v1`，否则返回 `VERSION_MISMATCH` |
| `session_id` | string | non-empty |
| `seq` | int64 | 从 1 开始严格递增，同 session 内原子分配 |
| `timestamp` | int64 | Unix ms，> 0 |
| `event` | object | non-null，包含 `type` 字段 |
| `priority` | string | 缺省 `data`；`control` 跳过 backpressure |

### 编解码约束
```go
// DecodeLine: 验证所有必填字段 + DisallowUnknownFields
// EncodeLine: json.Encoder，避免 []byte→string 复制
```

## 消息类型

### C→S（Client → Server）
- `init`：握手，必须是 WS 连接后第一帧，30s 超时
- `input`：用户任务，Session 繁忙时硬拒绝 (`SESSION_BUSY`)
- `control`：terminate / delete / gc / reset / park / restart
- `ping`：心跳，回复 pong

### S→C（Server → Client）
- `init_ack`：握手响应
- `state`：状态变更（created/running/idle/terminated）
- `message.start` / `message.delta` / `message.end`：流式输出
- `message`：Turn 结束时完整消息聚合
- `tool_call` / `tool_result`：Tool 调用通知
- `permission_request` / `question_request` / `elicitation_request`：用户交互
- `reasoning` / `step` / `raw`：辅助事件
- `context_usage` / `mcp_status` / `worker_cmd`：Worker 控制事件
- `done`：Turn 终止符
- `error`：错误通知
- `pong`：ping 响应
- `control`：reconnect / throttle（Server 发起）

## Seq 分配规则

**需要序号**：所有业务、状态、工具、原始、控制消息
**不需要序号**：`ping` / `pong` — seq 字段为 0（未分配）

```go
// conn.go ReadPump — 仅非 Ping 消息分配 seq
if env.Event.Type != events.Ping {
    env.Seq = c.hub.NextSeq(c.sessionID)
}
```

**Seq 特性**：每 session 独立空间，从 1 严格递增，原子操作，0=未分配

## Backpressure（有界通道与 delta 丢弃）

```go
// hub.go SendToSession
if env.Event.Type == "message.delta" || env.Event.Type == "raw" {
    // 非阻塞 select，通道满时静默丢弃
    select {
    case ch <- env:
        return nil
    default:
        sessionDropped[sessionID] = true
        return nil  // 不返回错误，丢弃的 delta 不消耗 seq
    }
}
// 关键事件 (state/done/error) 阻塞发送，永不丢弃
ch <- env
```

**丢弃标记**：`done` 事件检查 `sessionDropped`，在 `stats.dropped` 中体现

## 时序约束

- Turn 开始：`state(running)` 必须是第一个 S→C event
- Turn 结束：`done` 必须是最后一个 S→C event
- `error` 必须在 `done` 之前
- `tool_result.tool_call_id` 必须与对应 `tool_call.id` 匹配

## Init 握手

```go
ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
// 第一帧类型必须为 init，否则 PROTOCOL_VIOLATION
```

## 用户交互事件

| 类型 | 方向 | 说明 |
|------|------|------|
| `permission_request` | S→C | 请求用户授权 tool 执行 |
| `question_request` | S→C | 请求用户回答问题 |
| `elicitation_request` | S→C | MCP server 请求用户输入 |

**交互超时**：默认 5 分钟自动拒绝 (auto-deny)，通过 `InteractionManager` 管理

## Control 事件

- C→S: `terminate` / `delete` / `gc` / `reset` / `park` / `restart`
- Messaging 触发：slash 命令 (`/gc`, `/reset`) 或 `$` 前缀自然语言 (`$gc`, `$休眠`)
- 自然语言触发**必须**带 `$` 前缀，防止误匹配
